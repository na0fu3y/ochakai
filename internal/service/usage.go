// The write-back loop's measurements: revisions, per-concept usage,
// outcome reports from agents that acted on a concept, and the instance
// statistics that let somebody see whether the loop is turning (design
// docs 0025, 0029, 0051).
package service

import (
	"context"
	"strings"
	"time"

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
	return s.Store.ListRevisions(ctx, domain.Normalize(id), limit)
}

// Usage returns usage totals for one concept (404 when the concept is gone).
func (s *Service) Usage(ctx context.Context, id string) (*domain.Usage, error) {
	id = domain.Normalize(id)
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
	f, err := checkedFilter(store.Filter{Prefixes: prefixes})
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	st, err := s.Store.Stats(ctx, now.AddDate(0, 0, -days), f.Prefixes)
	if err != nil {
		return nil, err
	}
	st.At = now
	st.WindowDays = days
	st.Misses.Recording = s.Config == nil || s.Config.RecordMisses
	return st, nil
}
