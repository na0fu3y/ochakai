-- knowledge-catalog alignment (design doc 0016): recommended type slugs
-- become plural, matching the OKF reference bundles, and "resource" (the
-- canonical URI of the underlying asset) is promoted from attrs to an
-- envelope column. If a free type already uses a plural slug ("metrics")
-- alongside the old singular one with the same id, the UPDATEs below abort
-- on the primary-key conflict and the whole migration rolls back: resolve
-- the collision, then re-apply.

ALTER TABLE knowledge ADD COLUMN resource text NOT NULL DEFAULT '';

-- attrs.resource was the resting place for the OKF "resource" key on
-- imported bundles; attrs.source played that role for ochakai's own table
-- entries (design doc 0005 §3.3, retired by 0016 §2.3).
UPDATE knowledge
   SET resource = COALESCE(attrs->>'resource', ''), attrs = attrs - 'resource'
 WHERE attrs ? 'resource';
UPDATE knowledge
   SET resource = COALESCE(attrs->>'source', ''), attrs = attrs - 'source'
 WHERE type = 'table' AND resource = '' AND attrs ? 'source';

-- Fully-qualified BigQuery sources ("project.dataset.table") normalize to
-- the canonical REST resource URL the OKF knowledge-catalog bundles use
-- (0016 §2.3); unqualified sources have no canonical URL and stay as-is.
UPDATE knowledge
   SET resource = regexp_replace(resource, '^([^.]+)\.([^.]+)\.([^.]+)$',
                  'https://bigquery.googleapis.com/v2/projects/\1/datasets/\2/tables/\3')
 WHERE type = 'table' AND resource ~ '^[^.]+\.[^.]+\.[^.]+$';

-- Rename the five pre-0016 recommended slugs everywhere a type is stored.
UPDATE knowledge SET type = type || 's'
 WHERE type IN ('metric', 'insight', 'term', 'table');
UPDATE knowledge SET type = 'queries' WHERE type = 'query';

UPDATE knowledge_revision SET type = type || 's'
 WHERE type IN ('metric', 'insight', 'term', 'table');
UPDATE knowledge_revision SET type = 'queries' WHERE type = 'query';

UPDATE knowledge_event SET knowledge_type = knowledge_type || 's'
 WHERE knowledge_type IN ('metric', 'insight', 'term', 'table');
UPDATE knowledge_event SET knowledge_type = 'queries' WHERE knowledge_type = 'query';

UPDATE knowledge_usage SET knowledge_type = knowledge_type || 's'
 WHERE knowledge_type IN ('metric', 'insight', 'term', 'table');
UPDATE knowledge_usage SET knowledge_type = 'queries' WHERE knowledge_type = 'query';

UPDATE attachment SET knowledge_type = knowledge_type || 's'
 WHERE knowledge_type IN ('metric', 'insight', 'term', 'table');
UPDATE attachment SET knowledge_type = 'queries' WHERE knowledge_type = 'query';

-- knowledge_embedding exists only when semantic search is configured
-- (created outside versioned migrations, see migrate.go).
DO $$
BEGIN
    IF to_regclass('knowledge_embedding') IS NOT NULL THEN
        UPDATE knowledge_embedding SET type = type || 's'
         WHERE type IN ('metric', 'insight', 'term', 'table');
        UPDATE knowledge_embedding SET type = 'queries' WHERE type = 'query';
    END IF;
END $$;

-- Link targets carry the type inline ("metrics/revenue", with or without
-- the ochakai:// scheme). Rewrite them in live rows and in revision
-- snapshots — a rename applied consistently, so restoring or auditing a
-- snapshot works in the post-rename world (0016 §3).
UPDATE knowledge SET links = (
    SELECT jsonb_agg(
        CASE WHEN l->>'target' IS NULL THEN l
             ELSE jsonb_set(l, '{target}', to_jsonb(
                regexp_replace(
                    regexp_replace(l->>'target', '^((ochakai://)?)query/', '\1queries/'),
                    '^((ochakai://)?)(metric|insight|term|table)/', '\1\3s/')))
        END ORDER BY ord)
      FROM jsonb_array_elements(links) WITH ORDINALITY AS t(l, ord))
 WHERE jsonb_typeof(links) = 'array' AND links <> '[]'::jsonb;

UPDATE knowledge_revision SET snapshot = jsonb_set(snapshot, '{type}', to_jsonb(
    CASE snapshot->>'type' WHEN 'query' THEN 'queries'
         ELSE (snapshot->>'type') || 's' END))
 WHERE snapshot->>'type' IN ('metric', 'query', 'insight', 'term', 'table');

UPDATE knowledge_revision SET snapshot = jsonb_set(snapshot, '{links}', (
    SELECT jsonb_agg(
        CASE WHEN l->>'target' IS NULL THEN l
             ELSE jsonb_set(l, '{target}', to_jsonb(
                regexp_replace(
                    regexp_replace(l->>'target', '^((ochakai://)?)query/', '\1queries/'),
                    '^((ochakai://)?)(metric|insight|term|table)/', '\1\3s/')))
        END ORDER BY ord)
      FROM jsonb_array_elements(snapshot->'links') WITH ORDINALITY AS t(l, ord)))
 WHERE jsonb_typeof(snapshot->'links') = 'array' AND snapshot->'links' <> '[]'::jsonb;
