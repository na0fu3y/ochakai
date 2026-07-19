// Package service implements ochakai's behavior shared by the MCP server
// and the REST API: knowledge CRUD with the verification policy, hybrid
// search, and deterministic SQL compilation.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/na0fu3y/ochakai/internal/compiler"
	"github.com/na0fu3y/ochakai/internal/config"
	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/embed"
	"github.com/na0fu3y/ochakai/internal/httpauth"
	"github.com/na0fu3y/ochakai/internal/store"
)

type Service struct {
	Store    *store.Store
	Embedder embed.Embedder // nil when semantic search is disabled
	Config   *config.Config
	Log      *slog.Logger
}

// InvalidInputError marks a failure caused by the client's input, so
// transport layers can classify it (REST: 400) without string matching.
type InvalidInputError struct{ msg string }

func (e *InvalidInputError) Error() string { return e.msg }

// Invalidf builds an InvalidInputError from a format string.
func Invalidf(format string, args ...any) error {
	return &InvalidInputError{msg: fmt.Sprintf(format, args...)}
}

// UnsupportedError marks an operation this deployment cannot perform —
// not a client mistake and not a crash, but a missing capability the
// operator would have to configure (REST: 501).
type UnsupportedError struct{ msg string }

func (e *UnsupportedError) Error() string { return e.msg }

// Unsupportedf builds an UnsupportedError from a format string.
func Unsupportedf(format string, args ...any) error {
	return &UnsupportedError{msg: fmt.Sprintf(format, args...)}
}

// --- knowledge CRUD ---

func (s *Service) Get(ctx context.Context, typ domain.Type, id string) (*domain.Knowledge, error) {
	k, err := s.Store.Get(ctx, typ, id)
	if err != nil {
		return nil, err
	}
	// Metadata only — the bytes are a separate, deliberate fetch
	// (design doc 0008): images are heavy in agent context.
	if k.Attachments, err = s.Store.ListAttachments(ctx, typ, id); err != nil {
		return nil, err
	}
	s.recordUsage(ctx, domain.EventFetched, []store.EventTarget{{Type: typ, ID: id}})
	return k, nil
}

func (s *Service) Create(ctx context.Context, k *domain.Knowledge, actor domain.Actor) (*domain.Knowledge, error) {
	if err := validate(k); err != nil {
		return nil, err
	}
	if k.Status == "" {
		k.Status = domain.StatusDraft
	}
	s.applyVerification(k, nil, actor)
	k.CreatedBy = actor
	if err := s.Store.Create(ctx, k); err != nil {
		return nil, err
	}
	s.updateEmbedding(ctx, k)
	return k, nil
}

// Update replaces an entry's content. changed=false means the payload
// matched the stored content exactly, so nothing was written: revisions
// are the audit trail of changes and updated_at means "content last
// changed" — recurring bundle imports and agents re-saving what they
// just read must not bury real history under identical snapshots.
func (s *Service) Update(ctx context.Context, k *domain.Knowledge, actor domain.Actor) (updated *domain.Knowledge, changed bool, err error) {
	if err := validate(k); err != nil {
		return nil, false, err
	}
	old, err := s.Store.Get(ctx, k.Type, k.ID)
	if err != nil {
		return nil, false, err
	}
	if k.Status == "" {
		k.Status = old.Status
	}
	k.CreatedBy = old.CreatedBy
	k.CreatedAt = old.CreatedAt
	k.VerifiedBy, k.VerifiedAt = old.VerifiedBy, old.VerifiedAt
	k.RejectedBy, k.RejectedAt = old.RejectedBy, old.RejectedAt
	if k.SameContent(old) {
		return old, false, nil
	}
	s.applyVerification(k, old, actor)
	if err := s.Store.Update(ctx, k, actor); err != nil {
		return nil, false, err
	}
	s.updateEmbedding(ctx, k)
	return k, true, nil
}

func (s *Service) Delete(ctx context.Context, typ domain.Type, id string, actor domain.Actor) error {
	return s.Store.SoftDelete(ctx, typ, id, actor)
}

