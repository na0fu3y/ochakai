-- Feedback-loop hardening (design doc 0001 §9): rejected status with
-- rejection provenance, a free-form status note, and knowledge usage
-- recording (raw events pruned after 180 days; aggregates kept forever).

ALTER TABLE knowledge DROP CONSTRAINT knowledge_status_check;
ALTER TABLE knowledge ADD CONSTRAINT knowledge_status_check
    CHECK (status IN ('draft', 'verified', 'deprecated', 'rejected'));

ALTER TABLE knowledge
    ADD COLUMN rejected_by_kind text,
    ADD COLUMN rejected_by_name text,
    ADD COLUMN rejected_at      timestamptz,
    ADD COLUMN status_note      text NOT NULL DEFAULT '';

-- Raw usage events: who used which knowledge, when. Append-only;
-- pruned after 180 days (totals survive in knowledge_usage).
CREATE TABLE IF NOT EXISTS knowledge_event (
    knowledge_type text        NOT NULL,
    knowledge_id   text        NOT NULL,
    event          text        NOT NULL,
    actor_kind     text        NOT NULL,
    actor_name     text        NOT NULL,
    at             timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT knowledge_event_check CHECK (event IN ('search_hit', 'fetched', 'compiled'))
);

CREATE INDEX IF NOT EXISTS knowledge_event_target ON knowledge_event (knowledge_type, knowledge_id, at);
CREATE INDEX IF NOT EXISTS knowledge_event_at ON knowledge_event (at);

-- Running totals per entry and event, updated on every recording; the
-- source for GET /api/v1/knowledge/{type}/{id}/usage.
CREATE TABLE IF NOT EXISTS knowledge_usage (
    knowledge_type text        NOT NULL,
    knowledge_id   text        NOT NULL,
    event          text        NOT NULL,
    count          bigint      NOT NULL DEFAULT 0,
    last_at        timestamptz NOT NULL,
    PRIMARY KEY (knowledge_type, knowledge_id, event)
);
