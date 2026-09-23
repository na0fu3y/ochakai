package service

import (
	"context"
	"errors"
	"slices"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/httpauth"
	"github.com/na0fu3y/ochakai/internal/store"
)

// A turn's texts are cut to this, like a miss's query: they are what the
// loop reads to see what was asked, not a transcript.
const maxTurnText = 1000

// RecordAgentTurn keeps the shape of one answered turn and returns its
// id (design doc 0142 §6). Like a usage event it is best effort: a turn
// that could not be kept is logged, and the answer still goes out — an
// id of "" says there is nothing to judge.
func (s *Service) RecordAgentTurn(ctx context.Context, asked, latest string, read []string, sql string) string {
	id, err := s.Store.RecordAgentTurn(ctx, httpauth.Actor(ctx),
		cutText(asked, maxTurnText), cutText(latest, maxTurnText), read, sql)
	if err != nil {
		if s.Log != nil {
			s.Log.Warn("agent turn not kept", "error", err)
		}
		return ""
	}
	return id
}

// Judgment is the person's verdict on one answer.
type Judgment struct {
	Verdict string   `json:"verdict"` // good or bad
	Note    string   `json:"note"`
	Blame   []string `json:"blame"` // bad only: the concepts that misled it
	Keep    bool     `json:"keep"`  // good only: use the question for comparison
}

// JudgeResult is the judged turn and the outcome reports it became.
type JudgeResult struct {
	Turn     *store.AgentTurn `json:"turn"`
	Reported []string         `json:"reported"`
}

// JudgeAgentTurn records the verdict of the person who asked, once, and
// turns it into their own outcome reports (design doc 0142 §3): good is
// "worked" for every concept the answer read, bad is "failed" only for
// the ones the person blamed. A verdict is not a verification and moves
// no tier.
func (s *Service) JudgeAgentTurn(ctx context.Context, id string, j Judgment) (*JudgeResult, error) {
	if err := s.readOnly(); err != nil {
		return nil, err
	}
	t, err := s.Store.AgentTurn(ctx, id)
	if err != nil {
		return nil, err
	}
	// Only the person who asked judges the answer. Anybody else is told
	// the turn is not there, the way a read outside a grant is (0109).
	if t.Actor != domain.PrincipalOf(httpauth.Actor(ctx)) {
		return nil, store.ErrNotFound
	}
	switch j.Verdict {
	case "good":
		if len(j.Blame) > 0 {
			return nil, Invalidf("blame goes with a bad verdict: a good answer blames nothing")
		}
	case "bad":
		if j.Keep {
			return nil, Invalidf("keep goes with a good verdict: a question is kept for comparison with the answer it should get")
		}
		for _, b := range j.Blame {
			if !slices.Contains(t.Read, b) {
				return nil, Invalidf("blame names %q, which this answer did not read", b)
			}
		}
	default:
		return nil, Invalidf("verdict is %q; it is good or bad", j.Verdict)
	}
	if len(j.Note) > maxOutcomeNote {
		return nil, Invalidf("note exceeds %d bytes", maxOutcomeNote)
	}
	fresh, err := s.Store.JudgeAgentTurn(ctx, id, j.Verdict, j.Note, j.Blame, j.Keep)
	if err != nil {
		return nil, err
	}
	if !fresh {
		return nil, Invalidf("this answer has already been judged")
	}
	outcome, targets := domain.EventWorked, t.Read
	if j.Verdict == "bad" {
		outcome, targets = domain.EventFailed, j.Blame
	}
	res := &JudgeResult{Reported: []string{}}
	for _, cid := range targets {
		// A concept deleted or moved since the answer read it has nothing
		// to report against; the verdict stands without it.
		if _, err := s.ReportOutcome(ctx, cid, outcome, j.Note); err != nil {
			if errors.Is(err, store.ErrNotFound) || errors.Is(err, ErrForbidden) {
				continue
			}
			return nil, err
		}
		res.Reported = append(res.Reported, cid)
	}
	if res.Turn, err = s.Store.AgentTurn(ctx, id); err != nil {
		return nil, err
	}
	return res, nil
}

// AgentTurns lists turns for the agent's own reading: an administrator's
// covers everybody, anyone else's only what they asked themselves
// (design doc 0142 §6).
func (s *Service) AgentTurns(ctx context.Context, verdict string, keep bool, limit int) ([]store.AgentTurn, error) {
	f := store.AgentTurnFilter{Verdict: verdict, Keep: keep, Limit: limit}
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 30
	}
	if err := s.RequireAdmin(ctx, "read other people's agent turns"); err != nil {
		f.Actor = domain.PrincipalOf(httpauth.Actor(ctx))
	}
	return s.Store.AgentTurns(ctx, f)
}

func cutText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n]
}
