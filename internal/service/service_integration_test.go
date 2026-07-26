package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/embed"
	"github.com/na0fu3y/ochakai/internal/store"
)

// TestUpdateNoOpIntegration exercises the no-op update path against a real
// PostgreSQL: a content-identical update must write nothing — no revision,
// no updated_at bump — so recurring imports stop flooding the audit trail.
// Skipped unless OCHAKAI_TEST_DATABASE_URL is set (see store integration
// test for the docker one-liner).
func TestUpdateNoOpIntegration(t *testing.T) {
	dbURL := os.Getenv("OCHAKAI_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("OCHAKAI_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	s, err := store.New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// embedDim 0: this test needs no embeddings, so plain PostgreSQL
	// (pg_trgm only, no pgvector) is enough.
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	svc := &Service{Store: s, Log: slog.New(slog.DiscardHandler)}
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "test"}
	// The ID is unique per run: entries stay live after the test, and the
	// service layer has no hard delete to clean up with.
	id := fmt.Sprintf("svcit-%d", time.Now().UnixNano())

	entry := func() *domain.Knowledge {
		return &domain.Knowledge{
			Type: domain.TypeMetrics, ID: id, Title: "売上",
			Description: "統合テスト用", Tags: []string{"sales"},
			Status: domain.StatusDraft,
			Attrs:  map[string]any{"threshold": 5},
			Body:   "受注合計。[sales モデル](/model/sales.md) で定義。",
		}
	}
	if _, err := svc.Create(ctx, entry(), actor); err != nil {
		t.Fatal(err)
	}
	// Read the baseline back through the store: PostgreSQL timestamptz is
	// microsecond precision, so the Create return value's nanosecond
	// time.Now() would never equal a value round-tripped through the DB.
	created, err := svc.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}

	// Same content, but shaped like a re-imported payload: no server
	// fields, status omitted, the attr number decoded as float64 (JSON).
	same := entry()
	same.Status = ""
	same.Attrs = map[string]any{"threshold": float64(5)}
	got, changed, err := svc.Update(ctx, same, actor, nil)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("content-identical update reported changed=true")
	}
	if !got.UpdatedAt.Equal(created.UpdatedAt) {
		t.Errorf("no-op update bumped updated_at: %v -> %v", created.UpdatedAt, got.UpdatedAt)
	}
	revs, err := svc.Revisions(ctx, id, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 1 {
		t.Errorf("no-op update wrote a revision: got %d revisions, want 1", len(revs))
	}

	// A real change still writes: revision recorded, updated_at bumped.
	edited := entry()
	edited.Body = "受注合計。返品は含まない。"
	got, changed, err = svc.Update(ctx, edited, actor, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("content change reported changed=false")
	}
	if !got.UpdatedAt.After(created.UpdatedAt) {
		t.Errorf("real update did not bump updated_at: %v", got.UpdatedAt)
	}
	if revs, err = svc.Revisions(ctx, id, 10); err != nil {
		t.Fatal(err)
	}
	if len(revs) != 2 || revs[0].Change != "update" {
		t.Errorf("real update must add an update revision: %+v", revs)
	}
}

