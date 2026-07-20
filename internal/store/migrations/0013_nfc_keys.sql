-- NFC normalization of byte-compared keys (design doc 0022): ids, link
-- targets, attachment names/paths. 0019 (v0.10.0) began accepting
-- non-ASCII ids without prescribing a Unicode form, so NFD spellings
-- (macOS filesystems decompose path names) could have been stored; from
-- 0022 on, every write boundary normalizes to NFC, and this migration
-- makes the invariant true for stored data. On real data this is a
-- no-op scan (ASCII is untouched by NFC); a normalization collision
-- (two ids becoming one) fails the migration loudly for a human to
-- resolve. Content columns (title, description, body) stay as written.

UPDATE knowledge SET id = normalize(id, NFC)
    WHERE id IS DISTINCT FROM normalize(id, NFC);
UPDATE knowledge SET links = (
        SELECT COALESCE(jsonb_agg(
            jsonb_set(l, '{target}', to_jsonb(normalize(COALESCE(l->>'target', ''), NFC)))
            ORDER BY ord), '[]'::jsonb)
        FROM jsonb_array_elements(links) WITH ORDINALITY AS t(l, ord))
    WHERE links::text IS DISTINCT FROM normalize(links::text, NFC);

-- Revision snapshots embed the entry id; rewrite it in the same pass so
-- history reads back under the entry's one address (as 0011 did).
UPDATE knowledge_revision SET
        id = normalize(id, NFC),
        snapshot = jsonb_set(snapshot, '{id}', to_jsonb(normalize(id, NFC)))
    WHERE id IS DISTINCT FROM normalize(id, NFC);

UPDATE knowledge_event SET knowledge_id = normalize(knowledge_id, NFC)
    WHERE knowledge_id IS DISTINCT FROM normalize(knowledge_id, NFC);
UPDATE knowledge_usage SET knowledge_id = normalize(knowledge_id, NFC)
    WHERE knowledge_id IS DISTINCT FROM normalize(knowledge_id, NFC);

UPDATE attachment SET
        knowledge_id = normalize(knowledge_id, NFC),
        name = normalize(name, NFC),
        okf_path = normalize(okf_path, NFC)
    WHERE (knowledge_id || name || okf_path)
        IS DISTINCT FROM normalize(knowledge_id || name || okf_path, NFC);

-- Embedding tables exist only once semantic search has been enabled
-- (their DDL lives outside versioned migrations; see migrate.go).
DO $$
BEGIN
    IF to_regclass('knowledge_embedding') IS NOT NULL THEN
        UPDATE knowledge_embedding SET id = normalize(id, NFC)
            WHERE id IS DISTINCT FROM normalize(id, NFC);
    END IF;
    IF to_regclass('attachment_embedding') IS NOT NULL THEN
        UPDATE attachment_embedding SET
                knowledge_id = normalize(knowledge_id, NFC),
                name = normalize(name, NFC)
            WHERE (knowledge_id || name)
                IS DISTINCT FROM normalize(knowledge_id || name, NFC);
    END IF;
END $$;
