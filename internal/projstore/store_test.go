package projstore

import (
	"context"
	"testing"
	"time"

	"github.com/na0fu3y/ochakai/internal/domain"
)

func newTestStore() *Store { return New(NewMemObjectStore()) }

func mkK(id, title, body string, status domain.Status) *domain.Knowledge {
	return &domain.Knowledge{
		Type: "concept", ID: id, Title: title, Body: body, Status: status,
		CreatedBy: domain.Actor{Kind: "human", Name: "t"},
	}
}

func TestCreateGetAndCreateOnly(t *testing.T) {
	ctx := context.Background()
	s := newTestStore()
	k := mkK("metrics/revenue", "売上", "四半期の売上を集計する", domain.StatusDraft)
	if err := s.Create(ctx, k); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Read-your-writes: Get sees it immediately.
	got, err := s.Get(ctx, "metrics/revenue")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Title != "売上" || got.CreatedAt.IsZero() {
		t.Errorf("unexpected entry: %+v", got)
	}
	// Create-only: a second create on the same id is rejected.
	if err := s.Create(ctx, mkK("metrics/revenue", "dup", "x", domain.StatusDraft)); err != ErrAlreadyExists {
		t.Errorf("duplicate create err = %v, want ErrAlreadyExists", err)
	}
}

func TestUpdateOptimisticLockAndCAS(t *testing.T) {
	ctx := context.Background()
	s := newTestStore()
	k := mkK("a", "A", "body", domain.StatusDraft)
	if err := s.Create(ctx, k); err != nil {
		t.Fatalf("create: %v", err)
	}
	cur, _ := s.Get(ctx, "a")

	// Stale ifMatch is rejected (the #108 optimistic lock).
	stale := cur.UpdatedAt.Add(-time.Hour)
	if err := s.Update(ctx, mkK("a", "A2", "body2", domain.StatusDraft), cur.CreatedBy, &stale); err != ErrConflict {
		t.Errorf("stale ifMatch err = %v, want ErrConflict", err)
	}
	// Correct ifMatch succeeds.
	up := mkK("a", "A2", "body2", domain.StatusDraft)
	if err := s.Update(ctx, up, cur.CreatedBy, &cur.UpdatedAt); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := s.Get(ctx, "a")
	if got.Title != "A2" || !got.CreatedAt.Equal(cur.CreatedAt) {
		t.Errorf("update lost fields: %+v", got)
	}
	// Missing entry.
	if err := s.Update(ctx, mkK("nope", "x", "y", domain.StatusDraft), cur.CreatedBy, nil); err != ErrNotFound {
		t.Errorf("update missing err = %v, want ErrNotFound", err)
	}
}

func TestSoftDeleteRemovesFromReadsAndSearch(t *testing.T) {
	ctx := context.Background()
	s := newTestStore()
	_ = s.Create(ctx, mkK("gone", "消える", "検索対象の本文", domain.StatusDraft))
	if _, err := s.Get(ctx, "gone"); err != nil {
		t.Fatalf("precondition get: %v", err)
	}
	if err := s.SoftDelete(ctx, "gone", domain.Actor{Kind: "human", Name: "t"}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Get(ctx, "gone"); err != ErrNotFound {
		t.Errorf("get after delete = %v, want ErrNotFound", err)
	}
	hits, _ := s.SearchLexical(ctx, "検索対象", Filter{}, 10)
	if len(hits) != 0 {
		t.Errorf("deleted entry still in search: %+v", hits)
	}
}

// TestRefreshPicksUpForeignWrites simulates a second instance: writes land
// in the object store directly, and a Refresh must project them (0026 §5.2).
func TestRefreshPicksUpForeignWrites(t *testing.T) {
	ctx := context.Background()
	os := NewMemObjectStore()
	a := New(os) // instance A
	b := New(os) // instance B, shares the same objects

	// A creates an entry; B hasn't refreshed yet.
	if err := a.Create(ctx, mkK("x", "タイトル", "ベクトル検索の本文", domain.StatusVerified)); err != nil {
		t.Fatalf("create: %v", err)
	}
	// B's search refreshes from LIST and finds it.
	hits, err := b.SearchLexical(ctx, "ベクトル検索", Filter{}, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != "x" {
		t.Fatalf("B did not project A's write: %+v", hits)
	}
	// A deletes it; B's next refresh drops it.
	if err := a.SoftDelete(ctx, "x", domain.Actor{Kind: "human", Name: "t"}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	hits, _ = b.SearchLexical(ctx, "ベクトル検索", Filter{}, 10)
	if len(hits) != 0 {
		t.Errorf("B still sees deleted entry: %+v", hits)
	}
}
