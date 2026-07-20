-- The type vocabulary becomes OKF's own (design doc 0023): the eight
-- recommended slugs are replaced by the OKF type values ochakai already
-- exported, so import and export are identity on the type key and the
-- translation layer in internal/okf disappears.
--
-- This is a small migration because 0011 took the type out of every key
-- and address: knowledge_revision, knowledge_event, knowledge_usage,
-- attachment, and knowledge_embedding dropped their type columns, URIs
-- are "ochakai://<id>", and link targets are ids. Unlike 0010 there are
-- no references to rewrite — only the metadata column and the type field
-- inside revision snapshots. Nor can this collide: since 0011 the
-- knowledge primary key is the id alone, so renaming a type cannot
-- conflict with a free type that already uses the new spelling.
--
-- No CHECK constraint exists to update (0003 dropped it). Embeddings are
-- not rebuilt: embeddingText is built from id, title, description, tags,
-- question, and body — never the type.
--
-- Two passes, in this order:
--
--   1. attrs.okf_type is folded back into the type. That attr held the
--      authored spelling the old slugifier destroyed ("Data Contract"
--      stored under type "data-contract"), and export wrote it as the
--      type — so promoting it to the type column is exactly what the old
--      export already produced, now stored rather than reconstructed.
--      Running first also leaves such an entry out of pass 2, which is
--      right: its authored spelling wins over any slug rename.
--   2. The eight recommended slugs become their OKF values.

UPDATE knowledge SET
        type = attrs->>'okf_type',
        attrs = attrs - 'okf_type'
 WHERE attrs ? 'okf_type'
   AND COALESCE(TRIM(attrs->>'okf_type'), '') <> ''
   AND POSITION('/' IN attrs->>'okf_type') = 0
   AND OCTET_LENGTH(attrs->>'okf_type') <= 128;

-- A stored okf_type that the new type rules cannot hold (empty, longer
-- than 128 bytes, or containing "/") keeps its slug type; drop the attr
-- so it does not re-export as a producer-defined extension key that was
-- never the producer's.
UPDATE knowledge SET attrs = attrs - 'okf_type' WHERE attrs ? 'okf_type';

UPDATE knowledge SET type = CASE type
    WHEN 'models'     THEN 'Semantic Model'
    WHEN 'metrics'    THEN 'Metric'
    WHEN 'queries'    THEN 'Golden Query'
    WHEN 'insights'   THEN 'Insight'
    WHEN 'terms'      THEN 'Glossary Term'
    WHEN 'datasets'   THEN 'BigQuery Dataset'
    WHEN 'tables'     THEN 'BigQuery Table'
    WHEN 'references' THEN 'Reference'
END
 WHERE type IN ('models', 'metrics', 'queries', 'insights', 'terms',
                'datasets', 'tables', 'references');

-- Revision snapshots carry the type and attrs inline; rewrite them in the
-- same passes so restoring or auditing a snapshot works in the post-rename
-- world (as 0010 and 0013 did for their renames).
UPDATE knowledge_revision SET snapshot = jsonb_set(
        jsonb_set(snapshot, '{type}', snapshot->'attrs'->'okf_type'),
        '{attrs}', (snapshot->'attrs') - 'okf_type')
 WHERE snapshot->'attrs' ? 'okf_type'
   AND COALESCE(TRIM(snapshot->'attrs'->>'okf_type'), '') <> ''
   AND POSITION('/' IN snapshot->'attrs'->>'okf_type') = 0
   AND OCTET_LENGTH(snapshot->'attrs'->>'okf_type') <= 128;

UPDATE knowledge_revision SET snapshot = jsonb_set(snapshot, '{attrs}',
        (snapshot->'attrs') - 'okf_type')
 WHERE snapshot->'attrs' ? 'okf_type';

UPDATE knowledge_revision SET snapshot = jsonb_set(snapshot, '{type}', to_jsonb(
    CASE snapshot->>'type'
        WHEN 'models'     THEN 'Semantic Model'
        WHEN 'metrics'    THEN 'Metric'
        WHEN 'queries'    THEN 'Golden Query'
        WHEN 'insights'   THEN 'Insight'
        WHEN 'terms'      THEN 'Glossary Term'
        WHEN 'datasets'   THEN 'BigQuery Dataset'
        WHEN 'tables'     THEN 'BigQuery Table'
        WHEN 'references' THEN 'Reference'
    END))
 WHERE snapshot->>'type' IN ('models', 'metrics', 'queries', 'insights',
                             'terms', 'datasets', 'tables', 'references');
