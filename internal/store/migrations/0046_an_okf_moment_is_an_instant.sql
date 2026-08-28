-- stale_after is an instant, not a day (design doc 0133).
--
-- Migration 0019 added this column as `date` and said why: "A date, not a
-- timestamp: staleness is `today >= stale_after`, and neither side of
-- that comparison should depend on a timezone." The first half of that is
-- not what SPEC says. §5 opens with "Every timestamp-valued key in OKF is
-- an ISO 8601 datetime with an explicit UTC offset" and §5.5 calls
-- stale_after "an absolute instant" — the spec's own worked example
-- (Appendix A) writes `stale_after: 2026-12-31T00:00:00Z`, and a `date`
-- column is the reason a writer's time of day was dropped on the way in.
--
-- The second half survives, and this column is how. timestamptz is an
-- absolute instant: pgx sends a Go time.Time with its offset and now()
-- returns one, so `stale_after <= now()` is the same comparison on every
-- deployment whatever TimeZone the session carries. It is the `date`
-- column that had to spell UTC out by hand (pastExpiry), because a bare
-- date has no offset to compare against.
--
-- The existing values are days, and a day is the UTC midnight that opens
-- it — the same reading the parser has always given a bare `2026-12-31`.
-- ::timestamp then AT TIME ZONE 'UTC' is that in SQL, and it is spelled
-- in two steps for the reason above: a direct ::timestamptz cast would
-- resolve the midnight in the session's own timezone.
--
-- ALTER COLUMN TYPE rebuilds the dependent index (knowledge_stale_after,
-- migration 0021) on its own, so the partial index over this column
-- carries over without being named here. The table is `object` since
-- migration 0028 renamed it; the index kept its older name.

ALTER TABLE object
    ALTER COLUMN stale_after TYPE timestamptz
    USING stale_after::timestamp AT TIME ZONE 'UTC';