// applyVerification stamps verification and rejection provenance. There is
// no promotion restriction (design doc 0002): anyone who can reach ochakai
// may verify or reject, and verified_by / rejected_by record who did —
// trust is judged from provenance.
func (s *Service) applyVerification(k *domain.Knowledge, old *domain.Knowledge, actor domain.Actor) {
	now := time.Now().UTC()
	wasVerified := old != nil && old.Status == domain.StatusVerified
	if k.Status == domain.StatusVerified && !wasVerified {
		k.VerifiedBy, k.VerifiedAt = &actor, &now
	}
	if k.Status != domain.StatusVerified {
		k.VerifiedBy, k.VerifiedAt = nil, nil
	}
	wasRejected := old != nil && old.Status == domain.StatusRejected
	if k.Status == domain.StatusRejected && !wasRejected {
		k.RejectedBy, k.RejectedAt = &actor, &now
	}
	if k.Status != domain.StatusRejected {
		k.RejectedBy, k.RejectedAt = nil, nil
	}
}

func validate(k *domain.Knowledge) error {
	if !domain.ValidType(k.Type) {
		return Invalidf("invalid type %q (one slug segment, e.g. metrics; recommended: metrics, queries, insights, terms, datasets, tables, references)", k.Type)
	}
	if !domain.ValidID(k.ID) {
		return Invalidf(`invalid id %q (slug segments separated by "/", e.g. sales/orders; the last segment must not be "index")`, k.ID)
	}
	if strings.TrimSpace(k.Title) == "" {
		return Invalidf("title is required")
	}
	if k.Status != "" && !domain.ValidStatus(k.Status) {
		return Invalidf("invalid status %q (valid: draft, verified, deprecated, rejected)", k.Status)
	}
	return nil
}

// --- search ---

// Search runs trigram search, and when an embedder is configured, fuses it
// with vector search via reciprocal rank fusion.
func (s *Service) Search(ctx context.Context, query string, f store.Filter, limit int) ([]domain.SearchHit, error) {
	hits, err := s.search(ctx, query, f, limit)
	if err != nil {
		return nil, err
	}
	targets := make([]store.EventTarget, len(hits))
	for i, h := range hits {
		targets[i] = store.EventTarget{Type: h.Type, ID: h.ID}
	}
	s.recordUsage(ctx, domain.EventSearchHit, targets)
	return hits, nil
}

func (s *Service) search(ctx context.Context, query string, f store.Filter, limit int) ([]domain.SearchHit, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	lexical, err := s.Store.SearchLexical(ctx, query, f, limit*2)
	if err != nil {
		return nil, err
	}
	if s.Embedder == nil {
		if len(lexical) > limit {
			lexical = lexical[:limit]
		}
		return lexical, nil
	}

	vecs, err := s.Embedder.Embed(ctx, embed.TaskQuery, []string{query})
	if err != nil {
		// Degrade to lexical-only rather than failing the search.
		s.Log.Warn("query embedding failed; falling back to trigram-only", "error", err)
		if len(lexical) > limit {
			lexical = lexical[:limit]
		}
		return lexical, nil
	}
	vector, err := s.Store.SearchVector(ctx, vecs[0], f, limit*2)
	if err != nil {
		return nil, err
	}
	fused := rrfFuse(limit, lexical, vector)
	return fused, nil
}

// ContextResult is the one-call context pack behind get_context and
// `ochakai context`: the ranked hits, plus the full entries behind the
// top ones expanded one hop through links.
type ContextResult struct {
	Hits    []domain.SearchHit `json:"hits"`
	Entries []domain.Knowledge `json:"entries"`
}

