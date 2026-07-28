-- The frontmatter becomes the index (design doc 0046 §3.11).
--
-- Storage became additive when the document became the stored form: a key
-- OKF adds in a later version is kept, round-tripped and served without
-- ochakai knowing anything about it. Querying did not follow. Every key a
-- filter can name is a typed column, so "can I ask for this?" still has a
-- release-shaped answer — which is the same coupling design doc 0043 set
-- out to remove, surviving in the one place it did not look.
--
-- So the whole frontmatter is indexed as jsonb, with a containment index
-- over it. A key the spec adds is queryable the day a writer writes it,
-- with no column, no migration and no release.
--
-- The typed columns stay. They are the same values with better indexes —
-- trigram over the text, a date column for stale_after, an array for
-- tags — and this is an index beside them, not a replacement for them.
ALTER TABLE knowledge ADD COLUMN IF NOT EXISTS frontmatter jsonb NOT NULL DEFAULT '{}';

-- jsonb_path_ops: this index answers containment and nothing else, which
-- is exactly what the fm filter asks, and it is the smaller of the two
-- jsonb operator classes.
CREATE INDEX IF NOT EXISTS knowledge_frontmatter_gin
    ON knowledge USING gin (frontmatter jsonb_path_ops);

-- The work list for the backfill, which is Go: reading the frontmatter
-- out of a stored document means parsing YAML, and the parser that
-- decides what a key means has to be the one that already does
-- (internal/okf), not a second reading of the same text written in SQL.
CREATE TABLE IF NOT EXISTS frontmatter_backfill (
    id text PRIMARY KEY
);

INSERT INTO frontmatter_backfill (id)
SELECT id FROM knowledge WHERE doc <> '' AND frontmatter = '{}'
ON CONFLICT (id) DO NOTHING;
