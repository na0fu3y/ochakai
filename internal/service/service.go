// Package service implements ochakai's behavior shared by the MCP server
// and the REST API: knowledge CRUD with the verification policy, hybrid
// search, and the usage/outcome write-back loop.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

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

func (s *Service) Get(ctx context.Context, id string) (*domain.Knowledge, error) {
	id = domain.Normalize(id)
	k, err := s.Store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	// Metadata only — the bytes are a separate, deliberate fetch
	// (design doc 0008): images are heavy in agent context.
	if k.Attachments, err = s.Store.ListAttachments(ctx, id); err != nil {
		return nil, err
	}
	s.recordUsage(ctx, domain.EventFetched, []string{id})
	return k, nil
}

func (s *Service) Create(ctx context.Context, k *domain.Knowledge, actor domain.Actor) (*domain.Knowledge, error) {
	normalizeKeys(k)
	if err := validate(k); err != nil {
		return nil, err
	}
	deriveLinks(k)
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
//
// ifMatch is an optional optimistic-concurrency precondition (design doc
// 0025 §11): when non-nil it is the updated_at the caller based the edit
// on, and an entry that has changed since — whether before this call's
// read, or in the read-modify-write window below — yields store.ErrConflict
// rather than silently clobbering the other write. nil keeps the prior
// last-write-wins behavior for callers that do not opt in.
func (s *Service) Update(ctx context.Context, k *domain.Knowledge, actor domain.Actor, ifMatch *time.Time) (updated *domain.Knowledge, changed bool, err error) {
	normalizeKeys(k)
	if err := validate(k); err != nil {
		return nil, false, err
	}
	deriveLinks(k)
	old, err := s.Store.Get(ctx, k.ID)
	if err != nil {
		return nil, false, err
	}
	// The caller's version is already stale if the stored entry moved on
	// between their read and ours — reject before doing any work.
	if ifMatch != nil && !old.UpdatedAt.Equal(ifMatch.UTC()) {
		return nil, false, store.ErrConflict
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
	// Pass the precondition through so the write is also guarded against a
	// concurrent update landing between the Get above and the write.
	if err := s.Store.Update(ctx, k, actor, ifMatch); err != nil {
		return nil, false, err
	}
	s.updateEmbedding(ctx, k)
	return k, true, nil
}

// RefuseIfCurated reports an error when id names an entry a human has
// already ruled on — verified, rejected, or deprecated — for surfaces that
// must not overwrite that ruling in place. It returns the entry's version
// so the caller can pass it as an If-Match precondition and close the
// window between this check and the write.
//
// The rule is about preconditions, not authority. Design doc 0002 settled
// that ochakai has no authorization and that verification is judged from
// provenance, and this does not reopen it: an agent may still create, may
// still edit drafts, may still promote a draft to verified. What it may
// not do from a surface with no If-Match channel (MCP, design doc 0025
// §11) is silently replace a state a human put there.
//
// Rejected matters at least as much as verified, and it is the one the
// obvious rule would have missed. Create refuses a live entry, so an agent
// blocked by a rejection has a two-call path around it — delete, then
// create, which revives the tombstone as a fresh draft and takes the
// status_note with it. That erases the memory of no that the write-back
// loop is built on (design doc 0025) and that the shipped instructions
// tell agents to consult before re-proposing. Nobody notices: unlike a
// deleted verified entry, a deleted rejection leaves nothing anyone was
// using.
func (s *Service) RefuseIfCurated(ctx context.Context, id, op string) (*time.Time, error) {
	k, err := s.Store.Get(ctx, domain.Normalize(id))
	if err != nil {
		return nil, err
	}
	var instead string
	switch k.Status {
	case domain.StatusVerified:
		instead = "If it is wrong, say so with report_outcome failed — that puts it in the " +
			"re-verification feed. If you have something better, create_knowledge a new draft."
	case domain.StatusRejected:
		instead = "The rejection is the record of a decision, and status_note says why; read it " +
			"before proposing this again. If you disagree, create_knowledge a new entry at a " +
			"different id and let a human judge it."
	case domain.StatusDeprecated:
		instead = "Deprecated means it was correct and is no longer recommended. If it is worth " +
			"reviving, create_knowledge a draft that says why."
	default:
		return &k.UpdatedAt, nil
	}
	return nil, Invalidf("cannot %s %s from this surface: it is %s, and this surface has no "+
		"If-Match precondition to replace curated knowledge safely. %s A human changes curated "+
		"entries from the web UI or CLI.", op, id, k.Status, instead)
}

func (s *Service) Delete(ctx context.Context, id string, actor domain.Actor) error {
	return s.Store.SoftDelete(ctx, domain.Normalize(id), actor)
}

// Purge hard-deletes an already soft-deleted entry, freeing its id for a
// move (design doc 0021: Move cannot revive a tombstone the way Create
// can). Not an MCP tool: this is the one operation that destroys history,
// so it belongs to the human surfaces (design doc 0015).
//
// Which surface exposes it is not the same as who can reach it — ochakai
// has no authorization, so any caller that reaches the REST API can purge.
// The actor is therefore recorded twice over: in knowledge_purge, the one
// table a purge does not touch, and in the log. An operation that leaves
// no trace has no place in a product whose value is provenance.
func (s *Service) Purge(ctx context.Context, id string, actor domain.Actor) error {
	id = domain.Normalize(id)
	if err := s.Store.Purge(ctx, id, actor); err != nil {
		return err
	}
	s.Log.Info("knowledge purged", "id", id, "actor", actor.String())
	return nil
}

// Move renames an entry to newID, carrying every id-keyed record along
// (revisions, usage, attachments, embeddings) and rewriting inbound
// references so nothing breaks (design doc 0021). Moving to the current
// id is a no-op read.
func (s *Service) Move(ctx context.Context, id, newID string, actor domain.Actor) (*domain.Knowledge, error) {
	id, newID = domain.Normalize(id), domain.Normalize(newID)
	if !domain.ValidID(newID) {
		return nil, Invalidf(`invalid destination id %q (path segments separated by "/", e.g. sales/orders; segments must not start with "." and the last must not be "index" or "log")`, newID)
	}
	if newID == id {
		return s.Store.Get(ctx, id)
	}
	return s.Store.Move(ctx, id, newID, actor)
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

// normalizeKeys rewrites a write payload's byte-compared keys — the id
// and the type — into NFC (design doc 0022); content fields are kept as
// written. The type joined them in 0023: it is free text now, so a
// foreign bundle can carry a non-ASCII type, and search filters compare
// against it. Filters fold case themselves (domain.FoldType), but not
// width or accent composition — so the type is stored NFC and trimmed,
// leaving lower() enough to fold the column. A recommended type also
// settles on its canonical spelling, so storage holds one casing.
//
// Links are not normalized here because they are not read from the
// payload at all — deriveLinks recomputes them from the body, normalizing
// each target on the way (design doc 0024).
func normalizeKeys(k *domain.Knowledge) {
	k.ID = domain.Normalize(k.ID)
	k.Type = domain.CanonicalType(domain.Type(strings.TrimSpace(domain.Normalize(string(k.Type)))))
}

// deriveLinks replaces whatever links the payload carried with the ones
// the body actually contains. The body is the sole source of edges
// (design doc 0024): a writer links by writing a markdown link, and the
// links column is a derived index that backlinks and get_context read.
// Called on every write path, so no route can store links the body does
// not back.
func deriveLinks(k *domain.Knowledge) {
	k.Links = domain.LinksFromBody(k.ID, k.Body)
}

func validate(k *domain.Knowledge) error {
	if !domain.ValidType(k.Type) {
		return Invalidf(`invalid type %q (one line, no "/", up to 128 bytes; recommended: Metric, Golden Query, Insight, Glossary Term, BigQuery Dataset, BigQuery Table, Reference)`, k.Type)
	}
	if !domain.ValidID(k.ID) {
		return Invalidf(`invalid id %q (path segments separated by "/", e.g. sales/orders; segments must not start with "." and the last must not be "index" or "log")`, k.ID)
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
	// Stored text is NFC (design doc 0022); an NFD query (pasted from a
	// macOS path) must still match it byte-wise.
	query = domain.Normalize(query)
	hits, err := s.search(ctx, query, f, limit)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(hits))
	for i, h := range hits {
		ids[i] = h.ID
	}
	s.recordUsage(ctx, domain.EventSearchHit, ids)
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
	// Entries whose attachments match are the third list (design doc
	// 0020): an entry matching in both body and attachment gains rank
	// from both, so evidence-backed entries surface first.
	attachments, err := s.Store.SearchVectorAttachments(ctx, vecs[0], f, limit*2)
	if err != nil {
		return nil, err
	}
	fused := rrfFuse(limit, lexical, vector, attachments)
	return fused, nil
}

// ContextResult is the one-call context pack behind get_context and
// `ochakai context`: the ranked hits, plus the full entries behind the
// top ones expanded one hop through links. Entries that did not fit the
// caller's budget are listed in Outline instead — named, not delivered.
type ContextResult struct {
	Hits      []domain.SearchHit      `json:"hits"`
	Entries   []domain.Knowledge      `json:"entries"`
	Outline   []domain.ContextOutline `json:"outline,omitempty"`
	Truncated int                     `json:"truncated,omitempty"`
}

// ContextRequest is the input to Context. Budget caps the serialized size
// of the entries returned in full; 0 means no cap.
type ContextRequest struct {
	Query    string
	Filter   store.Filter
	Limit    int
	MinScore float64
	Budget   int
}

// Context gathers what an agent should read before answering a data
// question, in one call: search (verified entries rank higher), fetch
// the entries behind the top hits in full, and follow their links one
// hop so companion knowledge — the insight that says how to read a
// metric, the golden query that answers the question — arrives without
// further round trips.
//
// req.MinScore drops hits scoring below it before expansion, for callers
// that inject the pack automatically (hooks) and prefer nothing over
// junk. It defaults to 0 (off) because scores are search-mode dependent
// and uncalibrated: trigram similarity plus boosts in lexical mode, RRF
// rank fusion (~0.02 scale) in hybrid mode — a floor meaningful in one
// mode is nonsense in the other.
//
// req.Budget caps how many bytes of full entries come back; the rest
// become outline rows (packWithinBudget). Callers whose context window is
// the scarce resource — an agent, not a UI — should always set it.
func (s *Service) Context(ctx context.Context, req ContextRequest) (*ContextResult, error) {
	if strings.TrimSpace(req.Query) == "" {
		return nil, Invalidf("invalid context request: query is required")
	}
	limit := req.Limit
	if limit <= 0 || limit > 20 {
		limit = 5
	}
	hits, err := s.Search(ctx, req.Query, req.Filter, 2*limit)
	if err != nil {
		return nil, err
	}
	if req.MinScore > 0 {
		kept := hits[:0]
		for _, h := range hits {
			if h.Score >= req.MinScore {
				kept = append(kept, h)
			}
		}
		hits = kept
	}
	seen := map[string]bool{}
	var entries []domain.Knowledge
	addFetched := func(k *domain.Knowledge) {
		if len(entries) >= 2*limit || seen[k.ID] || k.Status == domain.StatusRejected {
			return // rejected companions stay out of the pack
		}
		seen[k.ID] = true
		entries = append(entries, *k)
	}
	add := func(id string) {
		if len(entries) >= 2*limit || seen[id] {
			return
		}
		k, err := s.Store.Get(ctx, id)
		if err != nil {
			return // deleted targets stay out of the pack
		}
		addFetched(k)
	}
	for _, h := range hits {
		if len(entries) >= limit {
			break
		}
		add(h.ID)
	}
	// One hop through the primary entries' links, both directions: the
	// query a metric links to, and the insight that links to the metric
	// (rel: explains points at the metric, not the other way round).
	// Companions share the 2*limit cap and are never expanded themselves.
	primaries := len(entries)
	for i := range primaries {
		for _, l := range entries[i].Links {
			add(strings.TrimPrefix(l.Target, "ochakai://"))
		}
		linking, err := s.Store.ListLinkingTo(ctx, entries[i].ID, 2*limit)
		if err != nil {
			s.Log.Warn("backlink lookup failed", "id", entries[i].ID, "error", err)
			continue
		}
		for j := range linking {
			addFetched(&linking[j])
		}
	}
	entries, outline := packWithinBudget(entries, req.Budget)
	// Only delivered entries count as fetched. An outline row names an
	// entry; it does not hand over the knowledge, so counting it as a use
	// would inflate the demand signal that drives the review feeds.
	ids := make([]string, len(entries))
	for i := range entries {
		ids[i] = entries[i].ID
	}
	s.recordUsage(ctx, domain.EventFetched, ids)
	return &ContextResult{Hits: hits, Entries: entries, Outline: outline, Truncated: len(outline)}, nil
}

// packWithinBudget splits entries into the ones that fit within budget
// serialized bytes and outline rows for the rest, preserving rank order in
// both. budget <= 0 means no cap.
//
// Entries are atomic: an entry is delivered whole or not at all. Cutting a
// body mid-way would be worse than dropping it — half of a Golden Query's
// attrs.sql still looks executable, and half a semantic model spec is not
// a spec. Nothing downstream can tell a truncated field from a short one.
//
// Packing is greedy rather than a prefix cut: one oversized entry high in
// the ranking (a Semantic Model carries its entire spec in attrs) would
// otherwise starve everything below it. An entry larger than the whole
// budget always becomes an outline row, even at rank 1 — the caller sees
// its size and can fetch it deliberately.
func packWithinBudget(entries []domain.Knowledge, budget int) ([]domain.Knowledge, []domain.ContextOutline) {
	if budget <= 0 {
		return entries, nil
	}
	kept := make([]domain.Knowledge, 0, len(entries))
	var outline []domain.ContextOutline
	used := 0
	for i := range entries {
		k := &entries[i]
		size := serializedSize(k)
		if used+size <= budget {
			kept = append(kept, *k)
			used += size
			continue
		}
		outline = append(outline, domain.ContextOutline{
			ID: k.ID, Type: k.Type, Title: k.DisplayTitle(),
			Description: k.Description, Status: k.Status, Bytes: size,
		})
	}
	return kept, outline
}

// serializedSize is what the entry costs on the wire — attrs included. A
// body-only measure would miss the largest payload in the base, a
// Semantic Model's attrs.spec.
func serializedSize(k *domain.Knowledge) int {
	b, err := json.Marshal(k)
	if err != nil {
		return len(k.Body) // unreachable in practice; never drop on a marshal quirk
	}
	return len(b)
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

// ListByFailed lists entries with failed outcome reports, worst first —
// the re-verification feed (design doc 0025): in-force knowledge that
// callers report is producing wrong results, the evidence-based
// counterpart to the verified_at feed. A healthy base yields an empty
// feed. Not a search: no usage is recorded (triaging the queue must not
// inflate the signal it ranks by). Each hit carries its usage totals;
// score is 0, keeping the wire shape of a search across all list modes.
func (s *Service) ListByFailed(ctx context.Context, f store.Filter, limit int) ([]domain.SearchHit, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	return s.Store.ListByFailed(ctx, f, limit)
}

// Revisions returns an entry's change history, newest first — the
// audit surface behind "every change kept as a revision". Not a search:
// no usage is recorded (auditing an entry is not using it).
func (s *Service) Revisions(ctx context.Context, id string, limit int) ([]domain.Revision, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.Store.ListRevisions(ctx, domain.Normalize(id), limit)
}

// Backlinks lists live entries whose links point at the given entry,
// most recently updated first — the reverse edge the web UI shows as
// "linked from" (Context already follows it when packing companions).
// No usage is recorded: browsing an entry's neighbors is not a search.
func (s *Service) Backlinks(ctx context.Context, id string, limit int) ([]domain.Knowledge, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return s.Store.ListLinkingTo(ctx, domain.Normalize(id), limit)
}

// Usage returns usage totals for one entry (404 when the entry is gone).
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

// ReportOutcome records a worked/failed report against one entry and
// returns the updated usage totals. The last edge of the write-back
// loop: an agent that ran a golden query and got a wrong number can say
// so, instead of the next agent trusting the same entry blind. Unlike
// passive usage recording, a failed write is returned — the reporter
// should know the report was lost.
func (s *Service) ReportOutcome(ctx context.Context, id, outcome, note string) (*domain.Usage, error) {
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
			key := hit.ID
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
	vecs, err := s.embedDocument(ctx, embeddingText(k))
	if err != nil {
		s.Log.Warn("document embedding failed; entry remains searchable via trigram", "type", k.Type, "id", k.ID, "error", err)
		return
	}
	if err := s.Store.UpsertEmbedding(ctx, k.ID, s.Embedder.Model(), vecs[0]); err != nil {
		s.Log.Warn("storing embedding failed", "id", k.ID, "error", err)
	}
}

// maxEmbedBytes caps the text handed to the embedding model. The real
// limit is tokens (2048 for gemini-embedding-001), and UTF-8 bytes track
// tokens across scripts far better than characters do — but only
// roughly, and the ratio is worst where it matters most: Japanese runs
// three bytes per character and can approach one token per character, so
// 6000 bytes is 2000 characters and possibly 2000 tokens. Against a 2048
// limit that is not headroom, it is a coin flip.
//
// So the cap covers the whole text, envelope included (the id, title,
// description, tags and question used to ride on top of the body's
// budget), and sits at 5000 bytes — comfortably inside the window even
// at one token per character. English pays for that in unused budget,
// which is the right trade: an overrun does not degrade the embedding,
// it removes the entry from vector search entirely.
//
// The estimate is still an estimate, so embedDocument retries shorter
// rather than trusting it.
const maxEmbedBytes = 5000

// ReembedResult reports what a Reembed pass did. Missing is how many
// entries still have no vector once the pass is over — the number that
// decides whether the operator runs it again, so it counts what is
// actually left rather than what this pass did not get to.
type ReembedResult struct {
	Embedded int `json:"embedded"`
	Failed   int `json:"failed"`
	Missing  int `json:"missing"`
}

// Reembed fills in the vectors that entry writes never produced. Vectors
// are written on create and update only, so a base loaded before
// OCHAKAI_VERTEX_PROJECT was set has none, and hybrid search silently
// stays lexical-only until every entry happens to be rewritten. Switching
// models has the same shape: the old vectors are in a space nothing
// queries any more (design doc 0020).
//
// Failures are counted, not fatal: one entry the provider chokes on must
// not strand the rest of the corpus.
func (s *Service) Reembed(ctx context.Context, limit int) (*ReembedResult, error) {
	if s.Embedder == nil {
		return nil, Unsupportedf("semantic search is not configured: set OCHAKAI_VERTEX_PROJECT")
	}
	if limit <= 0 || limit > 5000 {
		limit = 500
	}
	ids, err := s.Store.ListUnembedded(ctx, s.Embedder.Model(), limit)
	if err != nil {
		return nil, err
	}
	res := &ReembedResult{}
	for _, id := range ids {
		k, err := s.Store.Get(ctx, id)
		if err != nil {
			res.Failed++
			s.Log.Warn("reembed: entry disappeared", "id", id, "error", err)
			continue
		}
		vecs, err := s.embedDocument(ctx, embeddingText(k))
		if err != nil {
			res.Failed++
			s.Log.Warn("reembed: embedding failed", "id", id, "error", err)
			continue
		}
		if err := s.Store.UpsertEmbedding(ctx, id, s.Embedder.Model(), vecs[0]); err != nil {
			res.Failed++
			s.Log.Warn("reembed: storing failed", "id", id, "error", err)
			continue
		}
		res.Embedded++
	}
	// Counted after the pass, so it is what is left — not what was left
	// minus what we did, which double-counts the work and reports a
	// bounded pass as a finished one. Entries this pass failed on are
	// still unembedded and belong in the total.
	missing, err := s.Store.CountUnembedded(ctx, s.Embedder.Model())
	if err != nil {
		// The pass itself succeeded; refusing to report it because the
		// tally failed would be worse than reporting it without one.
		s.Log.Warn("reembed: counting what is left failed", "error", err)
		return res, nil
	}
	res.Missing = missing
	return res, nil
}

// embedDocument embeds text, halving it and retrying while the model says
// it is too long. Byte budgets only approximate a token count, and the
// approximation is worst for Japanese; without this, an entry that
// overruns is dropped from vector search silently — logged, but with the
// write reported as a success. Halving converges in a couple of rounds
// and only runs on the rejection, so an outage still costs one call.
func (s *Service) embedDocument(ctx context.Context, text string) ([][]float32, error) {
	// Four attempts halve a 5000-byte text to 625, well under any
	// plausible window; the floor below stops it before it embeds a
	// fragment too short to mean anything.
	for attempt := range 4 {
		vecs, err := s.Embedder.Embed(ctx, embed.TaskDocument, []string{text})
		if err == nil {
			if attempt > 0 {
				s.Log.Info("embedded after shortening", "bytes", len(text), "attempts", attempt+1)
			}
			return vecs, nil
		}
		if !errors.Is(err, embed.ErrInputTooLong) || len(text) < 500 {
			return nil, err
		}
		text = truncateUTF8(text, len(text)/2)
	}
	return nil, fmt.Errorf("still over the embedding model's input limit after shortening")
}

// embeddingText builds the document text to embed: envelope fields plus the
// golden query question, body truncated to keep within model input limits.
// The id leads (design doc 0022): with title optional, the filename may be
// the entry's only name, and the path carries the domain hierarchy.
func embeddingText(k *domain.Knowledge) string {
	parts := []string{k.ID, k.Title, k.Description, strings.Join(k.Tags, " ")}
	if q, ok := k.Attrs["question"].(string); ok {
		parts = append(parts, q)
	}
	parts = append(parts, k.Body)
	// The cap covers the joined text: an entry whose envelope is long
	// (a deep path, a paragraph of description) must not push the total
	// past the window just because the body fit on its own.
	return truncateUTF8(strings.TrimSpace(strings.Join(parts, "\n")), maxEmbedBytes)
}

// truncateUTF8 caps s at max bytes and then drops a trailing partial rune.
// A plain s[:max] lands mid-character two times in three on Japanese text
// (3 bytes per character), and half a character is not valid UTF-8.
func truncateUTF8(s string, max int) string {
	if len(s) > max {
		s = s[:max]
	}
	for len(s) > 0 {
		// DecodeLastRuneInString reports (RuneError, 1) for a byte that
		// cannot end a valid encoding; a real U+FFFD decodes with size 3.
		if r, size := utf8.DecodeLastRuneInString(s); r != utf8.RuneError || size > 1 {
			break
		}
		s = s[:len(s)-1]
	}
	return s
}
