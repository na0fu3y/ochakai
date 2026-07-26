-- Indexes for the two things design doc 0037 makes queryable: the expiry
-- a writer declared, and the material an entry cites.
--
-- Design doc 0036 §3.13 put sources in a column and deliberately left it
-- unindexed — nothing queried by source, and an index with no query is
-- cost without benefit. 0037 adds the query, so the index lands with it.
--
-- Neither statement touches a row. No column is added, no value is
-- rewritten, and updated_at does not move — so no held ETag is
-- invalidated and no entry's generated.at is misdated.

-- Containment over the sources array: sources @> '[{"resource": "..."}]'.
-- The default jsonb_ops operator class is what @> uses; jsonb_path_ops
-- would be smaller but only supports @>, and this column is young enough
-- that the extra generality is worth more than the bytes.
CREATE INDEX IF NOT EXISTS knowledge_sources ON knowledge USING gin (sources);

-- The stale feed reads "stale_after IS NOT NULL AND stale_after <=
-- current_date", ordered by stale_after. Partial on the two conditions
-- that never vary, so entries that declare no expiry — expected to be
-- most of them — stay out of the index entirely.
CREATE INDEX IF NOT EXISTS knowledge_stale_after ON knowledge (stale_after)
    WHERE deleted_at IS NULL AND stale_after IS NOT NULL;
