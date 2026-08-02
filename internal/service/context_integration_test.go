package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/httpauth"
	"github.com/na0fu3y/ochakai/internal/store"
	"github.com/na0fu3y/ochakai/internal/testdb"
)

// newIntegrationService dials the test database, skipping without
// OCHAKAI_TEST_DATABASE_URL (see the store integration test for the
// docker one-liner). Tests use run-unique IDs (uid below) instead of
// cleanup, the same pattern as TestUpdateNoOpIntegration.
func newIntegrationService(t *testing.T, ctx context.Context) *Service {
	t.Helper()
	dbURL := os.Getenv("OCHAKAI_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("OCHAKAI_TEST_DATABASE_URL not set")
	}
	s, err := store.New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	return &Service{Store: s, Log: slog.New(slog.NewTextHandler(os.Stderr, nil))}
}

// uid returns a run-unique slug token, so reruns against a shared test
// database never collide on primary keys — and, since testdb owns it,
// so the rows written under it go away when the test ends.
func uid(t *testing.T, name string) string {
	t.Helper()
	return testdb.Unique(t, name)
}

// TestContextIntegration exercises the one-call context pack end to end:
// the entries behind the top hits arrive in full, links expand one hop in
// both directions (the query a metric links to, and the insight linking
// to the metric), and rejected companions stay out.
func TestContextIntegration(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t, ctx)
	actor := domain.Actor{Kind: "human", Name: "test"}

	id := uid(t, "ctxit")
	metricID, queryID, insightID, rejectedID := "metrics/"+id+"-revenue", "queries/"+id+"-monthly", "insights/"+id+"-reading", "insights/"+id+"-rejected"
	entries := []*domain.Knowledge{
		{Type: domain.TypeMetrics, ID: metricID, Title: id + "-revenue metric",
			Status: domain.StatusStable,
			Body:   "Answered by [the monthly query](/" + queryID + ".md)."},
		// A verified query is an Attested Computation, and SPEC §10.2 makes
		// runtime required on that type — the one schema the write path
		// enforces (design doc 0036 §3.10).
		{Type: domain.TypeComputations, ID: queryID, Title: "monthly numbers", Runtime: "bigquery"},
		{Type: domain.TypeInsights, ID: insightID, Title: "how to read it",
			Body: "Explains [the metric](/" + metricID + ".md)."},
		{Type: domain.TypeInsights, ID: rejectedID, Title: "bad take",
			Status: domain.StatusDraft,
			Body:   "Explains [the metric](/" + metricID + ".md)."},
	}
	for _, k := range entries {
		if _, err := svc.Create(ctx, k, actor); err != nil {
			t.Fatalf("create %s: %v", k.ID, err)
		}
	}
	// Rejecting is a ruling rather than a status (design doc 0043 §3.3),
	// so the pack has to consult the ledger to keep the entry out.
	if _, err := svc.Reject(ctx, rejectedID, "bad take", actor); err != nil {
		t.Fatal(err)
	}

	res, err := svc.Context(ctx, ContextRequest{Query: id + "-revenue", Limit: 5})
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	got := map[string]bool{}
	for _, e := range res.Concepts {
		got[e.ID] = true
	}
	for _, want := range []string{metricID, queryID, insightID} {
		if !got[want] {
			t.Errorf("context pack misses %s; got %v", want, got)
		}
	}
	if got[rejectedID] {
		t.Error("rejected companions must stay out of the pack")
	}

	// A blank question is the client's mistake.
	var invalid *InvalidInputError
	if _, err := svc.Context(ctx, ContextRequest{Query: "  ", Limit: 5}); !errors.As(err, &invalid) {
		t.Errorf("empty query: want InvalidInputError, got %v", err)
	}
}

// TestSearchRecordsUsageIntegration pins the passive half of the
// write-back loop: appearing in search results bumps search_hits.
func TestSearchRecordsUsageIntegration(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t, ctx)
	actor := domain.Actor{Kind: "human", Name: "test"}

	id := uid(t, "usgit")
	if _, err := svc.Create(ctx, &domain.Knowledge{
		Type: domain.TypeTerms, ID: id, Title: id + " term"}, actor); err != nil {
		t.Fatal(err)
	}

	hits, err := svc.Search(httpauth.WithActor(ctx, actor), id, store.Filter{}, 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 || hits[0].ID != id {
		t.Fatalf("search missed the entry: %+v", hits)
	}
	// Usage recording buffers off the read path (design doc 0029).
	if err := svc.Store.FlushUsage(ctx); err != nil {
		t.Fatalf("FlushUsage: %v", err)
	}
	u, err := svc.Usage(ctx, id)
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if u.SearchHits < 1 {
		t.Errorf("search_hits = %d, want >= 1", u.SearchHits)
	}
}

