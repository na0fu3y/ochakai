package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// A purge's promise reaches the bytes (design doc 0031, condition C1):
// once nothing references a hash, SweepBlobs deletes the blob row and
// the stored bytes — and while anything still references it, both stay,
// because content-addressed bytes are shared and the last reference is
// the only one whose removal frees them.
func TestIntegrationSweepBlobs(t *testing.T) {
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
		`DELETE FROM attachment WHERE knowledge_id LIKE 'it-sweep%'`,
		`DELETE FROM object WHERE id LIKE 'it-sweep%' OR starts_with(path, 'it-sweep')`,
		`DELETE FROM knowledge_revision WHERE id LIKE 'it-sweep%' OR starts_with(path, 'it-sweep')`,
		`DELETE FROM knowledge_purge WHERE id LIKE 'it-sweep%'`,
	} {
		if _, err := s.pool.Exec(ctx, del); err != nil {
			t.Fatal(err)
		}
	}

	actor := domain.Actor{Kind: "human", Name: "test"}
	newEntry := func(id string) *domain.Knowledge {
		k := &domain.Knowledge{
			Type: domain.TypeInsights, ID: id, Title: id,
			Status: domain.StatusDraft, CreatedBy: actor,
		}
		if err := s.Create(ctx, k, false); err != nil {
			t.Fatal(err)
		}
		return k
	}
	a, b := newEntry("it-sweep-a"), newEntry("it-sweep-b")
	// The same bytes attached twice: one blob, two references.
	data := []byte("it-sweep shared bytes")
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	for _, k := range []*domain.Knowledge{a, b} {
		if _, err := s.PutAttachment(ctx, k.ID, "seed.txt", "text/plain", "", data, actor); err != nil {
			t.Fatal(err)
		}
	}
	// Leave no live attachment behind for other packages' export scans,
	// whatever this test's own fate. Runs before the deferred Close.
	defer func() {
		_ = s.SoftDelete(ctx, a.ID, actor, nil)
		_ = s.SoftDelete(ctx, b.ID, actor, nil)
	}()

	blobRows := func() int {
		var n int
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM blob WHERE sha256=$1`, sha).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// Both references live: the sweep must not touch the blob.
	if _, err := s.SweepBlobs(ctx); err != nil {
		t.Fatal(err)
	}
	if !fake.has(sha) || blobRows() != 1 {
		t.Fatal("a referenced blob must survive a sweep")
	}

	// One reference gone (purge takes the files under it-sweep-a/ with
	// it), one still standing: still not orphaned.
	if err := s.SoftDelete(ctx, a.ID, actor, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Purge(ctx, a.ID, actor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SweepBlobs(ctx); err != nil {
		t.Fatal(err)
	}
	if !fake.has(sha) || blobRows() != 1 {
		t.Fatal("a blob another entry still references must survive its co-owner's purge")
	}

	// The last reference gone: the sweep takes the row and the bytes.
	if err := s.SoftDelete(ctx, b.ID, actor, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Purge(ctx, b.ID, actor); err != nil {
		t.Fatal(err)
	}
	n, err := s.SweepBlobs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// At least ours: the sweep is global, so a stale orphan another test
	// left behind may leave with it.
	if n < 1 {
		t.Errorf("swept %d blobs, want at least 1", n)
	}
	if fake.has(sha) {
		t.Error("purging the last reference must erase the bytes")
	}
	if blobRows() != 0 {
		t.Error("purging the last reference must drop the blob row")
	}

	// Idempotent: a second sweep finds its work done.
	if n, err := s.SweepBlobs(ctx); err != nil || n != 0 {
		t.Errorf("second sweep = (%d, %v), want (0, nil)", n, err)
	}
}
