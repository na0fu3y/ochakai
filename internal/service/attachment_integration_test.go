package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/embed"
	"github.com/na0fu3y/ochakai/internal/store"
)

// lockLiveAttachments serializes, across the test packages sharing the
// test database, the tests that hold live attachments or scan them all
// (OKF export, ListAllAttachments): bytes resolve against each test's
// own in-memory blob fake, so a foreign live attachment breaks a
// whole-KB scan. Key shared with the store and restapi test packages.
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

// stubEmbedder returns canned vectors by exact input text, so the test
// controls which documents a query lands near.
type stubEmbedder struct {
	vecs map[string][]float32
}

func (e stubEmbedder) Embed(_ context.Context, _ embed.Task, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v, ok := e.vecs[t]
		if !ok {
			v = []float32{0, 0, 0, 1} // far from every canned document
		}
		out[i] = v
	}
	return out, nil
}

func (e stubEmbedder) Model() string { return "stub" }

// stubFileEmbedder adds file input (the gemini-embedding-2 dialect),
// returning canned vectors by filename.
type stubFileEmbedder struct {
	stubEmbedder
	files map[string][]float32
}

func (e stubFileEmbedder) EmbedFile(_ context.Context, name, _ string, _ []byte) ([]float32, error) {
	if v, ok := e.files[name]; ok {
		return v, nil
	}
	return []float32{0, 0, 0, 1}, nil
}

// memBlobStore is a minimal in-memory blob.Store for attach tests.
type memBlobStore map[string][]byte

func (m memBlobStore) Put(_ context.Context, sum, _ string, data []byte) error {
	m[sum] = data
	return nil
}

func (m memBlobStore) Get(_ context.Context, sum string) ([]byte, error) {
	data, ok := m[sum]
	if !ok {
		return nil, fmt.Errorf("mem blob store: %s not found", sum)
	}
	return data, nil
}

// TestAttachmentSearchIntegration exercises the write-to-search loop of
// design doc 0020: Attach embeds a text/plain file, hybrid search then
// surfaces the owning entry for a query that matches only the attachment;
// non-text attachments are skipped.
func TestAttachmentSearchIntegration(t *testing.T) {
	dbURL := os.Getenv("OCHAKAI_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("OCHAKAI_TEST_DATABASE_URL not set")
	}
	lockLiveAttachments(t, dbURL)
	ctx := context.Background()
	s, err := store.New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.UseBlobStore(memBlobStore{})
	if err := s.Migrate(ctx, 4); err != nil { // dim 4, as the store tests
		t.Fatal(err)
	}

	id := fmt.Sprintf("svcit-att-%d", time.Now().UnixNano())
	content := "quarterly revenue by region, expected results"
	query := "四半期の地域別売上の検証結果"
	emb := stubEmbedder{vecs: map[string][]float32{
		// Attachment document text is filename + newline + content.
		"expected.txt\n" + content: {1, 0, 0, 0},
		query:                      {1, 0, 0, 0},
	}}
	svc := &Service{Store: s, Embedder: emb, Log: slog.New(slog.DiscardHandler)}
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "test"}

	if _, err := svc.Create(ctx, &domain.Knowledge{
		Type: domain.TypeQueries, ID: id, Title: "golden query",
		Status: domain.StatusDraft, Body: "SELECT 1", CreatedBy: actor,
	}, actor); err != nil {
		t.Fatal(err)
	}
	// Leave no live attachments behind: the store package's blob test
	// resolves every live attachment against its own blob fake.
	defer func() { _ = s.SoftDelete(ctx, id, actor) }()
	if _, err := svc.Attach(ctx, id, "expected.txt", "", []byte(content), actor); err != nil {
		t.Fatalf("Attach: %v", err)
	}

	// The Japanese query shares no trigrams with the entry or filename;
	// only the attachment vector can surface it.
	hits, err := svc.Search(ctx, query, store.Filter{}, 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	found := false
	for _, h := range hits {
		found = found || h.ID == id
	}
	if !found {
		t.Errorf("hybrid search missed the entry whose attachment matches the query: %+v", hits)
	}

	// Non-text attachments are not embeddable yet (design doc 0020 §3):
	// attach must succeed without leaving an embedding row.
	png := append([]byte("\x89PNG\r\n\x1a\n"), []byte("fake image bytes")...)
	if _, err := svc.Attach(ctx, id, "chart.png", "", png, actor); err != nil {
		t.Fatalf("Attach png: %v", err)
	}
	att, data, err := svc.Attachment(ctx, id, "chart.png")
	if err != nil || att.MediaType != "image/png" {
		t.Fatalf("Attachment(chart.png) = %+v, %v", att, err)
	}
	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != att.SHA256 {
		t.Error("attachment bytes did not round-trip")
	}
	// Had the png slipped past the media gate, its stub vector would be
	// the fallback {0,0,0,1} and the entry would score ~1 here; with only
	// expected.txt embedded, the entry's best attachment is orthogonal.
	vhits, err := s.SearchVectorAttachments(ctx, []float32{0, 0, 0, 1}, store.Filter{}, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range vhits {
		if h.ID == id && h.Score > 0.5 {
			t.Errorf("png attachment appears to have been embedded (score %v)", h.Score)
		}
	}

	// With a file-capable model (gemini-embedding-2 dialect, design doc
	// 0020 §2.3), re-attaching the image embeds its bytes.
	fsvc := &Service{Store: s, Embedder: stubFileEmbedder{
		stubEmbedder: emb,
		files:        map[string][]float32{"chart.png": {0, 0, 1, 0}},
	}, Log: slog.New(slog.DiscardHandler)}
	if _, err := fsvc.Attach(ctx, id, "chart.png", "", png, actor); err != nil {
		t.Fatalf("re-attach png with file embedder: %v", err)
	}
	vhits, err = s.SearchVectorAttachments(ctx, []float32{0, 0, 1, 0}, store.Filter{}, 50)
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, h := range vhits {
		if h.ID == id && h.Score > 0.9 {
			found = true
		}
	}
	if !found {
		t.Errorf("image attachment not searchable with a file-capable embedder: %+v", vhits)
	}
}
