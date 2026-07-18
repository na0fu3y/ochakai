package store

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// GCS-only blob storage (design doc 0013): attachment bytes live only in
// the blob store, an instance without one refuses attachment operations,
// and the legacy bytea backfill (MigrateBlobsOut) still moves inline rows
// out before migration 0009 drops the column.
func TestIntegrationBlobStoreOnly(t *testing.T) {
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
		Type: domain.TypeInsight, ID: "it-ext-reading", Title: "GCS一本化テスト",
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
	att, err := s.PutAttachment(ctx, k.Type, k.ID, "chart.png", "image/png", "", png, actor)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := fake.m[att.SHA256]; !ok {
		t.Error("attach should upload to the blob store")
	}
	if _, data, err := s.GetAttachment(ctx, k.Type, k.ID, "chart.png"); err != nil || string(data) != string(png) {
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
	if _, err := bare.PutAttachment(ctx, k.Type, k.ID, "other.png", "image/png", "", png, actor); err == nil ||
		!strings.Contains(err.Error(), "OCHAKAI_GCS_BUCKET") {
		t.Errorf("attach without a blob store = %v, want config hint", err)
	}
	if _, _, err := bare.GetAttachment(ctx, k.Type, k.ID, "chart.png"); err == nil ||
		!strings.Contains(err.Error(), "OCHAKAI_GCS_BUCKET") {
		t.Errorf("read without a blob store = %v, want config hint", err)
	}

	// Legacy backfill: simulate a pre-0013 database by re-adding the bytea
	// column with one inline row, and verify MigrateBlobsOut moves it out
	// (idempotently) — the step a real upgrade runs before migration 0009.
	if _, err := s.pool.Exec(ctx, `ALTER TABLE blob ADD COLUMN bytes bytea`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := s.pool.Exec(ctx, `ALTER TABLE blob DROP COLUMN IF EXISTS bytes`); err != nil {
			t.Errorf("restore schema: %v", err)
		}
	}()
	legacy := append([]byte("\x89PNG\r\n\x1a\n"), []byte("legacy inline bytes")...)
	legacySum := "it-ext-legacy-sum" // content addressing is not enforced by the table
	if _, err := s.pool.Exec(ctx, `INSERT INTO blob (sha256, media_type, size, bytes)
		VALUES ($1, 'image/png', $2, $3) ON CONFLICT (sha256) DO UPDATE SET bytes = EXCLUDED.bytes`,
		legacySum, len(legacy), legacy); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := s.pool.Exec(ctx, `DELETE FROM blob WHERE sha256=$1`, legacySum); err != nil {
			t.Errorf("cleanup legacy blob: %v", err)
		}
	}()
	moved, err := s.MigrateBlobsOut(ctx)
	if err != nil {
		t.Fatalf("MigrateBlobsOut: %v", err)
	}
	if moved != 1 {
		t.Errorf("moved = %d, want 1", moved)
	}
	if got, err := fake.Get(ctx, legacySum); err != nil || string(got) != string(legacy) {
		t.Errorf("backfill should upload the legacy blob: %v", err)
	}
	var inline []byte
	if err := s.pool.QueryRow(ctx, `SELECT bytes FROM blob WHERE sha256=$1`, legacySum).Scan(&inline); err != nil {
		t.Fatal(err)
	}
	if inline != nil {
		t.Error("backfill should NULL the inline bytes")
	}
	if moved, err = s.MigrateBlobsOut(ctx); err != nil || moved != 0 {
		t.Errorf("second backfill = (%d, %v), want (0, nil)", moved, err)
	}
}
