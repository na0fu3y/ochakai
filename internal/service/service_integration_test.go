package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/embed"
	"github.com/na0fu3y/ochakai/internal/okf"
	"github.com/na0fu3y/ochakai/internal/store"
	"github.com/na0fu3y/ochakai/internal/testdb"
)

// A document that is reformatted but says the same thing is a change to
// the file and not to the entry, and design doc 0046 §3.4 splits the two:
// the bytes are stored and the version moves, while generated — the actor
// and the timestamp — stays with whoever the content already stood by.
// Byte-identical bytes are still nothing happening at all.
func TestReformattingIsAChangeToTheFileOnlyIntegration(t *testing.T) {
	dbURL := testdb.URL(t)
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
	author := domain.Actor{Kind: domain.ActorHuman, Name: "author"}
	editor := domain.Actor{Kind: domain.ActorHuman, Name: "editor"}
	id := testdb.Unique(t, "svcit-reformat-")

	put := func(doc string, actor domain.Actor) (*domain.Knowledge, bool) {
		t.Helper()
		d, _, err := okf.Parse([]byte(doc))
		if err != nil {
			t.Fatal(err)
		}
		d.ID = id
		out, _, changed, err := svc.Put(ctx, &d.Knowledge, actor, nil)
		if err != nil {
			t.Fatal(err)
		}
		return out, changed
	}

	const written = "---\ntype: Metric\ntitle: 売上\nstatus: draft\n---\n\n受注合計。\n"
	// The same entry, laid out differently: a comment, and the title after
	// the status. Nothing it says has changed.
	const reformatted = "---\ntype: Metric\n# 定義は財務の合意による\nstatus: draft\ntitle: 売上\n---\n\n受注合計。\n"

	put(written, author)
	created, err := svc.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}

	got, changed := put(reformatted, editor)
	if !changed {
		t.Error("a reformatted document reported changed=false; the file did change")
	}
	if got.ContentHash == created.ContentHash {
		t.Error("a reformatted document left the version where it was")
	}
	if got.Doc != reformatted {
		t.Errorf("the reformatted bytes were not what got stored:\n%q", got.Doc)
	}
	if !got.ContentChangedAt.Equal(created.ContentChangedAt) {
		t.Errorf("reformatting moved generated.at: %v -> %v", created.ContentChangedAt, got.ContentChangedAt)
	}
	if got.UpdatedBy != created.UpdatedBy {
		t.Errorf("reformatting reassigned generated.by: %v -> %v", created.UpdatedBy, got.UpdatedBy)
	}

	// The same bytes again: nothing happened.
	before, err := svc.Revisions(ctx, id, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, changed := put(reformatted, editor); changed {
		t.Error("an identical document reported changed=true")
	}
	after, err := svc.Revisions(ctx, id, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Errorf("an identical document wrote a revision: %d -> %d", len(before), len(after))
	}

	// And a real edit moves both.
	edited, changed := put("---\ntype: Metric\ntitle: 売上\nstatus: draft\n---\n\n受注合計。返品は含まない。\n", editor)
	if !changed {
		t.Fatal("an edit reported changed=false")
	}
	if edited.ContentChangedAt.Equal(created.ContentChangedAt) {
		t.Error("an edit left generated.at where it was")
	}
	if edited.UpdatedBy != editor {
		t.Errorf("generated.by = %v, want the editor", edited.UpdatedBy)
	}
}

