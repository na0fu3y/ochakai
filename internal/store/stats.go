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
		Entries: domain.StatsEntries{
			Status: zeroed(domain.Statuses),
			Trust:  zeroed(domain.Trusts),
		},
	}
	if err := s.statsEntries(ctx, st, since, prefixes); err != nil {
		return nil, err
	}
	if err := s.statsReview(ctx, st, since, prefixes); err != nil {
		return nil, err
	}
	if err := s.statsOutcomes(ctx, st, since, prefixes); err != nil {
		return nil, err
	}
	if err := s.statsMisses(ctx, st, since); err != nil {
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

// statsEntries tallies the live entries by lifecycle and trust tier in one
// pass, and counts the rejections and the new arrivals beside them.
//
// The two tiers are derived exactly as the trust filter derives them
// (buildWhere) and as SPEC §5.3 defines them: a person makes it
// human-reviewed, any other confirmation machine-confirmed, none
// unverified.
func (s *Store) statsEntries(ctx context.Context, st *domain.Stats, since time.Time, prefixes []string) error {
	scope, scopeArgs := statsScope("k.id", prefixes)
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
		WHERE k.deleted_at IS NULL AND k.id IS NOT NULL%s
		GROUP BY 1, 2, 3`,
		domain.ActorHuman, domain.TrustHuman, domain.TrustMachine, domain.TrustUnverified, scope),
		append([]any{since}, scopeArgs...)...)
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
