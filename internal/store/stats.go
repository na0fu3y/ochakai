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

// Stats computes the instance-level view of the loop (design doc 0049
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
func (s *Store) Stats(ctx context.Context, since time.Time) (*domain.Stats, error) {
	st := &domain.Stats{
		Entries: domain.StatsEntries{
			Status: zeroed(domain.Statuses),
			Trust:  zeroed(domain.Trusts),
		},
	}
	if err := s.statsEntries(ctx, st, since); err != nil {
		return nil, err
	}
	if err := s.statsReview(ctx, st, since); err != nil {
		return nil, err
	}
	if err := s.statsOutcomes(ctx, st, since); err != nil {
		return nil, err
	}
	if err := s.statsMisses(ctx, st, since); err != nil {
		return nil, err
	}
	return st, nil
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

// statsEntries tallies the live entries by lifecycle and trust tier in one
// pass, and counts the rejections and the new arrivals beside them.
//
// The two tiers are derived exactly as the trust filter derives them
// (buildWhere) and as SPEC §5.3 defines them: a person makes it
// human-reviewed, any other confirmation machine-confirmed, none
// unverified.
func (s *Store) statsEntries(ctx context.Context, st *domain.Stats, since time.Time) error {
	// The tiers are spelled from the domain vocabulary rather than
	// written out here, so the tally cannot answer in a vocabulary the
	// filters have moved on from.
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		SELECT k.status,
			CASE
				WHEN EXISTS (SELECT 1 FROM knowledge_verification v
					WHERE v.id = k.id AND v.by_kind = '%s') THEN '%s'
				WHEN EXISTS (SELECT 1 FROM knowledge_verification v
					WHERE v.id = k.id) THEN '%s'
				ELSE '%s'
			END AS trust,
			EXISTS (SELECT 1 FROM knowledge_rejection r WHERE r.id = k.id) AS rejected,
			count(*) FILTER (WHERE k.created_at >= $1) AS created,
			count(*) AS n
		FROM object k
		WHERE k.deleted_at IS NULL AND k.id IS NOT NULL
		GROUP BY 1, 2, 3`,
		domain.ActorHuman, domain.TrustHuman, domain.TrustMachine, domain.TrustUnverified), since)
	if err != nil {
		return fmt.Errorf("stats: entries: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var status, trust string
		var rejected bool
		var created, n int64
		if err := rows.Scan(&status, &trust, &rejected, &created, &n); err != nil {
			return fmt.Errorf("stats: entries: %w", err)
		}
		// An entry whose document names no status reads as OKF's default
		// (design doc 0046 §3.9), which is what every other surface shows
		// — the tally has to agree with them.
		st.Entries.Status[string(domain.LifecycleOf(domain.Status(status)))] += n
		st.Entries.Trust[trust] += n
		st.Entries.Total += n
		st.Entries.Created += created
		if rejected {
			st.Entries.Rejected += n
		}
	}
	return rows.Err()
}

// statsReview counts what review did in the window and what is waiting
// for it now. The two queues are counted with the feeds' own predicates
// (unansweredFailure, pastExpiry), so a depth reported here cannot mean
// something different from the feed a reviewer then opens.
func (s *Store) statsReview(ctx context.Context, st *domain.Stats, since time.Time) error {
	// Verifications of entries that still exist: the ledger keeps rows
	// for purged ids, and a count including them would say review
	// happened on a base that no longer has it.
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM knowledge_verification v
		WHERE v.at >= $1
			AND EXISTS (SELECT 1 FROM object k WHERE k.id = v.id AND k.deleted_at IS NULL)`,
		since).Scan(&st.Review.Verifications); err != nil {
		return fmt.Errorf("stats: verifications: %w", err)
	}

	// Both queues are counted through the default filter — live, not
	// rejected — which is the population the feeds themselves list.
	where, args := Filter{}.buildWhere("k.")
	if err := s.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT count(*) FROM object k`+usageLateral+`
		WHERE %s AND `+unansweredFailure("k"), where), args...).Scan(&st.Review.NeedsReview); err != nil {
		return fmt.Errorf("stats: needs_review: %w", err)
	}

	where, args = Filter{}.buildWhere("")
	if err := s.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT count(*) FROM object WHERE %s AND `+pastExpiry(""), where),
		args...).Scan(&st.Review.PastExpiry); err != nil {
		return fmt.Errorf("stats: past_expiry: %w", err)
	}
	return nil
}

// statsOutcomes counts the outcome reports in the window from the raw
// events — the running totals in knowledge_usage cannot answer a window,
// only a lifetime.
//
// Reports about entries that have since been deleted are counted: the
// report happened, and the question here is how much evidence came back,
// not what it is attached to today.
func (s *Store) statsOutcomes(ctx context.Context, st *domain.Stats, since time.Time) error {
	rows, err := s.pool.Query(ctx, `
		SELECT event, count(*) FROM knowledge_event
		WHERE at >= $1 AND event = ANY($2) GROUP BY 1`,
		since, []string{domain.EventWorked, domain.EventFailed})
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
	rows, err := s.pool.Query(ctx, `
		SELECT query, count(*) AS n, max(at) AS last_at FROM search_miss
		WHERE at >= $1 GROUP BY query
		ORDER BY n DESC, last_at DESC, query LIMIT $2`, since, missQueryLimit)
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
