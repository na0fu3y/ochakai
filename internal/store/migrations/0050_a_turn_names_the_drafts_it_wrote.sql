-- A turn names the drafts the deployment's own agent wrote in it
-- (design doc 0142 §3, §6).
--
-- 0142 §6 listed the drafts' ids among what a turn keeps, and the agent
-- wrote none until now. A verdict on the answer is still a report on
-- what it read; the drafts are listed so a reviewer can go from the
-- question to what it proposed, and back.
ALTER TABLE agent_turn ADD COLUMN IF NOT EXISTS drafts text[] NOT NULL DEFAULT '{}';
