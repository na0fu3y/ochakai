package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// missQueryLimit is how many distinct unanswered questions the stats
// carry. The list is meant to be read by a person deciding what to write
// next, and a page of it is more than anybody acts on in one sitting; the
// count beside it says how much is behind the list.
const missQueryLimit = 10

// Stats computes the instance-level view of the loop (design doc 0051
// §3.5). since bounds every flow number — what was created, verified,
// reported and missed after it — while the state numbers describe the
// base as it stands.
//
// It is computed on demand, with no rollup table behind it: at the scale
// a curated knowledge base reaches, each of these is a count over an
// indexed column or a scan of the entries, and a rollup would be a second
// copy of numbers the tables already hold — one that can be wrong
// (§3.6). What that costs is that nothing answers for the time before the
// raw events were pruned; the window the service accepts is capped at the
// retention for exactly that reason.
//
// prefixes scopes the answer to one or more subtrees, exactly as it
// scopes a search (design doc 0041): a team on a shared deployment asks
// about its own knowledge. Everything keyed by an entry honors it —
// what the base is made of, what review did, what came back, what is
// waiting. Misses do not, and cannot: a search that found nothing found
// it nowhere, so there is no id to scope by and the questions this base
// could not answer stay the instance's (design doc 0051 §3.7).
func (s *Store) Stats(ctx context.Context, since time.Time, prefixes []string) (*domain.Stats, error) {
	st := &domain.Stats{
		Concepts: domain.StatsConcepts{
			Status: zeroed(domain.Statuses),
			Trust:  zeroed(domain.Trusts),
		},
	}
	if err := s.statsConcepts(ctx, st, since, prefixes); err != nil {
		return nil, err
	}
	if err := s.statsReview(ctx, st, since, prefixes); err != nil {
		return nil, err
	}
	if err := s.statsOutcomes(ctx, st, since, prefixes); err != nil {
		return nil, err
	}
	if err := s.statsCoverage(ctx, st, since, prefixes); err != nil {
		return nil, err
	}
	if err := s.statsMisses(ctx, st, since); err != nil {
		return nil, err
	}
	if err := s.statsDropped(ctx, st, since); err != nil {
		return nil, err
	}
	return st, nil
}

// statsScope is the subtree condition ready to splice into a WHERE, with
// the argument that goes with it. Every stats query takes `since` as $1,
// so the prefixes are always $2 and the clause is written into the query
// rather than appended — each of these ends in a GROUP BY or an ORDER BY
// that has to stay last.
func statsScope(column string, prefixes []string) (string, []any) {
	cond, args := prefixScope(column, prefixes, 2)
	if cond == "" {
		return "", nil
	}
	return " AND " + cond, args
}

// zeroed builds the tally for a vocabulary with every value present at 0,
// so a caller reads "none" rather than having to know the vocabulary to
// notice a missing key.
func zeroed[T ~string](values []T) map[string]int64 {
	m := make(map[string]int64, len(values))
	for _, v := range values {
		m[string(v)] = 0
	}
	return m
}

