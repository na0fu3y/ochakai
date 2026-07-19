package store

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// GCS-only blob storage (design doc 0013): attachment bytes live only in
// the blob store, and an instance without one refuses attachment
// operations.
func TestIntegrationBlobStoreOnly(t *testing.T) {
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
	fake := newFakeBlobStore()
	s.UseBlobStore(fake)
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
		Type: domain.TypeInsights, ID: "it-ext-reading", Title: "GCS一本化テスト",
		Status: domain.StatusDraft, CreatedBy: actor,
	}
	if err := s.Create(ctx, k); err != nil {
		t.Fatal(err)
	}

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
	att, err := s.PutAttachment(ctx, k.ID, "chart.png", "image/png", "", png, actor)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := fake.m[att.SHA256]; !ok {
		t.Error("attach should upload to the blob store")
	}
	if _, data, err := s.GetAttachment(ctx, k.ID, "chart.png"); err != nil || string(data) != string(png) {
		t.Errorf("attachment did not round-trip: %v", err)
	}

	// The export listing resolves bytes from the blob store.
	all, err := s.ListAllAttachments(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range all {
		if e.ID == k.ID && e.Att.Name == "chart.png" && string(e.Data) == string(png) {
			found = true
		}
	}
	if !found {
		t.Error("ListAllAttachments did not resolve the blob")
	}

	// Without a blob store, writes and reads refuse with the config hint.
	bare, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer bare.Close()
	if _, err := bare.PutAttachment(ctx, k.ID, "other.png", "image/png", "", png, actor); err == nil ||
		!strings.Contains(err.Error(), "OCHAKAI_GCS_BUCKET") {
		t.Errorf("attach without a blob store = %v, want config hint", err)
	}
	if _, _, err := bare.GetAttachment(ctx, k.ID, "chart.png"); err == nil ||
		!strings.Contains(err.Error(), "OCHAKAI_GCS_BUCKET") {
		t.Errorf("read without a blob store = %v, want config hint", err)
	}
}
