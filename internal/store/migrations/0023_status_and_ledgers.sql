-- Status becomes OKF's three values, and the two rulings that were
-- statuses become ledgers (design doc 0043 §§3.2-3.3).
--
-- ochakai had four statuses where SPEC §5.4 has three. The two extra ones
-- were not lifecycle values at all:
--
--   verified — SPEC §5.3 spells "somebody confirmed this" with the
--     verified key, not with a status. Holding it as a status made the
--     two signals one, so an entry could not be stable-but-unconfirmed,
--     and a foreign bundle's stable entry had to be demoted to draft on
--     import or it would have claimed a confirmation nobody made. It also
--     made re-verification an overwrite of the single verified_by /
--     verified_at pair, so "when was this last checked" was answerable
--     and "how often, and by whom" was not.
--
--   rejected — a ruling about the concept, not a stage of it. It could
--     not round-trip: export folded it onto deprecated, which asserts the
--     opposite thing about the entry ("was correct, no longer current").
--
-- So: verified → stable plus a verification row; rejected → draft plus a
-- rejection row. Nothing is lost in either direction, and the export that
-- used to lie about a rejection now says draft, with the ruling kept
-- where a ruling belongs — in this instance's ledger, which a bundle does
-- not carry (design doc 0009).

CREATE TABLE IF NOT EXISTS knowledge_verification (
    id       text        NOT NULL,
    seq      integer     NOT NULL,
    by_kind  text        NOT NULL,
    by_name  text        NOT NULL,
    by_via   text        NOT NULL DEFAULT '',
    at       timestamptz NOT NULL,
    PRIMARY KEY (id, seq)
);

-- The verification-age feed reads the newest row per entry, and the
-- re-verification feed compares it against the last failure report.
CREATE INDEX IF NOT EXISTS knowledge_verification_at ON knowledge_verification (id, at DESC);

-- One live ruling per entry: a rejection is a state ("we said no"), not a
-- log. Lifting it deletes the row, and rejecting again replaces it — the
-- history of both lives in the revisions.
CREATE TABLE IF NOT EXISTS knowledge_rejection (
    id       text        PRIMARY KEY,
    by_kind  text        NOT NULL,
    by_name  text        NOT NULL,
    by_via   text        NOT NULL DEFAULT '',
    at       timestamptz NOT NULL,
    note     text        NOT NULL DEFAULT ''
);

-- Verified entries: one ledger row each, then the status becomes stable.
--
-- An entry marked verified without a stamped verifier or time is still a
-- verification — the status was the claim, and dropping the row because
-- the provenance is thin would silently unverify it. Fall back to the
-- entry's own last write, which is the closest thing the row knows.
INSERT INTO knowledge_verification (id, seq, by_kind, by_name, by_via, at)
SELECT id, 1,
       COALESCE(verified_by_kind, updated_by_kind),
       COALESCE(verified_by_name, updated_by_name),
       COALESCE(verified_by_via, ''),
       COALESCE(verified_at, updated_at)
FROM knowledge
WHERE status = 'verified'
ON CONFLICT DO NOTHING;

-- Rejected entries: the ruling moves to the ledger and the entry falls
-- back to draft.
--
-- status_note carries the rejection reason for these rows — design doc
-- 0001 §9.1 made it the place to write one — so it moves to the ledger's
-- note and is cleared. Left behind, it would read as a draft explaining
-- why it was rejected, which is neither true of the status nor useful to
-- the next writer.
INSERT INTO knowledge_rejection (id, by_kind, by_name, by_via, at, note)
SELECT id,
       COALESCE(rejected_by_kind, updated_by_kind),
       COALESCE(rejected_by_name, updated_by_name),
       COALESCE(rejected_by_via, ''),
       COALESCE(rejected_at, updated_at),
       COALESCE(status_note, '')
FROM knowledge
WHERE status = 'rejected'
ON CONFLICT (id) DO NOTHING;

UPDATE knowledge SET status_note = '' WHERE status = 'rejected';

-- The constraint comes off first: it still names the old vocabulary, and
-- "stable" is not in it, so the rewrite below cannot run underneath it.
ALTER TABLE knowledge DROP CONSTRAINT IF EXISTS knowledge_status_check;

-- updated_at is deliberately untouched by every statement in this file.
-- It is the ETag version (design doc 0030) and OKF's generated.at: the
-- entry's content did not change, so no held precondition may be
-- invalidated and no generated.at may be misdated.
UPDATE knowledge SET status = 'stable' WHERE status = 'verified';
UPDATE knowledge SET status = 'draft'  WHERE status = 'rejected';

ALTER TABLE knowledge ADD CONSTRAINT knowledge_status_check
    CHECK (status IN ('draft', 'stable', 'deprecated'));

ALTER TABLE knowledge
    DROP COLUMN IF EXISTS verified_by_kind,
    DROP COLUMN IF EXISTS verified_by_name,
    DROP COLUMN IF EXISTS verified_by_via,
    DROP COLUMN IF EXISTS verified_at,
    DROP COLUMN IF EXISTS rejected_by_kind,
    DROP COLUMN IF EXISTS rejected_by_name,
    DROP COLUMN IF EXISTS rejected_by_via,
    DROP COLUMN IF EXISTS rejected_at;

-- History is rewritten into the current shape rather than left in the one
-- it had. Design doc 0043 §3.9 withdraws 0036 §3.13's "history keeps the
-- shape it had": a record whose shape depends on the reader knowing every
-- past generation of the struct is the distortion, not the fidelity — and
-- a snapshot whose status is a value the program no longer accepts cannot
-- be fed back to any surface.
UPDATE knowledge_revision SET snapshot =
    jsonb_set(snapshot, '{verifications}', jsonb_build_array(jsonb_build_object(
        'by', COALESCE(snapshot -> 'verified_by',
                       snapshot -> 'updated_by', '{"kind":"process","name":"unknown"}'::jsonb),
        'at', COALESCE(snapshot -> 'verified_at', snapshot -> 'updated_at'))))
WHERE snapshot ->> 'status' = 'verified';

UPDATE knowledge_revision SET snapshot =
    jsonb_set(snapshot, '{rejection}', jsonb_build_object(
        'by', COALESCE(snapshot -> 'rejected_by',
                       snapshot -> 'updated_by', '{"kind":"process","name":"unknown"}'::jsonb),
        'at', COALESCE(snapshot -> 'rejected_at', snapshot -> 'updated_at'),
        'note', COALESCE(snapshot -> 'status_note', '""'::jsonb)))
    #- '{status_note}'
WHERE snapshot ->> 'status' = 'rejected';

UPDATE knowledge_revision SET
    snapshot = jsonb_set(snapshot, '{status}',
        CASE snapshot ->> 'status' WHEN 'verified' THEN '"stable"'::jsonb ELSE '"draft"'::jsonb END)
        #- '{verified_by}' #- '{verified_at}' #- '{rejected_by}' #- '{rejected_at}'
WHERE snapshot ->> 'status' IN ('verified', 'rejected');

-- Snapshots that never carried one of the two retired statuses still
-- carry the retired provenance keys when they were stamped and later
-- cleared. Strip them everywhere, so no revision offers a field the
-- current shape has no place for.
UPDATE knowledge_revision
   SET snapshot = snapshot #- '{verified_by}' #- '{verified_at}' #- '{rejected_by}' #- '{rejected_at}'
 WHERE snapshot ?| ARRAY['verified_by', 'verified_at', 'rejected_by', 'rejected_at'];
