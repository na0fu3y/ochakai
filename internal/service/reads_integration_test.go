package service

import (
	"context"
	"errors"
	"log/slog"
	"os"
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
	dbURL := testdb.URL(t)
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

	hits, _, err := svc.Search(httpauth.WithActor(ctx, actor), id, store.Filter{}, 5)
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

// TestGetCarriesBacklinksIntegration pins the read's linked_from rows
// (design doc 0106): an entry whose body is linked at arrives with the
// linker named, rejected linkers stay out, and the rows do not count as
// fetches of the concepts they name.
func TestGetCarriesBacklinksIntegration(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t, ctx)
	actor := domain.Actor{Kind: "human", Name: "test"}

	id := uid(t, "blit")
	metric, insight, spurned := "metrics/"+id, "insights/"+id+"-reading", "insights/"+id+"-spurned"
	for _, k := range []*domain.Knowledge{
		{Type: domain.TypeMetrics, ID: metric, Title: "the metric"},
		{Type: domain.TypeInsights, ID: insight, Title: "how to read it",
			Description: "seasonality caveats",
			Body:        "Caveats for [the metric](/" + metric + ".md)."},
		{Type: domain.TypeInsights, ID: spurned,
			Body: "Rejected take on [the metric](/" + metric + ".md)."},
	} {
		if _, err := svc.Create(ctx, k, actor); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Reject(ctx, spurned, "wrong baseline", actor); err != nil {
		t.Fatalf("Reject: %v", err)
	}

	k, err := svc.Get(httpauth.WithActor(ctx, actor), metric)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(k.LinkedFrom) != 1 || k.LinkedFrom[0].ID != insight {
		t.Fatalf("linked_from = %+v, want just the live insight", k.LinkedFrom)
	}
	row := k.LinkedFrom[0]
	if row.Description != "seasonality caveats" || row.Trust != domain.TrustUnverified {
		t.Errorf("row = %+v, want the projection's description and trust", row)
	}

	// A row is a pointer, not a delivery: the named concept's fetch count
	// must not move for having been named.
	if err := svc.Store.FlushUsage(ctx); err != nil {
		t.Fatalf("FlushUsage: %v", err)
	}
	u, err := svc.Usage(ctx, insight)
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if u.Fetches != 0 {
		t.Errorf("fetches of the linker = %d after only being named, want 0", u.Fetches)
	}
}