// TestDeleteIntegration pins soft delete through the service: the entry
// vanishes from reads, and a second delete reports not-found instead of
// stacking another delete revision.
func TestDeleteIntegration(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t, ctx)
	actor := domain.Actor{Kind: "human", Name: "test"}

	id := uid(t, "delit")
	if _, err := svc.Create(ctx, &domain.Knowledge{
		Type: domain.TypeTerms, ID: id, Title: "to delete"}, actor); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, id, actor, nil); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := svc.Get(ctx, id); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("deleted entry still readable: %v", err)
	}
	if err := svc.Delete(ctx, id, actor, nil); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("double delete: want ErrNotFound, got %v", err)
	}
}

// TestContextBudgetIntegration pins the embedding-host path end to end: a
// budget too small for everything delivers whole entries up to the cap and
// names the rest, and the named ones are not counted as fetched — an
// outline row is not a use of the knowledge.
func TestContextBudgetIntegration(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t, ctx)
	actor := domain.Actor{Kind: "human", Name: "test"}

	// The shared test database carries other tests' entries, and fuzzy
	// search does not respect test boundaries — assert over our own ids.
	id := uid(t, "budgetit")
	mine := map[string]bool{}
	for i := range 4 {
		k := &domain.Knowledge{
			Type: domain.TypeInsights, ID: fmt.Sprintf("insights/%s-%d", id, i),
			Title: id + " budget", Description: "an entry about " + id,
			Body: strings.Repeat("x", 2000),
		}
		if _, err := svc.Create(ctx, k, actor); err != nil {
			t.Fatalf("create %s: %v", k.ID, err)
		}
		mine[k.ID] = true
	}

	full, err := svc.Context(ctx, ContextRequest{Query: id, Limit: 5})
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if full.Truncated != 0 || full.Outline != nil {
		t.Errorf("no budget must not truncate: %d outlined", full.Truncated)
	}
	delivered := 0
	for _, e := range full.Concepts {
		if mine[e.ID] {
			delivered++
		}
	}
	if delivered != len(mine) {
		t.Fatalf("unbudgeted pack carries %d of our %d entries", delivered, len(mine))
	}

	// Room for roughly one entry.
	const budget = 2500
	capped, err := svc.Context(ctx, ContextRequest{Query: id, Limit: 5, Budget: budget})
	if err != nil {
		t.Fatalf("Context with budget: %v", err)
	}
	used := 0
	for i := range capped.Concepts {
		used += serializedSize(&capped.Concepts[i])
	}
	if used > budget {
		t.Errorf("delivered %d bytes over a %d budget", used, budget)
	}
	if len(capped.Concepts) >= len(full.Concepts) {
		t.Errorf("budget delivered %d entries, unbudgeted %d — nothing was capped",
			len(capped.Concepts), len(full.Concepts))
	}
	if capped.Truncated != len(capped.Outline) || capped.Truncated == 0 {
		t.Fatalf("truncated = %d, outline = %d; want both nonzero and equal",
			capped.Truncated, len(capped.Outline))
	}
	// Nothing is lost: every entry the unbudgeted pack carried is either
	// delivered or named.
	seen := map[string]bool{}
	for _, e := range capped.Concepts {
		seen[e.ID] = true
	}
	for _, o := range capped.Outline {
		seen[o.ID] = true
		if o.Bytes == 0 {
			t.Errorf("outline row carries no size: %+v", o)
		}
		if mine[o.ID] && o.Description == "" {
			t.Errorf("outline row dropped the description: %+v", o)
		}
	}
	for _, e := range full.Concepts {
		if !seen[e.ID] {
			t.Errorf("%s vanished under a budget: neither delivered nor outlined", e.ID)
		}
	}
	// Hits still name everything that matched — the ranking is not cut.
	if len(capped.Hits) != len(full.Hits) {
		t.Errorf("budget changed the hit list: %d vs %d", len(capped.Hits), len(full.Hits))
	}

	// The budget has to govern what the caller actually receives, not one
	// field of it. Hits used to embed the whole entry, so an entry the
	// packer had just refused to deliver arrived in full anyway — the
	// outline row told the caller to go fetch what it already had, and the
	// cap it set was spent twice over (design doc 0033).
	wire, err := json.Marshal(capped)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range capped.Outline {
		if !mine[o.ID] {
			continue
		}
		k, err := svc.Get(ctx, o.ID)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(wire, []byte(k.Body)) {
			t.Errorf("%s was outlined but its body still shipped in the response", o.ID)
		}
	}
	ranking, err := json.Marshal(capped.Hits)
	if err != nil {
		t.Fatal(err)
	}
	// The ranking is the one thing outside the cap: no bodies, bounded by
	// the hit count. Anything else growing past the budget is a leak.
	if over := len(wire) - len(ranking); over > budget {
		t.Errorf("response carries %d bytes of knowledge over a %d budget", over, budget)
	}
}