// statsConcepts tallies the live concepts by lifecycle and trust tier in
// one pass, and counts the new arrivals beside them.
//
// The two tiers are derived exactly as the trust filter derives them
// (buildWhere, through standingVerification) and as SPEC §5.3 defines
// them over the verifications that stand (design doc 0138): a person
// makes it human-reviewed, any other confirmation machine-confirmed,
// none — or none standing — unverified.
func (s *Store) statsConcepts(ctx context.Context, st *domain.Stats, since time.Time, prefixes []string) error {
	scope, scopeArgs := statsScope("k.id", prefixes)
	// The tiers are spelled from the domain vocabulary rather than
	// written out here, so the tally cannot answer in a vocabulary the
	// filters have moved on from.
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		SELECT k.status,
			CASE
				WHEN %s THEN '%s'
				WHEN %s THEN '%s'
				ELSE '%s'
			END AS trust,
			count(*) FILTER (WHERE k.created_at >= $1) AS created,
			count(*) AS n
		FROM object k
		WHERE k.deleted_at IS NULL AND k.id IS NOT NULL%s
		GROUP BY 1, 2`,
		standingVerification("k.", domain.ActorHuman), domain.TrustHuman,
		standingVerification("k.", ""), domain.TrustMachine, domain.TrustUnverified, scope),
		append([]any{since}, scopeArgs...)...)
	if err != nil {
		return fmt.Errorf("stats: concepts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var status, trust string
		var created, n int64
		if err := rows.Scan(&status, &trust, &created, &n); err != nil {
			return fmt.Errorf("stats: concepts: %w", err)
		}
		// A concept whose document names no status reads as OKF's default
		// (design doc 0046 §3.9), which is what every other surface shows
		// — the tally has to agree with them.
		st.Concepts.Status[string(domain.LifecycleOf(domain.Status(status)))] += n
		st.Concepts.Trust[trust] += n
		st.Concepts.Total += n
		st.Concepts.Created += created
	}
	return rows.Err()
}

// statsReview counts what review did in the window, and asks
// QueueCounts what is waiting for it now — the queues are design doc
// 0049's face and its query, borrowed rather than recomputed here. Two
// counts of one queue are two chances to disagree about it.
func (s *Store) statsReview(ctx context.Context, st *domain.Stats, since time.Time, prefixes []string) error {
	scope, scopeArgs := statsScope("v.id", prefixes)
	// Verifications of entries that still exist: the ledger keeps rows
	// for purged ids, and a count including them would say review
	// happened on a base that no longer has it.
	if err := s.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT count(*) FROM knowledge_verification v
		WHERE v.at >= $1
			AND EXISTS (SELECT 1 FROM object k WHERE k.id = v.id AND k.deleted_at IS NULL)%s`, scope),
		append([]any{since}, scopeArgs...)...).Scan(&st.Review.Verifications); err != nil {
		return fmt.Errorf("stats: verifications: %w", err)
	}

	// The same flow, bucketed. generate_series makes the buckets rather
	// than the rows: a week nobody verified anything has to be a zero in
	// the shape, and grouping the ledger alone would leave it out and
	// draw the weeks either side as neighbours.
	//
	// The buckets end today rather than starting there: the newest one
	// opens six days back and closes tomorrow at midnight, so it holds a
	// full trailing week — today included. Anchoring the series on today
	// would put the newest bucket ahead of the clock, able to hold only
	// today's rulings, which is exactly the always-partial bucket the
	// design counts back from today to avoid (design doc 0095).
	//
	// Bounded by the trend's own window, not by the caller's: the shape
	// is a fixed eight weeks so that two readings of it compare, while
	// `since` is whatever period the caller asked the rest of these
	// numbers about.
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		SELECT w::date::text, count(v.id)
		  FROM generate_series(
		         date_trunc('day', now()) - make_interval(days => 7 * $1::int - 1),
		         date_trunc('day', now()) - make_interval(days => 6),
		         '7 days') AS w
		  LEFT JOIN knowledge_verification v
		    ON v.at >= w AND v.at < w + interval '7 days'
		   AND EXISTS (SELECT 1 FROM object k WHERE k.id = v.id AND k.deleted_at IS NULL)%s
		 GROUP BY w ORDER BY w`, scope),
		append([]any{domain.ReviewTrendWeeks}, scopeArgs...)...)
	if err != nil {
		return fmt.Errorf("stats: review trend: %w", err)
	}
	weekly, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.ReviewWeek, error) {
		var w domain.ReviewWeek
		return w, row.Scan(&w.From, &w.Verifications)
	})
	if err != nil {
		return fmt.Errorf("stats: review trend: %w", err)
	}
	// A base nobody has ruled on yet gets no shape rather than eight
	// zeroes: a flat line reads as "review stopped", and "review has not
	// started" is a different thing to tell somebody.
	for _, w := range weekly {
		if w.Verifications > 0 {
			st.Review.Weekly = weekly
			break
		}
	}

	// The queue depths are design doc 0049's answer, borrowed rather than
	// recomputed: two counts of one queue are two chances to disagree
	// about it. They are scoped like everything else here now that this
	// is the only face that carries them (design doc 0049 §3.1).
	queues, err := s.QueueCounts(ctx, Filter{Prefixes: prefixes})
	if err != nil {
		return fmt.Errorf("stats: queues: %w", err)
	}
	st.Queues = queues
	return nil
}

// statsOutcomes counts the outcome reports in the window from the raw
// events — the running totals in knowledge_usage cannot answer a window,
// only a lifetime.
//
// Reports about entries that have since been deleted are counted: the
// report happened, and the question here is how much evidence came back,
// not what it is attached to today. That is why the subtree scope reads
// the event's own id rather than joining the entries — a join would
// quietly drop the deleted ones the moment a prefix was given, making the
// scoped answer mean something the unscoped one does not.
func (s *Store) statsOutcomes(ctx context.Context, st *domain.Stats, since time.Time, prefixes []string) error {
	// The events are $1/$2 here, so the scope takes $3 rather than the
	// $2 statsScope assumes.
	scope, scopeArgs := prefixScope("knowledge_id", prefixes, 3)
	if scope != "" {
		scope = " AND " + scope
	}
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		SELECT event, count(*) FROM knowledge_event
		WHERE at >= $1 AND event = ANY($2)%s GROUP BY 1`, scope),
		append([]any{since, []string{domain.EventWorked, domain.EventFailed}}, scopeArgs...)...)
	if err != nil {
		return fmt.Errorf("stats: outcomes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var event string
		var n int64
		if err := rows.Scan(&event, &n); err != nil {
			return fmt.Errorf("stats: outcomes: %w", err)
		}
		switch event {
		case domain.EventWorked:
			st.Outcomes.Worked = n
		case domain.EventFailed:
			st.Outcomes.Failed = n
		}
	}
	return rows.Err()
}

