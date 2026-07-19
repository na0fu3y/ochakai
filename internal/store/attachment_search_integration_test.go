package store

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// lockLiveAttachments serializes, across the test packages sharing the
// test database, the tests that hold live attachments or scan them all
// (OKF export, ListAllAttachments): bytes resolve against each test's
// own in-memory blob fake, so a foreign live attachment breaks a
// whole-KB scan. Key shared with the service and restapi test packages.
func lockLiveAttachments(t *testing.T, dbURL string) {
	t.Helper()
	ctx := context.Background() // outlives t.Context()'s pre-Cleanup cancel
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(0x0c8a1a77)`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) }) // closing the session releases the lock
}

// TestIntegrationAttachmentSearch exercises attachment search (design doc
// 0020): filenames in the lexical haystack, attachment vectors mapped to
// their best owning entry, and embedding rows dropped on replace/detach.
func TestIntegrationAttachmentSearch(t *testing.T) {
	dbURL := os.Getenv("OCHAKAI_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("OCHAKAI_TEST_DATABASE_URL not set")
	}
	lockLiveAttachments(t, dbURL)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.UseBlobStore(newFakeBlobStore())
	// Dim 4 matches TestIntegration: the tables persist across runs and
	// CREATE TABLE IF NOT EXISTS keeps the first dimension.
	if err := s.Migrate(ctx, 4); err != nil {
		t.Fatal(err)
	}
	for _, del := range []string{
		`DELETE FROM attachment_embedding WHERE knowledge_id LIKE 'it-attsearch%'`,
		`DELETE FROM attachment WHERE knowledge_id LIKE 'it-attsearch%'`,
		`DELETE FROM knowledge WHERE id LIKE 'it-attsearch%'`,
		`DELETE FROM knowledge_revision WHERE id LIKE 'it-attsearch%'`,
	} {
		if _, err := s.pool.Exec(ctx, del); err != nil {
			t.Fatal(err)
		}
	}

	actor := domain.Actor{Kind: "human", Name: "test"}
	a := &domain.Knowledge{
		Type: domain.TypeMetrics, ID: "it-attsearch-a", Title: "売上",
		Status: domain.StatusDraft, CreatedBy: actor,
	}
	b := &domain.Knowledge{
		Type: domain.TypeMetrics, ID: "it-attsearch-b", Title: "利益",
		Status: domain.StatusDraft, CreatedBy: actor,
	}
	for _, k := range []*domain.Knowledge{a, b} {
		if err := s.Create(ctx, k); err != nil {
			t.Fatal(err)
		}
	}
	// Leave no live attachments behind: TestIntegrationBlobStoreOnly's
	// ListAllAttachments resolves every live attachment against its own
	// blob fake and would trip over this test's leftovers.
	defer func() {
		_ = s.SoftDelete(ctx, a.ID, actor)
		_ = s.SoftDelete(ctx, b.ID, actor)
	}()
	if _, err := s.PutAttachment(ctx, a.ID, "sales-seeds.txt", "text/plain", "", []byte("region,amount\n"), actor); err != nil {
		t.Fatal(err)
	}

	// Lexical: the filename alone finds the owning entry.
	hits, err := s.SearchLexical(ctx, "seeds", Filter{}, 5)
	if err != nil {
		t.Fatalf("SearchLexical: %v", err)
	}
	found := false
	for _, h := range hits {
		found = found || h.ID == a.ID
	}
	if !found {
		t.Errorf("SearchLexical(seeds) missed the entry carrying sales-seeds.txt: %+v", hits)
	}

	// Vector: an entry with several attachments surfaces once, scored by
	// its best match; a worse-matching entry ranks below.
	if _, err := s.PutAttachment(ctx, a.ID, "notes.txt", "text/plain", "", []byte("misc notes"), actor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutAttachment(ctx, b.ID, "other.txt", "text/plain", "", []byte("other"), actor); err != nil {
		t.Fatal(err)
	}
	for _, e := range []struct {
		id, name string
		vec      []float32
	}{
		{a.ID, "sales-seeds.txt", []float32{1, 0, 0, 0}},
		{a.ID, "notes.txt", []float32{0, 1, 0, 0}},
		{b.ID, "other.txt", []float32{0, 0, 1, 0}},
	} {
		if err := s.UpsertAttachmentEmbedding(ctx, e.id, e.name, "test-model", e.vec); err != nil {
			t.Fatalf("UpsertAttachmentEmbedding(%s/%s): %v", e.id, e.name, err)
		}
	}
	vhits, err := s.SearchVectorAttachments(ctx, []float32{0.9, 0.1, 0.1, 0}, Filter{}, 5)
	if err != nil {
		t.Fatalf("SearchVectorAttachments: %v", err)
	}
	var ids []string
	for _, h := range vhits {
		if h.ID == a.ID || h.ID == b.ID {
			ids = append(ids, h.ID)
		}
	}
	if len(ids) != 2 || ids[0] != a.ID || ids[1] != b.ID {
		t.Errorf("SearchVectorAttachments order = %v, want [%s %s] (best attachment per entry)", ids, a.ID, b.ID)
	}

	countEmbeddings := func(id, name string) int {
		var n int
		if err := s.pool.QueryRow(ctx,
			`SELECT count(*) FROM attachment_embedding WHERE knowledge_id=$1 AND name=$2`, id, name).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// Replacing an attachment invalidates its vector: the row is dropped
	// in the same transaction, re-embedding is the writer's follow-up.
	if _, err := s.PutAttachment(ctx, a.ID, "sales-seeds.txt", "text/plain", "", []byte("region,amount,updated\n"), actor); err != nil {
		t.Fatal(err)
	}
	if n := countEmbeddings(a.ID, "sales-seeds.txt"); n != 0 {
		t.Errorf("replace kept %d stale embedding rows, want 0", n)
	}

	// Detach drops the vector with the mapping.
	if err := s.UpsertAttachmentEmbedding(ctx, a.ID, "sales-seeds.txt", "test-model", []float32{1, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAttachment(ctx, a.ID, "sales-seeds.txt", actor); err != nil {
		t.Fatal(err)
	}
	if n := countEmbeddings(a.ID, "sales-seeds.txt"); n != 0 {
		t.Errorf("detach kept %d embedding rows, want 0", n)
	}

	// A soft-deleted entry's attachment vectors stay stored (revival
	// restores them, design doc 0020) but never surface in search.
	if err := s.SoftDelete(ctx, b.ID, actor); err != nil {
		t.Fatal(err)
	}
	if n := countEmbeddings(b.ID, "other.txt"); n != 1 {
		t.Errorf("soft delete removed attachment embeddings: got %d rows, want 1", n)
	}
	vhits, err = s.SearchVectorAttachments(ctx, []float32{0, 0, 1, 0}, Filter{}, 5)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range vhits {
		if h.ID == b.ID {
			t.Error("soft-deleted entry surfaced via attachment vector search")
		}
	}
}
