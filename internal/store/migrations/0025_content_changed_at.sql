-- generated.at gets a column of its own (design doc 0044 §3.4).
--
-- updated_at has been standing in for it. That worked while the stored
-- document was the canonical rendering: a write that changed no content
-- rendered the same bytes, so it was refused as a no-op and updated_at
-- could not move without the content moving with it.
--
-- Storing the document as received (0044 §2.2) breaks that coincidence.
-- Reformatting an entry — reordering its frontmatter, adding a comment —
-- changes the file and therefore its version, while what the entry says
-- is exactly what it said before. SPEC §5.2 defines generated.at as the
-- last meaningful change, so the two need separate columns: updated_at
-- is when the row was last written, content_changed_at is when what it
-- says last changed.
--
-- The backfill is updated_at, which is what the column meant until now.
ALTER TABLE knowledge ADD COLUMN IF NOT EXISTS content_changed_at timestamptz;

UPDATE knowledge SET content_changed_at = updated_at WHERE content_changed_at IS NULL;

ALTER TABLE knowledge
    ALTER COLUMN content_changed_at SET DEFAULT now(),
    ALTER COLUMN content_changed_at SET NOT NULL;
