package service

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/config"
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

	// The asker reads their kept turn back, which is what the agent's
	// triage reads. With no access policy every caller reads the whole
	// bundle's turns, so other tests' turns sharing this database may be
	// here too; only this test's are asserted on.
	kept, err := svc.AgentTurns(askerCtx, "", true, 100)
	if err != nil {
		t.Fatal(err)
	}
	var sawGood bool
	for _, k := range kept {
		sawGood = sawGood || k.ID == good
		if k.ID == bad {
			t.Error("a turn judged bad is in the comparison set")
		}
	}
	if !sawGood {
		t.Errorf("kept turns miss %s: %+v", good, kept)
	}
}

// A turn some other agent answered is kept as the caller's, says which
// agent answered, is judged the same way, and is read back a page at a
// time (design doc 0144).
func TestTurnsFromAnyAgentIntegration(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(ctx, testdb.URL(t), false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	svc := &Service{Store: s, Log: slog.New(slog.DiscardHandler), Config: &config.Config{Version: "v9.9.9"}}
	run := testdb.Unique(t, "svcit-anyturn-")
	asker := domain.Actor{Kind: domain.ActorHuman, Name: run + "@example.com"}
	ids := []string{run + "/a", run + "/b"}
	for _, id := range ids {
		if err := s.Create(ctx, &domain.Knowledge{Type: domain.TypeInsights, ID: id, Title: id, Status: domain.StatusDraft, CreatedBy: asker}, false); err != nil {
			t.Fatal(err)
		}
	}
	// The application's agent answered; the application forwards the
	// person it answered and names itself.
	app := asker
	app.Via, app.Producer = "process:app@example.iam.gserviceaccount.com", "insightflow/1.4.0"
	appCtx := httpauth.WithActor(ctx, app)

	kept, err := svc.KeepAgentTurn(appCtx, TurnIn{Asked: "先月の売上は?", Read: []string{ids[0], ids[1], ids[0]}, SQL: "SELECT 1"})
	if err != nil {
		t.Fatal(err)
	}
	if kept.Actor != domain.PrincipalOf(asker) || kept.Via != app.Via || kept.Producer != app.Producer {
		t.Errorf("kept turn is by %q via %q from %q; want the forwarded person, the app, and its producer", kept.Actor, kept.Via, kept.Producer)
	}
	if kept.Latest != kept.Asked || !slices.Equal(kept.Read, ids) || kept.ProposedSQL != "SELECT 1" {
		t.Errorf("kept turn = %+v; want asked twice, read once each, the sql", kept)
	}

	// What cannot be kept whole is refused, not cut.
	for name, in := range map[string]TurnIn{
		"no question":       {Read: []string{}},
		"a long question":   {Asked: strings.Repeat("あ", 400), Read: []string{}},
		"no read":           {Asked: "q"},
		"too many reads":    {Asked: "q", Read: make([]string, maxTurnRead+1)},
		"a long query":      {Asked: "q", Read: []string{}, SQL: strings.Repeat("x", maxTurnSQL+1)},
		"a missing concept": {Asked: "q", Read: []string{run + "/missing"}},
	} {
		if _, err := svc.KeepAgentTurn(appCtx, in); !errors.As(err, new(*InvalidInputError)) {
			t.Errorf("%s: err = %v, want invalid input", name, err)
		}
	}

	// The deployment's own agent's turns say they are its own, whatever
	// the caller claimed to be.
	own := svc.RecordAgentTurn(appCtx, "粗利は?", "粗利は?", ids[:1], "")
	ownTurn, err := s.AgentTurn(ctx, own)
	if err != nil {
		t.Fatal(err)
	}
	if ownTurn.Producer != "ochakai/v9.9.9" {
		t.Errorf("own agent's turn producer = %q, want ochakai/v9.9.9", ownTurn.Producer)
	}

	// Judged by the person it was kept for, into their own reports.
	askerCtx := httpauth.WithActor(ctx, asker)
	res, err := svc.JudgeAgentTurn(askerCtx, kept.ID, Judgment{Verdict: "good", Keep: true})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Reported, ids) {
		t.Errorf("reported = %v, want %v", res.Reported, ids)
	}

	keptOnly, err := svc.AgentTurnPage(askerCtx, "good", true, 100, "")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(keptOnly.Turns, func(tr store.AgentTurn) bool { return tr.ID == kept.ID }) {
		t.Errorf("verdict=good&keep=true misses the kept turn %s", kept.ID)
	}
	for name, call := range map[string]func() error{
		"a verdict nobody gives":  func() error { _, err := svc.AgentTurnPage(askerCtx, "meh", false, 0, ""); return err },
		"a limit over 100":        func() error { _, err := svc.AgentTurnPage(askerCtx, "", false, 101, ""); return err },
		"a cursor not handed out": func() error { _, err := svc.AgentTurnPage(askerCtx, "", false, 0, "bm9wZQ"); return err },
	} {
		if err := call(); !errors.As(err, new(*InvalidInputError)) {
			t.Errorf("%s: err = %v, want invalid input", name, err)
		}
	}
}
