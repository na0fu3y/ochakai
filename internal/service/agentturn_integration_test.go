package service

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/httpauth"
	"github.com/na0fu3y/ochakai/internal/store"
	"github.com/na0fu3y/ochakai/internal/testdb"
)

// A verdict is the asker's, once, and becomes the asker's own outcome
// reports — worked for everything a good answer read, failed only for
// what a bad one was blamed on (design doc 0142 §3, §6).
func TestAgentTurnsAreJudgedByWhoAskedIntegration(t *testing.T) {
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
	run := testdb.Unique(t, "svcit-turn-")
	asker := domain.Actor{Kind: domain.ActorHuman, Name: run + "@example.com"}
	askerCtx := httpauth.WithActor(ctx, asker)
	ids := []string{run + "/a", run + "/b"}
	for _, id := range ids {
		if err := s.Create(ctx, &domain.Knowledge{Type: domain.TypeInsights, ID: id, Title: id, Status: domain.StatusDraft, CreatedBy: asker}, false); err != nil {
			t.Fatal(err)
		}
	}

	good := svc.RecordAgentTurn(askerCtx, "売上は?", "売上は?", ids, "")
	bad := svc.RecordAgentTurn(askerCtx, "粗利は?", "粗利は?", ids, "SELECT 1")
	if good == "" || bad == "" {
		t.Fatal("a turn was not kept")
	}

	// Somebody else is told the turn is not there.
	stranger := httpauth.WithActor(ctx, domain.Actor{Kind: domain.ActorHuman, Name: "stranger@example.com"})
	if _, err := svc.JudgeAgentTurn(stranger, good, Judgment{Verdict: "good"}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a stranger's verdict: err = %v, want not found", err)
	}

	res, err := svc.JudgeAgentTurn(askerCtx, good, Judgment{Verdict: "good", Keep: true})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Reported, ids) || res.Turn.Verdict != "good" || !res.Turn.Keep {
		t.Errorf("good verdict = %+v", res)
	}
	if _, err := svc.JudgeAgentTurn(askerCtx, good, Judgment{Verdict: "bad"}); err == nil {
		t.Error("a turn was judged twice")
	}

	// Blame outside what the answer read is refused; blame inside it is
	// the only thing that gets a failed report.
	if _, err := svc.JudgeAgentTurn(askerCtx, bad, Judgment{Verdict: "bad", Blame: []string{"elsewhere"}}); err == nil {
		t.Error("blame on a concept the answer did not read was accepted")
	}
	res, err = svc.JudgeAgentTurn(askerCtx, bad, Judgment{Verdict: "bad", Note: "返品を引いていない", Blame: ids[:1]})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Reported, ids[:1]) {
		t.Errorf("reported = %v, want only the blamed concept", res.Reported)
	}
	a, _ := s.Usage(ctx, ids[0])
	b, _ := s.Usage(ctx, ids[1])
	if a.Worked != 1 || a.Failed != 1 || b.Worked != 1 || b.Failed != 0 {
		t.Errorf("usage a=%+v b=%+v; want a worked+failed, b worked only", a, b)
	}

	// The asker reads their own turns back, which is what the agent's
	// triage reads.
	kept, err := svc.AgentTurns(askerCtx, "", true, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 1 || kept[0].ID != good {
		t.Errorf("kept turns = %+v", kept)
	}
}
