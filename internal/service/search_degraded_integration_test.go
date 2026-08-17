package service

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/embed"
	"github.com/na0fu3y/ochakai/internal/store"
)

// failingEmbedder is a provider that is configured and does not answer —
// the outage design doc 0114 is about. It embeds nothing, which is the
// difference from having no embedder at all.
type failingEmbedder struct{}

func (failingEmbedder) Embed(context.Context, embed.Task, []string) ([][]float32, error) {
	return nil, errors.New("provider unavailable")
}

func (failingEmbedder) Model() string { return "failing" }

// TestSearchSaysWhenItDegradedIntegration is design doc 0114: an embedder
// that cannot answer degrades the ranking to lexical alone, and the
// answer says so rather than only the log.
//
// The three cases are the whole of the rule. A deployment with no
// embedder is the one worth pinning hardest: it also ranks lexically, and
// calling that degraded would put the flag on every response of every
// deployment that never configured embeddings — which is how a signal
// stops being one.
func TestSearchSaysWhenItDegradedIntegration(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t, ctx)
	actor := domain.Actor{Kind: "human", Name: "test"}

	typ := domain.Type(uid(t, "degr"))
	id := string(typ) + "/revenue"
	if _, err := svc.Create(ctx, &domain.Knowledge{
		Type: typ, ID: id, Title: "revenue", Body: "受注合計。",
	}, actor); err != nil {
		t.Fatal(err)
	}
	f := store.Filter{Types: []domain.Type{typ}}

	// No embedder: lexical is the answer, not a fallback from one.
	hits, degraded, err := svc.Search(ctx, "revenue", f, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("the lexical search found nothing to rank")
	}
	if degraded {
		t.Error("a deployment with no embedder reported a degraded ranking; " +
			"lexical is its whole answer, every time")
	}

	// An embedder that cannot answer: the same hits, and the caller is
	// told they are not the ones this deployment would ordinarily give.
	failing := &Service{Store: svc.Store, Embedder: failingEmbedder{}, Log: slog.New(slog.DiscardHandler)}
	hits, degraded, err = failing.Search(ctx, "revenue", f, 10)
	if err != nil {
		t.Fatalf("the search failed instead of degrading: %v", err)
	}
	if len(hits) == 0 {
		t.Error("degrading dropped the lexical hits too; the fallback is the point")
	}
	if !degraded {
		t.Error("the embedding failed and the answer did not say so")
	}

	// A listing has no query to embed, so it is never degraded — the
	// same service, the same outage.
	page, err := failing.SearchOrList(ctx, "", "verified_at", "", f, 10)
	if err != nil {
		t.Fatal(err)
	}
	if page.Degraded {
		t.Error("a listing reported a degraded ranking; it ranks nothing")
	}
}
