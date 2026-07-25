-- Provenance gains the caller that acted on the actor's behalf (design
-- doc 0027). An application with its own service-account identity can
-- forward the identity of the person using it, and both are recorded:
-- the human is the actor, the application is the "via".
--
-- Recording only the human would make a delegated write indistinguishable
-- from one the human made with their own credentials — which is the
-- definition of a forgery, and would quietly destroy the thing ochakai
-- sells. Recording only the application is the status quo, where every
-- user of an embedding host collapses into one service account (design
-- doc 0002 §3 has said so about the sample web UI since v0.2).
--
-- Nullable and empty for every existing row: nothing acted on anyone's
-- behalf before this.
--
-- knowledge_event deliberately gets no such column. Its actor is
-- write-only telemetry pruned after 180 days — no surface reads it back,
-- so there is nothing for a "via" to inform.

ALTER TABLE knowledge ADD COLUMN IF NOT EXISTS created_by_via  text NOT NULL DEFAULT '';
ALTER TABLE knowledge ADD COLUMN IF NOT EXISTS verified_by_via text NOT NULL DEFAULT '';
ALTER TABLE knowledge ADD COLUMN IF NOT EXISTS rejected_by_via text NOT NULL DEFAULT '';

ALTER TABLE knowledge_revision ADD COLUMN IF NOT EXISTS changed_by_via text NOT NULL DEFAULT '';
