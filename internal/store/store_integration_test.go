package store

import (
	"context"
	"os"
	"testing"

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

	k := &domain.Knowledge{
		Type: domain.TypeMetric, ID: "it-revenue", Title: "売上",
		Description: "統合テスト用", Status: domain.StatusVerified,
		CreatedBy: domain.Actor{Kind: "human", Name: "test"},
	}
	_ = s.SoftDelete(ctx, k.Type, k.ID, k.CreatedBy) // clean rerun
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
