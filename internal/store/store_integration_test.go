package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// TestIntegration exercises the store against a real PostgreSQL with
// pgvector. Skipped unless OCHAKAI_TEST_DATABASE_URL is set, e.g.:
//
//	docker run -d --rm -p 55433:5432 -e POSTGRES_PASSWORD=t -e POSTGRES_USER=t -e POSTGRES_DB=t pgvector/pgvector:pg17
//	OCHAKAI_TEST_DATABASE_URL='postgres://t:t@localhost:55433/t?sslmode=disable' go test ./internal/store/
func TestIntegration(t *testing.T) {
	dbURL := os.Getenv("OCHAKAI_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("OCHAKAI_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 4); err != nil {
		t.Fatal(err)
	}
	// Hard-delete leftovers from prior runs (soft-deleted rows still
	// conflict on the primary key).
	for _, table := range []string{"knowledge", "knowledge_revision", "knowledge_embedding"} {
		if _, err := s.pool.Exec(ctx, `DELETE FROM `+table+` WHERE id LIKE 'it-%'`); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range []string{"knowledge_event", "knowledge_usage"} {
		if _, err := s.pool.Exec(ctx, `DELETE FROM `+table+` WHERE knowledge_id LIKE 'it-%'`); err != nil {
			t.Fatal(err)
		}
	}

	k := &domain.Knowledge{
		Type: domain.TypeMetric, ID: "it-revenue", Title: "売上",
		Description: "統合テスト用", Status: domain.StatusVerified,
		CreatedBy: domain.Actor{Kind: "human", Name: "test"},
	}
	if err := s.Create(ctx, k); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertEmbedding(ctx, k.Type, k.ID, "test-model", []float32{1, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}

	lex, err := s.SearchLexical(ctx, "売上", Filter{}, 5)
	if err != nil {
		t.Fatalf("SearchLexical: %v", err)
	}
	if len(lex) == 0 || lex[0].ID != "it-revenue" {
		t.Errorf("lexical search missed the entry: %+v", lex)
	}

	// The joined vector query must not have ambiguous column references
	// (knowledge_embedding shares type/id/updated_at with knowledge).
	vec, err := s.SearchVector(ctx, []float32{1, 0, 0, 0}, Filter{
		Types: []domain.Type{domain.TypeMetric}, Statuses: []domain.Status{domain.StatusVerified},
	}, 5)
	if err != nil {
		t.Fatalf("SearchVector: %v", err)
	}
	if len(vec) == 0 || vec[0].ID != "it-revenue" || vec[0].Score < 0.99 {
		t.Errorf("vector search wrong result: %+v", vec)
	}

	// Rejected entries: provenance round-trips, and search excludes them
	// unless the status filter asks for them.
	rejectedAt := time.Now().UTC().Truncate(time.Second)
	rej := &domain.Knowledge{
		Type: domain.TypeMetric, ID: "it-revenue-dup", Title: "売上(重複)",
		Description: "統合テスト用", Status: domain.StatusRejected,
		StatusNote: "it-revenue と重複",
		CreatedBy:  domain.Actor{Kind: "agent", Name: "claude-code"},
		RejectedBy: &domain.Actor{Kind: "human", Name: "test"}, RejectedAt: &rejectedAt,
	}
	if err := s.Create(ctx, rej); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, rej.Type, rej.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.StatusRejected || got.StatusNote != "it-revenue と重複" ||
		got.RejectedBy == nil || got.RejectedBy.Name != "test" || got.RejectedAt == nil {
		t.Errorf("rejection fields did not round-trip: %+v", got)
	}
	def, err := s.SearchLexical(ctx, "売上", Filter{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range def {
		if h.ID == rej.ID {
			t.Error("default search must exclude rejected entries")
		}
	}
	only, err := s.SearchLexical(ctx, "売上", Filter{Statuses: []domain.Status{domain.StatusRejected}}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(only) != 1 || only[0].ID != rej.ID {
		t.Errorf("status=rejected filter should return the rejected entry: %+v", only)
	}

	// Usage recording: raw events plus running totals.
	actor := domain.Actor{Kind: "agent", Name: "claude-code"}
	target := []EventTarget{{Type: k.Type, ID: k.ID}}
	if err := s.RecordEvents(ctx, domain.EventSearchHit, actor, target); err != nil {
		t.Fatalf("RecordEvents: %v", err)
	}
	if err := s.RecordEvents(ctx, domain.EventSearchHit, actor, target); err != nil {
		t.Fatalf("RecordEvents: %v", err)
	}
	if err := s.RecordEvents(ctx, domain.EventFetched, actor, target); err != nil {
		t.Fatalf("RecordEvents: %v", err)
	}
	usage, err := s.Usage(ctx, k.Type, k.ID)
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if usage.SearchHits < 2 || usage.Fetches < 1 || usage.LastUsedAt == nil {
		t.Errorf("usage totals wrong: %+v", usage)
	}

	// ListByVerifiedAt: oldest verification first, rejected excluded by
	// the default filter.
	oldAt := time.Now().UTC().Add(-365 * 24 * time.Hour)
	older := &domain.Knowledge{
		Type: domain.TypeQuery, ID: "it-old-query", Title: "古い検証済みクエリ",
		Status:     domain.StatusVerified,
		CreatedBy:  domain.Actor{Kind: "human", Name: "test"},
		VerifiedBy: &domain.Actor{Kind: "human", Name: "test"}, VerifiedAt: &oldAt,
	}
	if err := s.Create(ctx, older); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListByVerifiedAt(ctx, Filter{Statuses: []domain.Status{domain.StatusVerified}}, 100)
	if err != nil {
		t.Fatalf("ListByVerifiedAt: %v", err)
	}
	if len(list) < 2 || list[0].ID != older.ID {
		t.Errorf("oldest verification must come first: %+v", list)
	}
}

// SoftDelete must survive a database where semantic search was never
// enabled: knowledge_embedding does not exist, and the failed DELETE used
// to abort the surrounding transaction (25P02) before the revision insert.
func TestIntegrationSoftDeleteWithoutEmbeddingTable(t *testing.T) {
	dbURL := os.Getenv("OCHAKAI_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("OCHAKAI_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil { // trigram-only: no embedding table
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `DROP TABLE IF EXISTS knowledge_embedding`); err != nil {
		t.Fatal(err)
	}

	k := &domain.Knowledge{
		Type: domain.TypeTerm, ID: "it-delete-me", Title: "delete me",
		Status: domain.StatusDraft, CreatedBy: domain.Actor{Kind: "human", Name: "test"},
	}
	_ = s.SoftDelete(ctx, k.Type, k.ID, k.CreatedBy) // clean rerun
	if err := s.Create(ctx, k); err != nil {
		t.Fatal(err)
	}
	if err := s.SoftDelete(ctx, k.Type, k.ID, k.CreatedBy); err != nil {
		t.Fatalf("SoftDelete without knowledge_embedding: %v", err)
	}
	if _, err := s.Get(ctx, k.Type, k.ID); err == nil {
		t.Error("entry still visible after SoftDelete")
	}
}
