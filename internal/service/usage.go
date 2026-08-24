// The write-back loop's measurements: revisions, per-concept usage,
// outcome reports from agents that acted on a concept, and the instance
// statistics that let somebody see whether the loop is turning (design
// docs 0025, 0029, 0051).
package service

import (
	"context"
	"strings"
	"time"

	"github.com/na0fu3y/ochakai/internal/config"
	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/httpauth"
	"github.com/na0fu3y/ochakai/internal/store"
)

// Revisions returns a concept's change history, newest first — the
// audit surface behind "every change kept as a revision". Not a search:
// no usage is recorded (auditing a concept is not using it).
func (s *Service) Revisions(ctx context.Context, id string, limit int) ([]domain.Revision, error) {
	limit, err := checkedLimit(limit, 50, 200)
	if err != nil {
		return nil, err
	}
	id = domain.Normalize(id)
	if err := s.mayRead(ctx, id); err != nil {
		return nil, err
	}
	return s.Store.ListRevisions(ctx, id, limit)
}

// Usage returns usage totals for one concept (404 when the concept is gone).
func (s *Service) Usage(ctx context.Context, id string) (*domain.Usage, error) {
	id = domain.Normalize(id)
	if err := s.mayRead(ctx, id); err != nil {
		return nil, err
	}
	if _, err := s.Store.Get(ctx, id); err != nil {
		return nil, err
	}
	return s.Store.Usage(ctx, id)
}

// maxOutcomeNote bounds the free-form note recorded with an outcome
// report; notes live in raw knowledge_event rows (pruned after 180 days).
const maxOutcomeNote = 2000

// maxRejectionNote bounds a rejection's reason, matching the outcome
// note: both are a human sentence or two explaining a judgment, and
// neither is a place to paste a transcript.
const maxRejectionNote = 2000

// ReportOutcome records a worked/failed report against one concept and
// returns the updated usage totals. The last edge of the write-back
// loop: an agent that ran a golden query and got a wrong number can say
// so, instead of the next agent trusting the same concept blind. Unlike
// passive usage recording, a failed write is returned — the reporter
// should know the report was lost.
func (s *Service) ReportOutcome(ctx context.Context, id, outcome, note string) (*domain.Usage, error) {
	if err := s.readOnly(); err != nil {
		return nil, err
	}
	id = domain.Normalize(id)
	// A read grant is enough. Reporting an outcome is the machine's
	// observation of a concept it used, not a change to what the concept
	// says (design doc 0069), and an agent that may read a golden query
	// and run it is exactly the caller whose report is worth having.
	if err := s.mayRead(ctx, id); err != nil {
		return nil, err
	}
	if !domain.ValidOutcome(outcome) {
		return nil, Invalidf("invalid outcome %q (valid: %s)", outcome, strings.Join(domain.Outcomes, ", "))
	}
	if len(note) > maxOutcomeNote {
		return nil, Invalidf("note exceeds %d bytes", maxOutcomeNote)
	}
	if _, err := s.Store.Get(ctx, id); err != nil {
		return nil, err
	}
	actor := httpauth.Actor(ctx)
	if err := s.Store.RecordOutcome(ctx, outcome, actor, id, note); err != nil {
		return nil, err
	}
	return s.Store.Usage(ctx, id)
}

// recordUsage writes usage events with the acting caller as provenance.
// Failures are logged, never returned: usage recording must not fail reads.
func (s *Service) recordUsage(ctx context.Context, event string, ids []string) {
	if len(ids) == 0 {
		return
	}
	if err := s.Store.RecordEvents(ctx, event, httpauth.Actor(ctx), ids); err != nil {
		s.Log.Warn("usage recording failed", "event", event, "error", err)
	}
}

// maxMissQuery bounds the query text kept for a search that found
// nothing. A question a person would answer fits easily; past this the
// string is a payload rather than a question, and the miss is worth
// counting without keeping all of it (design doc 0051 §3.3).
const maxMissQuery = 500

