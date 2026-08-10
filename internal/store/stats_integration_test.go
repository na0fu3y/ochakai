package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/testdb"
)

// A miss buffers off the read path like a usage event, and lands as its
// own row (design doc 0051 §3.2); the stats then read it back as a
// count and as the list of what to write next.
//
// The rest of the tally is asserted as a delta rather than as an
// absolute: every package shares one test database (see CONTRIBUTING), so
// this test owns the entries it creates and nothing else.
func TestIntegrationStatsAndMisses(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	// Registered rather than deferred, so it runs after the cleanups
	// below: a deferred Close takes the pool out from under them.
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	// A query nobody else asks, so the list assertion below is about this
	// run's misses and not a neighbour's.
	query := fmt.Sprintf("it-stats-%d の定義", time.Now().UnixNano())
	for _, table := range []string{"object", "knowledge_revision"} {
		if _, err := s.pool.Exec(ctx, `DELETE FROM `+table+` WHERE id LIKE 'it-stats-%'`); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		if _, err := s.pool.Exec(ctx, `DELETE FROM search_miss WHERE query = $1`, query); err != nil {
			t.Errorf("cleaning up misses: %v", err)
		}
	})

	since := time.Now().Add(-time.Hour)

	// Every assertion below is about rows this test owns, or is a bound
	// rather than an equality: the test database is shared and the other
	// packages run against it at the same time (see CONTRIBUTING), so a
	// tally of the whole instance moves under this test's feet.
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "tanaka"}
	unconfirmed := &domain.Knowledge{Type: domain.TypeMetrics, ID: "it-stats-draft", Title: "下書き",
		Status: domain.StatusDraft, CreatedBy: actor}
	confirmed := &domain.Knowledge{Type: domain.TypeMetrics, ID: "it-stats-stable", Title: "確定",
		Status: domain.StatusStable, CreatedBy: actor}
	for _, k := range []*domain.Knowledge{unconfirmed, confirmed} {
		if err := s.Create(ctx, k, false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Verify(ctx, confirmed.ID, actor); err != nil {
		t.Fatal(err)
	}
	// A second ruling three days back. It belongs in the trend's newest
	// bucket — seven days ending today — and telling that apart from a
	// bucket that merely starts today needs a ruling that today alone
	// would not contain.
	backdateVerification(t, ctx, s, confirmed.ID, time.Now().AddDate(0, 0, -3))

	// Three misses of one question, buffered: nothing is written until
	// the flush (design doc 0029's bargain, extended to misses).
	for range 3 {
		if err := s.RecordMiss(ctx, query, domain.MissKey(query), actor); err != nil {
			t.Fatalf("RecordMiss: %v", err)
		}
	}
	mid, err := s.Stats(ctx, since, nil)
	if err != nil {
		t.Fatal(err)
	}
	if q := missedQuery(mid, query); q != nil {
		t.Errorf("a miss was counted before the flush: %+v", *q)
	}
	if err := s.FlushUsage(ctx); err != nil {
		t.Fatal(err)
	}

	got, err := s.Stats(ctx, since, nil)
	if err != nil {
		t.Fatal(err)
	}
	// This test's two entries are in the tally, one per tier: the
	// verified one is human-reviewed (SPEC §5.3), the other is confirmed
	// by nobody.
	if got.Concepts.Total < 2 || got.Concepts.Created < 2 {
		t.Errorf("concepts = %+v, want at least the two this test created", got.Concepts)
	}
	if got.Concepts.Status[string(domain.StatusDraft)] < 1 ||
		got.Concepts.Trust[string(domain.TrustHuman)] < 1 ||
		got.Concepts.Trust[string(domain.TrustUnverified)] < 1 {
		t.Errorf("status = %v, trust = %v, want this test's two entries in them",
			got.Concepts.Status, got.Concepts.Trust)
	}
	if got.Review.Verifications < 1 {
		t.Errorf("review.verifications = %d, want at least the one this test recorded", got.Review.Verifications)
	}
	if got.Misses.Count < 3 {
		t.Errorf("misses.count = %d, want at least the three this test recorded", got.Misses.Count)
	}
	// The same flow as a shape (design doc 0095). Eight buckets always,
	// oldest first, with both of this test's verifications in the last of
	// them — a week nobody ruled in has to be a zero in the row rather
	// than a missing bucket, or the weeks either side are drawn as
	// neighbours.
	if n := len(got.Review.Weekly); n != domain.ReviewTrendWeeks {
		t.Fatalf("review.weekly has %d buckets, want %d", n, domain.ReviewTrendWeeks)
	}
	last := got.Review.Weekly[len(got.Review.Weekly)-1]
	if last.Verifications < 2 {
		t.Errorf("the newest bucket = %+v, want this test's two verifications (one now, one three days back) in it", last)
	}
	// The newest bucket is a full trailing week, so it opens six days
	// back — a bucket opening today could only ever hold today, the
	// always-partial shape 0095 rejects. Asked of the database rather
	// than computed here, so the expectation and the query read the same
	// clock and the same time zone.
	var wantFrom string
	if err := s.pool.QueryRow(ctx,
		`SELECT (date_trunc('day', now()) - interval '6 days')::date::text`).Scan(&wantFrom); err != nil {
		t.Fatal(err)
	}
	if last.From != wantFrom {
		t.Errorf("the newest bucket opens at %q, want %q (six days back, closing tomorrow)", last.From, wantFrom)
	}
	for i, w := range got.Review.Weekly {
		if w.From == "" {
			t.Errorf("bucket %d carries no date: %+v", i, w)
		}
		if i > 0 && w.From <= got.Review.Weekly[i-1].From {
			t.Errorf("buckets are not oldest-first: %q after %q", w.From, got.Review.Weekly[i-1].From)
		}
	}
	// The trend has its own window and does not follow the caller's: a
	// shape whose length changed with `days` would be two readings that
	// cannot be compared.
	narrow, err := s.Stats(ctx, time.Now().Add(-time.Minute), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(narrow.Review.Weekly) != domain.ReviewTrendWeeks {
		t.Errorf("a one-minute window changed the trend to %d buckets", len(narrow.Review.Weekly))
	}
	// Every value of both vocabularies is present, so a reader tells
	// "none" from "not reported" without knowing the vocabulary.
	for _, st := range domain.Statuses {
		if _, ok := got.Concepts.Status[string(st)]; !ok {
			t.Errorf("status tally has no %q", st)
		}
	}
	for _, tr := range domain.Trusts {
		if _, ok := got.Concepts.Trust[string(tr)]; !ok {
			t.Errorf("trust tally has no %q", tr)
		}
	}
	// Three of one query outrank the neighbours' singletons, and ties
	// break towards the most recent — so this run's question is on the
	// list even on a database full of older ones.
	found := missedQuery(got, query)
	if found == nil {
		t.Fatalf("the unanswered question is not in the list: %+v", got.Misses.Queries)
	}
	if found.Count != 3 || found.LastAt.IsZero() {
		t.Errorf("missed query = %+v, want count 3 and a time", *found)
	}

	// A window that starts after everything does not see any of it: the
	// flow numbers answer for the window they were asked about, and
	// nothing can be recorded in the future.
	empty, err := s.Stats(ctx, time.Now().Add(time.Hour), nil)
	if err != nil {
		t.Fatal(err)
	}
	if empty.Misses.Count != 0 || empty.Concepts.Created != 0 || empty.Review.Verifications != 0 {
		t.Errorf("a future window is not empty: %+v", empty)
	}
	// The state numbers are not windowed, and say so by staying put.
	if empty.Concepts.Total < 2 {
		t.Errorf("concepts.total = %d in a future window, want the state numbers unwindowed", empty.Concepts.Total)
	}

	for _, table := range []string{"object", "knowledge_revision"} {
		if _, err := s.pool.Exec(ctx, `DELETE FROM `+table+` WHERE id LIKE 'it-stats-%'`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM knowledge_verification WHERE id LIKE 'it-stats-%'`); err != nil {
		t.Fatal(err)
	}
}

// The queue depths the stats carry are design doc 0049's, counted with
// the feeds' own predicates — so a depth cannot come to mean something
// other than the feed a reviewer then opens (design doc 0051 §3.5).
func TestIntegrationStatsQueuesMatchTheFeeds(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	// Registered rather than deferred, so it runs after the cleanups
	// below: a deferred Close takes the pool out from under them.
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "tanaka"}
	failing := &domain.Knowledge{Type: domain.TypeComputations, ID: "it-stats-q-failed", Title: "壊れている",
		Status: domain.StatusStable, CreatedBy: actor}
	expired := &domain.Knowledge{Type: domain.TypeComputations, ID: "it-stats-q-expired", Title: "期限切れ",
		Status: domain.StatusStable, CreatedBy: actor, StaleAfter: "2020-01-01"}
	for _, table := range []string{"object", "knowledge_revision"} { // leftovers from an interrupted run
		if _, err := s.pool.Exec(ctx, `DELETE FROM `+table+` WHERE id LIKE 'it-stats-q-%'`); err != nil {
			t.Fatal(err)
		}
	}
	for _, k := range []*domain.Knowledge{failing, expired} {
		if err := s.Create(ctx, k, false); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, table := range []string{"object", "knowledge_revision"} {
			if _, err := s.pool.Exec(ctx, `DELETE FROM `+table+` WHERE id LIKE 'it-stats-q-%'`); err != nil {
				t.Errorf("cleaning up: %v", err)
			}
		}
		if _, err := s.pool.Exec(ctx, `DELETE FROM knowledge_usage WHERE knowledge_id LIKE 'it-stats-q-%'`); err != nil {
			t.Errorf("cleaning up: %v", err)
		}
	})
	if err := s.RecordOutcome(ctx, domain.EventFailed, actor, failing.ID, "wrong number"); err != nil {
		t.Fatal(err)
	}

	// The feeds are measured on both sides of the stats call, and the
	// depth has to land between them: the test database is shared, so a
	// neighbour may join or leave a queue in the meantime, and only a
	// predicate that disagrees with the feed's can fall outside.
	failedBefore, err := s.ListByFailed(ctx, Filter{}, nil, 1000)
	if err != nil {
		t.Fatal(err)
	}
	staleBefore, err := s.ListByStaleAfter(ctx, Filter{}, nil, 1000)
	if err != nil {
		t.Fatal(err)
	}
	st, err := s.Stats(ctx, time.Now().Add(-time.Hour), nil)
	if err != nil {
		t.Fatal(err)
	}
	failedAfter, err := s.ListByFailed(ctx, Filter{}, nil, 1000)
	if err != nil {
		t.Fatal(err)
	}
	staleAfter, err := s.ListByStaleAfter(ctx, Filter{}, nil, 1000)
	if err != nil {
		t.Fatal(err)
	}
	between := func(t *testing.T, name string, got int64, a, b int) {
		t.Helper()
		lo, hi := min(a, b), max(a, b)
		if got < int64(lo) || got > int64(hi) {
			t.Errorf("%s = %d, the feed held %d then %d", name, got, a, b)
		}
	}
	between(t, "queues.failed", st.Queues.Failed, len(failedBefore), len(failedAfter))
	between(t, "queues.stale_after", st.Queues.StaleAfter, len(staleBefore), len(staleAfter))
	if st.Queues.Failed == 0 || st.Queues.StaleAfter == 0 {
		t.Errorf("the entries this test just made are in neither queue: %+v", st.Queues)
	}
}

// missedQuery finds one question in a stats blob, or nil.
func missedQuery(st *domain.Stats, query string) *domain.MissedQuery {
	for i := range st.Misses.Queries {
		if st.Misses.Queries[i].Query == query {
			return &st.Misses.Queries[i]
		}
	}
	return nil
}
