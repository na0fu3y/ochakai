package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

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
	if err := s.Create(ctx, k, false); err != nil {
		t.Fatal(err)
	}
	// Leave no live attachment behind: the bytes below exist only in this
	// test's blob fake, and other packages' export scans resolve every
	// live attachment against their own. Runs before the deferred Close.
	defer func() { _ = s.SoftDelete(ctx, k.ID, actor) }()

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

	// The export path finds the attachment in its snapshot and resolves
	// the bytes from the blob store, one at a time.
	snap, err := s.BeginExport(ctx)
	if err != nil {
		t.Fatal(err)
	}
	metas, err := snap.AttachmentMeta(ctx)
	snap.Close(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range metas {
		if m.ID != k.ID || m.Att.Name != "chart.png" {
			continue
		}
		data, err := s.AttachmentBytes(ctx, m.Att.SHA256)
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
	if _, err := bare.PutAttachment(ctx, k.ID, "other.png", "image/png", "", png, actor); err == nil ||
		!strings.Contains(err.Error(), "OCHAKAI_GCS_BUCKET") {
		t.Errorf("attach without a blob store = %v, want config hint", err)
	}
	if _, _, err := bare.GetAttachment(ctx, k.ID, "chart.png"); err == nil ||
		!strings.Contains(err.Error(), "OCHAKAI_GCS_BUCKET") {
		t.Errorf("read without a blob store = %v, want config hint", err)
	}
}

// An attach and a delete can be in flight together: PutAttachment reads
// the entry outside its transaction, so a delete can commit in the window
// between that read and the write. The attach must lose. Before the guard
// it wrote the attachment and an "attach" revision onto the tombstone,
// where they sat invisible until someone revived the entry and got back a
// file they never attached to the entry they were reviving.
func TestIntegrationAttachRacingDeleteLoses(t *testing.T) {
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
	s.UseBlobStore(newFakeBlobStore())
	actor := domain.Actor{Kind: "human", Name: "test"}

	id := fmt.Sprintf("it-attach-race-%d", time.Now().UnixNano())
	defer func() {
		_, _ = s.pool.Exec(ctx, `DELETE FROM attachment WHERE knowledge_id = $1`, id)
		for _, table := range []string{"knowledge_revision", "knowledge"} {
			_, _ = s.pool.Exec(ctx, `DELETE FROM `+table+` WHERE id = $1`, id)
		}
	}()
	if err := s.Create(ctx, &domain.Knowledge{
		Type: domain.TypeTerms, ID: id, Title: id, Status: domain.StatusDraft, CreatedBy: actor,
	}, false); err != nil {
		t.Fatal(err)
	}

	// A delete that has not committed yet: PutAttachment's own read runs
	// on another connection and still sees a live entry, which is the
	// window the guard closes.
	del, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = del.Rollback(ctx) }()
	if _, err := del.Exec(ctx,
		`UPDATE knowledge SET deleted_at = now(), updated_at = now() WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}

	attached := make(chan error, 1)
	go func() {
		png := append([]byte("\x89PNG\r\n\x1a\n"), []byte("racing bytes")...)
		_, err := s.PutAttachment(ctx, id, "chart.png", "image/png", "", png, actor)
		attached <- err
	}()

	// Wait for the attach to reach the entry row and block on the delete.
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int
		if err := s.pool.QueryRow(ctx,
			`SELECT count(*) FROM pg_stat_activity
			  WHERE wait_event_type = 'Lock' AND query LIKE '%knowledge%'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			break
		}
		select {
		case err := <-attached:
			t.Fatalf("attach finished without blocking: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("attach never blocked on the delete")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := del.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	if err := <-attached; !errors.Is(err, ErrNotFound) {
		t.Fatalf("attach racing a delete: got %v, want ErrNotFound", err)
	}
	var atts, revs int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM attachment WHERE knowledge_id = $1`, id).Scan(&atts); err != nil {
		t.Fatal(err)
	}
	if atts != 0 {
		t.Errorf("refused attach left %d attachment rows on the tombstone", atts)
	}
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM knowledge_revision WHERE id = $1 AND change = 'attach'`, id).Scan(&revs); err != nil {
		t.Fatal(err)
	}
	if revs != 0 {
		t.Errorf("refused attach left %d attach revisions", revs)
	}
}