// TestUpdateIfMatchIntegration exercises the opt-in optimistic lock (design
// doc 0025 §11): a matching If-Match updates, a stale one is rejected with
// ErrConflict, and once the entry has moved on the old version stays
// rejected until re-read.
func TestUpdateIfMatchIntegration(t *testing.T) {
	dbURL := os.Getenv("OCHAKAI_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("OCHAKAI_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	s, err := store.New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	svc := &Service{Store: s, Log: slog.New(slog.DiscardHandler)}
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "test"}
	id := fmt.Sprintf("svcit-ifmatch-%d", time.Now().UnixNano())

	mk := func(body string) *domain.Knowledge {
		return &domain.Knowledge{Type: domain.TypeMetrics, ID: id, Title: "売上",
			Status: domain.StatusDraft, Body: body}
	}
	if _, err := svc.Create(ctx, mk("v1"), actor); err != nil {
		t.Fatal(err)
	}
	base, err := svc.Get(ctx, id) // the version both writers below read
	if err != nil {
		t.Fatal(err)
	}

	// Writer A updates against the current version — accepted.
	v2, _, err := svc.Update(ctx, mk("v2"), actor, &base.UpdatedAt)
	if err != nil {
		t.Fatalf("If-Match update on the current version: %v", err)
	}

	// Writer B still holds the pre-A version: its update must be rejected,
	// not silently clobber A's write (the lost-update this fix closes).
	if _, _, err := svc.Update(ctx, mk("v3-from-stale"), actor, &base.UpdatedAt); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale If-Match: got %v, want ErrConflict", err)
	}
	// The stored content is still A's, untouched by B.
	if cur, err := svc.Get(ctx, id); err != nil {
		t.Fatal(err)
	} else if cur.Body != "v2" {
		t.Errorf("stale update clobbered the row: body = %q, want v2", cur.Body)
	}

	// After re-reading A's version, B can proceed.
	if _, _, err := svc.Update(ctx, mk("v3"), actor, &v2.UpdatedAt); err != nil {
		t.Fatalf("If-Match update after re-read: %v", err)
	}

	// Opting out (nil) keeps last-write-wins — no precondition, always writes.
	if _, _, err := svc.Update(ctx, mk("v4-lww"), actor, nil); err != nil {
		t.Fatalf("opt-out update: %v", err)
	}
}

