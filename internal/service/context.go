// get_context: the one call an agent makes before a data question. It
// returns whole concepts until a byte budget runs out and outlines the
// rest, because a truncated concept is worse than a named one (design doc
// 0033).
package service

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/okf"
	"github.com/na0fu3y/ochakai/internal/store"
)

// ContextResult is the one-call context pack behind get_context and
// `ochakai context`: the ranking, plus the full concepts behind the top
// hits expanded one hop through links. Concepts that did not fit the
// caller's budget are listed in Outline instead — named, not delivered.
//
// Hits is the ranking only (design doc 0033). The knowledge travels once,
// in Concepts; a hit that was not expanded into a concept is a pointer the
// caller can spend a round trip on.
type ContextResult struct {
	Hits      []domain.ContextRank    `json:"hits"`
	Entries   []domain.View           `json:"entries"`
	Outline   []domain.ContextOutline `json:"outline,omitempty"`
	Truncated int                     `json:"truncated,omitempty"`
}

// ContextRequest is the input to Context. Budget caps the serialized size
// of the response — the concepts returned in full and the outline rows
// naming the rest, which carry a description and are not free; 0 means no
// cap.
type ContextRequest struct {
	Query  string
	Filter store.Filter
	Limit  int
	Budget int
}

// Context gathers what an agent should read before answering a data
// question, in one call: search (verified concepts rank higher), fetch
// the concepts behind the top hits in full, and follow their links one
// hop so companion knowledge — the insight that says how to read a
// metric, the golden query that answers the question — arrives without
// further round trips.
//
// req.Budget caps how many bytes of full concepts come back; the rest
// become outline rows (packWithinBudget). Callers whose context window is
// the scarce resource — an agent, not a UI — should always set it.
func (s *Service) Context(ctx context.Context, req ContextRequest) (*ContextResult, error) {
	if strings.TrimSpace(req.Query) == "" {
		return nil, Invalidf("invalid context request: query is required")
	}
	limit, err := checkedLimit(req.Limit, 5, 20)
	if err != nil {
		return nil, err
	}
	hits, err := s.Search(ctx, req.Query, req.Filter, 2*limit)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var entries []domain.Knowledge
	addFetched := func(k *domain.Knowledge) {
		if len(entries) >= 2*limit || seen[k.ID] || k.Rejection != nil {
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
	// One hop through the primary concepts' links, both directions: the
	// query a metric links to, and the insight that links to the metric
	// (rel: explains points at the metric, not the other way round).
	// Companions share the 2*limit cap and are never expanded themselves.
	primaries := len(entries)
	for i := range primaries {
		for _, l := range entries[i].Links {
			add(l.Target)
		}
		linking, err := s.Store.ListLinkingToDocs(ctx, entries[i].ID, 2*limit)
		if err != nil {
			s.Log.Warn("backlink lookup failed", "id", entries[i].ID, "error", err)
			continue
		}
		for j := range linking {
			addFetched(&linking[j])
		}
	}
	// Concepts become views here: a context pack hands over the document
	// (design doc 0043 §3.5), and the budget below has to measure what is
	// actually sent.
	views := make([]domain.View, 0, len(entries))
	for i := range entries {
		v, err := okf.ViewOf(&entries[i])
		if err != nil {
			// The concept came from the store, so this means the renderer
			// cannot express something it holds. Naming it and leaving it
			// out beats failing the whole pack.
			s.Log.Warn("cannot render a concept for a context pack", "id", entries[i].ID, "error", err)
			continue
		}
		views = append(views, v)
	}
	kept, outline := packWithinBudget(views, req.Budget)
	// Only delivered concepts count as fetched. An outline row names an
	// concept; it does not hand over the knowledge, so counting it as a use
	// would inflate the demand signal that drives the review feeds.
	ids := make([]string, len(kept))
	for i := range kept {
		ids[i] = kept[i].ID
	}
	s.recordUsage(ctx, domain.EventFetched, ids)
	return &ContextResult{
		Hits: domain.ContextRanks(hits), Entries: kept,
		Outline: outline, Truncated: len(outline),
	}, nil
}

// packWithinBudget splits concepts into the ones that fit within budget
// serialized bytes and outline rows for the rest, preserving rank order in
// both. budget <= 0 means no cap.
//
// Concepts are atomic: a concept is delivered whole or not at all. Cutting a
// body mid-way would be worse than dropping it — half of an attested
// computation's SQL still looks executable, and half a model spec is not a
// spec. Nothing downstream can tell a truncated field from a short one.
//
// Packing is greedy rather than a prefix cut: one oversized concept high in
// the ranking (a model concept carries its entire spec in attrs) would
// otherwise starve everything below it. A concept larger than the whole
// budget always becomes an outline row, even at rank 1 — the caller sees
// its size and can fetch it deliberately.
//
// The outline is inside the budget, not on top of it. Naming what was left
// out costs bytes too, and an outline row carries a description, which is
// unbounded on a concept: a budget that governed only the delivered concepts
// left the response as a whole ungoverned, which is the thing callers
// actually have to fit in a context window. So rows are counted, their
// descriptions are capped, and concepts are demoted from the bottom of the
// ranking until the two halves fit together.
//
// The floor is the outline of everything: a request whose budget cannot
// even hold the names of what matched gets those names anyway. Silently
// dropping concepts the caller never hears about is worse than a response
// slightly over budget, and the caller can raise the budget only if it
// knows there was something there.
func packWithinBudget(entries []domain.View, budget int) ([]domain.View, []domain.ContextOutline) {
	if budget <= 0 {
		return entries, nil
	}
	sizes := make([]int, len(entries))
	rows := make([]domain.ContextOutline, len(entries))
	rowSizes := make([]int, len(entries))
	deliver := make([]bool, len(entries))
	for i := range entries {
		sizes[i] = serializedSize(&entries[i])
		rows[i] = outlineRow(&entries[i], sizes[i])
		rowSizes[i] = jsonSize(rows[i])
	}
	used, outlineBytes := 0, 0
	for i := range entries {
		if used+sizes[i] <= budget {
			deliver[i] = true
			used += sizes[i]
			continue
		}
		outlineBytes += rowSizes[i]
	}
	// Demote from the bottom of the ranking until the outline the skipped
	// concepts cost fits alongside what was kept. Each demotion frees the
	// concept's bytes and pays for its row, which is strictly smaller — the
	// guard is there so a pathological concept cannot spin this loop.
	for used+outlineBytes > budget {
		last := -1
		for i := range deliver {
			if deliver[i] {
				last = i
			}
		}
		if last < 0 || sizes[last] <= rowSizes[last] {
			break
		}
		deliver[last] = false
		used -= sizes[last]
		outlineBytes += rowSizes[last]
	}
	kept := make([]domain.View, 0, len(entries))
	var outline []domain.ContextOutline
	for i := range entries {
		if deliver[i] {
			kept = append(kept, entries[i])
			continue
		}
		outline = append(outline, rows[i])
	}
	return kept, outline
}

// outlineDescriptionBytes caps the description an outline row carries.
// The description is what makes a row worth reading — it is the caller's
// only basis for spending a round trip — but nothing bounds it on the
// concept, so one concept with a page-long description could outweigh the
// budget on its own. Enough for a sentence or two; the rest is what the
// fetch is for.
const outlineDescriptionBytes = 200

func outlineRow(v *domain.View, size int) domain.ContextOutline {
	return domain.ContextOutline{
		ID: v.Summary.ID, Type: v.Summary.Type, Title: v.Summary.Title,
		Description: truncateUTF8(v.Summary.Description, outlineDescriptionBytes),
		Status:      v.Summary.Status, Bytes: size,
	}
}

// serializedSize is what the concept costs on the wire. It measures the
// whole view, document included: the document is the concept, so a measure
// that skipped it would be budgeting for a fraction of what is sent.
func serializedSize(v *domain.View) int {
	b, err := json.Marshal(v)
	if err != nil {
		return len(v.Document) // unreachable in practice; never drop on a marshal quirk
	}
	return len(b)
}

// jsonSize is what any part of the response costs on the wire.
func jsonSize(v any) int {
	b, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	return len(b)
}
