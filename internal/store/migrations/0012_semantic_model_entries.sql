-- Semantic models become knowledge entries (design doc 0018): each
-- semantic_model row moves to a 'models/<name>' entry with the spec kept
-- verbatim in attrs.spec, metric entries reference their model by entry
-- id, table entries' defined_in links gain a real target, and the table
-- is dropped. If an entry already occupies 'models/<name>', the INSERT
-- below aborts on the primary-key conflict and the whole migration rolls
-- back (0010 precedent): resolve the collision, then re-apply.

-- Migrated entries land as drafts: the migration does not stand in for a
-- human's verification. created_at/updated_at are the migration time —
-- the old table kept no creation time or provenance to carry over.
INSERT INTO knowledge (type, id, title, description, status, status_note,
                       created_by_kind, created_by_name, attrs)
SELECT 'models', 'models/' || name, name,
       COALESCE(spec->>'description', ''), 'draft',
       'migrated from the semantic_model table (design doc 0018); verify after review',
       'system', 'migration-0012', jsonb_build_object('spec', spec)
  FROM semantic_model;

INSERT INTO knowledge_revision (id, rev, change, changed_by_kind, changed_by_name, snapshot)
SELECT k.id, 1, 'create', 'system', 'migration-0012',
       jsonb_build_object(
           'type', 'models', 'id', k.id, 'title', k.title,
           'description', k.description, 'status', 'draft',
           'status_note', k.status_note,
           'created_by', jsonb_build_object('kind', 'system', 'name', 'migration-0012'),
           'attrs', k.attrs,
           'created_at', k.created_at, 'updated_at', k.updated_at)
  FROM knowledge k
 WHERE k.created_by_name = 'migration-0012' AND k.type = 'models';

-- attrs.model on metric entries was the bare model name (the old
-- semantic_model key); it becomes the models entry's id, in live rows
-- and in revision snapshots (0016 precedent: history must keep resolving).
UPDATE knowledge
   SET attrs = jsonb_set(attrs, '{model}', to_jsonb('models/' || (attrs->>'model')))
 WHERE type = 'metrics' AND jsonb_typeof(attrs->'model') = 'string'
   AND attrs->>'model' NOT LIKE 'models/%';

UPDATE knowledge_revision
   SET snapshot = jsonb_set(snapshot, '{attrs,model}',
                            to_jsonb('models/' || (snapshot->'attrs'->>'model')))
 WHERE snapshot->>'type' = 'metrics'
   AND jsonb_typeof(snapshot->'attrs'->'model') = 'string'
   AND snapshot->'attrs'->>'model' NOT LIKE 'models/%';

-- Table entries' defined_in links carried 'model/<name>' — a target no
-- entry ever occupied (the importer's spelling predates 0016's plural
-- slugs). Point them at the models entry that now exists.
UPDATE knowledge SET links = (
    SELECT jsonb_agg(
        CASE WHEN l->>'target' IS NULL THEN l
             ELSE jsonb_set(l, '{target}', to_jsonb(
                regexp_replace(l->>'target', '^((ochakai://)?)model/', '\1models/')))
        END ORDER BY ord)
      FROM jsonb_array_elements(links) WITH ORDINALITY AS t(l, ord))
 WHERE jsonb_typeof(links) = 'array' AND links <> '[]'::jsonb
   AND links::text LIKE '%model/%';

UPDATE knowledge_revision SET snapshot = jsonb_set(snapshot, '{links}', (
    SELECT jsonb_agg(
        CASE WHEN l->>'target' IS NULL THEN l
             ELSE jsonb_set(l, '{target}', to_jsonb(
                regexp_replace(l->>'target', '^((ochakai://)?)model/', '\1models/')))
        END ORDER BY ord)
      FROM jsonb_array_elements(snapshot->'links') WITH ORDINALITY AS t(l, ord)))
 WHERE jsonb_typeof(snapshot->'links') = 'array' AND snapshot->'links' <> '[]'::jsonb
   AND (snapshot->'links')::text LIKE '%model/%';

DROP TABLE semantic_model;