// statsCoverage counts the concepts the window handed over and how many
// of them anybody reported on — the coverage of report_outcome.
//
// Both sides are windowed, and the reported set is intersected with the
// used one rather than counted on its own: a report about a concept
// nobody read in this window says nothing about whether the concepts
// that *were* read are coming back with evidence, which is the question.
//
// "Handed over" is the fetched event, not the search hit. Appearing in a
// ranking is not use — it is the ranker's opinion, and counting it would
// make the coverage look better the more results a search returns.
func (s *Store) statsCoverage(ctx context.Context, st *domain.Stats, since time.Time, prefixes []string) error {
	scope, scopeArgs := prefixScope("knowledge_id", prefixes, 4)
	if scope != "" {
		scope = " AND " + scope
	}
	args := append([]any{since, domain.EventFetched,
		[]string{domain.EventWorked, domain.EventFailed}}, scopeArgs...)
	if err := s.pool.QueryRow(ctx, fmt.Sprintf(`
		WITH used AS (
			SELECT DISTINCT knowledge_id FROM knowledge_event
			WHERE at >= $1 AND event = $2%[1]s
		), reported AS (
			SELECT DISTINCT knowledge_id FROM knowledge_event
			WHERE at >= $1 AND event = ANY($3)%[1]s
		)
		SELECT (SELECT count(*) FROM used),
			(SELECT count(*) FROM used JOIN reported USING (knowledge_id))`, scope), args...).
		Scan(&st.Outcomes.ConceptsUsed, &st.Outcomes.ConceptsReported); err != nil {
		return fmt.Errorf("stats: outcome coverage: %w", err)
	}
	return nil
}

// statsMisses counts the searches that found nothing in the window and
// lists the most-asked of them. Grouping is on the query as it was typed:
// folding case or whitespace would make the list read like something the
// server understood, and it understands nothing about these strings — it
// only knows it had no answer.
func (s *Store) statsMisses(ctx context.Context, st *domain.Stats, since time.Time) error {
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM search_miss WHERE at >= $1`, since).Scan(&st.Misses.Count); err != nil {
		return fmt.Errorf("stats: misses: %w", err)
	}
	// Grouped by what the askings meant, shown in the words of the most
	// recent one (migration 0037). DISTINCT ON picks that asking; the
	// counts and the last time are the whole group's.
	rows, err := s.pool.Query(ctx, `
		SELECT query, n, last_at FROM (
			SELECT DISTINCT ON (normalized)
				normalized,
				first_value(query) OVER (PARTITION BY normalized ORDER BY at DESC, seq DESC) AS query,
				count(*) OVER (PARTITION BY normalized) AS n,
				max(at) OVER (PARTITION BY normalized) AS last_at
			FROM search_miss WHERE at >= $1
		) g ORDER BY n DESC, last_at DESC, query LIMIT $2`, since, missQueryLimit)
	if err != nil {
		return fmt.Errorf("stats: missed queries: %w", err)
	}
	st.Misses.Queries, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.MissedQuery, error) {
		var q domain.MissedQuery
		err := row.Scan(&q.Query, &q.Count, &q.LastAt)
		return q, err
	})
	if err != nil {
		return fmt.Errorf("stats: missed queries: %w", err)
	}
	return nil
}

// statsDropped reads what the loop lost in the window (migration 0038).
// The rows are written by whichever instance recovered from the loss, so
// this is the deployment's gap rather than one process's.
func (s *Store) statsDropped(ctx context.Context, st *domain.Stats, since time.Time) error {
	if err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(sum(events), 0), COALESCE(sum(misses), 0)
		FROM usage_drop WHERE at >= $1`, since).
		Scan(&st.Dropped.Events, &st.Dropped.Misses); err != nil {
		return fmt.Errorf("stats: dropped observations: %w", err)
	}
	return nil
}