// TestUpdateNoOpIntegration exercises the no-op update path against a real
// PostgreSQL: a content-identical update must write nothing — no revision,
// no updated_at bump — so recurring imports stop flooding the audit trail.
// Skipped unless OCHAKAI_TEST_DATABASE_URL is set (see store integration
// test for the docker one-liner).
func TestUpdateNoOpIntegration(t *testing.T) {
	dbURL := testdb.URL(t)
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
	id := testdb.Unique(t, "svcit-")

	entry := func() *domain.Knowledge {
		return &domain.Knowledge{
			Type: domain.TypeMetrics, ID: id, Title: "売上",
			Description: "統合テスト用", Tags: []string{"sales"},
			// No status: the entry reads as OKF's default, which is what
			// makes the status-omitting payload below content-identical
			// rather than a claim about a different lifecycle value
			// (design doc 0046 §3.9).
			Attrs: map[string]any{"threshold": 5},
			Body:  "受注合計。[sales モデル](/model/sales.md) で定義。",
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
// doc 0030): a matching If-Match updates, a stale one is rejected with
// ErrConflict, and once the entry has moved on the old version stays
// rejected until re-read.
func TestUpdateIfMatchIntegration(t *testing.T) {
	dbURL := testdb.URL(t)
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
	id := testdb.Unique(t, "svcit-ifmatch-")

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
	v2, _, err := svc.Update(ctx, mk("v2"), actor, &base.ContentHash)
	if err != nil {
		t.Fatalf("If-Match update on the current version: %v", err)
	}

	// Writer B still holds the pre-A version: its update must be rejected,
	// not silently clobber A's write (the lost-update this fix closes).
	if _, _, err := svc.Update(ctx, mk("v3-from-stale"), actor, &base.ContentHash); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale If-Match: got %v, want ErrConflict", err)
	}
	// The stored content is still A's, untouched by B.
	if cur, err := svc.Get(ctx, id); err != nil {
		t.Fatal(err)
	} else if cur.Body != "v2" {
		t.Errorf("stale update clobbered the row: body = %q, want v2", cur.Body)
	}

	// After re-reading A's version, B can proceed.
	if _, _, err := svc.Update(ctx, mk("v3"), actor, &v2.ContentHash); err != nil {
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
	// Each ruling and how a human records it. Two are ledger writes now
	// and one is still a lifecycle value (design doc 0043 §§3.2-3.3), so
	// the guards are exercised through the three real paths rather than
	// through a status enum they no longer share.
	// A rejection is not among them: it is a deletion carrying its reason
	// (design doc 0135), and it gates nothing — the id it leaves behind is
	// open to the next writer, which "a rejected id stays writable" below
	// is the test for.
	rulings := []struct {
		ruling domain.Ruling
		rule   func(t *testing.T, k *domain.Knowledge)
		undo   func(t *testing.T, k *domain.Knowledge)
	}{
		{domain.RulingVerified, func(t *testing.T, k *domain.Knowledge) {
			t.Helper()
			if _, err := svc.Verify(ctx, k.ID, human); err != nil {
				t.Fatal(err)
			}
		}, nil},
		{domain.RulingDeprecated, func(t *testing.T, k *domain.Knowledge) {
			t.Helper()
			setStatus(t, k, domain.StatusDeprecated, human)
		}, func(t *testing.T, k *domain.Knowledge) {
			t.Helper()
			setStatus(t, k, domain.StatusDraft, human)
		}},
	}

	t.Run("drafts stay writable", func(t *testing.T) {
		id := "queries/" + uid(t, "guard-draft")
		k, err := svc.Create(ctx, &domain.Knowledge{Type: domain.TypeMetrics, ID: id, Title: "draft"}, actor)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = svc.Delete(ctx, id, actor, nil, "") })
		version, err := svc.RefuseIfCurated(ctx, id, "update")
		if err != nil {
			t.Fatalf("draft must be writable: %v", err)
		}
		// The guard hands back a usable precondition, not just a verdict.
		if version == nil || *version != k.ContentHash {
			t.Errorf("version = %v, want the entry's content hash %q", version, k.ContentHash)
		}
		// Promotion to stable is not restricted — 0002 stands.
		setStatus(t, k, domain.StatusStable, actor)
	})

	// Each curated state is refused, and each refusal names a way forward:
	// an agent that meets a wall with no door stalls or works around it.
	advice := map[domain.Ruling]string{
		domain.RulingVerified:   "report_outcome failed",
		domain.RulingDeprecated: "put_concept",
	}
	for _, tc := range rulings {
		t.Run(string(tc.ruling)+" is refused", func(t *testing.T) {
			id := "queries/" + uid(t, "guard-"+string(tc.ruling))
			k, err := svc.Create(ctx, &domain.Knowledge{
				Type: domain.TypeMetrics, ID: id, Title: string(tc.ruling),
			}, actor)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = svc.Delete(ctx, id, actor, nil, "") })
			tc.rule(t, k)

			for _, op := range []string{"update", "delete"} {
				_, err := svc.RefuseIfCurated(ctx, id, op)
				if _, ok := errors.AsType[*InvalidInputError](err); !ok {
					t.Fatalf("%s %s: got %v, want a refusal", op, tc.ruling, err)
				}
				if !strings.Contains(err.Error(), advice[tc.ruling]) {
					t.Errorf("%s refusal does not offer %q: %v", tc.ruling, advice[tc.ruling], err)
				}
			}

			// A human reopening it makes it writable again. A verification
			// has no undo: confirming an entry is not a state somebody
			// takes back, and re-drafting means editing the entry.
			if tc.undo == nil {
				return
			}
			latest, err := svc.Get(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			tc.undo(t, latest)
			if _, err := svc.RefuseIfCurated(ctx, id, "update"); err != nil {
				t.Errorf("reopened entry must be writable: %v", err)
			}
		})
	}

	// A rejected id stays writable, from every surface including the one
	// with no precondition (design doc 0135). The reason is information
	// rather than a wall: it is on the revision that recorded the removal,
	// which is what the §9 log publishes, and nothing blocks the next
	// writer from proposing there again.
	t.Run("a rejected id stays writable", func(t *testing.T) {
		id := "queries/" + uid(t, "guard-rejected")
		if _, err := svc.Create(ctx, &domain.Knowledge{
			Type: domain.TypeMetrics, ID: id, Title: "rejected proposal",
		}, actor); err != nil {
			t.Fatal(err)
		}
		if err := svc.Delete(ctx, id, human, nil, "duplicate of an existing golden query"); err != nil {
			t.Fatal(err)
		}
		if err := svc.RefuseIfRevivingCurated(ctx, id); err != nil {
			t.Errorf("a rejected id must stay open to the next writer: %v", err)
		}
		back, err := svc.CreateKeepingCurated(ctx, &domain.Knowledge{
			Type: domain.TypeMetrics, ID: id, Title: "second attempt"}, actor)
		if err != nil {
			t.Fatalf("re-proposing at a rejected id: %v", err)
		}
		t.Cleanup(func() { _ = svc.Delete(ctx, id, actor, nil, "") })
		if back.Title != "second attempt" {
			t.Errorf("the re-proposal did not land: %+v", back)
		}
		// And the reason is still readable, on the revision that removed it.
		revs, err := svc.Revisions(ctx, id, 10)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, r := range revs {
			if r.Change == "reject" && r.Note == "duplicate of an existing golden query" {
				found = true
			}
		}
		if !found {
			t.Errorf("the reason is not in the history: %+v", revs)
		}
	})

	// Closing the delete step is not enough: a tombstone that already
	// exists — deleted before the rule, or deleted from a human surface as
	// housekeeping — is revived in place by Create, status_note included.
	t.Run("a curated tombstone cannot be revived", func(t *testing.T) {
		tombAdvice := map[domain.Ruling]string{
			domain.RulingVerified:   "different id",
			domain.RulingDeprecated: "no longer recommended",
		}
		for _, tc := range rulings {
			status, wantAdvice := tc.ruling, tombAdvice[tc.ruling]
			id := "queries/" + uid(t, "guard-tomb-"+string(status))
			k, err := svc.Create(ctx, &domain.Knowledge{
				Type: domain.TypeMetrics, ID: id, Title: "ruled on then deleted",
			}, actor)
			if err != nil {
				t.Fatal(err)
			}
			tc.rule(t, k)
			// A human deletes it: legitimate on the REST/CLI surfaces.
			if err := svc.Delete(ctx, id, human, nil, ""); err != nil {
				t.Fatal(err)
			}
			err = svc.RefuseIfRevivingCurated(ctx, id)
			if _, ok := errors.AsType[*InvalidInputError](err); !ok {
				t.Fatalf("reviving a deleted %s entry: got %v, want a refusal", status, err)
			}
			if !strings.Contains(err.Error(), wantAdvice) {
				t.Errorf("%s refusal does not offer %q: %v", status, wantAdvice, err)
			}
			// The human surfaces still revive it, ruling and all.
			revived, err := svc.Create(ctx, &domain.Knowledge{
				Type: domain.TypeMetrics, ID: id, Title: "revived by a human"}, human)
			if err != nil {
				t.Fatalf("a human surface must still be able to reuse the id: %v", err)
			}
			t.Cleanup(func() { _ = svc.Delete(ctx, revived.ID, human, nil, "") })
		}
	})

	// A draft tombstone is not a ruling: reviving an abandoned draft is
	// what create-on-a-deleted-id is for.
	t.Run("a draft tombstone stays revivable", func(t *testing.T) {
		id := "queries/" + uid(t, "guard-tomb-draft")
		if _, err := svc.Create(ctx, &domain.Knowledge{
			Type: domain.TypeMetrics, ID: id, Title: "abandoned draft"}, actor); err != nil {
			t.Fatal(err)
		}
		if err := svc.Delete(ctx, id, actor, nil, ""); err != nil {
			t.Fatal(err)
		}
		if err := svc.RefuseIfRevivingCurated(ctx, id); err != nil {
			t.Fatalf("a deleted draft must stay revivable: %v", err)
		}
		if _, err := svc.Create(ctx, &domain.Knowledge{
			Type: domain.TypeMetrics, ID: id, Title: "second attempt"}, actor); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = svc.Delete(ctx, id, actor, nil, "") })
	})

	// A free id is not a refusal, and neither is a live one: Create's own
	// ErrAlreadyExists covers that case.
	t.Run("free and live ids are untouched", func(t *testing.T) {
		if err := svc.RefuseIfRevivingCurated(ctx, "queries/"+uid(t, "guard-tomb-free")); err != nil {
			t.Errorf("free id: %v", err)
		}
		id := "queries/" + uid(t, "guard-tomb-live")
		k, err := svc.Create(ctx, &domain.Knowledge{
			Type: domain.TypeMetrics, ID: id, Title: "live"}, actor)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = svc.Delete(ctx, id, actor, nil, "") })
		if _, err := svc.Verify(ctx, k.ID, human); err != nil {
			t.Fatal(err)
		}
		if err := svc.RefuseIfRevivingCurated(ctx, id); err != nil {
			t.Errorf("live entry: %v", err)
		}
	})

	// Creating over a live entry is still ErrAlreadyExists, but it has to
	// say what holds the id: the caller that lands here is the agent that
	// skipped the "search rejected first" step, and a bare "already
	// exists" leaves it guessing while every neighbouring refusal names
	// the next move.
	t.Run("a live entry says what holds the id", func(t *testing.T) {
		liveAdvice := map[domain.Ruling]string{
			domain.RulingVerified:   "report_outcome failed",
			domain.RulingDeprecated: "no longer recommended",
			"":                      "put_concept",
		}
		cases := append([]struct {
			ruling domain.Ruling
			rule   func(t *testing.T, k *domain.Knowledge)
			undo   func(t *testing.T, k *domain.Knowledge)
		}{{"", func(*testing.T, *domain.Knowledge) {}, nil}}, rulings...)
		for _, tc := range cases {
			status, wantAdvice := tc.ruling, liveAdvice[tc.ruling]
			id := "queries/" + uid(t, "guard-live-"+string(status)+"x")
			k, err := svc.Create(ctx, &domain.Knowledge{
				Type: domain.TypeMetrics, ID: id, Title: "ruled on"}, actor)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = svc.Delete(ctx, id, human, nil, "") })
			{
				tc.rule(t, k)
			}

			_, err = svc.CreateKeepingCurated(ctx, &domain.Knowledge{
				Type: domain.TypeMetrics, ID: id, Title: "re-proposal"}, actor)
			if !errors.Is(err, store.ErrAlreadyExists) {
				t.Fatalf("create over a live %s entry: got %v, want ErrAlreadyExists", status, err)
			}
			if !strings.Contains(err.Error(), string(status)) || !strings.Contains(err.Error(), wantAdvice) {
				t.Errorf("create over a live %s entry names neither the status nor a way forward: %v", status, err)
			}
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
	if _, ok := errors.AsType[*UnsupportedError](err); !ok {
		t.Errorf("without an embedder: got %v, want an UnsupportedError naming the setting", err)
	}
	svc.Embedder = embedder
	id := "insights/" + uid(t, "reembed")
	if _, err := svc.Create(ctx, &domain.Knowledge{
		Type: domain.TypeInsights, ID: id, Title: "reembed me", Body: "text",
	}, actor); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Delete(ctx, id, actor, nil, "") })

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
	// Only Concepts is assertable here: a corpus-wide pass runs over
	// whatever the other packages have in the shared database, including
	// entries they delete while it runs (counted as Failed).
	if res.Concepts == 0 {
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
	svc := newEmbeddingService(t, ctx, uid(t, "m"))
	model := svc.Embedder.Model()

	// Entries written before a provider was configured: created with no
	// embedder, so nothing wrote a vector. That is the state enabling
	// semantic search on an existing corpus leaves behind.
	svc.Embedder = nil
	for i := range 3 {
		id := "insights/" + uid(t, "missing")
		if _, err := svc.Create(ctx, &domain.Knowledge{
			Type: domain.TypeInsights, ID: id, Title: fmt.Sprintf("filler %d", i),
		}, actor); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = svc.Delete(ctx, id, actor, nil, "") })
	}
	svc.Embedder = &fixedEmbedder{model: model, dim: 4}

	before, err := svc.Store.CountUnembedded(ctx, model)
	if err != nil {
		t.Fatal(err)
	}
	if before < 3 {
		t.Fatalf("setup left %d unembedded entries, want at least 3", before)
	}

	// A pass smaller than the corpus must say what it did not reach.
	//
	// Attempted several times, because one attempt cannot tell drift from
	// the bug. Missing is counted inside Reembed, between the two counts
	// taken here, so an entry another package creates after the baseline
	// and deletes before the one below lands in Missing and in neither of
	// them: both measurements agree, the corpus looks still, and the
	// equality fails on a row this test never saw. Bracketing it tighter
	// is not possible from out here — the fix is repetition. Drift is a
	// coincidence of timing and misses most attempts; the two-off this
	// covers is in the arithmetic and misses every one.
	var stable bool // an attempt whose corpus did hold still
	var missing, left int
	for range reembedPinAttempts {
		base := unembeddedTotal(t, ctx, svc, model)
		res, err := svc.Reembed(ctx, "", 2)
		if err != nil {
			t.Fatal(err)
		}
		if res.Concepts > 2 {
			t.Fatalf("bounded pass embedded %d, over its limit of 2", res.Concepts)
		}
		// Missing is a fresh count taken after the pass, so the assertion
		// is an equality against a count taken the same way — not
		// arithmetic on the baseline. The bug this covers is a two-off
		// (subtracting the pass's work from a total that already excluded
		// it), which no tolerance wide enough for the drift could catch.
		after := unembeddedTotal(t, ctx, svc, model)
		// The pass takes the oldest unembedded entries in the whole
		// database, so a parallel package deleting one of its own
		// candidates mid-pass leaves the pass short, and one writing
		// entries moves the baseline under it. Neither says anything
		// about the reporting, so that attempt measured nothing.
		if res.Concepts < 2 || base-res.Concepts != after {
			continue
		}
		stable, missing, left = true, res.Missing, after
		if missing == left {
			return
		}
	}
	if !stable {
		t.Skipf("the corpus moved under all %d attempts; the count Reembed reports "+
			"cannot be pinned against a moving baseline", reembedPinAttempts)
	}
	if missing == 0 {
		t.Fatal("a bounded pass over a larger corpus reported nothing left")
	}
	t.Errorf("Missing = %d, want %d — the pass must report what is left, not what is left minus its own work",
		missing, left)
}

