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
	"github.com/na0fu3y/ochakai/internal/store"
)

// A search that finds nothing is recorded as a question this base could
// not answer (design doc 0049 §3.1) — through the service, so every
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
	nonsense := fmt.Sprintf("qqzzxx%d", time.Now().UnixNano())
	t.Cleanup(func() { deleteMisses(ctx, t, dbURL, nonsense) })

	since := time.Now().Add(-time.Hour)
	before, err := s.Stats(ctx, since)
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

	got, err := s.Stats(ctx, since)
	if err != nil {
		t.Fatal(err)
	}
	if d := got.Misses.Count - before.Misses.Count; d != 2 {
		t.Errorf("misses.count moved by %d, want 2 (the search and the context call)", d)
	}
	if on, err := svc.Stats(ctx, 1); err != nil {
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
	after, err := off.Stats(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if after.Misses.Count != got.Misses.Count {
		t.Errorf("a miss was recorded with recording off: %d, was %d", after.Misses.Count, got.Misses.Count)
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
		_, err := svc.Stats(ctx, days)
		var invalid *InvalidInputError
		if err == nil || !errors.As(err, &invalid) {
			t.Errorf("Stats(%d) = %v, want an invalid-input error", days, err)
		} else if !strings.Contains(err.Error(), "180") {
			t.Errorf("Stats(%d) does not say what the limit is: %v", days, err)
		}
	}
	st, err := svc.Stats(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if st.WindowDays != defaultStatsWindow || st.At.IsZero() {
		t.Errorf("Stats(0) = window %d at %v, want the default window and a time", st.WindowDays, st.At)
	}
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
