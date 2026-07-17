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

// ListLinkingTo powers get_context's reverse-link expansion: entries
// whose links point at the target must surface — in both the bare and
// the ochakai:// target forms — and soft-deleted entries must not.
func TestIntegrationListLinkingTo(t *testing.T) {
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
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"knowledge", "knowledge_revision"} {
		if _, err := s.pool.Exec(ctx, `DELETE FROM `+table+` WHERE id LIKE 'it-link-%'`); err != nil {
			t.Fatal(err)
		}
	}

	actor := domain.Actor{Kind: "human", Name: "test"}
	entries := []*domain.Knowledge{
		{Type: domain.TypeMetric, ID: "it-link-metric", Title: "target", Status: domain.StatusDraft, CreatedBy: actor},
		{Type: domain.TypeInsight, ID: "it-link-bare", Title: "bare link", Status: domain.StatusDraft, CreatedBy: actor,
			Links: []domain.Link{{Rel: "explains", Target: "metric/it-link-metric"}}},
		{Type: domain.TypeInsight, ID: "it-link-uri", Title: "uri link", Status: domain.StatusDraft, CreatedBy: actor,
			Links: []domain.Link{{Rel: "explains", Target: "ochakai://metric/it-link-metric"}}},
		{Type: domain.TypeInsight, ID: "it-link-gone", Title: "deleted link", Status: domain.StatusDraft, CreatedBy: actor,
			Links: []domain.Link{{Rel: "explains", Target: "metric/it-link-metric"}}},
	}
	for _, k := range entries {
		if err := s.Create(ctx, k); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SoftDelete(ctx, domain.TypeInsight, "it-link-gone", actor); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListLinkingTo(ctx, domain.TypeMetric, "it-link-metric", 10)
	if err != nil {
		t.Fatalf("ListLinkingTo: %v", err)
	}
	ids := map[string]bool{}
	for _, k := range got {
		ids[k.ID] = true
	}
	if len(got) != 2 || !ids["it-link-bare"] || !ids["it-link-uri"] {
		t.Errorf("ListLinkingTo = %v, want it-link-bare and it-link-uri", ids)
	}
}

// Create over a soft-deleted entry revives it — otherwise the ID is dead
// forever (the deleted row still owns the primary key while Update
// refuses deleted rows). Live entries, including rejected ones, still
// conflict.
func TestIntegrationCreateRevivesSoftDeleted(t *testing.T) {
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
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"knowledge", "knowledge_revision"} {
		if _, err := s.pool.Exec(ctx, `DELETE FROM `+table+` WHERE id LIKE 'it-revive-%'`); err != nil {
			t.Fatal(err)
		}
	}

	actor := domain.Actor{Kind: "human", Name: "test"}
	first := &domain.Knowledge{
		Type: domain.TypeTerm, ID: "it-revive-me", Title: "first life",
		Status: domain.StatusDraft, CreatedBy: actor,
	}
	if err := s.Create(ctx, first); err != nil {
		t.Fatal(err)
	}

	// Live entry: create must still conflict.
	dup := &domain.Knowledge{
		Type: domain.TypeTerm, ID: "it-revive-me", Title: "imposter",
		Status: domain.StatusDraft, CreatedBy: actor,
	}
	if err := s.Create(ctx, dup); err != ErrAlreadyExists {
		t.Fatalf("create over a live entry = %v, want ErrAlreadyExists", err)
	}

	if err := s.SoftDelete(ctx, first.Type, first.ID, actor); err != nil {
		t.Fatal(err)
	}

	// Soft-deleted entry: create revives it with the new content.
	second := &domain.Knowledge{
		Type: domain.TypeTerm, ID: "it-revive-me", Title: "second life",
		Status: domain.StatusDraft, CreatedBy: domain.Actor{Kind: "agent", Name: "claude-code"},
	}
	if err := s.Create(ctx, second); err != nil {
		t.Fatalf("create over a soft-deleted entry: %v", err)
	}
	got, err := s.Get(ctx, second.Type, second.ID)
	if err != nil {
		t.Fatalf("revived entry not readable: %v", err)
	}
	if got.Title != "second life" || got.CreatedBy.Name != "claude-code" {
		t.Errorf("revived entry = %+v, want the new content and creator", got)
	}

	// The full lineage stays: create, delete, create.
	var changes []string
	rows, err := s.pool.Query(ctx,
		`SELECT change FROM knowledge_revision WHERE type=$1 AND id=$2 ORDER BY rev`,
		second.Type, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatal(err)
		}
		changes = append(changes, c)
	}
	want := []string{"create", "delete", "create"}
	if len(changes) != len(want) {
		t.Fatalf("revisions = %v, want %v", changes, want)
	}
	for i := range want {
		if changes[i] != want[i] {
			t.Fatalf("revisions = %v, want %v", changes, want)
		}
	}

	// Rejected entries are live: the memory of no is not overwritable.
	rejected := &domain.Knowledge{
		Type: domain.TypeTerm, ID: "it-revive-no", Title: "rejected",
		Status: domain.StatusRejected, StatusNote: "duplicate",
		CreatedBy: actor, RejectedBy: &actor,
	}
	if err := s.Create(ctx, rejected); err != nil {
		t.Fatal(err)
	}
	again := &domain.Knowledge{
		Type: domain.TypeTerm, ID: "it-revive-no", Title: "try again",
		Status: domain.StatusDraft, CreatedBy: actor,
	}
	if err := s.Create(ctx, again); err != ErrAlreadyExists {
		t.Fatalf("create over a rejected entry = %v, want ErrAlreadyExists", err)
	}
}
