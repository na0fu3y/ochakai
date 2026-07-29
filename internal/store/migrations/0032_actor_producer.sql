-- Every actor gains the software that made the write (design doc 0052).
--
-- SPEC §7's third actor form, "<producer>/<version>", names software
-- where the other two name an identity. ochakai never records it *as* the
-- actor — a write here always has an authenticated caller behind it, and
-- a self-declared string put in that slot would make "who wrote this"
-- answerable by anyone (0009). It is recorded beside the actor instead,
-- which is the same composition 0027 chose for delegation: the claim is
-- kept, and the name that vouches for it is never dropped.
--
-- One column per actor, wherever that actor already has a via column, and
-- for the same reason it has one — these are the actors some surface
-- reads back. NOT NULL DEFAULT '' because a caller that named no producer
-- and one that named an empty producer are the same state, so a NULL
-- would draw a distinction nothing can act on.
--
-- knowledge_event is left alone, exactly as 0027 §5.3 left it: its actor
-- is write-only telemetry, pruned at 180 days and read back by no
-- surface. A column nothing reads records nothing.
--
-- attachment is left alone too. Its read paths moved to object in this
-- release and the table is waiting to be dropped (migration 0029) — a
-- column added to a table on its way out would only have to be dropped
-- with it.
--
-- Nothing is backfilled. There is no value to backfill *to*: no past
-- write declared a producer, and stamping one now would invent an
-- observation the server never made. An entry written before this
-- migration says nothing about its producer, which is the truth.
--
-- No timestamp moves. This adds a place to record something, not a change
-- to any entry's content: no held ETag is invalidated, no generated.at is
-- misdated, and nothing needs re-embedding.

ALTER TABLE object ADD COLUMN IF NOT EXISTS created_by_producer text NOT NULL DEFAULT '';
ALTER TABLE object ADD COLUMN IF NOT EXISTS updated_by_producer text NOT NULL DEFAULT '';

ALTER TABLE knowledge_verification ADD COLUMN IF NOT EXISTS by_producer text NOT NULL DEFAULT '';
ALTER TABLE knowledge_rejection    ADD COLUMN IF NOT EXISTS by_producer text NOT NULL DEFAULT '';

ALTER TABLE knowledge_revision ADD COLUMN IF NOT EXISTS changed_by_producer text NOT NULL DEFAULT '';

ALTER TABLE knowledge_purge ADD COLUMN IF NOT EXISTS purged_by_producer text NOT NULL DEFAULT '';
