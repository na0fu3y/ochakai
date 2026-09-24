-- A turn says which agent answered it (design doc 0144 §3).
--
-- Turns used to come from the deployment's own agent only. Now any agent
-- that read the base to answer can keep one, so the comparison set holds
-- two kinds of answer, and a comparison that cannot tell them apart is
-- not one. The turn carries the recording call's via and producer, the
-- way the ledger does for a write.
--
-- Existing turns keep both empty. Every one of them was the deployment's
-- own agent's, but writing that in now would be an inference, not an
-- observation.
ALTER TABLE agent_turn ADD COLUMN IF NOT EXISTS actor_via text NOT NULL DEFAULT '';
ALTER TABLE agent_turn ADD COLUMN IF NOT EXISTS producer text NOT NULL DEFAULT '';