// The curated-write rule: a surface with no If-Match precondition (MCP)
// must not replace a state a human put there. Everything else an agent
// does stays open — creating, editing drafts, and promoting a draft to
// verified are all still allowed, because this is about preconditions,
// not authority (design docs 0002, 0015 §3.1).
func TestRefuseIfCuratedIntegration(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t, ctx)
	actor := domain.Actor{Kind: "agent", Name: "insightflow@example.iam.gserviceaccount.com"}
	human := domain.Actor{Kind: "human", Name: "na0"}

	setStatus := func(t *testing.T, k *domain.Knowledge, status domain.Status, by domain.Actor) {
		t.Helper()
		k.Status = status
		updated, _, err := svc.Update(ctx, k, by, nil)
		if err != nil {
			t.Fatalf("set status %s: %v", status, err)
		}
		*k = *updated
	}

	t.Run("drafts stay writable", func(t *testing.T) {
		id := "queries/" + uid("guard-draft")
		k, err := svc.Create(ctx, &domain.Knowledge{Type: domain.TypeQueries, ID: id, Title: "draft"}, actor)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = svc.Delete(ctx, id, actor) })
		version, err := svc.RefuseIfCurated(ctx, id, "update")
		if err != nil {
			t.Fatalf("draft must be writable: %v", err)
		}
		// The guard hands back a usable precondition, not just a verdict.
		if version == nil || !version.Equal(k.UpdatedAt) {
			t.Errorf("version = %v, want the entry's updated_at %v", version, k.UpdatedAt)
		}
		// Promotion to verified is not restricted — 0002 stands.
		setStatus(t, k, domain.StatusVerified, actor)
	})

	// Each curated state is refused, and each refusal names a way forward:
	// an agent that meets a wall with no door stalls or works around it.
	for status, wantAdvice := range map[domain.Status]string{
		domain.StatusVerified:   "report_outcome failed",
		domain.StatusRejected:   "status_note",
		domain.StatusDeprecated: "create_knowledge",
	} {
		t.Run(string(status)+" is refused", func(t *testing.T) {
			id := "queries/" + uid("guard-"+string(status))
			k, err := svc.Create(ctx, &domain.Knowledge{
				Type: domain.TypeQueries, ID: id, Title: string(status),
				StatusNote: "because the numbers were wrong",
			}, actor)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = svc.Delete(ctx, id, actor) })
			setStatus(t, k, status, human)

			for _, op := range []string{"update", "delete"} {
				_, err := svc.RefuseIfCurated(ctx, id, op)
				var invalid *InvalidInputError
				if !errors.As(err, &invalid) {
					t.Fatalf("%s %s: got %v, want a refusal", op, status, err)
				}
				if !strings.Contains(err.Error(), wantAdvice) {
					t.Errorf("%s refusal does not offer %q: %v", status, wantAdvice, err)
				}
			}

			// A human reopening it makes it writable again.
			setStatus(t, k, domain.StatusDraft, human)
			if _, err := svc.RefuseIfCurated(ctx, id, "update"); err != nil {
				t.Errorf("reopened entry must be writable: %v", err)
			}
		})
	}

	// The path this rule exists to close: Create refuses a live rejected
	// entry, so without the guard an agent could delete it and recreate it
	// as a draft, taking the reason for the rejection with it.
	t.Run("a rejection cannot be laundered into a draft", func(t *testing.T) {
		id := "queries/" + uid("guard-launder")
		k, err := svc.Create(ctx, &domain.Knowledge{
			Type: domain.TypeQueries, ID: id, Title: "rejected proposal",
			StatusNote: "duplicate of an existing golden query",
		}, actor)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = svc.Delete(ctx, id, actor) })
		setStatus(t, k, domain.StatusRejected, human)

		if _, err := svc.RefuseIfCurated(ctx, id, "delete"); err == nil {
			t.Fatal("deleting a rejected entry was allowed; the memory of no is erasable")
		}
		stored, err := svc.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Status != domain.StatusRejected || stored.StatusNote == "" {
			t.Errorf("the rejection and its reason must survive: %+v", stored)
		}
	})

	// Closing the delete step is not enough: a tombstone that already
	// exists — deleted before the rule, or deleted from a human surface as
	// housekeeping — is revived in place by Create, status_note included.
	t.Run("a curated tombstone cannot be revived", func(t *testing.T) {
		for status, wantAdvice := range map[domain.Status]string{
			domain.StatusRejected:   "status_note",
			domain.StatusVerified:   "different id",
			domain.StatusDeprecated: "no longer recommended",
		} {
			id := "queries/" + uid("guard-tomb-"+string(status))
			k, err := svc.Create(ctx, &domain.Knowledge{
				Type: domain.TypeQueries, ID: id, Title: "ruled on then deleted",
				StatusNote: "duplicate of an existing golden query",
			}, actor)
			if err != nil {
				t.Fatal(err)
			}
			setStatus(t, k, status, human)
			// A human deletes it: legitimate on the REST/CLI surfaces.
			if err := svc.Delete(ctx, id, human); err != nil {
				t.Fatal(err)
			}
			err = svc.RefuseIfRevivingCurated(ctx, id)
			var invalid *InvalidInputError
			if !errors.As(err, &invalid) {
				t.Fatalf("reviving a deleted %s entry: got %v, want a refusal", status, err)
			}
			if !strings.Contains(err.Error(), wantAdvice) {
				t.Errorf("%s refusal does not offer %q: %v", status, wantAdvice, err)
			}
			// The human surfaces still revive it, ruling and all.
			revived, err := svc.Create(ctx, &domain.Knowledge{
				Type: domain.TypeQueries, ID: id, Title: "revived by a human"}, human)
			if err != nil {
				t.Fatalf("a human surface must still be able to reuse the id: %v", err)
			}
			t.Cleanup(func() { _ = svc.Delete(ctx, revived.ID, human) })
		}
	})

	// A draft tombstone is not a ruling: reviving an abandoned draft is
	// what create-on-a-deleted-id is for.
	t.Run("a draft tombstone stays revivable", func(t *testing.T) {
		id := "queries/" + uid("guard-tomb-draft")
		if _, err := svc.Create(ctx, &domain.Knowledge{
			Type: domain.TypeQueries, ID: id, Title: "abandoned draft"}, actor); err != nil {
			t.Fatal(err)
		}
		if err := svc.Delete(ctx, id, actor); err != nil {
			t.Fatal(err)
		}
		if err := svc.RefuseIfRevivingCurated(ctx, id); err != nil {
			t.Fatalf("a deleted draft must stay revivable: %v", err)
		}
		if _, err := svc.Create(ctx, &domain.Knowledge{
			Type: domain.TypeQueries, ID: id, Title: "second attempt"}, actor); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = svc.Delete(ctx, id, actor) })
	})

	// A free id is not a refusal, and neither is a live one: Create's own
	// ErrAlreadyExists covers that case.
	t.Run("free and live ids are untouched", func(t *testing.T) {
		if err := svc.RefuseIfRevivingCurated(ctx, "queries/"+uid("guard-tomb-free")); err != nil {
			t.Errorf("free id: %v", err)
		}
		id := "queries/" + uid("guard-tomb-live")
		k, err := svc.Create(ctx, &domain.Knowledge{
			Type: domain.TypeQueries, ID: id, Title: "live"}, actor)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = svc.Delete(ctx, id, actor) })
		setStatus(t, k, domain.StatusRejected, human)
		if err := svc.RefuseIfRevivingCurated(ctx, id); err != nil {
			t.Errorf("live entry: %v", err)
		}
	})
}

