package store

import (
	"bytes"
	"context"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/testdb"
)

// TestPostgresBlobsHoldAFileIntegration pins decision 0156: with no
// bucket, a file's bytes live in PostgreSQL, read back as written, and
// leave with the sweep once nothing references them — the same promise
// the GCS store keeps.
func TestPostgresBlobsHoldAFileIntegration(t *testing.T) {
	dbURL := testdb.URL(t)
	lockLiveAttachments(t, dbURL)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.UseBlobStore(s.PostgresBlobs())
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	path := testdb.Unique(t, "it-pgblob") + "/attesters/eq.py"
	data := []byte("def attest(receipt):\n    return receipt['rows'] > 0\n")
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "test"}

	f, _, err := s.PutFile(ctx, path, "text/plain", data, actor)
	if err != nil {
		t.Fatal(err)
	}
	_, got, err := s.GetFile(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("read back %q, want %q", got, data)
	}

	count := func() int {
		t.Helper()
		var n int
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM blob_bytes WHERE sha256 = $1`, f.SHA256).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count() != 1 {
		t.Fatal("the bytes are not in blob_bytes")
	}
	// Nothing references the hash once the object and its history are
	// gone, and the sweep takes the bytes with the ledger row.
	for _, q := range []string{
		`DELETE FROM object WHERE path = $1`,
		`DELETE FROM knowledge_revision WHERE path = $1`,
	} {
		if _, err := s.pool.Exec(ctx, q, path); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.SweepBlobs(ctx); err != nil {
		t.Fatal(err)
	}
	if count() != 0 {
		t.Error("the sweep left the bytes behind")
	}
}
