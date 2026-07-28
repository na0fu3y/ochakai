-- The bundle path becomes the address (design doc 0046 §§2.1, 3.1).
--
-- ochakai has held a table of knowledge entries keyed by concept id, and
-- a separate table of attachments keyed by (entry, filename). 0046 turns
-- that around: what an instance holds is one bundle — a mapping from
-- path to object — and a knowledge entry is one kind of object in it.
-- The key of that mapping is the path, so it becomes the primary key
-- here, and the table stops being named after one of the two kinds.
--
-- The concept id stays, as the derived address it always was (design doc
-- 0017, SPEC §2: the path with ".md" removed). Every ledger keys by it —
-- verify, reject, usage and move are called with an id and will go on
-- being called with one (0046 §3.1) — and a unique index keeps it the
-- key it is. The one ledger that moves is the revision log: 0046 §3.1
-- makes a revision an event about an *object*, so that creating and
-- deleting a file lands in the same history as editing a concept.
--
-- Nothing about a file arrives in this migration. Concepts are the only
-- objects here, so path is exactly id || '.md' for every row; the next
-- change is where the attachment rows move in and the column stops being
-- derivable (0046 §3.13).

ALTER TABLE knowledge RENAME TO object;

ALTER TABLE object ADD COLUMN IF NOT EXISTS path text;
UPDATE object SET path = id || '.md' WHERE path IS NULL OR path = '';
ALTER TABLE object ALTER COLUMN path SET NOT NULL;

-- The primary key moves to the path; the id keeps a unique index of its
-- own, because it is what every other table joins on and what every
-- surface calls an entry by.
ALTER TABLE object DROP CONSTRAINT IF EXISTS knowledge_pkey;
ALTER TABLE object DROP CONSTRAINT IF EXISTS object_pkey;
ALTER TABLE object ADD CONSTRAINT object_pkey PRIMARY KEY (path);
CREATE UNIQUE INDEX IF NOT EXISTS object_id ON object (id);

-- A revision is an event about an object. The column is the path for the
-- same reason the primary key is, and the (path, rev) pair replaces
-- (id, rev) — including for the rows already written, whose entries are
-- all concepts.
ALTER TABLE knowledge_revision ADD COLUMN IF NOT EXISTS path text;
UPDATE knowledge_revision SET path = id || '.md' WHERE path IS NULL OR path = '';
ALTER TABLE knowledge_revision ALTER COLUMN path SET NOT NULL;
ALTER TABLE knowledge_revision DROP CONSTRAINT IF EXISTS knowledge_revision_pkey;
ALTER TABLE knowledge_revision ADD CONSTRAINT knowledge_revision_pkey PRIMARY KEY (path, rev);
-- The id column stays for one release: a revision of a concept is still
-- looked up by the id its surfaces call it by, and dropping the column
-- before the read paths stop using it would take the history with it.
CREATE INDEX IF NOT EXISTS knowledge_revision_id ON knowledge_revision (id);

-- A function body is text, not a reference: renaming the table does not
-- reach inside the one trigger function that names it (0016). The
-- attachment trigger touches the owning entry's row so the entry's
-- lexical haystack is recomputed from the current filenames, and it has
-- to touch it under the name the table now has.
CREATE OR REPLACE FUNCTION ochakai_attachment_search_text() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    target text := COALESCE(NEW.knowledge_id, OLD.knowledge_id);
BEGIN
    UPDATE object SET search_text = search_text WHERE id = target;
    RETURN NULL;
END $$;
