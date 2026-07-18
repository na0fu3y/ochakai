package store

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
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

	// Outcome reporting: worked/failed events with a note feed the same
	// totals (issue #41).
	if err := s.RecordOutcome(ctx, domain.EventFailed, actor, target[0], "joins dropped 2024 rows"); err != nil {
		t.Fatalf("RecordOutcome: %v", err)
	}
	if err := s.RecordOutcome(ctx, domain.EventWorked, actor, target[0], ""); err != nil {
		t.Fatalf("RecordOutcome: %v", err)
	}
	usage, err = s.Usage(ctx, k.Type, k.ID)
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if usage.Worked < 1 || usage.Failed < 1 {
		t.Errorf("outcome totals wrong: %+v", usage)
	}
	var note string
	if err := s.pool.QueryRow(ctx,
		`SELECT note FROM knowledge_event WHERE knowledge_id = $1 AND event = $2`,
		k.ID, domain.EventFailed).Scan(&note); err != nil {
		t.Fatalf("read outcome note: %v", err)
	}
	if note != "joins dropped 2024 rows" {
		t.Errorf("outcome note did not round-trip: %q", note)
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

	// ListByUsage: the draft review feed. Two drafts, unequal demand — the
	// more-searched one ranks first, and each hit carries its usage totals.
	hot := &domain.Knowledge{
		Type: domain.TypeInsight, ID: "it-draft-hot", Title: "よく検索される草案",
		Status: domain.StatusDraft, CreatedBy: domain.Actor{Kind: "agent", Name: "claude-code"},
	}
	cold := &domain.Knowledge{
		Type: domain.TypeInsight, ID: "it-draft-cold", Title: "検索されない草案",
		Status: domain.StatusDraft, CreatedBy: domain.Actor{Kind: "agent", Name: "claude-code"},
	}
	for _, d := range []*domain.Knowledge{hot, cold} {
		if err := s.Create(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3; i++ {
		if err := s.RecordEvents(ctx, domain.EventSearchHit, actor, []EventTarget{{Type: hot.Type, ID: hot.ID}}); err != nil {
			t.Fatalf("RecordEvents: %v", err)
		}
	}
	if err := s.RecordOutcome(ctx, domain.EventFailed, actor, EventTarget{Type: hot.Type, ID: hot.ID}, ""); err != nil {
		t.Fatalf("RecordOutcome: %v", err)
	}
	feed, err := s.ListByUsage(ctx, Filter{
		Types:    []domain.Type{domain.TypeInsight},
		Statuses: []domain.Status{domain.StatusDraft},
	}, 100)
	if err != nil {
		t.Fatalf("ListByUsage: %v", err)
	}
	if len(feed) < 2 || feed[0].ID != hot.ID {
		t.Errorf("most-searched draft must come first: %+v", feed)
	}
	if feed[0].Usage == nil || feed[0].Usage.SearchHits != 3 || feed[0].Usage.Failed != 1 {
		t.Errorf("hit must carry usage totals (search_hits=3, failed=1): %+v", feed[0].Usage)
	}
	if feed[0].Score != 0 {
		t.Errorf("listing hits carry score 0, got %v", feed[0].Score)
	}
	var sawCold bool
	for _, h := range feed {
		if h.ID == cold.ID {
			sawCold = true
			if h.Usage == nil || h.Usage.SearchHits != 0 {
				t.Errorf("never-searched draft must report 0 search_hits: %+v", h.Usage)
			}
		}
	}
	if !sawCold {
		t.Error("ListByUsage must include never-used drafts (the inventory tail)")
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

// Attachments (design doc 0008): content-addressed blobs, replace-by-name,
// revisions on attach/detach, and disappearance with the soft-deleted entry.
func TestIntegrationAttachments(t *testing.T) {
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
	for _, del := range []string{
		`DELETE FROM attachment WHERE knowledge_id LIKE 'it-att%'`,
		`DELETE FROM knowledge WHERE id LIKE 'it-att%'`,
		`DELETE FROM knowledge_revision WHERE id LIKE 'it-att%'`,
	} {
		if _, err := s.pool.Exec(ctx, del); err != nil {
			t.Fatal(err)
		}
	}

	actor := domain.Actor{Kind: "human", Name: "test"}
	k := &domain.Knowledge{
		Type: domain.TypeInsight, ID: "it-att-reading", Title: "売上の読み方",
		Status: domain.StatusDraft, CreatedBy: actor,
	}
	if err := s.Create(ctx, k); err != nil {
		t.Fatal(err)
	}

	png := append([]byte("\x89PNG\r\n\x1a\n"), []byte("fake image bytes")...)
	att, err := s.PutAttachment(ctx, k.Type, k.ID, "weekly.png", "image/png", "", png, actor)
	if err != nil {
		t.Fatalf("PutAttachment: %v", err)
	}
	if att.Size != int64(len(png)) || att.SHA256 == "" {
		t.Errorf("attachment metadata wrong: %+v", att)
	}

	got, data, err := s.GetAttachment(ctx, k.Type, k.ID, "weekly.png")
	if err != nil {
		t.Fatalf("GetAttachment: %v", err)
	}
	if string(data) != string(png) || got.MediaType != "image/png" {
		t.Errorf("bytes did not round-trip: %+v", got)
	}

	// Replace by name: same entry, same name, new bytes.
	png2 := append([]byte("\x89PNG\r\n\x1a\n"), []byte("updated bytes")...)
	if _, err := s.PutAttachment(ctx, k.Type, k.ID, "weekly.png", "image/png", "", png2, actor); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListAttachments(ctx, k.Type, k.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].SHA256 == att.SHA256 {
		t.Errorf("replace should keep one attachment with new content: %+v", list)
	}

	// The dedup blob store keeps both contents (revision history can
	// reference the old hash).
	var blobs int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM blob WHERE sha256 IN ($1, $2)`, att.SHA256, list[0].SHA256).Scan(&blobs); err != nil {
		t.Fatal(err)
	}
	if blobs != 2 {
		t.Errorf("blob count = %d, want 2", blobs)
	}

	// Attach/detach are revisions on the entry.
	if err := s.DeleteAttachment(ctx, k.Type, k.ID, "weekly.png", actor); err != nil {
		t.Fatalf("DeleteAttachment: %v", err)
	}
	var changes []string
	rows, err := s.pool.Query(ctx,
		`SELECT change FROM knowledge_revision WHERE type=$1 AND id=$2 ORDER BY rev`, k.Type, k.ID)
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
	want := []string{"create", "attach", "attach", "detach"}
	if len(changes) != len(want) {
		t.Fatalf("revisions = %v, want %v", changes, want)
	}

	// Attachments of soft-deleted entries are unreachable.
	if _, err := s.PutAttachment(ctx, k.Type, k.ID, "weekly.png", "image/png", "", png, actor); err != nil {
		t.Fatal(err)
	}
	if err := s.SoftDelete(ctx, k.Type, k.ID, actor); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.GetAttachment(ctx, k.Type, k.ID, "weekly.png"); err != ErrNotFound {
		t.Errorf("attachment of a deleted entry = %v, want ErrNotFound", err)
	}
}

// ListRevisions returns the full audit trail newest-first, including for
// soft-deleted entries; an entry that never existed is ErrNotFound.
func TestIntegrationListRevisions(t *testing.T) {
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
		if _, err := s.pool.Exec(ctx, `DELETE FROM `+table+` WHERE id LIKE 'it-revs-%'`); err != nil {
			t.Fatal(err)
		}
	}

	actor := domain.Actor{Kind: "human", Name: "test"}
	k := &domain.Knowledge{
		Type: domain.TypeTerm, ID: "it-revs-1", Title: "v1",
		Status: domain.StatusDraft, CreatedBy: actor,
	}
	if err := s.Create(ctx, k); err != nil {
		t.Fatal(err)
	}
	k.Title = "v2"
	if err := s.Update(ctx, k, actor); err != nil {
		t.Fatal(err)
	}
	if err := s.SoftDelete(ctx, k.Type, k.ID, actor); err != nil {
		t.Fatal(err)
	}

	revs, err := s.ListRevisions(ctx, k.Type, k.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range revs {
		got = append(got, r.Change)
	}
	want := []string{"delete", "update", "create"} // newest first
	if len(got) != len(want) {
		t.Fatalf("revisions = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("revisions = %v, want %v", got, want)
		}
	}
	if revs[0].Rev != 3 || revs[2].Rev != 1 {
		t.Errorf("revs = %d..%d, want 3..1", revs[0].Rev, revs[2].Rev)
	}
	if revs[1].Snapshot.Title != "v2" || revs[2].Snapshot.Title != "v1" {
		t.Errorf("snapshot titles = %q, %q; want v2, v1", revs[1].Snapshot.Title, revs[2].Snapshot.Title)
	}
	if revs[0].ChangedBy != actor {
		t.Errorf("changed_by = %+v, want %+v", revs[0].ChangedBy, actor)
	}

	if _, err := s.ListRevisions(ctx, domain.TypeTerm, "it-revs-never-existed", 50); err != ErrNotFound {
		t.Errorf("revisions of a nonexistent entry = %v, want ErrNotFound", err)
	}
}

// fakeBlobStore is an in-memory blob.Store for exercising the external
// blob path (design doc 0011) without GCS.
type fakeBlobStore struct {
	mu sync.Mutex
	m  map[string][]byte
}

func newFakeBlobStore() *fakeBlobStore { return &fakeBlobStore{m: map[string][]byte{}} }

func (f *fakeBlobStore) Put(_ context.Context, sum, _ string, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.m[sum]; ok {
		return nil // create-only, like GCS with DoesNotExist
	}
	f.m[sum] = append([]byte(nil), data...)
	return nil
}

func (f *fakeBlobStore) Get(_ context.Context, sum string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.m[sum]
	if !ok {
		return nil, fmt.Errorf("fake blob store: %s not found", sum)
	}
	return append([]byte(nil), data...), nil
}

// External blob storage (design doc 0011): startup backfill moves inline
// bytes out, reads resolve from the blob store, new attachments skip the
// bytea column, and re-attaching inline heals a migrated blob.
func TestIntegrationExternalBlobs(t *testing.T) {
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
	for _, del := range []string{
		`DELETE FROM attachment WHERE knowledge_id LIKE 'it-ext%'`,
		`DELETE FROM knowledge WHERE id LIKE 'it-ext%'`,
		`DELETE FROM knowledge_revision WHERE id LIKE 'it-ext%'`,
	} {
		if _, err := s.pool.Exec(ctx, del); err != nil {
			t.Fatal(err)
		}
	}

	actor := domain.Actor{Kind: "human", Name: "test"}
	k := &domain.Knowledge{
		Type: domain.TypeInsight, ID: "it-ext-reading", Title: "移行テスト",
		Status: domain.StatusDraft, CreatedBy: actor,
	}
	if err := s.Create(ctx, k); err != nil {
		t.Fatal(err)
	}

	// Attach in bytea mode: bytes are inline.
	png := append([]byte("\x89PNG\r\n\x1a\n"), []byte("external blob test bytes")...)
	att, err := s.PutAttachment(ctx, k.Type, k.ID, "chart.png", "image/png", "", png, actor)
	if err != nil {
		t.Fatal(err)
	}
	var inline []byte
	if err := s.pool.QueryRow(ctx, `SELECT bytes FROM blob WHERE sha256=$1`, att.SHA256).Scan(&inline); err != nil {
		t.Fatal(err)
	}
	if inline == nil {
		t.Fatal("bytea mode should store bytes inline")
	}

	// Enable the blob store and backfill. The backfill is table-wide, so
	// restore every moved row afterwards to keep the shared test DB
	// usable for other tests and re-runs.
	fake := newFakeBlobStore()
	s.UseBlobStore(fake)
	// defer, not t.Cleanup: it must run before the deferred s.Close.
	defer func() {
		for sum, data := range fake.m {
			if _, err := s.pool.Exec(ctx,
				`UPDATE blob SET bytes=$2 WHERE sha256=$1 AND bytes IS NULL`, sum, data); err != nil {
				t.Errorf("restore blob %s: %v", sum, err)
			}
		}
	}()
	moved, err := s.MigrateBlobsOut(ctx)
	if err != nil {
		t.Fatalf("MigrateBlobsOut: %v", err)
	}
	if moved < 1 {
		t.Fatalf("moved = %d, want >= 1", moved)
	}
	if err := s.pool.QueryRow(ctx, `SELECT bytes FROM blob WHERE sha256=$1`, att.SHA256).Scan(&inline); err != nil {
		t.Fatal(err)
	}
	if inline != nil {
		t.Error("backfill should NULL the inline bytes")
	}
	if _, ok := fake.m[att.SHA256]; !ok {
		t.Error("backfill should upload the blob")
	}
	// Idempotent: a second run has nothing to do.
	if moved, err = s.MigrateBlobsOut(ctx); err != nil || moved != 0 {
		t.Errorf("second backfill = (%d, %v), want (0, nil)", moved, err)
	}

	// Reads resolve from the blob store.
	_, data, err := s.GetAttachment(ctx, k.Type, k.ID, "chart.png")
	if err != nil {
		t.Fatalf("GetAttachment after migration: %v", err)
	}
	if string(data) != string(png) {
		t.Error("migrated bytes did not round-trip")
	}

	// New attachments skip bytea entirely.
	png2 := append([]byte("\x89PNG\r\n\x1a\n"), []byte("born external")...)
	att2, err := s.PutAttachment(ctx, k.Type, k.ID, "born.png", "image/png", "", png2, actor)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT bytes FROM blob WHERE sha256=$1`, att2.SHA256).Scan(&inline); err != nil {
		t.Fatal(err)
	}
	if inline != nil {
		t.Error("with a blob store, bytes must not be stored inline")
	}
	if _, data, err = s.GetAttachment(ctx, k.Type, k.ID, "born.png"); err != nil || string(data) != string(png2) {
		t.Errorf("external attachment did not round-trip: %v", err)
	}

	// The export listing resolves external bytes too.
	all, err := s.ListAllAttachments(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, e := range all {
		if e.ID == k.ID && (e.Att.Name == "chart.png" || e.Att.Name == "born.png") && len(e.Data) > 0 {
			found++
		}
	}
	if found != 2 {
		t.Errorf("ListAllAttachments resolved %d of 2 external blobs", found)
	}

	// Without a blob store, a migrated blob is unreadable with a
	// pointed error…
	bare, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer bare.Close()
	if _, _, err := bare.GetAttachment(ctx, k.Type, k.ID, "born.png"); err == nil ||
		!strings.Contains(err.Error(), "OCHAKAI_GCS_BUCKET") {
		t.Errorf("bytea-mode read of migrated blob = %v, want config hint", err)
	}
	// …and re-attaching the same content inline heals it (0011 §2.4).
	if _, err := bare.PutAttachment(ctx, k.Type, k.ID, "born.png", "image/png", "", png2, actor); err != nil {
		t.Fatal(err)
	}
	if _, data, err = bare.GetAttachment(ctx, k.Type, k.ID, "born.png"); err != nil || string(data) != string(png2) {
		t.Errorf("healed attachment did not round-trip: %v", err)
	}
}
