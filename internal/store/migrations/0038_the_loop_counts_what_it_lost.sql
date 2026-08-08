-- What the loop threw away, beside what it kept.
--
-- Usage is best-effort by decision (design doc 0029 §3.1): events buffer
-- in memory, the buffer is capped so a stalled database cannot grow it
-- without bound, and a flush that fails loses its batch rather than
-- spending memory on a retry queue. That decision also asked for the
-- loss to be visible — and it was, in a log line, which is the one place
-- nobody is looking while reading the numbers the loss damaged.
--
-- On Cloud Run the loss is not exotic: an instance is killed on
-- scale-to-zero and on every revision rollout, and whatever it was
-- holding goes with it. So a curator reading "this concept was fetched
-- 4 times" had no way to tell whether it was 4 or 40. The number stays
-- best-effort; what changes is that it stops being silently so.
--
-- Rows, not a counter, because every other flow number in the stats is
-- windowed by `since` and this one has to be comparable with them: a
-- thousand events lost last quarter says nothing about the month being
-- read. A row is written by the next flush that succeeds, which is how
-- the count outlives the instance that dropped it — and why a drop never
-- followed by a successful flush is itself lost. That last case is the
-- process dying between the two, and nothing short of the retry queue
-- 0029 refused would catch it.
--
-- Not scoped by prefix, for the reason misses are not (0051 §3.7): a
-- dropped batch is a number, and the ids that were in it are gone.

CREATE TABLE IF NOT EXISTS usage_drop (
    seq    bigserial   PRIMARY KEY,
    at     timestamptz NOT NULL,
    events bigint      NOT NULL,
    misses bigint      NOT NULL
);

CREATE INDEX IF NOT EXISTS usage_drop_at ON usage_drop (at DESC);
