-- A file is an object in the bundle, not a property of an entry
-- (design doc 0046 §§2.1, 3.3, 3.13).
--
-- An attachment row was a mapping from (entry, filename) to a blob: the
-- file existed only as something an entry had. That is not what a bundle
-- is. A bundle is a directory tree, a diagram in it is a file at a path,
-- and what makes it "the metric's diagram" is that the metric's document
-- shows it. So the rows move into the object table, at the path each
-- file lives at, and attribution becomes derived.
--
-- Derived from two things, which is what the columns added here are for:
-- a file directly under the entry's own <id>/ namespace belongs to it
-- (that is a question about the path alone), and so does a file the
-- entry's body points at (the files column, maintained beside links the
-- way links are maintained beside the body — design doc 0024).
--
-- The seeding of files below is exact rather than a re-parse: the
-- attachment table records precisely which entry each file belonged to,
-- and that mapping is the answer this column would otherwise have to
-- rediscover. A file at the canonical <id>/<name> needs no entry there —
-- the path already says it.
--
-- attachment and attachment_embedding keep their rows. The read paths
-- move to object in this release and the tables go in a later one, the
-- way 0024 kept knowledge_revision.snapshot until the history had been
-- read out of it: dropping the only copy before anything has been
-- verified against it costs more than a release of untidiness.

ALTER TABLE object ADD COLUMN IF NOT EXISTS blob_hash  text   NOT NULL DEFAULT '';
ALTER TABLE object ADD COLUMN IF NOT EXISTS size       bigint NOT NULL DEFAULT 0;
ALTER TABLE object ADD COLUMN IF NOT EXISTS media_type text   NOT NULL DEFAULT '';
ALTER TABLE object ADD COLUMN IF NOT EXISTS files      jsonb  NOT NULL DEFAULT '[]';

-- A file has no concept id: an id is the address of a concept, and the
-- path with ".md" removed is not one for a .png. NULL rather than '' so
-- the unique index on id keeps meaning what it means — Postgres lets
-- NULLs repeat and would refuse a second empty string.
ALTER TABLE object ALTER COLUMN id DROP NOT NULL;

-- Everything that reads entries reads them by their concept id, and a
-- file has none: the partial index keeps those reads on an index that
-- holds only concepts, so a bundle full of files costs them nothing.
CREATE INDEX IF NOT EXISTS object_concept ON object (updated_at DESC) WHERE id IS NOT NULL;

-- The files an entry points at, asked from the other end: "which entry
-- shows this file?" is the containment query the derivation needs.
CREATE INDEX IF NOT EXISTS object_files ON object USING gin (files);

-- The lexical haystack follows the files (design doc 0020: a filename is
-- a name the entry can be found by). Two things change with them.
--
-- A file row has no id, and 0016's trigger would compute NULL for it and
-- fail the NOT NULL — a file is found by its path, not by a haystack, so
-- it gets an empty one and returns early. This has to be in place
-- *before* the rows below move, or the first file inserted on a base
-- that has any is the one that fails the migration and leaves the
-- schema half-changed.
CREATE OR REPLACE FUNCTION ochakai_knowledge_search_text() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id IS NULL THEN
        NEW.search_text := '';
        RETURN NEW;
    END IF;
    NEW.search_text := ochakai_search_text(NEW.id, NEW.title, NEW.description, NEW.tags, NEW.body)
        || ' ' || COALESCE((SELECT string_agg(regexp_replace(x, '^.*/', ''), ' ')
                              FROM jsonb_array_elements_text(NEW.files) x), '');
    RETURN NEW;
END $$;

-- And the names come from the objects under the entry's namespace rather
-- than from the attachment table.
CREATE OR REPLACE FUNCTION ochakai_search_text(
    p_id text, p_title text, p_description text, p_tags text[], p_body text
) RETURNS text LANGUAGE sql STABLE AS $$
    SELECT p_id || ' ' || p_title || ' ' || p_description || ' '
        || array_to_string(p_tags, ' ') || ' ' || p_body || ' '
        || COALESCE((SELECT string_agg(regexp_replace(f.path, '^.*/', ''), ' ')
                       FROM object f
                      WHERE f.id IS NULL AND f.deleted_at IS NULL
                        AND f.path LIKE p_id || '/%'
                        AND strpos(substr(f.path, length(p_id) + 2), '/') = 0), '')