// unembeddedTotal counts what Reembed reports as Missing: the concepts
// with no vector plus the files, which carry vectors of their own
// (design doc 0020) and are counted into the same number. Measuring only
// the concept half made the assertion fail on a row belonging to nobody
// in this package — internal/store's attachment tests hold unembedded
// file rows in the shared database, Missing saw them, and the count it
// was compared against did not. The stability guard could not catch it
// either, since it watched the same concept-only count.
//
// This is measurement, not tolerance: a file row is genuinely still
// unembedded, so the equality is restored by counting it on both sides
// rather than by allowing the two numbers to differ.
func unembeddedTotal(t *testing.T, ctx context.Context, svc *Service, model string) int {
	t.Helper()
	concepts, err := svc.Store.CountUnembedded(ctx, model)
	if err != nil {
		t.Fatal(err)
	}
	files, err := svc.Store.CountUnembeddedFiles(ctx, model, svc.embeddableFileTypes())
	if err != nil {
		t.Fatal(err)
	}
	return concepts + files
}

// reembedPinAttempts is how many times TestReembedReportsWhatIsLeft will
// try for a measurement the shared database did not move under. Passing
// needs one attempt to come back clean; failing needs all of them to
// agree, which is what keeps a retry from hiding the bug it covers.
const reembedPinAttempts = 5

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

