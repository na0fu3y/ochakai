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

	"github.com/na0fu3y/ochakai/internal/compiler"
	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/httpauth"
	"github.com/na0fu3y/ochakai/internal/store"
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
// database never collide on primary keys.
func uid(prefix string) string {
	return fmt.Sprintf("%s%d", prefix, time.Now().UnixNano())
}

// TestContextIntegration exercises the one-call context pack end to end:
// the entries behind the top hits arrive in full, links expand one hop in
// both directions (the query a metric links to, and the insight linking
// to the metric), and rejected companions stay out.
func TestContextIntegration(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t, ctx)
	actor := domain.Actor{Kind: "human", Name: "test"}

	id := uid("ctxit")
	metricID, queryID, insightID, rejectedID := "metrics/"+id+"-revenue", "queries/"+id+"-monthly", "insights/"+id+"-reading", "insights/"+id+"-rejected"
	entries := []*domain.Knowledge{
		{Type: domain.TypeMetrics, ID: metricID, Title: id + "-revenue metric",
			Status: domain.StatusVerified,
			Body:   "Answered by [the monthly query](/" + queryID + ".md)."},
		{Type: domain.TypeQueries, ID: queryID, Title: "monthly numbers"},
		{Type: domain.TypeInsights, ID: insightID, Title: "how to read it",
			Body: "Explains ochakai://" + metricID + "."},
		{Type: domain.TypeInsights, ID: rejectedID, Title: "bad take",
			Status: domain.StatusRejected,
			Body:   "Explains [the metric](/" + metricID + ".md)."},
	}
	for _, k := range entries {
		if _, err := svc.Create(ctx, k, actor); err != nil {
			t.Fatalf("create %s: %v", k.ID, err)
		}
	}

	res, err := svc.Context(ctx, id+"-revenue", store.Filter{}, 5, 0)
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	got := map[string]bool{}
	for _, e := range res.Entries {
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

	// A prohibitive min_score empties the pack instead of shipping junk.
	filtered, err := svc.Context(ctx, id+"-revenue", store.Filter{}, 5, 1e9)
	if err != nil {
		t.Fatalf("Context with min_score: %v", err)
	}
	if len(filtered.Hits) != 0 || len(filtered.Entries) != 0 {
		t.Errorf("min_score should drop everything, got %d hits / %d entries",
			len(filtered.Hits), len(filtered.Entries))
	}

	// A blank question is the client's mistake.
	var invalid *InvalidInputError
	if _, err := svc.Context(ctx, "  ", store.Filter{}, 5, 0); !errors.As(err, &invalid) {
		t.Errorf("empty query: want InvalidInputError, got %v", err)
	}
}

// TestSearchRecordsUsageIntegration pins the passive half of the
// write-back loop: appearing in search results bumps search_hits.
func TestSearchRecordsUsageIntegration(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t, ctx)
	actor := domain.Actor{Kind: "human", Name: "test"}

	id := uid("usgit")
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
	u, err := svc.Usage(ctx, id)
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if u.SearchHits < 1 {
		t.Errorf("search_hits = %d, want >= 1", u.SearchHits)
	}
}

