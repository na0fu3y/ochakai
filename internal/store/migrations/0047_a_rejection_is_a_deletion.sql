-- A rejection is a deletion, and its reason is the log entry's
-- (design doc 0135).
--
-- A rejection used to be a ledger row against a live concept, which every
-- listing then had to remember to hide, and which gated whether the id
-- could be written again. It was the one ruling that never decayed: no
-- expiry, no queue, carried over by every update — so rewriting the
-- document did not lift the no — and hidden by default from search and
-- from browsing. One ruling buried an address permanently, while the
-- reason it rested on goes stale in weeks.
--
-- Now the removal is the ruling and the reason travels with the event:
-- a delete carrying a note writes `reject` instead of `delete`, and the
-- note goes on that revision, which is what the OKF SPEC §9 `log.md`
-- renders. §9 is the only place the format has for a statement about
-- something that is no longer there — its signals are monotone, and
-- §5.3's trust ladder has no rung below `unverified`.
--
-- Three steps, in this order.

-- 1. A revision carries the reason it was made for. Only a rejection
--    writes one today; the column is the event's, not the rejection's,
--    because that is what a log line is.
ALTER TABLE knowledge_revision ADD COLUMN IF NOT EXISTS note text NOT NULL DEFAULT '';

-- 2. Every concept a curator turned down becomes a tombstone, and the
--    reason it was turned down moves onto the revision that recorded the
--    ruling. The newest `reject` revision is the ruling in force — an id
--    can have been rejected, revived and rejected again, and the ledger
--    only ever held the last of those.
UPDATE knowledge_revision r
   SET note = j.note
  FROM (SELECT DISTINCT ON (rev.id) rev.id, rev.rev, x.note
          FROM knowledge_revision rev
          JOIN knowledge_rejection x ON x.id = rev.id
         WHERE rev.change = 'reject'
         ORDER BY rev.id, rev.rev DESC) AS j
 WHERE r.id = j.id AND r.rev = j.rev AND j.note <> '';

UPDATE object
   SET deleted_at = now(), updated_at = now()
 WHERE deleted_at IS NULL
   AND EXISTS (SELECT 1 FROM knowledge_rejection r WHERE r.id = object.id);

-- 3. The ledger goes. Nothing reads it: a rejection is not a state a
--    concept is in, and it gates nothing — the same id can be written
--    again from any surface, which is what makes the reason information
--    rather than a wall.
DROP TABLE IF EXISTS knowledge_rejection;

-- The vectors of a deleted concept are dropped the way SoftDelete drops
-- them, so a rejected concept cannot come back as a search hit through
-- the embedding index. The table exists only where semantic search is
-- configured (0010), so the guard is the same one 0013 uses.
DO $$
BEGIN
    IF to_regclass('knowledge_embedding') IS NOT NULL THEN
        DELETE FROM knowledge_embedding e
         WHERE EXISTS (SELECT 1 FROM object o
                        WHERE o.id = e.id AND o.deleted_at IS NOT NULL);
    END IF;
END
$$;