$$;

-- Writing a file refreshes the haystack of whoever it is attributed to:
-- the entry whose namespace it sits in, and any entry whose body names
-- it. The UPDATE fires the row trigger above, which recomputes from the
-- files that exist at that moment, so nothing is assigned here.
CREATE OR REPLACE FUNCTION ochakai_file_search_text() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    p text := COALESCE(NEW.path, OLD.path);
BEGIN
    UPDATE object SET search_text = search_text
     WHERE id IS NOT NULL AND (id = regexp_replace(p, '/[^/]*$', '') OR files ? p);
    RETURN NULL;
END $$;

-- Two triggers rather than one: a DELETE trigger's WHEN condition may
-- not mention NEW, so the condition is written against the row each
-- event actually has. They are in place before the move as well, so the
-- entries that own the arriving files have their haystacks recomputed by
-- the same path a later write would take.
DROP TRIGGER IF EXISTS object_file_search_text ON object;
DROP TRIGGER IF EXISTS object_file_written ON object;
DROP TRIGGER IF EXISTS object_file_removed ON object;
CREATE TRIGGER object_file_written AFTER INSERT OR UPDATE ON object
    FOR EACH ROW WHEN (NEW.id IS NULL) EXECUTE FUNCTION ochakai_file_search_text();
CREATE TRIGGER object_file_removed AFTER DELETE ON object
    FOR EACH ROW WHEN (OLD.id IS NULL) EXECUTE FUNCTION ochakai_file_search_text();

-- Where each file lands. Its own path when it has one and nothing is
-- there already, and the canonical <id>/<name> otherwise: an imported
-- bundle's file goes back where it came from (design doc 0046 §3.2, so
-- the bundle round-trips), while a collision moves the loser rather than
-- dropping it (§3.13 — losing one of two files is the worse answer).
CREATE TEMP TABLE object_from_attachment ON COMMIT DROP AS
SELECT a.knowledge_id, a.name, a.sha256, b.media_type, b.size,
       a.created_by_kind, a.created_by_name, a.created_at,
       CASE
           WHEN a.okf_path <> ''
            AND NOT EXISTS (SELECT 1 FROM object o WHERE o.path = a.okf_path)
            AND (SELECT count(*) FROM attachment x WHERE x.okf_path = a.okf_path) = 1
           THEN a.okf_path
           ELSE a.knowledge_id || '/' || a.name
       END AS path
  FROM attachment a
  JOIN blob b ON b.sha256 = a.sha256;

-- Attribution first, while the object table still says what was there
-- before this migration ran: every file whose path is not the entry's
-- own <id>/<name> is named by the entry it belonged to.
UPDATE object o SET files = COALESCE((
    SELECT jsonb_agg(DISTINCT m.path)
      FROM object_from_attachment m
     WHERE m.knowledge_id = o.id
       AND m.path <> m.knowledge_id || '/' || m.name
), '[]'::jsonb)
WHERE EXISTS (SELECT 1 FROM object_from_attachment m WHERE m.knowledge_id = o.id);

INSERT INTO object (path, type, title, body, blob_hash, size, media_type,
                    created_by_kind, created_by_name, updated_by_kind, updated_by_name,
                    created_at, updated_at, content_changed_at)
SELECT m.path, '', '', '', m.sha256, m.size, m.media_type,
       m.created_by_kind, m.created_by_name, m.created_by_kind, m.created_by_name,
       m.created_at, m.created_at, m.created_at
  FROM object_from_attachment m
ON CONFLICT (path) DO NOTHING;

-- The old attachment trigger has nothing left to fire on: nothing writes
-- the attachment table now. It goes with the table, a release from now.
UPDATE object SET search_text = '' WHERE id IS NULL;
