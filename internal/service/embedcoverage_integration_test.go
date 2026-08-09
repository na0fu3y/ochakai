package service

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/embed"
	"github.com/na0fu3y/ochakai/internal/store"
)

// A concept longer than the model's input window is embedded from its
// front and the rest is dropped. Nothing about the result looks wrong:
// the concept has a vector, ranks, comes back — it is simply unfindable
// by anything said in its second half, and until this was counted no
// surface said so (issue #532, design doc 0089).
//
// The test is the whole path, because the fact is assembled along it:
// embeddingText decides the text does not fit, embedDocument reports how
// much the provider took after any shortening, the store keeps the answer
// beside the vector, and Stats counts the rows. A unit test of any one of
// those would pass with the wiring cut.
//
// It runs in a schema of its own. Turning the vector half on is global —
// the column has one width for the whole database and a changed width
// rebuilds the tables empty — so doing it against the shared corpus would
// drop a neighbouring package's vectors mid-run, which is the flake this
// suite has been paying off elsewhere (#546).
func TestEmbeddingCoverageCountsTruncatedConceptsIntegration(t *testing.T) {
	ctx := context.Background()
	svc := newScopedEmbeddingService(t, ctx)

	actor := domain.Actor{Kind: domain.ActorHuman, Name: "coverage"}
	// "Fits" is measured against the whole embedded text — the id, the
	// envelope and the body together — so the short one is deliberately
	// far under rather than just under.
	short := &domain.Knowledge{
		Type: domain.TypeMetrics, ID: "metrics/short", Title: "Short",
		Body: "one paragraph, comfortably inside any window",
	}
	// Over embed.ConservativeInputBytes, which is what a fake embedder
	// gets: it reports no window of its own, so the product applies the
	// floor (embedBytes).
	long := &domain.Knowledge{
		Type: domain.TypeMetrics, ID: "metrics/long", Title: "Long",
		Body: strings.Repeat("the tail of this concept is not in semantic search. ",
			embed.ConservativeInputBytes/20),
	}
	for _, k := range []*domain.Knowledge{short, long} {
		if _, _, _, err := svc.Put(ctx, k, actor, nil); err != nil {
			t.Fatalf("put %s: %v", k.ID, err)
		}
	}

	vectors, truncated, err := svc.Store.EmbeddingCoverage(ctx, svc.Embedder.Model(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if vectors != 2 {
		t.Fatalf("vectors = %d, want both concepts embedded", vectors)
	}
	if truncated != 1 {
		t.Errorf("truncated = %d, want exactly the concept over the window", truncated)
	}

	// And the number reaches the surface a curator reads.
	st, err := svc.Stats(ctx, 7, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Embedding.Enabled {
		t.Error("stats reports semantic search off on a deployment that has an embedder")
	}
	if st.Embedding.Vectors != 2 || st.Embedding.Truncated != 1 {
		t.Errorf("stats embedding = %+v, want 2 vectors and 1 truncated", st.Embedding)
	}

	// Rewriting the long concept short clears it: the flag describes the
	// stored vector, not the concept's history.
	long.Body = "short now"
	if _, _, _, err := svc.Put(ctx, long, actor, nil); err != nil {
		t.Fatal(err)
	}
	if _, truncated, err = svc.Store.EmbeddingCoverage(ctx, svc.Embedder.Model(), nil); err != nil {
		t.Fatal(err)
	}
	if truncated != 0 {
		t.Errorf("truncated = %d after the concept was shortened, want 0", truncated)
	}
}

// A deployment with no embedder reports the difference between "none" and
// "not measured". Without it a base whose corpus is entirely unembedded
// and one that never had semantic search produce the same zero and want
// different actions — run `ochakai reembed`, or nothing at all.
func TestEmbeddingCoverageSaysWhenThereIsNoEmbedderIntegration(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t, ctx)
	st, err := svc.Stats(ctx, 7, []string{uid(t, "noembed")})
	if err != nil {
		t.Fatal(err)
	}
	if st.Embedding.Enabled {
		t.Errorf("stats embedding = %+v, want enabled=false with no embedder", st.Embedding)
	}
}

// newScopedEmbeddingService is a Service whose store owns a PostgreSQL
// schema of its own, migrated with the vector half on, plus the stand-in
// encoder from fakeencoder_test.go.
//
// The schema is the isolation: everything the vector tables do is global
// to the database they sit in, so a test that needs them cannot share one
// with the packages running beside it.
func newScopedEmbeddingService(t *testing.T, ctx context.Context) *Service {
	t.Helper()
	dbURL := os.Getenv("OCHAKAI_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("OCHAKAI_TEST_DATABASE_URL not set")
	}
	// The DDL goes through a plain connection rather than the store:
	// creating and dropping a schema is not something the product does,
	// and a method on Store existing only for this would be product
	// surface bought with a test's convenience.
	admin, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close(context.Background()) })
	schema := "svcembed" + strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, uid(t, ""))
	// The extension belongs in public: created inside the scoped schema
	// it would vanish with it and break later runs.
	if _, err := admin.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS vector`); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), `DROP SCHEMA `+schema+` CASCADE`); err != nil {
			t.Errorf("drop schema %s: %v", schema, err)
		}
	})
	sep := "?"
	if strings.Contains(dbURL, "?") {
		sep = "&"
	}
	s, err := store.New(ctx, dbURL+sep+"options=-csearch_path%3D"+schema+",public", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	dim, ok := embed.Dimension("gemini-embedding-001")
	if !ok {
		t.Fatal("the product no longer knows its own embedding width")
	}
	// The vector tables live outside the versioned migrations, and
	// 0010/0011/0013 probe for them with an *unqualified* to_regclass,
	// which walks the whole search path: with none in this schema they
	// find public's already-migrated table and try to rename a `type`
	// column that has not existed there since 0011. Pre-create the
	// pre-0010 shape at the width migrateEmbedding will want, exactly as
	// the store's own scoped tests do.
	if _, err := admin.Exec(ctx, `CREATE TABLE `+schema+`.knowledge_embedding (
		type       text NOT NULL,
		id         text NOT NULL,
		model      text NOT NULL,
		embedding  vector(`+strconv.Itoa(dim)+`) NOT NULL,
		updated_at timestamptz NOT NULL DEFAULT now(),
		PRIMARY KEY (type, id))`); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx, dim); err != nil {
		t.Fatal(err)
	}
	return &Service{
		Store:    s,
		Embedder: fakeEncoder{dim: dim},
		Log:      slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}
}