// Reembed closes the gap between "semantic search is configured" and
// "semantic search works". Vectors are written on entry writes only, so a
// base loaded before the provider was configured has none — and nothing
// says so: search just quietly stays lexical. Changing the model has the
// same shape, and is what this exercises, because it needs no reaching
// past the store to simulate.
func TestReembedIntegration(t *testing.T) {
	ctx := context.Background()
	actor := domain.Actor{Kind: "human", Name: "test"}
	svc := newEmbeddingService(t, ctx, "test-embedder-v1")

	// Without a provider the operator gets told which setting is missing,
	// not a nil dereference.
	embedder := svc.Embedder
	svc.Embedder = nil
	_, err := svc.Reembed(ctx, "", 10)
	var unsupported *UnsupportedError
	if !errors.As(err, &unsupported) {
		t.Errorf("without an embedder: got %v, want an UnsupportedError naming the setting", err)
	}
	svc.Embedder = embedder
	id := "insights/" + uid("reembed")
	if _, err := svc.Create(ctx, &domain.Knowledge{
		Type: domain.TypeInsights, ID: id, Title: "reembed me", Body: "text",
	}, actor); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Delete(ctx, id, actor) })

	// Assertions are about this entry, not about counts: the test database
	// is shared by every package, so a corpus-wide pass legitimately picks
	// up whatever else happens to be unembedded.
	unembedded := func(t *testing.T, model string) bool {
		t.Helper()
		ids, err := svc.Store.ListUnembedded(ctx, model, "", 5000)
		if err != nil {
			t.Fatal(err)
		}
		return slices.Contains(ids, id)
	}

	if unembedded(t, "test-embedder-v1") {
		t.Error("writing the entry should have embedded it")
	}

	// Switch models: the entry's vector is now in a space nothing queries,
	// and nothing but a reembed notices (design doc 0020).
	svc.Embedder = &fixedEmbedder{model: "test-embedder-v2", dim: 4}
	if !unembedded(t, "test-embedder-v2") {
		t.Fatal("a model change must leave the old vector behind")
	}

	res, err := svc.Reembed(ctx, "", 5000)
	if err != nil {
		t.Fatalf("Reembed after a model change: %v", err)
	}
	// Only Embedded is assertable here: a corpus-wide pass runs over
	// whatever the other packages have in the shared database, including
	// entries they delete while it runs (counted as Failed).
	if res.Embedded == 0 {
		t.Fatalf("reembed did nothing: %+v", res)
	}
	if unembedded(t, "test-embedder-v2") {
		t.Error("reembed left the entry without a vector for the new model")
	}

	// And it is idempotent: a second pass finds this entry already done.
	done, err := svc.Reembed(ctx, "", 5000)
	if err != nil {
		t.Fatal(err)
	}
	if unembedded(t, "test-embedder-v2") {
		t.Error("a second pass unembedded the entry")
	}
	// Missing is corpus-wide and the database is shared, so an entry
	// another package creates between the pass and the count is
	// legitimately still missing — nothing to assert from here. That the
	// count does not subtract the pass's own work is pinned by
	// TestReembedReportsWhatIsLeft, where the corpus is measured first.
	_ = done
}

