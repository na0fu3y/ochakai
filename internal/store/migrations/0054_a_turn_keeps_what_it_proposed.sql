-- A turn keeps the revisions the deployment's own agent proposed to a
-- draft nobody has ruled on (design doc 0149).
--
-- The agent does not write them. The person who asked reads each one as
-- a diff and applies it, and the server writes what it kept here —
-- never a document the page sends — so a proposal applied is the one
-- the agent made. Each element is {id, document, base}: the concept, the
-- whole OKF document proposed, and the content hash it was proposed
-- against, which the write carries as its precondition.
ALTER TABLE agent_turn ADD COLUMN IF NOT EXISTS revisions jsonb NOT NULL DEFAULT '[]';