// recordMiss keeps one unanswered question, with the acting caller as
// provenance — same treatment as a usage event, and the same silence on
// failure: nothing about measuring may fail a read.
//
// Deployments that keep no questions at all (public, or
// OCHAKAI_RECORD_MISSES=false) stop here, not in the store: the decision
// is about what this ochakai keeps, and the store is where things are
// kept.
func (s *Service) recordMiss(ctx context.Context, query string) {
	if s.Config != nil && !s.Config.RecordMisses {
		return
	}
	query = strings.TrimSpace(query)
	if len(query) > maxMissQuery {
		// On a rune boundary: a query is text somebody will read, and
		// half a character is not.
		query = strings.ToValidUTF8(query[:maxMissQuery], "")
	}
	if query == "" {
		return
	}
	// The key groups this asking with the others that meant the same
	// thing; the query is kept as it was typed (migration 0037).
	if err := s.Store.RecordMiss(ctx, query, domain.MissKey(query), httpauth.Actor(ctx)); err != nil {
		s.Log.Warn("search miss recording failed", "error", err)
	}
}

// maxStatsWindow is the longest window the stats will answer for, and it
// is the raw-event retention (design doc 0029 §3.4): past it the events
// and misses have been pruned, so a longer window would quietly answer
// with less than it was asked for. defaultStatsWindow is a month —
// long enough for a trend, short enough that a change shows in it.
const (
	maxStatsWindow     = 180
	defaultStatsWindow = 30
)

// Stats returns the instance-level view of the improvement loop over the
// last days days (design doc 0051 §3.5): what the base is made of, what
// review did, what callers reported, and what they asked for and did not
// find.
//
// days <= 0 means the default. A window past the retention is refused
// rather than clamped: a caller asking for a year and receiving six
// months' worth, silently, would read the answer as a year.
//
// prefixes scopes it to one or more subtrees (design doc 0041), which is
// how a team on a shared deployment asks about its own knowledge — and
// what let GET /api/v1/queues be folded into this face rather than
// dropped (design doc 0049 §3.1). Only the misses ignore it: a question
// that found nothing found it nowhere, so there is no id to scope by.
func (s *Service) Stats(ctx context.Context, days int, prefixes []string) (*domain.Stats, error) {
	if days == 0 {
		days = defaultStatsWindow
	}
	if days < 0 || days > maxStatsWindow {
		return nil, Invalidf("days must be between 1 and %d: raw events and misses are pruned after %d days, "+
			"so nothing answers for the time before that", maxStatsWindow, maxStatsWindow)
	}
	now := time.Now().UTC()
	f, err := checkedFilter(store.Filter{Prefixes: prefixes})
	if err != nil {
		return nil, err
	}
	// A scoped caller reads their own numbers, and the answer says which
	// ones (design doc 0123). 0109 §3 pooled these on the administrator
	// because a subset would be "a second meaning for numbers the loop is
	// measured by" — but `prefix` could already produce that subset, so
	// what made it a second meaning was an answer that did not say which
	// one it carried. It says now, for every caller: an administrator
	// asking about one subtree gets the same declaration.
	sc, err := s.scope(ctx)
	if err != nil {
		return nil, err
	}
	scoped := !sc.Everything()
	visible := true
	if scoped {
		f, visible, err = s.narrow(ctx, f)
		if err != nil {
			return nil, err
		}
	}
	if !visible {
		// The caller holds no grant here, or asked about a subtree
		// outside the ones they hold. The answer counts nothing, and
		// says it counts nothing — not a refusal, for the reason a read
		// outside the scope is a 404 rather than a 403: an error would
		// say the subtree is there. An empty prefix list cannot express
		// this, because in a filter it means the whole bundle.
		empty := emptyStats(now, days, s.Config)
		empty.Files = s.filesState(sc)
		return empty, nil
	}
	st, err := s.Store.Stats(ctx, now.AddDate(0, 0, -days), f.Prefixes)
	if err != nil {
		return nil, err
	}
	st.At = now
	st.WindowDays = days
	st.Scope = scopeOf(scoped, f.Prefixes)
	st.Misses.Recording = s.Config == nil || s.Config.RecordMisses
	if scoped {
		// The misses are the whole instance's — a question that found
		// nothing has no id to scope by (0069) — so a caller who reads
		// part of the bundle is told they are withheld rather than shown
		// what every other team searched for.
		st.Misses.Withheld = true
		st.Misses.Count, st.Misses.Queries = 0, nil
	}
	// A sandbox says so beside its own numbers: everything counted here
	// is about to be erased, and a caller that does not know that may
	// curate into it (design doc 0087).
	st.Sandbox = s.Config != nil && s.Config.Sandbox
	st.InsecureDev = s.Config != nil && s.Config.InsecureDev
	st.Files = s.filesState(sc)
	// The model is the service's, not the store's — the store holds
	// vectors and never asks who made them — so the coverage is read
	// here, beside the other two answers the service alone can give.
	if s.Embedder != nil {
		vectors, truncated, err := s.Store.EmbeddingCoverage(ctx, s.Embedder.Model(), f.Prefixes)
		if err != nil {
			return nil, err
		}
		st.Embedding = domain.StatsEmbedding{Enabled: true, Vectors: vectors, Truncated: truncated}
	}
	return st, nil
}