// Context gathers what an agent should read before answering a data
// question, in one call: search (verified entries rank higher), fetch
// the entries behind the top hits in full, and follow their links one
// hop so companion knowledge — the insight that says how to read a
// metric, the golden query that answers the question — arrives without
// further round trips.
//
// minScore drops hits scoring below it before expansion, for callers
// that inject the pack automatically (hooks) and prefer nothing over
// junk. It defaults to 0 (off) because scores are search-mode dependent
// and uncalibrated: trigram similarity plus boosts in lexical mode, RRF
// rank fusion (~0.02 scale) in hybrid mode — a floor meaningful in one
// mode is nonsense in the other.
func (s *Service) Context(ctx context.Context, query string, f store.Filter, limit int, minScore float64) (*ContextResult, error) {
	if strings.TrimSpace(query) == "" {
		return nil, Invalidf("invalid context request: query is required")
	}
	if limit <= 0 || limit > 20 {
		limit = 5
	}
	hits, err := s.Search(ctx, query, f, 2*limit)
	if err != nil {
		return nil, err
	}
	if minScore > 0 {
		kept := hits[:0]
		for _, h := range hits {
			if h.Score >= minScore {
				kept = append(kept, h)
			}
		}
		hits = kept
	}
	seen := map[string]bool{}
	var entries []domain.Knowledge
	addFetched := func(k *domain.Knowledge) {
		key := string(k.Type) + "/" + k.ID
		if len(entries) >= 2*limit || seen[key] || k.Status == domain.StatusRejected {
			return // rejected companions stay out of the pack
		}
		seen[key] = true
		entries = append(entries, *k)
	}
	add := func(typ domain.Type, id string) {
		if len(entries) >= 2*limit || seen[string(typ)+"/"+id] {
			return
		}
		k, err := s.Store.Get(ctx, typ, id)
		if err != nil {
			return // deleted targets stay out of the pack
		}
		addFetched(k)
	}
	for _, h := range hits {
		if len(entries) >= limit {
			break
		}
		add(h.Type, h.ID)
	}
	// One hop through the primary entries' links, both directions: the
	// query a metric links to, and the insight that links to the metric
	// (rel: explains points at the metric, not the other way round).
	// Companions share the 2*limit cap and are never expanded themselves.
	primaries := len(entries)
	for i := range primaries {
		for _, l := range entries[i].Links {
			typ, id, ok := strings.Cut(strings.TrimPrefix(l.Target, "ochakai://"), "/")
			if ok {
				add(domain.Type(typ), id)
			}
		}
		linking, err := s.Store.ListLinkingTo(ctx, entries[i].Type, entries[i].ID, 2*limit)
		if err != nil {
			s.Log.Warn("backlink lookup failed", "type", entries[i].Type, "id", entries[i].ID, "error", err)
			continue
		}
		for j := range linking {
			addFetched(&linking[j])
		}
	}
	targets := make([]store.EventTarget, len(entries))
	for i := range entries {
		targets[i] = store.EventTarget{Type: entries[i].Type, ID: entries[i].ID}
	}
	s.recordUsage(ctx, domain.EventFetched, targets)
	return &ContextResult{Hits: hits, Entries: entries}, nil
}

// ListByVerifiedAt lists entries by verification age, oldest first — the
// feed for canary runs over verified golden queries (see
// docs/guides/golden-query-canary.md). Not a search: no usage is recorded.
// Results are SearchHits with score 0, so the REST and MCP responses keep
// the exact wire shape of a search across both modes.
func (s *Service) ListByVerifiedAt(ctx context.Context, f store.Filter, limit int) ([]domain.SearchHit, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	entries, err := s.Store.ListByVerifiedAt(ctx, f, limit)
	if err != nil {
		return nil, err
	}
	hits := make([]domain.SearchHit, len(entries))
	for i, k := range entries {
		hits[i] = domain.SearchHit{Knowledge: k}
	}
	return hits, nil
}

// ListByUsage lists entries by demand — most-searched first, never-used
// drafts oldest-first at the bottom — for the web UI draft review queue.
// Not a search: no usage is recorded (reading the queue must not inflate
// the very signal it ranks by). Each hit carries its usage totals; score
// is 0, keeping the wire shape of a search across all list modes.
func (s *Service) ListByUsage(ctx context.Context, f store.Filter, limit int) ([]domain.SearchHit, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	return s.Store.ListByUsage(ctx, f, limit)
}

