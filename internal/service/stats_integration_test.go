package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/na0fu3y/ochakai/internal/config"
	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/store"
	"github.com/na0fu3y/ochakai/internal/testdb"
)

// A search that finds nothing is recorded as a question this base could
// not answer (design doc 0051 §3.1) — through the service, so every
// surface is covered by the one place they all search through. A search
// that finds something is not: only the absence is news.
func TestSearchMissesAreRecordedIntegration(t *testing.T) {
	dbURL := os.Getenv("OCHAKAI_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("OCHAKAI_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	s, err := store.New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	svc := &Service{Store: s, Log: slog.New(slog.DiscardHandler)}
	// Nothing in the base is like it — not even by trigram, which is
	// what a shared test database makes easy to get wrong.
	nonsense := testdb.Unique(t, "qqzzxx")
	t.Cleanup(func() { deleteMisses(ctx, t, dbURL, nonsense) })

	since := time.Now().Add(-time.Hour)
	before, err := s.Stats(ctx, since, nil)
	if err != nil {
		t.Fatal(err)
	}

	if hits, err := svc.Search(ctx, nonsense, store.Filter{}, 10); err != nil {
		t.Fatal(err)
	} else if len(hits) != 0 {
		t.Fatalf("the nonsense query matched %d entries", len(hits))
	}
	// get_context searches through the same function, so asking it the
	// same way is the same miss rather than a second path to maintain.
	if _, err := svc.Context(ctx, ContextRequest{Query: nonsense}); err != nil {
		t.Fatal(err)
	}
	if err := s.FlushUsage(ctx); err != nil {
		t.Fatal(err)
	}

	got, err := s.Stats(ctx, since, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Misses.Count <= before.Misses.Count {
		t.Errorf("misses.count did not move: %d, was %d", got.Misses.Count, before.Misses.Count)
	}
	// This query's own rows, counted directly: the instance-wide number
	// moves under a test that shares the database with three other
	// packages, and what is being asserted is that each way of asking
	// recorded exactly one miss.
	if n := countMisses(ctx, t, dbURL, nonsense); n != 2 {
		t.Errorf("the question was recorded %d times, want 2 (the search and the context call)", n)
	}
	if on, err := svc.Stats(ctx, 1, nil); err != nil {
		t.Fatal(err)
	} else if !on.Misses.Recording {
		t.Error("misses.recording is false on a deployment that records them")
	}

	// A deployment that keeps no questions records none — and says so,
	// rather than reporting zero (§3.4).
	off := &Service{Store: s, Log: slog.New(slog.DiscardHandler), Config: &config.Config{}}
	if _, err := off.Search(ctx, nonsense, store.Filter{}, 10); err != nil {
		t.Fatal(err)
	}
	if err := s.FlushUsage(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := off.Stats(ctx, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n := countMisses(ctx, t, dbURL, nonsense); n != 2 {
		t.Errorf("the question was recorded %d times after a search with recording off, want 2", n)
	}
	if after.Misses.Recording {
		t.Error("misses.recording is true on a deployment that keeps none")
	}
}

// The window is the flow numbers' whole meaning, so a window nothing can
// answer for is refused rather than quietly shortened.
func TestStatsWindowIsBounded(t *testing.T) {
	dbURL := os.Getenv("OCHAKAI_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("OCHAKAI_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	s, err := store.New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	svc := &Service{Store: s, Log: slog.New(slog.DiscardHandler)}

	for _, days := range []int{-1, maxStatsWindow + 1} {
		_, err := svc.Stats(ctx, days, nil)
		var invalid *InvalidInputError
		if err == nil || !errors.As(err, &invalid) {
			t.Errorf("Stats(%d) = %v, want an invalid-input error", days, err)
		} else if !strings.Contains(err.Error(), "180") {
			t.Errorf("Stats(%d) does not say what the limit is: %v", days, err)
		}
	}
	st, err := svc.Stats(ctx, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.WindowDays != defaultStatsWindow || st.At.IsZero() {
		t.Errorf("Stats(0) = window %d at %v, want the default window and a time", st.WindowDays, st.At)
	}
}

// countMisses is how many rows one query left, asked of the database
// directly: an assertion about this test's own misses must not be an
// assertion about the whole instance's.
func countMisses(ctx context.Context, t *testing.T, dbURL, query string) int64 {
	t.Helper()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("counting misses: %v", err)
	}
	defer conn.Close(ctx)
	var n int64
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM search_miss WHERE query = $1`, query).Scan(&n); err != nil {
		t.Fatalf("counting misses: %v", err)
	}
	return n
}

// deleteMisses removes the rows a test's own query left behind: the test
// database is shared and long-lived, and misses are pruned on the same
// 180-day schedule as raw events rather than by any test.
func deleteMisses(ctx context.Context, t *testing.T, dbURL, query string) {
	t.Helper()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Errorf("cleaning up misses: %v", err)
		return
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `DELETE FROM search_miss WHERE query = $1`, query); err != nil {
		t.Errorf("cleaning up misses: %v", err)
	}
}

// TestMissesGroupByMeaningIntegration pins what migration 0037 bought:
// the same question asked several ways is one row of the list a curator
// reads to decide what to write next, not several.
//
// The list is ten long, so spellings of one question used to spend the
// slots that other questions needed — and the count beside each spelling
// understated how much anybody wanted the answer.
func TestMissesGroupByMeaningIntegration(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t, ctx)
	dbURL := os.Getenv("OCHAKAI_TEST_DATABASE_URL")
	stem := uid(t, "gapit")
	// One question, five askings: capitalization, a trailing question
	// mark, doubled spacing, and full-width characters. The words are
	// nonsense on purpose — the test database is shared, and a question
	// made of real words would find somebody else's concepts and stop
	// being a miss at all.
	askings := []string{
		stem + " qqzz",
		stem + " QQZZ",
		stem + " qqzz?",
		stem + "  qqzz",
		stem + " ｑｑｚｚ",
	}
	t.Cleanup(func() {
		for _, q := range askings {
			deleteMisses(ctx, t, dbURL, q)
		}
	})

	since := time.Now().Add(-time.Hour)
	for _, q := range askings {
		if hits, err := svc.Search(ctx, q, store.Filter{}, 10); err != nil {
			t.Fatal(err)
		} else if len(hits) != 0 {
			t.Fatalf("%q matched %d concepts; the question has to go unanswered", q, len(hits))
		}
	}
	if err := svc.Store.FlushUsage(ctx); err != nil {
		t.Fatal(err)
	}

	st, err := svc.Store.Stats(ctx, since, nil)
	if err != nil {
		t.Fatal(err)
	}
	var rows []domain.MissedQuery
	for _, q := range st.Misses.Queries {
		if strings.Contains(q.Query, stem) {
			rows = append(rows, q)
		}
	}
	if len(rows) != 1 {
		t.Fatalf("five askings of one question became %d rows, want 1: %+v", len(rows), rows)
	}
	if rows[0].Count != int64(len(askings)) {
		t.Errorf("the gap is counted %d times, want %d — the group carries every asking",
			rows[0].Count, len(askings))
	}
	// Shown in somebody's own words: the query column is never folded,
	// and the most recent asking is the one a reader is shown.
	if rows[0].Query != askings[len(askings)-1] {
		t.Errorf("the row reads %q, want the most recent asking %q", rows[0].Query, askings[len(askings)-1])
	}
}

// TestDroppedObservationsReachTheStatsIntegration pins the honest
// footnote (migration 0038): usage is best-effort, and the loss now
// shows up beside the numbers it damaged instead of only in a log line.
//
// The buffer is filled past its cap rather than a failing database
// simulated, because that is the drop a deployment actually meets — an
// instance holding more than it can write. The other one, a flush whose
// statement fails, is counted on the same path.
func TestDroppedObservationsReachTheStatsIntegration(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t, ctx)
	since := time.Now().Add(-time.Hour)

	before, err := svc.Store.Stats(ctx, since, nil)
	if err != nil {
		t.Fatal(err)
	}
	// One call bigger than the whole buffer: refused entire, and counted.
	const dropped = 20001
	run := uid(t, "dropit")
	ids := make([]string, dropped)
	for i := range ids {
		ids[i] = fmt.Sprintf("%s/%d", run, i)
	}
	if err := svc.Store.RecordEvents(ctx, domain.EventSearchHit,
		domain.Actor{Kind: domain.ActorHuman, Name: "test"}, ids); err == nil {
		t.Fatal("a batch larger than the buffer was accepted; nothing was dropped to count")
	}
	// The count reaches the database with the next flush that works.
	if err := svc.Store.FlushUsage(ctx); err != nil {
		t.Fatal(err)
	}

	got, err := svc.Store.Stats(ctx, since, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n := got.Dropped.Events - before.Dropped.Events; n != dropped {
		t.Errorf("stats report %d dropped events, want %d: what the loop lost has to be "+
			"readable beside what it kept", n, dropped)
	}
}

// TestOutcomeCoverageIsCountedIntegration pins the number that says
// whether report_outcome is a real signal or an aspiration: of the
// concepts handed over in the window, how many came back with evidence.
//
// An agent that never reports leaves the re-verification feed empty, and
// an empty feed is what a healthy knowledge base looks like too. Only
// this pair tells the two apart.
func TestOutcomeCoverageIsCountedIntegration(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t, ctx)
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "coverage"}
	run := uid(t, "covit")
	since := time.Now().Add(-time.Hour)

	// Two concepts, both delivered; one reported on.
	var ids []string
	for _, name := range []string{"reported", "silent"} {
		id := run + "/" + name
		if _, err := svc.Create(ctx, &domain.Knowledge{
			Type: domain.TypeInsights, ID: id, Title: name,
			Status: domain.StatusStable, Body: "About " + run + ".",
		}, actor); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
		ids = append(ids, id)
	}
	if err := svc.Store.RecordEvents(ctx, domain.EventFetched, actor, ids); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReportOutcome(ctx, ids[0], domain.EventWorked, "checked"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Store.FlushUsage(ctx); err != nil {
		t.Fatal(err)
	}

	// Scoped to this run, so the shared database's other traffic is not
	// in the answer.
	st, err := svc.Store.Stats(ctx, since, []string{run})
	if err != nil {
		t.Fatal(err)
	}
	if st.Outcomes.ConceptsUsed != 2 {
		t.Errorf("concepts_used = %d, want 2", st.Outcomes.ConceptsUsed)
	}
	if st.Outcomes.ConceptsReported != 1 {
		t.Errorf("concepts_reported = %d, want 1 — the silent one is the whole point of the pair",
			st.Outcomes.ConceptsReported)
	}
}