// scopeOf is what the answer declares it covers: the prefixes counted,
// and nothing when they were the whole bundle. An administrator asking
// about one subtree declares it too — the field is about the numbers,
// not about the caller (design doc 0123).
func scopeOf(scoped bool, prefixes []string) []string {
	if !scoped && len(prefixes) == 0 {
		return nil
	}
	return prefixes
}

// emptyStats is the answer for a caller who can see none of what these
// numbers count. It is built here rather than by asking the store for a
// subtree that does not exist: there is no prefix that reliably matches
// nothing, and an empty prefix list means the opposite of nothing.
//
// The vocabularies are carried with zeros for the reason StatsConcepts
// gives — so a reader never has to tell "none" from "not reported".
func emptyStats(now time.Time, days int, cfg *config.Config) *domain.Stats {
	st := &domain.Stats{
		At:         now,
		WindowDays: days,
		Scope:      []string{},
		Concepts: domain.StatsConcepts{
			Status: zeroedCounts(domain.Statuses),
			Trust:  zeroedCounts(domain.Trusts),
		},
	}
	st.Misses.Recording = cfg == nil || cfg.RecordMisses
	st.Misses.Withheld = true
	st.Sandbox = cfg != nil && cfg.Sandbox
	st.InsecureDev = cfg != nil && cfg.InsecureDev
	return st
}

// filesState is what this deployment can do about files, and — for a
// caller who could do something about it — the variable that would
// change the answer (design doc 0131).
//
// Both halves are on the response for the same reason `sandbox` is: a
// deployment that does not say what it is takes what you write, and what
// it takes here is the time of somebody who was offered an upload and
// got a 501 (design doc 0087 §3). The variable is the operator's half,
// so it goes to the caller who holds the whole bundle — the same test
// every other operation over the bundle as a whole takes (RequireAdmin),
// which on a deployment that has written no grants is everybody, and
// that is the deployment where everybody is in fact its administrator.
func (s *Service) filesState(sc *Scope) *domain.StatsFiles {
	if s.Store != nil && s.Store.HasBlobStore() {
		return &domain.StatsFiles{Enabled: true}
	}
	st := &domain.StatsFiles{}
	if sc != nil && sc.Everything() {
		// Spelled here as it is spelled in the refusal a write gets
		// (settleFile): one name, said by whichever face the reader met.
		st.Variable = "OCHAKAI_GCS_BUCKET"
	}
	return st
}

func zeroedCounts[T ~string](values []T) map[string]int64 {
	m := make(map[string]int64, len(values))
	for _, v := range values {
		m[string(v)] = 0
	}
	return m
}
