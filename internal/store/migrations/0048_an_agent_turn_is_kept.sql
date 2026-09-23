-- The deployment's own agent keeps the shape of each turn, not its
-- contents (design doc 0142 §6).
--
-- A turn is what the loop learns from: what was asked, which concepts
-- were read to answer it, what query was proposed, and what the person
-- who asked said about the answer. The answer's text is not kept and a
-- query's result never reaches the server. Like a raw usage event, a
-- turn is measurement rather than knowledge: it is not exported, and it
-- is pruned after 180 days on the same schedule.
CREATE TABLE IF NOT EXISTS agent_turn (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    at           timestamptz NOT NULL DEFAULT now(),
    actor_kind   text NOT NULL,
    actor_name   text NOT NULL,
    -- The conversation's first question, and the message this turn
    -- answered. They differ once a conversation goes on, and the first is
    -- the one a comparison set wants.
    asked        text NOT NULL,
    latest       text NOT NULL,
    read_ids     text[] NOT NULL DEFAULT '{}',
    proposed_sql text NOT NULL DEFAULT '',
    -- The person's judgment of the answer: '' until they give one, then
    -- good or bad, once.
    verdict      text NOT NULL DEFAULT '' CHECK (verdict IN ('', 'good', 'bad')),
    note         text NOT NULL DEFAULT '',
    -- The concepts the person said misled the answer; each got a failed
    -- report. A subset of read_ids.
    blamed       text[] NOT NULL DEFAULT '{}',
    -- The person chose this question for the comparison set.
    keep         boolean NOT NULL DEFAULT false,
    judged_at    timestamptz
);

CREATE INDEX IF NOT EXISTS agent_turn_at ON agent_turn (at);
CREATE INDEX IF NOT EXISTS agent_turn_judged ON agent_turn (verdict, at) WHERE verdict <> '';
