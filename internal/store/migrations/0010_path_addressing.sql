-- Path addressing (design doc 0016): the full bundle path becomes the
-- sole primary key and type is demoted to entry metadata. Existing ids
-- are rewritten to '<type>/<id>' — the spelling every serialized
-- reference (export paths, URIs, link targets, REST paths) already uses,
-- so stored references keep resolving. Tables that carried type only as
-- part of the key drop the column entirely; knowledge.type stays as the
-- metadata column behind search filters and type-bound behaviors.

UPDATE knowledge SET id = type || '/' || id;
ALTER TABLE knowledge DROP CONSTRAINT knowledge_pkey;
ALTER TABLE knowledge ADD PRIMARY KEY (id);

-- Revision snapshots embed the entry id; rewrite it in the same pass so
-- history reads back under the entry's one address.
UPDATE knowledge_revision SET
    id = type || '/' || id,
    snapshot = jsonb_set(snapshot, '{id}', to_jsonb(type || '/' || id));
ALTER TABLE knowledge_revision DROP CONSTRAINT knowledge_revision_pkey;
ALTER TABLE knowledge_revision DROP COLUMN type;
ALTER TABLE knowledge_revision ADD PRIMARY KEY (id, rev);

UPDATE knowledge_event SET knowledge_id = knowledge_type || '/' || knowledge_id;
DROP INDEX IF EXISTS knowledge_event_target;
ALTER TABLE knowledge_event DROP COLUMN knowledge_type;
CREATE INDEX knowledge_event_target ON knowledge_event (knowledge_id, at);

UPDATE knowledge_usage SET knowledge_id = knowledge_type || '/' || knowledge_id;
ALTER TABLE knowledge_usage DROP CONSTRAINT knowledge_usage_pkey;
ALTER TABLE knowledge_usage DROP COLUMN knowledge_type;
ALTER TABLE knowledge_usage ADD PRIMARY KEY (knowledge_id, event);

UPDATE attachment SET knowledge_id = knowledge_type || '/' || knowledge_id;
ALTER TABLE attachment DROP CONSTRAINT attachment_pkey;
ALTER TABLE attachment DROP COLUMN knowledge_type;
ALTER TABLE attachment ADD PRIMARY KEY (knowledge_id, name);

-- knowledge_embedding exists only once semantic search has been enabled
-- (its DDL lives outside versioned migrations; see migrate.go).
DO $$
BEGIN
    IF to_regclass('knowledge_embedding') IS NOT NULL THEN
        UPDATE knowledge_embedding SET id = type || '/' || id;
        ALTER TABLE knowledge_embedding DROP CONSTRAINT knowledge_embedding_pkey;
        ALTER TABLE knowledge_embedding DROP COLUMN type;
        ALTER TABLE knowledge_embedding ADD PRIMARY KEY (id);
    END IF;
END $$;
