-- Outcome feedback (design doc 0001 §9.4): callers report whether used
-- knowledge actually worked. Two new event kinds close the write-back
-- loop's last edge, and events gain a free-form note ("what was run,
-- what went wrong") — recorded with the raw event, pruned with it.

ALTER TABLE knowledge_event DROP CONSTRAINT knowledge_event_check;
ALTER TABLE knowledge_event ADD CONSTRAINT knowledge_event_check
    CHECK (event IN ('search_hit', 'fetched', 'compiled', 'worked', 'failed'));

ALTER TABLE knowledge_event ADD COLUMN note text NOT NULL DEFAULT '';
