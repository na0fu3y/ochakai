package service

import (
	"context"
	"log/slog"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/store"
	"github.com/na0fu3y/ochakai/internal/testdb"
)

// TestMissesAreRecordedWhereTheBaseEmbedsIntegration is the case the
// miss existed for and could not see. The vector list has no floor — it
// returns the nearest N whatever the distance — so on a deployment that
// embeds, every question against a non-empty base came back with hits,
// and "a search that found nothing" stopped happening on the
// configuration every Google Cloud deployment runs. The miss is now read
// off the lexical list, which both kinds of deployment compute the same
// way; the answer the caller gets is unchanged.
func TestMissesAreRecordedWhereTheBaseEmbedsIntegration(t *testing.T) {
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

	typ := domain.Type(testdb.Unique(t, "svcmiss"))
	id := string(typ) + "/revenue"
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "test"}
	// A word only this run's concept holds: the misses table is shared
	// with every other test, and a common word has been missed there
	// before this test ran.
	word := testdb.Unique(t, "uriage")
	svc := &Service{Store: s, Embedder: stubEmbedder{}, Log: slog.New(slog.DiscardHandler)}
	if _, err := svc.Create(ctx, &domain.Knowledge{
		Type: typ, ID: id, Title: word, Status: domain.StatusDraft,
		Body: "受注合計。", CreatedBy: actor,
	}, actor); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.SoftDelete(ctx, id, actor, nil, "") })
	f := store.Filter{Types: []domain.Type{typ}}

	nonsense := testdb.Unique(t, "qqzzxx")
	t.Cleanup(func() { deleteMisses(ctx, t, dbURL, nonsense) })
	hits, _, err := svc.Search(ctx, nonsense, f, 10)
	if err != nil {
		t.Fatal(err)
	}
	// The ranking still carries the nearest concept: the vector half is
	// the answer's to give, and what changed is only what is recorded.
	if len(hits) == 0 {
		t.Fatal("the vector half returned nothing; the premise of this test is that it always returns the nearest")
	}
	if err := s.FlushUsage(ctx); err != nil {
		t.Fatal(err)
	}
	if n := countMisses(ctx, t, dbURL, nonsense); n != 1 {
		t.Errorf("a question no word of the base matched was recorded %d times, want 1 — "+
			"the vector list's nearest neighbour is not an answer", n)
	}

	// A question the words do match is not a miss, embedder or not.
	t.Cleanup(func() { deleteMisses(ctx, t, dbURL, word) })
	if _, _, err := svc.Search(ctx, word, f, 10); err != nil {
		t.Fatal(err)
	}
	if err := s.FlushUsage(ctx); err != nil {
		t.Fatal(err)
	}
	if n := countMisses(ctx, t, dbURL, word); n != 0 {
		t.Errorf("a question the base answers by its words was recorded as a miss %d times", n)
	}
}
