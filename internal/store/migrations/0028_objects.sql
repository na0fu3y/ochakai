-- Files become objects at their bundle path (design doc 0046 §§3.1-3.3).
--
-- An attachment was keyed by (entry, filename) and carried okf_path to
-- remember where it had really come from — two addresses for one file,
-- the authoritative one used only by export. Worse, the model had no
-- place at all for a file belonging to no entry, so an import dropped
-- it: a bundle round-tripped through ochakai came out smaller than it
-- went in, which is the defect 0046 exists to fix.
--
-- So a file is a row addressed by its path, like everything else in a
-- bundle, and which entry it belongs to is derived from the bodies that
-- link to it. Nothing is stored about belonging.
CREATE TABLE IF NOT EXISTS object (
    path            text        NOT NULL PRIMARY KEY,
    sha256          text        NOT NULL REFERENCES blob (sha256),
    created_by_kind text        NOT NULL,
    created_by_name text        NOT NULL,
    created_by_via  text        NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now(),
    deleted_at      timestamptz
);

CREATE INDEX IF NOT EXISTS object_live_path ON object (path) WHERE deleted_at IS NULL;

-- The move. A file's path is the one it already had in a bundle: the
-- foreign location it was imported from when there is one, and the
-- canonical <id>/<name> layout otherwise (design docs 0008 §3.4, 0013
-- §2.3 — the same two rules that decided attribution, now deciding the
-- address).
--
-- Two attachments can land on one path: a foreign okf_path that happens
-- to equal another entry's canonical layout. The okf_path wins, because
-- it is where a body's link points and losing it breaks the link; the
-- loser keeps the canonical address, which is where an export would have
-- written it anyway. Both files survive, which is the point.
INSERT INTO object (path, sha256, created_by_kind, created_by_name, created_at)
SELECT DISTINCT ON (path) path, sha256, created_by_kind, created_by_name, created_at
FROM (
    SELECT COALESCE(NULLIF(a.okf_path, ''), a.knowledge_id || '/' || a.name) AS path,
           a.sha256, a.created_by_kind, a.created_by_name, a.created_at,
           CASE WHEN a.okf_path <> '' THEN 0 ELSE 1 END AS rank
    FROM attachment a
    JOIN knowledge k ON k.id = a.knowledge_id AND k.deleted_at IS NULL
    UNION ALL
    SELECT a.knowledge_id || '/' || a.name, a.sha256, a.created_by_kind, a.created_by_name, a.created_at, 2
    FROM attachment a
    JOIN knowledge k ON k.id = a.knowledge_id AND k.deleted_at IS NULL
    WHERE a.okf_path <> ''
) candidates
ORDER BY path, rank
ON CONFLICT (path) DO NOTHING;

DROP TABLE IF EXISTS attachment;

-- Vectors follow the address (design doc 0020 keeps working; only its
-- key changes). Rebuilt rather than moved: the old key names an entry
-- and a filename, and mapping it back to a path would repeat the
-- decision above without the information to make it. Files re-embed on
-- the next `ochakai reembed`, which is the same treatment 0020 gave
-- attachments that predated it.
DROP TABLE IF EXISTS attachment_embedding;

-- The other half of the derivation, as an index column beside links
-- (design docs 0024, 0046 §3.1): the file paths an entry's body
-- references. Belonging is "what the body links to, plus what sits in
-- the entry's directory", and the second half is a string operation on
-- the path while the first is a reading of markdown — which has to be
-- derived on write, exactly as links already are.
ALTER TABLE knowledge ADD COLUMN IF NOT EXISTS file_links text[] NOT NULL DEFAULT '{}';

CREATE INDEX IF NOT EXISTS knowledge_file_links_gin ON knowledge USING gin (file_links);

-- Backfilled by the same pass that indexed the frontmatter: reading a
-- body's links is the parser's job, not SQL's.
CREATE TABLE IF NOT EXISTS file_link_backfill (
    id text PRIMARY KEY
);

INSERT INTO file_link_backfill (id) SELECT id FROM knowledge ON CONFLICT (id) DO NOTHING;
