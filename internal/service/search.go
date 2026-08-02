// Search: the hybrid read. Lexical and vector results come back as two
// rankings and are fused here (design doc 0001), and the miss — a question
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
	fused := rrfFuse(limit, lexical, vector, attachments)
	return fused, nil
}

// rrfFuse merges ranked lists with reciprocal rank fusion (k=60), adding a
// small boost for verified concepts so certified knowledge surfaces first.
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
		// Any confirmation earns the nudge, whichever tier it is: the
		// boost is about "somebody checked this", and weighting the tiers
		// against each other would be a ranking decision no record makes
		// (design docs 0033, 0046 §3.10).
		if e.hit.Trust == domain.TrustHuman || e.hit.Trust == domain.TrustMachine {
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