// Every timestamp a client reads back has to be the one the database
// holds, or a write's response describes an entry that never existed
// (design doc 0030 §3.2). The ruling ledgers stamp the database clock
// directly — the failure feed compares a verification against the last
// failure report, and two clocks a millisecond apart hide or keep the
// wrong reports (Store.Verify) — so what this checks is that what comes
// back out is exactly what went in, at a precision timestamptz can hold.
func TestVerificationTimestampsSurviveTheRoundTripIntegration(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t, ctx)
	actor := domain.Actor{Kind: "human", Name: "test"}

	for _, tc := range []struct {
		what string
		rule func(id string) (*domain.Knowledge, error)
		at   func(*domain.Knowledge) *time.Time
	}{
		{"verification",
			func(id string) (*domain.Knowledge, error) { return svc.Verify(ctx, id, actor) },
			func(k *domain.Knowledge) *time.Time {
				if v := k.LastVerified(); v != nil {
					return &v.At
				}
				return nil
			}},
	} {
		t.Run(tc.what, func(t *testing.T) {
			id := uid(t, "stampit")
			if _, err := svc.Create(ctx, &domain.Knowledge{
				Type: domain.TypeTerms, ID: id, Title: id,
			}, actor); err != nil {
				t.Fatal(err)
			}
			written, err := tc.rule(id)
			if err != nil {
				t.Fatal(err)
			}
			stamped := tc.at(written)
			if stamped == nil {
				t.Fatalf("%s recorded no timestamp", tc.what)
			}
			// Asserted directly, because a clock without nanosecond
			// resolution (macOS) would let the round-trip check below
			// pass on a value that loses precision on Linux.
			if truncated := stamped.Truncate(time.Microsecond); !truncated.Equal(*stamped) {
				t.Errorf("%s carries sub-microsecond precision timestamptz cannot store: %s",
					tc.what, stamped.Format(time.RFC3339Nano))
			}
			read, err := svc.Get(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := *tc.at(read), *tc.at(written); !got.Equal(want) {
				t.Errorf("%s: write returned %s, database holds %s", tc.what,
					want.Format(time.RFC3339Nano), got.Format(time.RFC3339Nano))
			}
		})
	}
}

