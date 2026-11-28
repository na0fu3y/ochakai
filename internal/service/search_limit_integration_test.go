package service

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/store"
	"github.com/na0fu3y/ochakai/internal/testdb"
)

// TestSearchOrderDoesNotDependOnLimitIntegration is the guarantee
// docs/spec/search.md gives a caller, asked of the real store: for any
// query and filter, search(limit=k) is the first k of search(limit=50),
// for every k a caller can ask for. Before the candidate lists were read
// at one depth, a short search fused shorter lists than a long one, and
// an embedding caller that wanted both the answer at k and a deep list to
// re-rank had to send the question twice.
//
// The corpus is larger than searchDepth and every concept says the
// query's first word, so each list is cut by the depth rather than by
// running out; the rest of each body is drawn from a small vocabulary so
// lexical scores tie and the vectors disagree with the words. It is run
// with the embedder (the fused path), without one (lexical alone), and
// under a narrower filter.
func TestSearchOrderDoesNotDependOnLimitIntegration(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := store.New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx, 4); err != nil { // dim 4, as the store tests
		t.Fatal(err)
	}
	fused := &Service{Store: s, Embedder: fakeEncoder{dim: 4}, Log: slog.New(slog.DiscardHandler)}
	lexical := &Service{Store: s, Log: slog.New(slog.DiscardHandler)}

	typ := domain.Type(testdb.Unique(t, "limitinv"))
	word := testdb.Unique(t, "uriage")
	vocab := []string{"revenue", "orders", "churn", "margin", "cohort", "refund", "region"}
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "test"}
	for i := range searchDepth + 20 {
		id := fmt.Sprintf("%s/c%03d", typ, i)
		body := word
		for j, w := range vocab {
			if (i>>j)&1 == 1 {
				body += " " + w
			}
		}
		var tags []string
		if i%3 == 0 {
			tags = []string{"third"}
		}
		if _, err := fused.Create(ctx, &domain.Knowledge{
			Type: typ, ID: id, Title: fmt.Sprintf("concept %d", i), Status: domain.StatusDraft,
			Body: body + "。", Tags: tags, CreatedBy: actor,
		}, actor); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = s.SoftDelete(ctx, id, actor, nil, "") })
	}

	ids := func(hits []domain.SearchHit) []string {
		out := make([]string, len(hits))
		for i, h := range hits {
			out[i] = h.ID
		}
		return out
	}
	filters := map[string]store.Filter{
		"type":       {Types: []domain.Type{typ}},
		"type & tag": {Types: []domain.Type{typ}, Tags: []string{"third"}},
	}
	services := map[string]*Service{"fused": fused, "lexical": lexical}
	for _, query := range []string{word, word + " revenue churn", "margin cohort"} {
		for fname, f := range filters {
			for sname, svc := range services {
				t.Run(fmt.Sprintf("%s/%s/%q", sname, fname, query), func(t *testing.T) {
					hits, degraded, err := svc.Search(ctx, query, f, maxSearchLimit)
					if err != nil {
						t.Fatal(err)
					}
					if degraded {
						t.Fatal("the ranking degraded; the invariant is about the ranking this deployment gives")
					}
					deepest := ids(hits)
					if len(deepest) == 0 {
						t.Fatal("the search found nothing to rank")
					}
					for k := 1; k <= maxSearchLimit; k++ {
						hits, _, err := svc.Search(ctx, query, f, k)
						if err != nil {
							t.Fatal(err)
						}
						want := deepest[:min(k, len(deepest))]
						if got := ids(hits); !slices.Equal(got, want) {
							t.Fatalf("limit %d answered %v, want the first %d at limit %d: %v",
								k, got, k, maxSearchLimit, want)
						}
					}
				})
			}
		}
	}
}
