// Search: the hybrid read. Lexical and vector results come back as two
// rankings and are fused here (design doc 0080), and the miss — a question
// nothing answered — is recorded on the way out (0051).
package service

import (
	"context"
	"sort"
	"strings"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/embed"
	"github.com/na0fu3y/ochakai/internal/store"
)

// Search runs lexical search, and when an embedder is configured, fuses
// it with vector search via reciprocal rank fusion.
func (s *Service) Search(ctx context.Context, query string, f store.Filter, limit int) ([]domain.SearchHit, error) {
	// Stored text is NFC (design doc 0022); an NFD query (pasted from a
	// macOS path) must still match it byte-wise.
	query = domain.Normalize(query)
	// An empty query has nothing to rank by: SearchLexical splits the
	// query into fragments and gets none, so guard here — once, for
	// every surface — and point the caller at the listing modes.
	if strings.TrimSpace(query) == "" {
		// The modes come from domain.ListSorts rather than a sentence:
		// this message named three of them for as long as there were
		// three, and stale_after (design doc 0037) never reached it.
		return nil, Invalidf("search needs a query; use sort=%s to list concepts without one, "+
			"or source=URI to list what cites a resource", strings.Join(domain.ListSorts, "|"))
	}
	f, err := checkedFilter(f)
	if err != nil {
		return nil, err
	}
	hits, err := s.search(ctx, query, f, limit)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(hits))
	for i, h := range hits {
		ids[i] = h.ID
	}
	s.recordUsage(ctx, domain.EventSearchHit, ids)
	// A search that found nothing is recorded as itself: it is the one
	// observation that says what this knowledge base is missing, and
	// until design doc 0051 it was the only one being discarded. Here
	// rather than in each surface, so every way of asking is counted —
	// get_context searches through this function too.
	if len(hits) == 0 {
		s.recordMiss(ctx, query)
	}
	return hits, nil
}

func (s *Service) search(ctx context.Context, query string, f store.Filter, limit int) ([]domain.SearchHit, error) {
	limit, err := checkedLimit(limit, 10, 50)
	if err != nil {
		return nil, err
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
		s.Log.Warn("query embedding failed; falling back to lexical-only", "error", err)
		if len(lexical) > limit {
			lexical = lexical[:limit]
		}
		return lexical, nil
	}
	vector, err := s.Store.SearchVector(ctx, vecs[0], s.Embedder.Model(), f, limit*2)
	if err != nil {
		return nil, err
	}
	// Concepts whose attachments match are the third list (design doc
	// 0020): a concept matching in both body and attachment gains rank
	// from both, so evidence-backed concepts surface first.
	attachments, err := s.Store.SearchVectorAttachments(ctx, vecs[0], s.Embedder.Model(), f, limit*2)
	if err != nil {
		return nil, err
	}
	fused := rrfFuse(query, limit, lexical, vector, attachments)
	return fused, nil
}

// namedBy reports whether the query is what the concept is called, rather
// than something it mentions. Either name answers, as in the store's
// scorer: the title, or the id's last segment (design doc 0022).
//
// The comparison is the store's, repeated here rather than carried: a hit
// arrives with both fields on it, and a flag would be a new thing to keep
// in sync across three lists that come from three queries.
func namedBy(h domain.SearchHit, query string) bool {
	name := h.ID
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	return strings.EqualFold(domain.Normalize(h.Title), query) ||
		strings.EqualFold(domain.Normalize(name), query)
}

// rrfFuse merges ranked lists with reciprocal rank fusion (k=60), adding a
// small boost for verified concepts so certified knowledge surfaces first,
// and keeping a concept the query names ahead of the rest.
//
// Fusion is where a name would otherwise be lost. The store ranks an
// exact name first by a margin no other term can close, but RRF reads
// rank alone: at k=60 a concept topping one list scores 1/61 and a
// concept placing well in all three scores three times that, so 売上 the
// concept lands below whatever the vectors liked — the store's ruling
// undone by the arithmetic of the merge, not by a judgement about
// relevance. So it is a sort key rather than another addend. Any weight
// large enough to be reliable here would be large enough to dominate
// every fused score, which is the same rule written less legibly.
//
// Ties among several concepts of the same name — the same filename in two
// bundles — fall through to the fused score, which is the ordering the
// question deserves.
func rrfFuse(query string, limit int, lists ...[]domain.SearchHit) []domain.SearchHit {
	const k = 60
	type entry struct {
		hit   domain.SearchHit
		score float64
		named bool
	}
	byKey := map[string]*entry{}
	for _, list := range lists {
		for rank, hit := range list {
			key := hit.ID
			e, ok := byKey[key]
			if !ok {
				e = &entry{hit: hit, named: namedBy(hit, query)}
				byKey[key] = e
			}
			e.score += 1.0 / float64(k+rank+1)
		}
	}
	ranked := make([]*entry, 0, len(byKey))
	for _, e := range byKey {
		// Any confirmation earns the nudge, whichever tier it is: the
		// boost is about "somebody checked this", and weighting the tiers
		// against each other would be a ranking decision no record makes
		// (design docs 0033, 0046 §3.10).
		if e.hit.Trust == domain.TrustHuman || e.hit.Trust == domain.TrustMachine {
			e.score += 0.002
		}
		e.hit.Score = e.score
		ranked = append(ranked, e)
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].named != ranked[j].named {
			return ranked[i].named
		}
		if ranked[i].hit.Score != ranked[j].hit.Score {
			return ranked[i].hit.Score > ranked[j].hit.Score
		}
		return ranked[i].hit.ID < ranked[j].hit.ID
	})
	out := make([]domain.SearchHit, 0, len(ranked))
	for _, e := range ranked {
		out = append(out, e.hit)
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