// updated_by is OKF's generated.by: who the content stands by now. It
// follows content changes and nothing else — a verification confirms the
// content, it does not produce it, which is the distinction v0.2 draws
// between verified and generated (design doc 0036 §3.3).
func TestUpdatedByFollowsContentIntegration(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t, ctx)
	author := domain.Actor{Kind: "agent", Name: "claude-code"}
	editor := domain.Actor{Kind: "human", Name: "tanaka"}
	reviewer := domain.Actor{Kind: "human", Name: "na0"}

	id := uid(t, "generated-by")
	k, err := svc.Create(ctx, &domain.Knowledge{
		Type: domain.TypeTerms, ID: id, Title: id, Body: "First.",
		StaleAfter: "2026-12-31",
	}, author)
	if err != nil {
		t.Fatal(err)
	}
	if k.UpdatedBy != author || k.CreatedBy != author {
		t.Fatalf("create: created_by=%v updated_by=%v, want both %v", k.CreatedBy, k.UpdatedBy, author)
	}
	if k.StaleAfter != "2026-12-31" {
		t.Errorf("stale_after = %q on the create response", k.StaleAfter)
	}

	// An update that changes nothing writes nothing, so it must not
	// re-attribute the content either.
	if _, changed, err := svc.Update(ctx, &domain.Knowledge{
		Type: domain.TypeTerms, ID: id, Title: id, Body: "First.", StaleAfter: "2026-12-31",
	}, editor, nil); err != nil || changed {
		t.Fatalf("no-op update: changed=%v err=%v", changed, err)
	}
	read, err := svc.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if read.UpdatedBy != author {
		t.Errorf("a no-op update re-attributed the content to %v", read.UpdatedBy)
	}
	// The update above sent the date and was still a no-op, which is the
	// property worth pinning: the two spellings are one value, so
	// re-sending a moment in the other one is not an edit. What comes
	// back is the spelling SPEC §5 defines, the column holding an instant
	// and not the text (design doc 0139 §3.2).
	if read.StaleAfter != "2026-12-31T00:00:00Z" {
		t.Errorf("stale_after = %q after a round-trip through the database", read.StaleAfter)
	}

	if _, changed, err := svc.Update(ctx, &domain.Knowledge{
		Type: domain.TypeTerms, ID: id, Title: id, Body: "Second.",
	}, editor, nil); err != nil || !changed {
		t.Fatalf("real update: changed=%v err=%v", changed, err)
	}
	if read, err = svc.Get(ctx, id); err != nil {
		t.Fatal(err)
	}
	if read.UpdatedBy != editor {
		t.Errorf("updated_by = %v after an edit by %v", read.UpdatedBy, editor)
	}
	if read.CreatedBy != author {
		t.Errorf("created_by moved to %v; it belongs to whoever created the entry", read.CreatedBy)
	}
	if read.StaleAfter != "" {
		t.Errorf("stale_after = %q; an update that omits it clears it, like every other content field", read.StaleAfter)
	}

	if _, err := svc.Verify(ctx, id, reviewer); err != nil {
		t.Fatal(err)
	}
	if read, err = svc.Get(ctx, id); err != nil {
		t.Fatal(err)
	}
	if read.UpdatedBy != editor {
		t.Errorf("a verification changed updated_by to %v; confirming is not producing", read.UpdatedBy)
	}
	if v := read.LastVerified(); v == nil || v.By != reviewer {
		t.Errorf("verification = %v, want one by %v", v, reviewer)
	}
}

