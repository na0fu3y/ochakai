package store

import (
	"context"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/testdb"
)

// GCS-only blob storage (design doc 0013): attachment bytes live only in
// the blob store, and an instance without one refuses attachment
// operations.
func TestIntegrationBlobStoreOnly(t *testing.T) {
	dbURL := testdb.URL(t)
	lockLiveAttachments(t, dbURL)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	fake := newFakeBlobStore()
	s.UseBlobStore(fake)
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	for _, del := range []string{
		`DELETE FROM attachment WHERE knowledge_id LIKE 'it-ext%'`,
		// The files too — an entry's namespace outlives it (0046 §3.3).
		`DELETE FROM object WHERE id LIKE 'it-ext%' OR starts_with(path, 'it-ext')`,
		`DELETE FROM knowledge_revision WHERE id LIKE 'it-ext%'`,
	} {
		if _, err := s.pool.Exec(ctx, del); err != nil {
			t.Fatal(err)
		}
	}
	// And again on the way out, inside the lock this test holds: the
	// rows would otherwise outlive it, and another package's
	// whole-bundle export would resolve this test's file against a blob
	// fake it does not have. Deferred rather than t.Cleanup so it runs
	// before the pool closes.
	defer func() {
		if _, err := s.pool.Exec(ctx,
			`DELETE FROM object WHERE id LIKE 'it-ext%' OR starts_with(path, 'it-ext')`); err != nil {
			t.Errorf("cleaning up: %v", err)
		}
	}()

	actor := domain.Actor{Kind: "human", Name: "test"}
	k := &domain.Knowledge{
		Type: domain.TypeInsights, ID: "it-ext-reading", Title: "GCS一本化テスト",
		Status: domain.StatusDraft, CreatedBy: actor,
	}
	if err := s.Create(ctx, k, false); err != nil {
		t.Fatal(err)
	}
	// Leave no live attachment behind: the bytes below exist only in this
	// test's blob fake, and other packages' export scans resolve every
	// live attachment against their own. Runs before the deferred Close.
	defer func() { _ = s.SoftDelete(ctx, k.ID, actor, nil, "") }()

	// Migration 0009 dropped the bytea column.
	var hasBytes bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'blob' AND column_name = 'bytes')`).Scan(&hasBytes); err != nil {
		t.Fatal(err)
	}
	if hasBytes {
		t.Fatal("migration 0009 should have dropped blob.bytes")
	}

	// Attach → the bytes live in the blob store, metadata in the blob row.
	png := append([]byte("\x89PNG\r\n\x1a\n"), []byte("gcs only bytes")...)
	att, _, err := s.PutFile(ctx, k.ID+"/chart.png", "image/png", png, actor)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := fake.m[att.SHA256]; !ok {
		t.Error("attach should upload to the blob store")
	}
	if _, data, err := s.GetFile(ctx, k.ID+"/chart.png"); err != nil || string(data) != string(png) {
		t.Errorf("attachment did not round-trip: %v", err)
	}

	// The export path finds the attachment in its snapshot and resolves
	// the bytes from the blob store, one at a time.
	snap, err := s.BeginExport(ctx)
	if err != nil {
		t.Fatal(err)
	}
	metas, err := snap.FileMeta(ctx, "")
	snap.Close(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range metas {
		if m.Path != k.ID+"/chart.png" {
			continue
		}
		data, err := s.AttachmentBytes(ctx, m.SHA256)
		if err != nil {
			t.Fatalf("AttachmentBytes: %v", err)
		}
		if string(data) != string(png) {
			t.Errorf("export read %q, want %q", data, png)
		}
		found = true
	}
	if !found {
		t.Error("the export snapshot did not list the attachment")
	}

	// Without a blob store, writes and reads refuse with the config hint.
	bare, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer bare.Close()
	if _, _, err := bare.PutFile(ctx, k.ID+"/other.png", "image/png", png, actor); err == nil ||
		!strings.Contains(err.Error(), "OCHAKAI_GCS_BUCKET") {
		t.Errorf("attach without a blob store = %v, want config hint", err)
	}
	if _, _, err := bare.GetFile(ctx, k.ID+"/chart.png"); err == nil ||
		!strings.Contains(err.Error(), "OCHAKAI_GCS_BUCKET") {
		t.Errorf("read without a blob store = %v, want config hint", err)
	}
}
