package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/store"
)

// TestUpdateNoOpIntegration exercises the no-op update path against a real
// PostgreSQL: a content-identical update must write nothing — no revision,
// no updated_at bump — so recurring imports stop flooding the audit trail.
// Skipped unless OCHAKAI_TEST_DATABASE_URL is set (see store integration
// test for the docker one-liner).
func TestUpdateNoOpIntegration(t *testing.T) {
	dbURL := os.Getenv("OCHAKAI_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("OCHAKAI_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	s, err := store.New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// embedDim 0: this test needs no embeddings, so plain PostgreSQL
	// (pg_trgm only, no pgvector) is enough.
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	svc := &Service{Store: s, Log: slog.New(slog.DiscardHandler)}
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "test"}
	// The ID is unique per run: entries stay live after the test, and the
	// service layer has no hard delete to clean up with.
	id := fmt.Sprintf("svcit-%d", time.Now().UnixNano())

	entry := func() *domain.Knowledge {
		return &domain.Knowledge{
			Type: domain.TypeMetric, ID: id, Title: "売上",
			Description: "統合テスト用", Tags: []string{"sales"},
			Status: domain.StatusDraft,
			Links:  []domain.Link{{Rel: "defined_in", Target: "model/sales"}},
			Attrs:  map[string]any{"threshold": 5},
			Body:   "受注合計。",
		}
	}
	if _, err := svc.Create(ctx, entry(), actor); err != nil {
		t.Fatal(err)
	}
	// Read the baseline back through the store: PostgreSQL timestamptz is
	// microsecond precision, so the Create return value's nanosecond
	// time.Now() would never equal a value round-tripped through the DB.
	created, err := svc.Get(ctx, domain.TypeMetric, id)
	if err != nil {
		t.Fatal(err)
	}

	// Same content, but shaped like a re-imported payload: no server
	// fields, status omitted, the attr number decoded as float64 (JSON).
	same := entry()
	same.Status = ""
	same.Attrs = map[string]any{"threshold": float64(5)}
	got, changed, err := svc.Update(ctx, same, actor)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("content-identical update reported changed=true")
	}
	if !got.UpdatedAt.Equal(created.UpdatedAt) {
		t.Errorf("no-op update bumped updated_at: %v -> %v", created.UpdatedAt, got.UpdatedAt)
	}
	revs, err := svc.Revisions(ctx, domain.TypeMetric, id, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 1 {
		t.Errorf("no-op update wrote a revision: got %d revisions, want 1", len(revs))
	}

	// A real change still writes: revision recorded, updated_at bumped.
	edited := entry()
	edited.Body = "受注合計。返品は含まない。"
	got, changed, err = svc.Update(ctx, edited, actor)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("content change reported changed=false")
	}
	if !got.UpdatedAt.After(created.UpdatedAt) {
		t.Errorf("real update did not bump updated_at: %v", got.UpdatedAt)
	}
	if revs, err = svc.Revisions(ctx, domain.TypeMetric, id, 10); err != nil {
		t.Fatal(err)
	}
	if len(revs) != 2 || revs[0].Change != "update" {
		t.Errorf("real update must add an update revision: %+v", revs)
	}
}
