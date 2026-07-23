package projstore

import (
	"context"
	"math"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// TestSearchLexicalScoring checks the composed score matches the
// store.SearchLexical formula: similarity + 0.3 substring + 0.05 verified,
// kept when > 0.05, ranked desc.
func TestSearchLexicalScoring(t *testing.T) {
	ctx := context.Background()
	s := newTestStore()
	// exact holds the query as a substring (gets the 0.3 floor) and is
	// verified (+0.05); partial only shares trigrams; unrelated scores ~0.
	_ = s.Create(ctx, mkK("metrics/revenue", "revenue", "monthly revenue rollup", domain.StatusVerified))
	_ = s.Create(ctx, mkK("metrics/revenues", "revenues", "yearly revenues", domain.StatusDraft))
	_ = s.Create(ctx, mkK("other/weather", "weather", "unrelated text", domain.StatusDraft))

	hits, err := s.SearchLexical(ctx, "revenue", Filter{}, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) < 2 {
		t.Fatalf("want >=2 hits, got %d: %+v", len(hits), hits)
	}
	if hits[0].ID != "metrics/revenue" {
		t.Errorf("top hit = %q, want metrics/revenue", hits[0].ID)
	}
	// The top hit carries the substring floor (0.3) plus verified (0.05)
	// on top of its trigram similarity, so it clears 0.35 and outranks the
	// draft "revenues" entry (which lacks the verified boost).
	if hits[0].Score <= 0.35 {
		t.Errorf("top score = %v, want > 0.35 (substring + verified + sim)", hits[0].Score)
	}
	if hits[0].Score <= hits[1].Score {
		t.Errorf("verified entry should outrank draft: %v vs %v", hits[0].Score, hits[1].Score)
	}
	// Unrelated entry is below the 0.05 floor and excluded.
	for _, h := range hits {
		if h.ID == "other/weather" {
			t.Errorf("unrelated entry should be filtered out: %v", h.Score)
		}
	}
}

func TestSearchLexicalFilters(t *testing.T) {
	ctx := context.Background()
	s := newTestStore()
	_ = s.Create(ctx, mkK("a", "revenue metric", "x", domain.StatusVerified))
	_ = s.Create(ctx, mkK("b", "revenue model", "x", domain.StatusDraft))

	hits, _ := s.SearchLexical(ctx, "revenue", Filter{Statuses: []domain.Status{domain.StatusVerified}}, 10)
	if len(hits) != 1 || hits[0].ID != "a" {
		t.Errorf("status filter failed: %+v", hits)
	}
	// Type matches case-insensitively (design doc 0023 §3.3).
	hits, _ = s.SearchLexical(ctx, "revenue", Filter{Types: []domain.Type{"CONCEPT"}}, 10)
	if len(hits) != 2 {
		t.Errorf("case-insensitive type filter failed: %+v", hits)
	}
}

func TestSearchVectorFlatScan(t *testing.T) {
	ctx := context.Background()
	s := newTestStore()
	_ = s.Create(ctx, mkK("near", "n", "n", domain.StatusDraft))
	_ = s.Create(ctx, mkK("far", "f", "f", domain.StatusDraft))
	if err := s.SetVector(ctx, "near", []float32{1, 0, 0}); err != nil {
		t.Fatalf("setvector: %v", err)
	}
	if err := s.SetVector(ctx, "far", []float32{0, 1, 0}); err != nil {
		t.Fatalf("setvector: %v", err)
	}
	hits, err := s.SearchVector(ctx, []float32{1, 0, 0}, Filter{}, 10)
	if err != nil {
		t.Fatalf("searchvector: %v", err)
	}
	if len(hits) != 2 || hits[0].ID != "near" {
		t.Fatalf("vector ranking wrong: %+v", hits)
	}
	if math.Abs(hits[0].Score-1) > 1e-6 {
		t.Errorf("near cosine = %v, want 1", hits[0].Score)
	}
}

// TestBacklinks derives links from the body (design doc 0024) and inverts
// them, the fold 0026 does in-RAM.
func TestBacklinks(t *testing.T) {
	ctx := context.Background()
	s := newTestStore()
	// Body references another entry via an ochakai:// link.
	src := mkK("models/churn", "churn", "uses ochakai://metrics/revenue as input", domain.StatusDraft)
	src.Links = domain.LinksFromBody(src.ID, src.Body)
	_ = s.Create(ctx, src)
	_ = s.Create(ctx, mkK("metrics/revenue", "revenue", "the metric", domain.StatusDraft))

	back, err := s.ListLinkingTo(ctx, "metrics/revenue", 10)
	if err != nil {
		t.Fatalf("backlinks: %v", err)
	}
	if len(back) != 1 || back[0].ID != "models/churn" {
		t.Errorf("backlinks = %+v, want [models/churn]", back)
	}
}