// A stale_after that is not an absolute date is refused at the boundary
// rather than reaching the date column (OKF SPEC §5.5).
func TestStaleAfterValidationIntegration(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t, ctx)
	actor := domain.Actor{Kind: "human", Name: "test"}
	_, err := svc.Create(ctx, &domain.Knowledge{
		Type: domain.TypeTerms, ID: uid(t, "bad-stale"), Title: "x", StaleAfter: "30 days",
	}, actor)
	if _, ok := errors.AsType[*InvalidInputError](err); !ok {
		t.Fatalf("create with a relative stale_after: %v, want an invalid-input error", err)
	}
}

// The OKF v0.2 families are refused at the write boundary when they do not
// match the shape SPEC §5.1 / §10.2 define (design doc 0036 §3.4). Bundle
// import is deliberately more forgiving — it drops the value and reports a
// note — but a caller naming a value it means gets told.
func TestOKFFamilyValidationIntegration(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t, ctx)
	actor := domain.Actor{Kind: "human", Name: "test"}

	for name, k := range map[string]*domain.Knowledge{
		"a source with no resource": {
			Type: domain.TypeTerms, Sources: []domain.Source{{ID: "rev", Title: "Policy"}}},
		"a source date that is not a date": {
			Type: domain.TypeTerms, Sources: []domain.Source{{Resource: "r.md", LastModified: "June"}}},
		"a usage window that is not dated": {
			Type: domain.TypeTerms, UsageWindow: &domain.UsageWindow{From: "last quarter"}},
		"a parameter with no type": {
			Type: domain.TypeTerms, Parameters: []domain.Parameter{{Name: "year"}}},
		"an executor with no resource": {
			Type: domain.TypeTerms, Executor: &domain.Executor{Receipt: []string{"job_id"}}},
		"an attester with no resource": {
			Type: domain.TypeTerms, Attester: &domain.Attester{}},
		"an Attested Computation with no runtime": {
			Type: domain.TypeComputations},
		// An attr shadowing an envelope key used to survive the write and
		// then vanish from the export; refusing it says so at the boundary.
		"an envelope key hidden in attrs": {
			Type: domain.TypeTerms, Attrs: map[string]any{"sources": []any{}}},
	} {
		t.Run(name, func(t *testing.T) {
			k.ID, k.Title = uid(t, "okf-invalid"), "x"
			var invalid *InvalidInputError
			if _, err := svc.Create(ctx, k, actor); !errors.As(err, &invalid) {
				t.Fatalf("create with %s: %v, want an invalid-input error", name, err)
			}
		})
	}

	// The runtime requirement is scoped to the one type SPEC §10.2 puts it
	// on; no other type gains a required field (design doc 0036 §5).
	if _, err := svc.Create(ctx, &domain.Knowledge{
		Type: domain.TypeTerms, ID: uid(t, "no-runtime-ok"), Title: "x",
	}, actor); err != nil {
		t.Fatalf("a Glossary Term must not need a runtime: %v", err)
	}

	// And the receipt was never one of SPEC §10.2's requirements: only
	// runtime is marked REQUIRED there, and §11 forbids refusing a
	// concept for a missing optional field (design doc 0079).
	if _, err := svc.Create(ctx, &domain.Knowledge{
		Type: domain.TypeComputations, ID: uid(t, "no-receipt-ok"), Title: "x",
		Runtime: "bigquery", Executor: &domain.Executor{Resource: "run.md"},
	}, actor); err != nil {
		t.Fatalf("an executor with no receipt must be writable: %v", err)
	}

	// A full contract round-trips through the database intact, including
	// the zero usage_count that means "counted, and never used".
	id := uid(t, "okf-full")
	zero := 0
	want := &domain.Knowledge{
		Type: domain.TypeComputations, ID: id, Title: "x", Runtime: "bigquery",
		Sources: []domain.Source{{
			Resource: "https://wiki.example/rev", ID: "rev", UsageCount: &zero,
			LastModified: "2026-06-15", UsageWindow: &domain.UsageWindow{From: "2026-05-01"},
		}},
		UsageWindow: &domain.UsageWindow{From: "2026-06-01", To: "2026-06-30"},
		Parameters:  []domain.Parameter{{Name: "year", Type: "integer", Required: true}},
		Computation: "queries/rev.sql",
		Executor:    &domain.Executor{Resource: "run.md", Receipt: []string{"job_id"}},
		Attester:    &domain.Attester{Resource: "check.py"},
	}
	if _, err := svc.Create(ctx, want, actor); err != nil {
		t.Fatal(err)
	}
	read, err := svc.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if read.Sources[0].UsageCount == nil || *read.Sources[0].UsageCount != 0 {
		t.Errorf("usage_count = %v, want a pointer to 0", read.Sources[0].UsageCount)
	}
	if !reflect.DeepEqual(read.Sources, want.Sources) ||
		!reflect.DeepEqual(read.Parameters, want.Parameters) ||
		!reflect.DeepEqual(read.Executor, want.Executor) ||
		!reflect.DeepEqual(read.Attester, want.Attester) ||
		!reflect.DeepEqual(read.UsageWindow, want.UsageWindow) ||
		read.Runtime != want.Runtime || read.Computation != want.Computation {
		t.Errorf("the contract did not survive the database:\ngot  %+v\nwant %+v", read, want)
	}

	// Editing only a citation is a content change: a write that reordered
	// or reworded the sources must not be swallowed as a no-op.
	edited := *read
	edited.Sources = []domain.Source{{Resource: "https://wiki.example/rev", ID: "rev",
		Title: "Revenue policy", UsageCount: &zero, LastModified: "2026-06-15",
		UsageWindow: &domain.UsageWindow{From: "2026-05-01"}}}
	if _, changed, err := svc.Update(ctx, &edited, actor, nil); err != nil || !changed {
		t.Fatalf("editing a source's title: changed=%v err=%v, want a write", changed, err)
	}
}

