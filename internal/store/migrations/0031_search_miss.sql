-- A question nobody could answer is a fact worth keeping
-- (design doc 0049 §3.1).
--
-- Every other measurement in this database hangs off a knowledge id:
-- knowledge_event and knowledge_usage both key on the entry that was
-- searched, fetched or reported on. A search that returned nothing has
-- no entry to hang off, which is exactly why it was being discarded —
-- and it is the one observation that says what to write next.
--
-- So the miss gets a table of its own, keyed by nothing. It carries the
-- query as it was asked, the caller who asked it, and when — the same
-- provenance any usage event carries (design doc 0027 applies here as it
-- does there), pruned on the same 180-day schedule as the raw events
-- (0029 §3.4), because a question from six months ago is not a gap
-- anybody is still waiting on.
--
-- No foreign key and nothing to join to: this table is about the absence
-- of a row elsewhere.

CREATE TABLE IF NOT EXISTS search_miss (
    seq        bigserial   PRIMARY KEY,
    query      text        NOT NULL,
    actor_kind text        NOT NULL,
    actor_name text        NOT NULL,
    at         timestamptz NOT NULL
);

-- Both reads are windowed: the count of misses since a moment, and the
-- most-asked queries since the same moment.
CREATE INDEX IF NOT EXISTS search_miss_at ON search_miss (at DESC);
