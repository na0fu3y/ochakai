package store

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/testdb"
)

// lockLiveAttachments serializes, across the test packages sharing the
// test database, the tests that hold live attachments or scan them all
// (OKF export, ExportSnapshot.AttachmentMeta): bytes resolve against each
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
	dbURL := testdb.URL(t)
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
		`DELETE FROM attachment_embedding WHERE path LIKE 'it-attsearch%'`,
		`DELETE FROM attachment WHERE knowledge_id LIKE 'it-attsearch%'`,
		// Files are objects with paths, not properties of an entry
		// (design doc 0046 §3.3): deleting the concepts leaves them
		// live, and the whole-bundle export another package runs then
		// resolves their bytes against a blob fake it does not have.
		`DELETE FROM object WHERE id LIKE 'it-attsearch%' OR starts_with(path, 'it-attsearch')`,
		`DELETE FROM knowledge_revision WHERE id LIKE 'it-attsearch%'`,
	} {
		if _, err := s.pool.Exec(ctx, del); err != nil {
			t.Fatal(err)
		}
	}
	// And again on the way out, inside the lock this test holds: the
	// files would otherwise outlive it, and another package's
	// whole-bundle export would resolve their bytes against a blob fake
	// it does not have. Deferred rather than t.Cleanup so it runs before
	// the pool closes.
	defer func() {
		if _, err := s.pool.Exec(ctx,
			`DELETE FROM object WHERE id LIKE 'it-attsearch%' OR starts_with(path, 'it-attsearch')`); err != nil {
			t.Errorf("cleaning up: %v", err)
		}
	}()

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
		if err := s.Create(ctx, k, false); err != nil {
			t.Fatal(err)
		}
	}
	// Leave no live attachments behind: TestIntegrationBlobStoreOnly's
	// The export scan resolves every live attachment against its own
	// blob fake and would trip over this test's leftovers.
	defer func() {
		_ = s.SoftDelete(ctx, a.ID, actor, nil)
		_ = s.SoftDelete(ctx, b.ID, actor, nil)
	}()
	if _, _, err := s.PutFile(ctx, a.ID+"/sales-seeds.txt", "text/plain", []byte("region,amount\n"), actor); err != nil {
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
	if _, _, err := s.PutFile(ctx, a.ID+"/notes.txt", "text/plain", []byte("misc notes"), actor); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.PutFile(ctx, b.ID+"/other.txt", "text/plain", []byte("other"), actor); err != nil {
		t.Fatal(err)
	}
	for _, e := range []struct {
		path string
		vec  []float32
	}{
		{a.ID + "/sales-seeds.txt", []float32{1, 0, 0, 0}},
		{a.ID + "/notes.txt", []float32{0, 1, 0, 0}},
		{b.ID + "/other.txt", []float32{0, 0, 1, 0}},
	} {
		if err := s.UpsertFileEmbedding(ctx, e.path, "test-model", e.vec); err != nil {
			t.Fatalf("UpsertFileEmbedding(%s): %v", e.path, err)
		}
	}
	vhits, err := s.SearchVectorAttachments(ctx, []float32{0.9, 0.1, 0.1, 0}, "test-model", Filter{}, 5)
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

	countEmbeddings := func(path string) int {
		var n int
		if err := s.pool.QueryRow(ctx,
			`SELECT count(*) FROM attachment_embedding WHERE path=$1`, path).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// Replacing an attachment invalidates its vector: the row is dropped
	// in the same transaction, re-embedding is the writer's follow-up.
	if _, _, err := s.PutFile(ctx, a.ID+"/sales-seeds.txt", "text/plain", []byte("region,amount,updated\n"), actor); err != nil {
		t.Fatal(err)
	}
	if n := countEmbeddings(a.ID + "/sales-seeds.txt"); n != 0 {
		t.Errorf("replace kept %d stale embedding rows, want 0", n)
	}

	// Detach drops the vector with the mapping.
	if err := s.UpsertFileEmbedding(ctx, a.ID+"/sales-seeds.txt", "test-model", []float32{1, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteFile(ctx, a.ID+"/sales-seeds.txt", actor); err != nil {
		t.Fatal(err)
	}
	if n := countEmbeddings(a.ID + "/sales-seeds.txt"); n != 0 {
		t.Errorf("detach kept %d embedding rows, want 0", n)
	}

	// A soft-deleted entry's attachment vectors stay stored (revival
	// restores them, design doc 0020) but never surface in search.
	if err := s.SoftDelete(ctx, b.ID, actor, nil); err != nil {
		t.Fatal(err)
	}
	if n := countEmbeddings(b.ID + "/other.txt"); n != 1 {
		t.Errorf("soft delete removed attachment embeddings: got %d rows, want 1", n)
	}
	vhits, err = s.SearchVectorAttachments(ctx, []float32{0, 0, 1, 0}, "test-model", Filter{}, 5)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range vhits {
		if h.ID == b.ID {
			t.Error("soft-deleted entry surfaced via attachment vector search")
		}
	}
}

// Two files of the same name, in two directories, both shown by one
// concept — which is exactly what the key this table used to carry could
// not express. (concept, last path segment) named one row for both, so
// the second vector overwrote the first and one of the two files was
// silently unsearchable. Keyed by path, both are stored and both rank
// (design doc 0091).
//
// The second half is the other thing the path key buys: attribution is a
// property of the bundle, read at search time, so a file in a shared
// directory ranks every concept whose body names it rather than the one
// that happened to upload it.
func TestIntegrationAttachmentVectorsAreKeyedByPath(t *testing.T) {
	dbURL := testdb.URL(t)
	lockLiveAttachments(t, dbURL)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.UseBlobStore(newFakeBlobStore())
	if err := s.Migrate(ctx, 4); err != nil {
		t.Fatal(err)
	}
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "t"}
	base := testdb.Unique(t, "it-attkey-")
	defer func() {
		_, _ = s.pool.Exec(ctx, `DELETE FROM attachment_embedding WHERE starts_with(path, $1)`, base)
		_, _ = s.pool.Exec(ctx, `DELETE FROM object WHERE starts_with(path, $1)`, base)
		_, _ = s.pool.Exec(ctx, `DELETE FROM knowledge_revision WHERE starts_with(path, $1)`, base)
	}()

	charts, tables := base+"/charts/q1.png", base+"/tables/q1.png"
	shared := base + "/shared/orders.csv"
	// One concept shows both files called q1.png; a second shows only the
	// shared CSV, which the first names too.
	one := &domain.Knowledge{
		Type: domain.TypeMetrics, ID: base + "/one", Title: "売上",
		Status: domain.StatusDraft, CreatedBy: actor, UpdatedBy: actor,
		Body: "![c](/" + charts + ")\n![t](/" + tables + ")\n[csv](/" + shared + ")\n",
	}
	two := &domain.Knowledge{
		Type: domain.TypeMetrics, ID: base + "/two", Title: "利益",
		Status: domain.StatusDraft, CreatedBy: actor, UpdatedBy: actor,
		Body: "[csv](/" + shared + ")\n",
	}
	for _, k := range []*domain.Knowledge{one, two} {
		if err := s.Create(ctx, k, false); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []struct {
		path string
		vec  []float32
	}{
		{charts, []float32{1, 0, 0, 0}},
		{tables, []float32{0, 1, 0, 0}},
		{shared, []float32{0, 0, 1, 0}},
	} {
		if _, _, err := s.PutFile(ctx, f.path, "text/plain", []byte(f.path), actor); err != nil {
			t.Fatal(err)
		}
		if err := s.UpsertFileEmbedding(ctx, f.path, "test-model", f.vec); err != nil {
			t.Fatal(err)
		}
	}
	var rows int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM attachment_embedding WHERE starts_with(path, $1)`, base).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 3 {
		t.Fatalf("stored vectors = %d, want 3 (two files named q1.png and the shared CSV)", rows)
	}

	// Both q1.png files rank their concept: under the old key one of
	// these two searches would have missed.
	for _, q := range [][]float32{{1, 0, 0, 0}, {0, 1, 0, 0}} {
		hits, err := s.SearchVectorAttachments(ctx, q, "test-model", Filter{}, 5)
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) == 0 || hits[0].ID != one.ID {
			t.Errorf("query %v: top hit = %+v, want %s", q, hits, one.ID)
		}
	}

	// One file, two concepts that name it, one vector: both rank.
	hits, err := s.SearchVectorAttachments(ctx, []float32{0, 0, 1, 0}, "test-model", Filter{}, 5)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, h := range hits {
		got[h.ID] = true
	}
	if !got[one.ID] || !got[two.ID] {
		t.Errorf("the shared file ranked %v, want both %s and %s", hits, one.ID, two.ID)
	}
}