// A request with no query and no feed is the plain enumeration: the set
// the filters describe, in address order (design doc 0068 §2). It is what
// a curator asks to take stock of a directory — "what is under here" —
// and until this landed the only way to ask was to name a feed whose
// order had nothing to do with the question.
//
// Two things have to hold, and neither is visible from one call. The
// order is the id, so a caller can pair the answer with a destination and
// a script can diff it. And nothing is recorded as read: walking a
// subtree is not demand for what is in it, and the usage feed ranks on
// exactly that count (design doc 0069), so an enumeration that counted
// would move the queue a reviewer works from.
func TestEnumerationListsInAddressOrderIntegration(t *testing.T) {
	dbURL := testdb.URL(t)
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

	run := testdb.Unique(t, "svcit-enumerate-")
	// Created out of address order, so an answer that came back in
	// insertion order would fail rather than pass by luck.
	var want []string
	for _, seg := range []string{"c", "a", "b"} {
		id := run + "/" + seg
		want = append(want, id)
		if err := s.Create(ctx, &domain.Knowledge{
			Type: domain.TypeInsights, ID: id, Title: "草案 " + seg,
			Status: domain.StatusDraft, CreatedBy: actor,
		}, false); err != nil {
			t.Fatal(err)
		}
	}
	slices.Sort(want)

	filter := func() store.Filter { return store.Filter{Prefixes: []string{run}} }
	// A query of nothing but spaces is a request with nothing to rank by,
	// which is the same request as one carrying no query at all — the
	// service trims before it decides, so the two cannot diverge.
	for _, query := range []string{"", "  \t"} {
		page, err := svc.SearchOrList(ctx, query, "", "", filter(), 100)
		if err != nil {
			t.Fatalf("query %q: %v", query, err)
		}
		var got []string
		for _, h := range page.Hits {
			got = append(got, h.ID)
		}
		if !slices.Equal(got, want) {
			t.Errorf("query %q listed %v, want %v in address order", query, got, want)
		}
		if page.Cursor != "" {
			t.Errorf("query %q: a listing that ended handed back a cursor: %q", query, page.Cursor)
		}
	}

	// The whole base is the same listing with the prefix dropped: asking
	// for everything is a request the server answers rather than a
	// mistake it reports. The test database is shared, so what is pinned
	// is that this run's entries are in it, still in address order.
	all, err := svc.SearchOrList(ctx, "", "", "", store.Filter{}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	var mine []string
	for _, h := range all.Hits {
		if strings.HasPrefix(h.ID, run+"/") {
			mine = append(mine, h.ID)
		}
	}
	// A shared database can hold more than one page of entries before
	// this run's, so an unfiltered walk may not reach them at all. What
	// it must never do is hand them back out of order.
	if !slices.IsSorted(mine) {
		t.Errorf("the unfiltered listing is out of address order: %v", mine)
	}

	if err := s.FlushUsage(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range want {
		u, err := svc.Usage(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if u.SearchHits != 0 || u.Fetches != 0 {
			t.Errorf("%s was counted as read by an enumeration: %d search hits, %d fetches",
				id, u.SearchHits, u.Fetches)
		}
	}
}