// TestCompileIntegration covers the compile path the unit tests cannot:
// model resolution from the models entries themselves (the one whose
// spec defines the metric, design doc 0019), and surfacing verified
// golden queries next to the SQL.
func TestCompileIntegration(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t, ctx)
	actor := domain.Actor{Kind: "human", Name: "test"}

	id := uid("cplit")
	modelName, metricName, goldenID := id+"_model", id+"_revenue", id+"-golden"
	spec := map[string]any{
		"name": modelName,
		"datasets": []any{map[string]any{
			"name": "orders", "source": "shop.orders",
			"fields": []any{map[string]any{"name": "amount", "expression": "total_price"}},
		}},
		"metrics": []any{map[string]any{
			"name": metricName, "expression": "SUM(orders.amount)",
		}},
	}
	modelID := "models/" + modelName
	metricEntryID := "metrics/" + metricName
	for _, k := range []*domain.Knowledge{
		// The model is a knowledge entry with the spec in attrs.spec
		// (design doc 0018); Compile resolves the model by scanning models
		// entries for the one defining the metric (design doc 0019). The
		// metric entry names its model via attrs.model, which attributes
		// compile usage to it.
		{Type: domain.TypeModels, ID: modelID, Title: modelName,
			Attrs: map[string]any{"spec": spec}},
		{Type: domain.TypeMetrics, ID: metricEntryID, Title: "compile-test revenue",
			Attrs: map[string]any{"model": modelID}},
		{Type: domain.TypeQueries, ID: goldenID, Title: metricName + " by month",
			Status: domain.StatusVerified},
	} {
		if _, err := svc.Create(ctx, k, actor); err != nil {
			t.Fatal(err)
		}
	}

	// Model omitted: resolved from the models entry defining the metric.
	res, err := svc.Compile(ctx, CompileRequest{Request: compiler.Request{Metrics: []string{metricName}}})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !strings.Contains(res.SQL, "SUM(orders.total_price)") {
		t.Errorf("compiled SQL wrong:\n%s", res.SQL)
	}
	if res.Model != modelID || res.ModelStatus != domain.StatusDraft {
		t.Errorf("model provenance = %q/%q, want %q/draft", res.Model, res.ModelStatus, modelID)
	}
	if len(res.VerifiedQueries) == 0 || res.VerifiedQueries[0].ID != goldenID {
		t.Errorf("verified golden query not surfaced: %+v", res.VerifiedQueries)
	}

	// Compile usage lands on the model entry and on the metric entry
	// naming it via attrs.model — and only on entries that exist.
	for _, target := range []string{modelID, metricEntryID} {
		u, err := svc.Usage(ctx, target)
		if err != nil {
			t.Fatalf("Usage(%s): %v", target, err)
		}
		if u.Compiles < 1 {
			t.Errorf("compiles(%s) = %d, want >= 1", target, u.Compiles)
		}
	}

	// A second models entry defining the same metric name makes the
	// implicit resolution ambiguous; the explicit model still compiles.
	spec2 := map[string]any{
		"name": modelName + "_b",
		"datasets": []any{map[string]any{
			"name": "orders", "source": "shop.orders_b",
			"fields": []any{map[string]any{"name": "amount", "expression": "total_price"}},
		}},
		"metrics": []any{map[string]any{
			"name": metricName, "expression": "SUM(orders.amount)",
		}},
	}
	if _, err := svc.Create(ctx, &domain.Knowledge{Type: domain.TypeModels,
		ID: modelID + "-b", Title: modelName + "_b",
		Attrs: map[string]any{"spec": spec2}}, actor); err != nil {
		t.Fatal(err)
	}
	var ambErr *compiler.Error
	if _, err := svc.Compile(ctx, CompileRequest{Request: compiler.Request{Metrics: []string{metricName}}}); !errors.As(err, &ambErr) || !strings.Contains(ambErr.Reason, modelID+"-b") {
		t.Errorf("ambiguous model: want compiler.Error naming the candidates, got %v", err)
	}
	if _, err := svc.Compile(ctx, CompileRequest{Model: modelID, Request: compiler.Request{Metrics: []string{metricName}}}); err != nil {
		t.Errorf("explicit model must disambiguate: %v", err)
	}

	// Unresolvable model and missing metrics are compile refusals, not crashes.
	var cErr *compiler.Error
	if _, err := svc.Compile(ctx, CompileRequest{Request: compiler.Request{Metrics: []string{id + "-none"}}}); !errors.As(err, &cErr) {
		t.Errorf("unresolvable model: want compiler.Error, got %v", err)
	}
	if _, err := svc.Compile(ctx, CompileRequest{Model: id + "-missing", Request: compiler.Request{Metrics: []string{metricName}}}); !errors.As(err, &cErr) {
		t.Errorf("unknown model: want compiler.Error, got %v", err)
	}
	if _, err := svc.Compile(ctx, CompileRequest{Model: goldenID, Request: compiler.Request{Metrics: []string{metricName}}}); !errors.As(err, &cErr) {
		t.Errorf("non-model entry as model: want compiler.Error, got %v", err)
	}
	if _, err := svc.Compile(ctx, CompileRequest{}); !errors.As(err, &cErr) {
		t.Errorf("no metrics: want compiler.Error, got %v", err)
	}

	// A broken model is rejected when written, not at compile time.
	var invErr *InvalidInputError
	if _, err := svc.Create(ctx, &domain.Knowledge{Type: domain.TypeModels,
		ID: modelID + "-broken", Title: "broken",
		Attrs: map[string]any{"spec": map[string]any{"datasets": []any{}}}}, actor); !errors.As(err, &invErr) {
		t.Errorf("broken model: want invalid input error, got %v", err)
	}
}

// TestDeleteIntegration pins soft delete through the service: the entry
// vanishes from reads, and a second delete reports not-found instead of
// stacking another delete revision.
func TestDeleteIntegration(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t, ctx)
	actor := domain.Actor{Kind: "human", Name: "test"}

	id := uid("delit")
	if _, err := svc.Create(ctx, &domain.Knowledge{
		Type: domain.TypeTerms, ID: id, Title: "to delete"}, actor); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, id, actor); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := svc.Get(ctx, id); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("deleted entry still readable: %v", err)
	}
	if err := svc.Delete(ctx, id, actor); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("double delete: want ErrNotFound, got %v", err)
	}
}
