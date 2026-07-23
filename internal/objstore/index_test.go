package objstore

import (
	"context"
	"errors"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
)

func mustOpen(t *testing.T, os ObjectStore) *Index {
	t.Helper()
	ix, err := Open(context.Background(), os)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return ix
}

func mk(id, title, body string, status domain.Status) *domain.Knowledge {
	return &domain.Knowledge{
		Type:      domain.TypeMetrics,
		ID:        id,
		Title:     title,
		Body:      body,
		Status:    status,
		CreatedBy: domain.Actor{Kind: domain.ActorHuman, Name: "tester"},
	}
}

// TestRebuildFromObjects is the cold-start claim: an Index built from
// nothing but the object store's bytes recovers every entry. This is the
// scan that replaces a warm database connection (design doc 0026 §5).
func TestRebuildFromObjects(t *testing.T) {
	ctx := context.Background()
	fake := newFake()
	ix := mustOpen(t, fake)
	if err := ix.Create(ctx, mk("metrics/revenue", "Revenue", "SUM(price)", domain.StatusVerified)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := ix.Create(ctx, mk("metrics/aov", "AOV", "revenue / orders", domain.StatusDraft)); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// A brand-new Index over the same bucket must see both entries — no
	// state lives anywhere but the object store.
	reopened := mustOpen(t, fake)
	for _, id := range []string{"metrics/revenue", "metrics/aov"} {
		if _, err := reopened.Get(ctx, id); err != nil {
			t.Errorf("after reopen, Get(%s): %v", id, err)
		}
	}
}

// TestCreateIsCreateOnly proves the generation-0 precondition rejects a
// duplicate id even across a fresh Index (a second instance's view).
func TestCreateIsCreateOnly(t *testing.T) {
	ctx := context.Background()
	fake := newFake()
	ix := mustOpen(t, fake)
	if err := ix.Create(ctx, mk("metrics/revenue", "Revenue", "x", domain.StatusDraft)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := ix.Create(ctx, mk("metrics/revenue", "Dup", "y", domain.StatusDraft)); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("duplicate Create: got %v, want ErrAlreadyExists", err)
	}
}

// TestOptimisticConcurrency is design doc 0025 §11's If-Match lock, now
// enforced by the object generation: an Update carrying a stale updated_at
// loses to a write that landed in between.
func TestOptimisticConcurrency(t *testing.T) {
	ctx := context.Background()
	ix := mustOpen(t, newFake())
	if err := ix.Create(ctx, mk("metrics/revenue", "Revenue", "x", domain.StatusDraft)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	first, _ := ix.Get(ctx, "metrics/revenue")
	stale := first.UpdatedAt // reader A's view

	// Reader B updates first, moving the stored version forward.
	bump := *first
	bump.Body = "changed by B"
	if err := ix.Update(ctx, &bump, &first.UpdatedAt); err != nil {
		t.Fatalf("B Update: %v", err)
	}

	// Reader A's conditional update, built on the now-stale version, must
	// be rejected rather than silently clobbering B.
	conflict := *first
	conflict.Body = "changed by A"
	if err := ix.Update(ctx, &conflict, &stale); !errors.Is(err, ErrConflict) {
		t.Fatalf("A Update: got %v, want ErrConflict", err)
	}
}

// TestSearchRanking checks the ordering the SQL store guarantees: an exact
// substring hit outranks a fuzzy one, and among substring hits the
// verified boost breaks the tie. Bit-parity with pg_trgm is not the claim
// (design doc 0026 §3).
func TestSearchRanking(t *testing.T) {
	ctx := context.Background()
	ix := mustOpen(t, newFake())
	_ = ix.Create(ctx, mk("metrics/revenue", "Revenue", "total revenue by month", domain.StatusVerified))
	_ = ix.Create(ctx, mk("metrics/revenge", "Revenge", "unrelated revenge text", domain.StatusDraft))
	_ = ix.Create(ctx, mk("metrics/aov", "Average order value", "orders and prices", domain.StatusDraft))

	hits, err := ix.SearchLexical(ctx, "revenue", Filter{}, 10)
	if err != nil {
		t.Fatalf("SearchLexical: %v", err)
	}
	if len(hits) == 0 || hits[0].ID != "metrics/revenue" {
		t.Fatalf("top hit = %v, want metrics/revenue", ids(hits))
	}
	// The fuzzy-only "revenge" match must rank below the exact one.
	if len(hits) >= 2 && hits[1].ID == "metrics/revenue" {
		t.Fatalf("duplicate top hit: %v", ids(hits))
	}
}

func TestSearchFilter(t *testing.T) {
	ctx := context.Background()
	ix := mustOpen(t, newFake())
	_ = ix.Create(ctx, mk("metrics/revenue", "Revenue", "revenue text", domain.StatusVerified))
	rejected := mk("metrics/bad", "Bad revenue", "revenue text", domain.StatusRejected)
	_ = ix.Create(ctx, rejected)

	// Without an explicit status filter, the rejected entry stays out of
	// answers (the memory of "no").
	hits, _ := ix.SearchLexical(ctx, "revenue", Filter{}, 10)
	for _, h := range hits {
		if h.ID == "metrics/bad" {
			t.Fatalf("rejected entry leaked into default search: %v", ids(hits))
		}
	}
	// But it is queryable on request.
	hits, _ = ix.SearchLexical(ctx, "revenue", Filter{Statuses: []domain.Status{domain.StatusRejected}}, 10)
	if len(hits) != 1 || hits[0].ID != "metrics/bad" {
		t.Fatalf("explicit rejected query = %v, want [metrics/bad]", ids(hits))
	}
}

// TestBacklinks proves the reverse-edge query (JSONB containment in SQL)
// works off the in-memory link graph, which is derived from bodies.
func TestBacklinks(t *testing.T) {
	ctx := context.Background()
	ix := mustOpen(t, newFake())
	_ = ix.Create(ctx, mk("metrics/revenue", "Revenue", "SUM(price)", domain.StatusVerified))
	insight := mk("insights/revenue-reading", "Reading revenue",
		"See [revenue](/metrics/revenue.md) for the definition.", domain.StatusDraft)
	insight.Type = domain.TypeInsights
	_ = ix.Create(ctx, insight)

	back, err := ix.ListLinkingTo(ctx, "metrics/revenue", 10)
	if err != nil {
		t.Fatalf("ListLinkingTo: %v", err)
	}
	if len(back) != 1 || back[0].ID != "insights/revenue-reading" {
		t.Fatalf("backlinks = %v, want [insights/revenue-reading]", idsK(back))
	}
}

// TestMoveRewritesReferences is the multi-object write the SQL store does
// in one transaction: the entry is renamed and referrers are repaired.
func TestMoveRewritesReferences(t *testing.T) {
	ctx := context.Background()
	ix := mustOpen(t, newFake())
	_ = ix.Create(ctx, mk("metrics/revenue", "Revenue", "SUM(price)", domain.StatusVerified))
	insight := mk("insights/revenue-reading", "Reading revenue",
		"See [revenue](/metrics/revenue.md).", domain.StatusDraft)
	insight.Type = domain.TypeInsights
	_ = ix.Create(ctx, insight)

	if _, err := ix.Move(ctx, "metrics/revenue", "metrics/net-revenue"); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if _, err := ix.Get(ctx, "metrics/revenue"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old id still present: %v", err)
	}
	if _, err := ix.Get(ctx, "metrics/net-revenue"); err != nil {
		t.Fatalf("new id missing: %v", err)
	}
	// The referrer's body and link now point at the new id.
	back, _ := ix.ListLinkingTo(ctx, "metrics/net-revenue", 10)
	if len(back) != 1 || back[0].ID != "insights/revenue-reading" {
		t.Fatalf("backlinks after move = %v, want [insights/revenue-reading]", idsK(back))
	}
}

// TestMoveCrashSelfHeals is the payoff of deriving links from bodies
// (design doc 0026 §4): a Move that crashes after the rename but before a
// referrer is rewritten leaves a dangling-but-visible edge, never index
// corruption, and a reopen + repair finishes the job — no transaction,
// no torn state.
func TestMoveCrashSelfHeals(t *testing.T) {
	ctx := context.Background()
	fake := newFake()
	ix := mustOpen(t, fake)
	_ = ix.Create(ctx, mk("metrics/revenue", "Revenue", "SUM(price)", domain.StatusVerified))
	insight := mk("insights/revenue-reading", "Reading revenue",
		"See [revenue](/metrics/revenue.md).", domain.StatusDraft)
	insight.Type = domain.TypeInsights
	_ = ix.Create(ctx, insight)

	// Crash on the referrer-rewrite Put: the rename (Put #1) + delete land,
	// then rewriteReferences' Put fails.
	fake.failPutAfter = fake.putCount + 2
	if _, err := ix.Move(ctx, "metrics/revenue", "metrics/net-revenue"); err == nil {
		t.Fatalf("Move: expected injected crash, got nil")
	}
	fake.failPutAfter = 0 // "restart" the process

	// A fresh Index over the crashed bucket: the entry is at its new id,
	// and the referrer is a visible dangling edge — not lost, not corrupt.
	reopened := mustOpen(t, fake)
	if _, err := reopened.Get(ctx, "metrics/net-revenue"); err != nil {
		t.Fatalf("after crash, new id missing: %v", err)
	}
	stale, _ := reopened.ListLinkingTo(ctx, "metrics/revenue", 10)
	if len(stale) != 1 {
		t.Fatalf("expected the dangling referrer to still point at the old id, got %v", idsK(stale))
	}

	// Startup repair (re-running the rewrite) heals it, with no torn state.
	if err := repairAfterMove(ctx, reopened, "metrics/revenue", "metrics/net-revenue"); err != nil {
		t.Fatalf("repair: %v", err)
	}
	healed, _ := reopened.ListLinkingTo(ctx, "metrics/net-revenue", 10)
	if len(healed) != 1 || healed[0].ID != "insights/revenue-reading" {
		t.Fatalf("after repair, backlinks = %v, want [insights/revenue-reading]", idsK(healed))
	}
}

func repairAfterMove(ctx context.Context, ix *Index, oldID, newID string) error {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	return ix.rewriteReferences(ctx, oldID, newID)
}

// TestModelDefiningMetric checks the JSONB-path containment query works in
// memory (compile-time model resolution, design doc 0019).
func TestModelDefiningMetric(t *testing.T) {
	ctx := context.Background()
	ix := mustOpen(t, newFake())
	model := mk("models/sales", "Sales model", "", domain.StatusVerified)
	model.Type = domain.TypeModels
	model.Attrs = map[string]any{
		"spec": map[string]any{
			"metrics": []any{
				map[string]any{"name": "revenue"},
				map[string]any{"name": "orders"},
			},
		},
	}
	_ = ix.Create(ctx, model)

	got, err := ix.ListModelsDefiningMetric(ctx, "revenue")
	if err != nil {
		t.Fatalf("ListModelsDefiningMetric: %v", err)
	}
	if len(got) != 1 || got[0].ID != "models/sales" {
		t.Fatalf("models defining revenue = %v, want [models/sales]", idsK(got))
	}
	if none, _ := ix.ListModelsDefiningMetric(ctx, "nonexistent"); len(none) != 0 {
		t.Fatalf("unexpected match for nonexistent metric: %v", idsK(none))
	}
}

// TestUsageAggregation shows counters aggregate in memory with no join.
func TestUsageAggregation(t *testing.T) {
	ctx := context.Background()
	ix := mustOpen(t, newFake())
	_ = ix.Create(ctx, mk("metrics/revenue", "Revenue", "x", domain.StatusVerified))
	ix.RecordEvents(ctx, "search_hit", []string{"metrics/revenue", "metrics/revenue"})
	ix.RecordEvents(ctx, "failed", []string{"metrics/revenue"})
	u := ix.Usage(ctx, "metrics/revenue")
	if u.SearchHits != 2 || u.Failed != 1 {
		t.Fatalf("usage = %+v, want SearchHits=2 Failed=1", u)
	}
	if u.LastUsedAt == nil {
		t.Fatalf("LastUsedAt not set")
	}
}

func ids(hits []domain.SearchHit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.ID
	}
	return out
}

func idsK(ks []domain.Knowledge) []string {
	out := make([]string, len(ks))
	for i, k := range ks {
		out[i] = k.ID
	}
	return out
}
