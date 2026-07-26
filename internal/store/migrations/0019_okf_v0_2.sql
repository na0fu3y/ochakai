-- OKF v0.2 (design doc 0034) needs two things the envelope did not have.
--
-- updated_by: OKF's generated.by is "who produced the content as it now
-- stands" (SPEC §5.2). ochakai only knew created_by — accurate until
-- somebody else edits the entry, after which exporting created_by as the
-- generator is a lie. The revision table always held the answer; this
-- lifts it onto the entry so a read (and an export) does not have to walk
-- history.
--
-- stale_after: the lifecycle date from SPEC §5.5. A date, not a
-- timestamp: staleness is `today >= stale_after`, and neither side of
-- that comparison should depend on a timezone.
--
-- The backfill takes the actor of the newest revision that changed
-- content (create, update, move), falling back to created_by for rows
-- whose revisions predate the change kinds or were purged. Verify
-- revisions are deliberately excluded: confirming an entry is not
-- producing its content, which is exactly the distinction v0.2 draws
-- between verified and generated.

ALTER TABLE knowledge ADD COLUMN IF NOT EXISTS updated_by_kind text NOT NULL DEFAULT '';
ALTER TABLE knowledge ADD COLUMN IF NOT EXISTS updated_by_name text NOT NULL DEFAULT '';
ALTER TABLE knowledge ADD COLUMN IF NOT EXISTS updated_by_via  text NOT NULL DEFAULT '';
ALTER TABLE knowledge ADD COLUMN IF NOT EXISTS stale_after     date;

UPDATE knowledge k SET
    updated_by_kind = COALESCE(r.changed_by_kind, k.created_by_kind),
    updated_by_name = COALESCE(r.changed_by_name, k.created_by_name),
    updated_by_via  = COALESCE(r.changed_by_via, k.created_by_via)
FROM (
    SELECT DISTINCT ON (id) id, changed_by_kind, changed_by_name, changed_by_via
    FROM knowledge_revision
    WHERE change IN ('create', 'update', 'move')
    ORDER BY id, rev DESC
) r
WHERE r.id = k.id AND k.updated_by_kind = '';

UPDATE knowledge SET
    updated_by_kind = created_by_kind,
    updated_by_name = created_by_name,
    updated_by_via  = created_by_via
WHERE updated_by_kind = '';