// Revisions returns an entry's change history, newest first — the
// audit surface behind "every change kept as a revision". Not a search:
// no usage is recorded (auditing an entry is not using it).
func (s *Service) Revisions(ctx context.Context, typ domain.Type, id string, limit int) ([]domain.Revision, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.Store.ListRevisions(ctx, typ, id, limit)
}

// Backlinks lists live entries whose links point at the given entry,
// most recently updated first — the reverse edge the web UI shows as
// "linked from" (Context already follows it when packing companions).
// No usage is recorded: browsing an entry's neighbors is not a search.
func (s *Service) Backlinks(ctx context.Context, typ domain.Type, id string, limit int) ([]domain.Knowledge, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return s.Store.ListLinkingTo(ctx, typ, id, limit)
}

// Usage returns usage totals for one entry (404 when the entry is gone).
func (s *Service) Usage(ctx context.Context, typ domain.Type, id string) (*domain.Usage, error) {
	if _, err := s.Store.Get(ctx, typ, id); err != nil {
		return nil, err
	}
	return s.Store.Usage(ctx, typ, id)
}

// maxOutcomeNote bounds the free-form note recorded with an outcome
// report; notes live in raw knowledge_event rows (pruned after 180 days).
const maxOutcomeNote = 2000

// ReportOutcome records a worked/failed report against one entry and
// returns the updated usage totals. The last edge of the write-back
// loop: an agent that ran a golden query and got a wrong number can say
// so, instead of the next agent trusting the same entry blind. Unlike
// passive usage recording, a failed write is returned — the reporter
// should know the report was lost.
func (s *Service) ReportOutcome(ctx context.Context, typ domain.Type, id, outcome, note string) (*domain.Usage, error) {
	if !domain.ValidOutcome(outcome) {
		return nil, Invalidf("invalid outcome %q (valid: %s)", outcome, strings.Join(domain.Outcomes, ", "))
	}
	if len(note) > maxOutcomeNote {
		return nil, Invalidf("note exceeds %d bytes", maxOutcomeNote)
	}
	if _, err := s.Store.Get(ctx, typ, id); err != nil {
		return nil, err
	}
	actor := httpauth.Actor(ctx)
	if err := s.Store.RecordOutcome(ctx, outcome, actor, store.EventTarget{Type: typ, ID: id}, note); err != nil {
		return nil, err
	}
	return s.Store.Usage(ctx, typ, id)
}

// recordUsage writes usage events with the acting caller as provenance.
// Failures are logged, never returned: usage recording must not fail reads.
func (s *Service) recordUsage(ctx context.Context, event string, targets []store.EventTarget) {
	if len(targets) == 0 {
		return
	}
	if err := s.Store.RecordEvents(ctx, event, httpauth.Actor(ctx), targets); err != nil {
		s.Log.Warn("usage recording failed", "event", event, "error", err)
	}
}

