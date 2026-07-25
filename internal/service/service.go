// Package service implements ochakai's behavior shared by the MCP server
// and the REST API: knowledge CRUD with the verification policy, hybrid
// search, and deterministic SQL compilation.
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
	if k.Type == domain.TypeModels {
		return validateModel(k)
	}
	return nil
}

// validateModel is the write-time guard behind type=models entries
// (design doc 0018 §4.2): compile_sql promises deterministic compilation
// from a validated document, so a broken model is rejected when written,
// not when someone asks for SQL. The spec is kept verbatim; only the
// parts the compiler reads are checked.
func validateModel(k *domain.Knowledge) error {
	spec, ok := k.Attrs["spec"].(map[string]any)
	if !ok {
		return Invalidf("a models entry carries its Ossie semantic model object in attrs.spec")
	}
	m, err := compiler.ModelFromSpec(spec)
	if err != nil {
		return Invalidf("invalid semantic model in attrs.spec: %v", err)
	}
	if err := m.Validate(); err != nil {
		return Invalidf("invalid semantic model in attrs.spec: %v", err)
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
	vecs, err := s.Embedder.Embed(ctx, embed.TaskDocument, []string{embeddingText(k)})
	if err != nil {
		s.Log.Warn("document embedding failed; entry remains searchable via trigram", "type", k.Type, "id", k.ID, "error", err)
		return
	}
	if err := s.Store.UpsertEmbedding(ctx, k.ID, s.Embedder.Model(), vecs[0]); err != nil {
		s.Log.Warn("storing embedding failed", "id", k.ID, "error", err)
	}
}

// maxEmbedBodyBytes caps the body text handed to the embedding model. The
// real limit is tokens (2048 for gemini-embedding-001), and UTF-8 bytes
// track tokens across scripts far better than characters do: roughly 4
// bytes per token in English, 4-5 in Japanese. Capping by rune count
// instead would let Japanese overrun the model's window while English
// stayed far under it — 6000 bytes lands near 1300-1500 tokens either way,
// with headroom for the envelope fields above.
const maxEmbedBodyBytes = 6000

// embeddingText builds the document text to embed: envelope fields plus the
// golden query question, body truncated to keep within model input limits.
// The id leads (design doc 0022): with title optional, the filename may be
// the entry's only name, and the path carries the domain hierarchy.
func embeddingText(k *domain.Knowledge) string {
	parts := []string{k.ID, k.Title, k.Description, strings.Join(k.Tags, " ")}
	if q, ok := k.Attrs["question"].(string); ok {
		parts = append(parts, q)
	}
	parts = append(parts, truncateUTF8(k.Body, maxEmbedBodyBytes))
	return strings.TrimSpace(strings.Join(parts, "\n"))
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

// --- compile ---

// CompileRequest wraps a compiler request with the semantic model
// reference: the id of a models entry (design doc 0018). Model may be
// empty when exactly one models entry's spec defines the first metric
// (design doc 0019) — pass it to disambiguate.
type CompileRequest struct {
	Model string `json:"model,omitempty"`
	compiler.Request
}

// CompileResult carries the SQL plus verified golden queries related to the
// requested metrics, which clients should prefer when applicable. Model and
// ModelStatus identify the models entry the SQL came from — compile does
// not gate on status (design doc 0018 §4.3); the caller judges from
// provenance.
type CompileResult struct {
	compiler.Result
	Model           string             `json:"model"`
	ModelStatus     domain.Status      `json:"model_status"`
	VerifiedQueries []domain.SearchHit `json:"verified_queries,omitempty"`
}

func (s *Service) Compile(ctx context.Context, req CompileRequest) (*CompileResult, error) {
	if len(req.Metrics) == 0 {
		return nil, &compiler.Error{Reason: "at least one metric is required"}
	}

	var entry *domain.Knowledge
	if req.Model != "" {
		e, err := s.Store.Get(ctx, req.Model)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, &compiler.Error{Reason: fmt.Sprintf("semantic model entry %q does not exist; create a models entry with the Ossie model object in attrs.spec", req.Model)}
			}
			return nil, err
		}
		entry = e
	} else {
		// The model is the source of truth for its metrics (design doc
		// 0018), so resolution scans the models entries themselves — no
		// path convention involved; entries live wherever the user put
		// them (design docs 0017, 0019). Ambiguity is the caller's call.
		candidates, err := s.Store.ListModelsDefiningMetric(ctx, req.Metrics[0])
		if err != nil {
			return nil, err
		}
		switch len(candidates) {
		case 1:
			entry = &candidates[0]
		case 0:
			return nil, &compiler.Error{Reason: fmt.Sprintf("no models entry defines metric %q; create one with the Ossie model object in attrs.spec, or pass model (a models entry id) explicitly", req.Metrics[0])}
		default:
			ids := make([]string, len(candidates))
			for i := range candidates {
				ids[i] = candidates[i].ID
			}
			return nil, &compiler.Error{Reason: fmt.Sprintf("metric %q is defined by %d models entries (%s); pass model to pick one", req.Metrics[0], len(candidates), strings.Join(ids, ", "))}
		}
	}
	spec, _ := entry.Attrs["spec"].(map[string]any)
	if entry.Type != domain.TypeModels || spec == nil {
		return nil, &compiler.Error{Reason: fmt.Sprintf("%q is not a semantic model entry (want type models with the Ossie model object in attrs.spec)", entry.ID)}
	}
	model, err := compiler.ModelFromSpec(spec)
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

	// The models entry is counted, plus the metrics entries that name it
	// via attrs.model (matched to the requested metric names by their
	// id's last segment): compiles are their usage signal too (a verified
	// model nobody compiles from is stale). Only entries that exist are
	// counted — no ghost usage rows for unregistered metric names.
	usageIDs := []string{entry.ID}
	if metricEntryIDs, err := s.Store.ListMetricEntryIDs(ctx, entry.ID); err != nil {
		s.Log.Warn("metric entry lookup failed", "model", entry.ID, "error", err)
	} else {
		requested := make(map[string]bool, len(req.Metrics))
		for _, m := range req.Metrics {
			requested[m] = true
		}
		for _, id := range metricEntryIDs {
			if requested[id[strings.LastIndex(id, "/")+1:]] {
				usageIDs = append(usageIDs, id)
			}
		}
	}
	s.recordUsage(ctx, domain.EventCompiled, usageIDs)
	queryIDs := make([]string, len(hits))
	for i, h := range hits {
		queryIDs[i] = h.ID
	}
	s.recordUsage(ctx, domain.EventSearchHit, queryIDs)

	return &CompileResult{Result: *result, Model: entry.ID, ModelStatus: entry.Status, VerifiedQueries: hits}, nil
}
