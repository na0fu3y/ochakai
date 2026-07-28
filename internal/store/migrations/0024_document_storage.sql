-- The canonical OKF document becomes the stored form (design doc 0043
-- §§3.1, 3.7), and its hash becomes the entry's version (§3.4).
--
-- Until now an entry was a row of columns that an exporter re-rendered
-- into a document on the way out, and its version was updated_at. Both
-- had costs the design doc lays out: the render was a mapping to
-- maintain against a spec that moves, and updated_at was doing three
-- jobs at once — the ETag, OKF's generated.at, and "when was this row
-- last touched" — which is why verifying an entry had to move a version
-- that no editor's content had changed.
--
-- doc holds the document a writer authored, with none of the keys this
-- instance owns; the columns beside it stay, as the derived index every
-- query already reads (§3.1). content_hash is the SHA-256 of doc.
--
-- Neither column can be filled here: composing a document means writing
-- YAML in the spelling and key order the spec fixes, which is Go's job,
-- not SQL's. Both default to empty and the server backfills them once at
-- startup, right after this migration (Store.backfillDocuments). An
-- empty doc is the marker for "not yet composed", so the backfill is
-- idempotent and a half-finished run simply resumes.
--
-- updated_at is untouched: composing the stored form is not a content
-- change, and no held ETag may be invalidated by a representation
-- change. (The ETag does move for every client here — but that is the
-- version scheme changing, which the changelog states, not this
-- migration silently editing entries.)

ALTER TABLE knowledge ADD COLUMN IF NOT EXISTS doc          text NOT NULL DEFAULT '';
ALTER TABLE knowledge ADD COLUMN IF NOT EXISTS content_hash text NOT NULL DEFAULT '';

-- History gets the same treatment: a revision is the document as it stood
-- (design doc 0043 §3.9), rather than a JSON snapshot of whatever the Go
-- struct happened to look like that release. A record whose shape depends
-- on the reader knowing every past generation of a struct is the
-- distortion, not the fidelity — and OKF is an anchor that outlasts them.
--
-- snapshot is kept, not dropped: it is the only source the backfill has
-- for the text of an existing revision. It loses NOT NULL because new
-- revisions no longer write one, and a later release removes it once no
-- deployment still has revisions to convert. Keeping a dead column for a
-- release costs a little tidiness; dropping the only copy of the history
-- before it has been read costs the history.
ALTER TABLE knowledge_revision ADD COLUMN IF NOT EXISTS doc text NOT NULL DEFAULT '';
ALTER TABLE knowledge_revision ALTER COLUMN snapshot DROP NOT NULL;