// rrfFuse merges ranked lists with reciprocal rank fusion (k=60), adding a
// small boost for verified entries so certified knowledge surfaces first.
func rrfFuse(limit int, lists ...[]domain.SearchHit) []domain.SearchHit {
	const k = 60
	type entry struct {
		hit   domain.SearchHit
		score float64
	}
	byKey := map[string]*entry{}
	for _, list := range lists {
		for rank, hit := range list {
			key := string(hit.Type) + "/" + hit.ID
			e, ok := byKey[key]
			if !ok {
				e = &entry{hit: hit}
				byKey[key] = e
			}
			e.score += 1.0 / float64(k+rank+1)
		}
	}
	out := make([]domain.SearchHit, 0, len(byKey))
	for _, e := range byKey {
		if e.hit.Status == domain.StatusVerified {
			e.score += 0.002
		}
		e.hit.Score = e.score
		out = append(out, e.hit)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].ID < out[j].ID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// updateEmbedding refreshes the stored document vector. Failures are logged,
// not returned: writes must not depend on the embedding provider being up.
func (s *Service) updateEmbedding(ctx context.Context, k *domain.Knowledge) {
	if s.Embedder == nil {
		return
	}
	vecs, err := s.Embedder.Embed(ctx, embed.TaskDocument, []string{embeddingText(k)})
	if err != nil {
		s.Log.Warn("document embedding failed; entry remains searchable via trigram", "type", k.Type, "id", k.ID, "error", err)
		return
	}
	if err := s.Store.UpsertEmbedding(ctx, k.Type, k.ID, s.Embedder.Model(), vecs[0]); err != nil {
		s.Log.Warn("storing embedding failed", "type", k.Type, "id", k.ID, "error", err)
	}
}

// embeddingText builds the document text to embed: envelope fields plus the
// golden query question, body truncated to keep within model input limits.
func embeddingText(k *domain.Knowledge) string {
	parts := []string{k.Title, k.Description, strings.Join(k.Tags, " ")}
	if q, ok := k.Attrs["question"].(string); ok {
		parts = append(parts, q)
	}
	body := k.Body
	if len(body) > 4000 {
		body = body[:4000]
	}
	parts = append(parts, body)
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

// --- compile ---

// CompileRequest wraps a compiler request with the semantic model reference.
// Model may be empty when the first metric's knowledge entry links to its
// model via attrs.model.
type CompileRequest struct {
	Model string `json:"model,omitempty"`
	compiler.Request
}

// CompileResult carries the SQL plus verified golden queries related to the
// requested metrics, which clients should prefer when applicable.
type CompileResult struct {
	compiler.Result
	VerifiedQueries []domain.SearchHit `json:"verified_queries,omitempty"`
}

func (s *Service) Compile(ctx context.Context, req CompileRequest) (*CompileResult, error) {
	if len(req.Metrics) == 0 {
		return nil, &compiler.Error{Reason: "at least one metric is required"}
	}

	modelName := req.Model
	if modelName == "" {
		k, err := s.Store.Get(ctx, domain.TypeMetrics, req.Metrics[0])
		if err == nil {
			if m, ok := k.Attrs["model"].(string); ok {
				modelName = m
			}
		}
		if modelName == "" {
			return nil, &compiler.Error{Reason: fmt.Sprintf("cannot resolve a semantic model for metric %q; pass model explicitly or import one with `ochakai import-ossie`", req.Metrics[0])}
		}
	}
	spec, err := s.Store.GetSemanticModel(ctx, modelName)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, &compiler.Error{Reason: fmt.Sprintf("semantic model %q is not imported", modelName)}
		}
		return nil, err
	}
	model, err := compiler.ModelFromSpec(spec, modelName)
	if err != nil {
		return nil, err
	}
	result, err := compiler.Compile(model, req.Request)
	if err != nil {
		return nil, err
	}

	// Surface verified golden queries about the requested metrics: a human-
	// checked query beats a compiled one when it answers the question.
	hits, err := s.Store.SearchLexical(ctx, strings.Join(req.Metrics, " "),
		store.Filter{Types: []domain.Type{domain.TypeQueries}, Statuses: []domain.Status{domain.StatusVerified}}, 3)
	if err != nil {
		s.Log.Warn("verified query lookup failed", "error", err)
		hits = nil
	}

	targets := make([]store.EventTarget, 0, len(req.Metrics)+len(hits))
	for _, m := range req.Metrics {
		targets = append(targets, store.EventTarget{Type: domain.TypeMetrics, ID: m})
	}
	s.recordUsage(ctx, domain.EventCompiled, targets)
	queryTargets := make([]store.EventTarget, len(hits))
	for i, h := range hits {
		queryTargets[i] = store.EventTarget{Type: h.Type, ID: h.ID}
	}
	s.recordUsage(ctx, domain.EventSearchHit, queryTargets)

	return &CompileResult{Result: *result, VerifiedQueries: hits}, nil
}
