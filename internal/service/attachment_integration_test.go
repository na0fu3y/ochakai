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
// (OKF export, ExportSnapshot.AttachmentMeta): bytes resolve against each
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

	// The search below runs against one run-unique type, not the whole
	// database. Reciprocal rank fusion gives an entry that matches in one
	// list ~1/61 and an entry that matches in two ~2/61, so "an
	// attachment-only match lands in the top 10" holds until ten entries
	// match both lexically and by vector — a property of the corpus, not
	// of the code under test. The shared test database (CONTRIBUTING)
	// grows past that, and the test would then fail for a reason that has
	// nothing to do with attachments. Scoping the search fixes the field.
	typ := domain.Type(fmt.Sprintf("svcatt%d", time.Now().UnixNano()))
	id := string(typ) + "/z-target" // sorts last: an RRF tie must not favor it
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
		Type: typ, ID: id, Title: "golden query",
		Status: domain.StatusDraft, Body: "SELECT 1", CreatedBy: actor,
	}, actor); err != nil {
		t.Fatal(err)
	}
	// Leave no live attachments behind: the store package's blob test
	// resolves every live attachment against its own blob fake.
	defer func() { _ = s.SoftDelete(ctx, id, actor) }()

	// Company for the target: same type, no overlap with the query in
	// either half of search. They are in the vector list (it returns the
	// nearest N whatever the distance) and nowhere else, so each scores
	// one RRF term — which is what makes the target's second term visible.
	for i := range 3 {
		decoy := fmt.Sprintf("%s/a-decoy-%d", typ, i)
		if _, err := svc.Create(ctx, &domain.Knowledge{
			Type: typ, ID: decoy, Title: "参考資料", Status: domain.StatusDraft,
			Body: "参考資料の置き場", CreatedBy: actor,
		}, actor); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = s.SoftDelete(ctx, decoy, actor) }()
	}

	if _, err := svc.Attach(ctx, id, "expected.txt", "", []byte(content), actor); err != nil {
		t.Fatalf("Attach: %v", err)
	}

	// The Japanese query shares no fragment with any of these entries or
	// filenames; only the attachment vector can tell them apart.
	hits, err := svc.Search(ctx, query, store.Filter{Types: []domain.Type{typ}}, 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 || hits[0].ID != id {
		t.Fatalf("the entry whose attachment matches the query must rank first: %+v", hits)
	}
	// Strictly first, not first by tie-break: without the attachment
	// vector every entry here carries the same single RRF term.
	for _, h := range hits[1:] {
		if h.Score >= hits[0].Score {
			t.Errorf("attachment match scored %v, no better than %s at %v", hits[0].Score, h.ID, h.Score)
		}
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
	vhits, err := s.SearchVectorAttachments(ctx, []float32{0, 0, 0, 1}, "stub", store.Filter{}, 50)
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
	vhits, err = s.SearchVectorAttachments(ctx, []float32{0, 0, 1, 0}, "stub", store.Filter{}, 50)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, h := range vhits {
		if h.ID == id && h.Score > 0.9 {
			found = true
		}
	}
	if !found {
		t.Errorf("image attachment not searchable with a file-capable embedder: %+v", vhits)
	}
}

// Reembed covers attachment vectors, not just entry vectors (design doc
// 0020). Only attach writes them, so the documented recovery from a
// dimension change — drop both embedding tables, restart, reembed —
// used to leave every file findable by name alone, forever. Also pins
// the cursor: a pass must not keep handing back the same window.
func TestReembedCoversAttachmentsIntegration(t *testing.T) {
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
	// Cleanup, not defer: the entry below is removed from a t.Cleanup,
	// which runs after the test function returns — a deferred Close would
	// already have shut the pool, leaving a live entry whose attachment
	// bytes exist only in this test's in-memory blob store. Registered
	// first, so LIFO runs it last.
	t.Cleanup(func() { s.Close() })
	s.UseBlobStore(memBlobStore{})
	if err := s.Migrate(ctx, 4); err != nil {
		t.Fatal(err)
	}
	model := fmt.Sprintf("reembed-att-%d", time.Now().UnixNano())
	svc := &Service{Store: s, Embedder: stubEmbedder{}, Log: slog.New(slog.DiscardHandler)}
	actor := domain.Actor{Kind: "human", Name: "test"}

	typ := domain.Type(fmt.Sprintf("svcre%d", time.Now().UnixNano()))
	id := string(typ) + "/host"
	if _, err := svc.Create(ctx, &domain.Knowledge{Type: typ, ID: id, Title: "host"}, actor); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Delete(ctx, id, actor) })
	for _, name := range []string{"a.txt", "b.txt"} {
		if _, err := svc.Attach(ctx, id, name, "", []byte("some text for "+name), actor); err != nil {
			t.Fatal(err)
		}
	}

	// Simulate the recovery procedure: the vectors written by attach are
	// under the stub's model, so switching the model leaves both
	// attachments unembedded for the model now in use.
	svc.Embedder = &fixedEmbedder{model: model, dim: 4}

	pending, err := s.ListUnembeddedAttachments(ctx, model, "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if got := countOwned(pending, id); got != 2 {
		t.Fatalf("unembedded attachments for %s = %d, want 2", id, got)
	}

	// Run the corpus down the way the CLI does. Entries go first, so the
	// attachments are reached only once the cursor has walked past every
	// entry — which is also the assertion that the cursor advances: a
	// pass that kept returning the same window would never get here.
	//
	// The loop ends when this test's own attachments are embedded, not
	// when the shared database reaches zero (CONTRIBUTING). Another
	// package creating an entry whose id sorts behind the cursor leaves
	// Missing above zero for reasons that have nothing to do with this
	// test, and waiting for a corpus other packages keep refilling is a
	// race with no winner.
	cursor := ""
	for pass := 1; ; pass++ {
		if pass > 100 {
			t.Fatalf("reembed did not converge in %d passes (cursor %q)", pass-1, cursor)
		}
		res, err := svc.Reembed(ctx, cursor, 500)
		if err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
		left, err := s.ListUnembeddedAttachments(ctx, model, "", "", 100)
		if err != nil {
			t.Fatal(err)
		}
		if countOwned(left, id) == 0 {
			break
		}
		// Still work of ours to do, so a cursor standing still is the
		// bug this loop is here for.
		if res.Cursor == "" || res.Cursor == cursor {
			t.Fatalf("pass %d left %d of our attachments unembedded but did not advance the cursor (%q)",
				pass, countOwned(left, id), cursor)
		}
		cursor = res.Cursor
	}

	// Both attachments now have a vector under the current model.
	if pending, err = s.ListUnembeddedAttachments(ctx, model, "", "", 100); err != nil {
		t.Fatal(err)
	}
	if got := countOwned(pending, id); got != 0 {
		t.Errorf("after reembed, %d of %s's attachments still lack a vector", got, id)
	}
}

func countOwned(pending []store.UnembeddedAttachment, id string) int {
	n := 0
	for _, p := range pending {
		if p.KnowledgeID == id {
			n++
		}
	}
	return n
}