// Missing is the number the operator acts on: it decides whether to run
// the command again. Reporting 0 while entries are still unembedded turns
// a half-done backfill into a finished one, and hybrid search stays
// quietly lexical for whatever was left — the exact failure this command
// exists to end.
func TestReembedReportsWhatIsLeft(t *testing.T) {
	ctx := context.Background()
	actor := domain.Actor{Kind: "human", Name: "test"}
	svc := newEmbeddingService(t, ctx, uid("m"))
	model := svc.Embedder.Model()

	// Entries written before a provider was configured: created with no
	// embedder, so nothing wrote a vector. That is the state enabling
	// semantic search on an existing corpus leaves behind.
	svc.Embedder = nil
	for i := range 3 {
		id := "insights/" + uid("missing")
		if _, err := svc.Create(ctx, &domain.Knowledge{
			Type: domain.TypeInsights, ID: id, Title: fmt.Sprintf("filler %d", i),
		}, actor); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = svc.Delete(ctx, id, actor) })
	}
	svc.Embedder = &fixedEmbedder{model: model, dim: 4}

	total, err := svc.Store.CountUnembedded(ctx, model)
	if err != nil {
		t.Fatal(err)
	}
	if total < 3 {
		t.Fatalf("setup left %d unembedded entries, want at least 3", total)
	}

	// A pass smaller than the corpus must say what it did not reach.
	res, err := svc.Reembed(ctx, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if res.Embedded != 2 {
		t.Fatalf("bounded pass embedded %d, want 2", res.Embedded)
	}
	// A lower bound, not an equality: the test database is shared by every
	// package (CONTRIBUTING), so entries can appear between the count above
	// and the one Reembed takes after the pass. What must hold is that the
	// pass does not report its own work as still missing — the bug this
	// covers subtracted it twice.
	if want := total - 2; res.Missing < want {
		t.Errorf("Missing = %d, want at least %d (%d unembedded, %d done)", res.Missing, want, total, res.Embedded)
	}
	if res.Missing == 0 {
		t.Error("a bounded pass over a larger corpus reported nothing left")
	}
}

// newEmbeddingService is newIntegrationService plus the pgvector schema,
// which exists only where an embedding dimension was configured. Reembed
// reads knowledge_embedding, so a test that skipped this would depend on
// some other package having migrated the shared database first.
func newEmbeddingService(t *testing.T, ctx context.Context, model string) *Service {
	t.Helper()
	svc := newIntegrationService(t, ctx)
	if err := svc.Store.Migrate(ctx, 4); err != nil {
		t.Fatal(err)
	}
	svc.Embedder = &fixedEmbedder{model: model, dim: 4}
	return svc
}

// fixedEmbedder stands in for Vertex: the integration database has no
// provider configured, and the model name is what Reembed keys on.
type fixedEmbedder struct {
	model string
	dim   int
}

func (e *fixedEmbedder) Model() string { return e.model }

func (e *fixedEmbedder) Embed(_ context.Context, _ embed.Task, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = make([]float32, e.dim)
		out[i][0] = 1
	}
	return out, nil
}
