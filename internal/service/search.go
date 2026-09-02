// Search: the hybrid read. Lexical and vector results come back as two
// rankings and are fused here (design doc 0080), and the miss — a question
// nothing answered — is recorded on the way out (0051).
package service

import (
	"context"
	"sort"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/embed"
	"github.com/na0fu3y/ochakai/internal/store"
)

// Search runs lexical search, and when an embedder is configured, fuses
// it with vector search via reciprocal rank fusion.
//
// The bool is "degraded": this deployment embeds, and this ranking did
// not — the query could not be embedded, so the fusion ran with two of
// its three lists missing and the answer is worse than the same question
// would ordinarily get here (design doc 0114). It is false on a
// deployment with no embedder, where lexical alone is the answer rather
// than a fallback from one.
func (s *Service) Search(ctx context.Context, query string, f store.Filter, limit int) ([]domain.SearchHit, bool, error) {
	// Stored text is NFC (design doc 0022); an NFD query (pasted from a
	// macOS path) must still match it byte-wise.
	query = domain.Normalize(query)
	// An empty query has nothing to rank by: SearchLexical splits the
	// query into fragments and gets none. SearchOrList now answers a
	// query-less request with a listing rather than sending it here, so
	// this guards a direct caller of the ranking half — a surface that
	// reaches past the router — and tells it what the router would have
	// done instead.
	if strings.TrimSpace(query) == "" {
		// The feeds come from domain.ListSorts rather than a sentence:
		// this message named three of them for as long as there were
		// three, and stale_after (design doc 0037) never reached it.
		return nil, false, Invalidf("search needs a query; a request without one lists instead — "+
			"in address order, or by feed with sort=%s", strings.Join(domain.ListSorts, "|"))
	}
	f, err := checkedFilter(f)
	if err != nil {
		return nil, false, err
	}
	// Search is reachable without SearchOrList (MCP's search_concepts,
	// and get_context before it retired), so the narrowing is here as
	// well as there. Doing it twice is free — an intersection of a set
	// with itself.
	f, ok, err := s.narrow(ctx, f)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, nil
	}
	hits, degraded, err := s.search(ctx, query, f, limit)
	if err != nil {
		return nil, false, err
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
	return hits, degraded, nil
}

func (s *Service) search(ctx context.Context, query string, f store.Filter, limit int) ([]domain.SearchHit, bool, error) {
	limit, err := checkedLimit(limit, 10, 50)
	if err != nil {
		return nil, false, err
	}
	lexical, err := s.Store.SearchLexical(ctx, query, f, limit*2)
	if err != nil {
		return nil, false, err
	}
	cut := func() []domain.SearchHit {
		if len(lexical) > limit {
			return lexical[:limit]
		}
		return lexical
	}
	// No embedder configured is not a degradation: lexical alone is what
	// this deployment answers with, every time, and a flag that stood on
	// every response would say nothing about any of them (design doc
	// 0114 §2).
	if s.Embedder == nil {
		return cut(), false, nil
	}

	vecs, err := s.Embedder.Embed(ctx, embed.TaskQuery, []string{query})
	if err != nil {
		// Degrade to lexical-only rather than failing the search — and
		// say so in the answer, not only here. The log is where an
		// operator looks afterwards; the caller is the one holding a
		// ranking that is worse than this deployment ordinarily gives,
		// and until design doc 0114 nothing told them (0080 §1 fuses
		// three lists, and this is two of them missing).
		s.Log.Warn("query embedding failed; falling back to lexical-only", "error", err)
		return cut(), true, nil
	}
	vector, err := s.Store.SearchVector(ctx, vecs[0], s.Embedder.Model(), f, limit*2)
	if err != nil {
		return nil, false, err
	}
	// Concepts whose attachments match are the third list (design doc
	// 0020): a concept matching in both body and attachment gains rank
	// from both, so evidence-backed concepts surface first.
	attachments, err := s.Store.SearchVectorAttachments(ctx, vecs[0], s.Embedder.Model(), f, limit*2)
	if err != nil {
		return nil, false, err
	}
	fused := rrfFuse(query, limit, lexical, vector, attachments)
	return fused, false, nil
}

// rrfK is reciprocal rank fusion's damping constant, and the unit the
// rest of this file's arithmetic is written in. At 60, first place in a
// list is worth 1/61 and second place 1/62, so **one rank position is
// worth 0.000264** — and any constant added to a fused score has to be
// read against that number or it is not a constant anybody can reason
// about.
const rrfK = 60

// verifiedBoost is what a confirmation adds to a fused score: exactly one
// rank position, the difference between placing first in a list and
// placing second.
//
// It was 0.002 for as long as its comment called it small. Against the
// step above that is 7.6 rank positions — enough that a confirmed concept
// the words put eighth overtook an unconfirmed one they put first, which
// is not a nudge but a re-ranking, and one no record asked for. Written
// in ranks it says what it means: **a confirmation wins a tie, and closes
// a gap of less than one place. It does not close more than that.** The
// unit is the point — the next person to weigh this has a number they can
// compare against something.
const verifiedBoost = 1.0/(rrfK+1) - 1.0/(rrfK+2)

// namedBy reports whether one of names — the query, and the terms of it
// (store.QueryTerms) — is what the concept is called, rather than
// something it mentions. Either name answers, as in the store's scorer:
// the title, or the id's last segment (design doc 0022).
//
// The comparison is the store's, repeated here rather than carried: a hit
// arrives with both fields on it, and a flag would be a new thing to keep
// in sync across three lists that come from three queries.
func namedBy(h domain.SearchHit, names []string) bool {
	seg := h.ID
	if i := strings.LastIndexByte(seg, '/'); i >= 0 {
		seg = seg[i+1:]
	}
	// Folded as the store folds the query it compares against
	// (migration 0043), or a concept named ｵｰﾀﾞｰ would keep the sort key
	// the store's own scorer gave it and lose it here.
	title, seg := norm.NFKC.String(h.Title), norm.NFKC.String(seg)
	for _, name := range names {
		if strings.EqualFold(title, name) || strings.EqualFold(seg, name) {
			return true
		}
	}
	return false
}

// sameTime compares two optional verification times, absent included.
func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

// rrfFuse merges ranked lists with reciprocal rank fusion, adding one rank
// position for verified concepts so certified knowledge wins the ties it
// is in, and keeping a concept the query — or a term of it — names ahead
// of the rest.
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
//
// primary is the lexical list, and it is separate from the rest because
// its score is the one thing in this function that was calibrated against
// the query. RRF reads rank and throws the number away; that is what
// makes it safe to merge lists whose scores share no scale, and it is
// also why a fused list arrives full of exact ties — two concepts placing
// second and fifth in the opposite lists score identically, and something
// has to choose. Until now that choice fell to verification recency,
// which is not a statement about the question at all. The store's score
// is, so it decides first: **RRF still rules wherever it ruled, and the
// discarded number now settles only what RRF left undecided.** A concept
// the words never reached scores zero here and loses those ties, which is
// the same sentence read the other way.
func rrfFuse(query string, limit int, primary []domain.SearchHit, others ...[]domain.SearchHit) []domain.SearchHit {
	type entry struct {
		hit     domain.SearchHit
		score   float64
		lexical float64 // the store's score, or zero if this list missed it
		named   bool
	}
	names := append([]string{query}, store.QueryTerms(query)...)
	byKey := map[string]*entry{}
	for li, list := range append([][]domain.SearchHit{primary}, others...) {
		for rank, hit := range list {
			key := hit.ID
			e, ok := byKey[key]
			if !ok {
				e = &entry{hit: hit, named: namedBy(hit, names)}
				byKey[key] = e
			}
			if li == 0 {
				e.lexical = hit.Score
			}
			e.score += 1.0 / float64(rrfK+rank+1)
		}
	}
	ranked := make([]*entry, 0, len(byKey))
	for _, e := range byKey {
		// Any standing confirmation earns the nudge, whichever tier it
		// is: the boost is about "somebody checked this, as it reads
		// now" — Trust is derived from the verifications at or after the
		// last content change (design doc 0138), so a concept edited
		// since its last confirmation arrives here unverified and takes
		// no nudge. Weighting the tiers against each other would be a
		// ranking decision no record makes (design docs 0033, 0046
		// §3.10).
		if e.hit.Trust == domain.TrustHuman || e.hit.Trust == domain.TrustMachine {
			e.score += verifiedBoost
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
		// What RRF left undecided, the store's own ranking decides: the
		// score it computed for this query, over fragment rarity and
		// where the fragments landed, is the only relevance judgement in
		// the tie.
		if ranked[i].lexical != ranked[j].lexical {
			return ranked[i].lexical > ranked[j].lexical
		}
		// The store's tie-break, repeated on the fused ranking for the
		// same reason: RRF gives concepts appearing at the same ranks in
		// the same lists exactly the same score, and something has to
		// decide which one an agent's byte budget spends itself on. The
		// most recently confirmed goes first; the id closes the order.
		if a, b := ranked[i].hit.VerifiedAt, ranked[j].hit.VerifiedAt; !sameTime(a, b) {
			if a == nil || b == nil {
				return b == nil // confirmed at all beats never confirmed
			}
			return a.After(*b)
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
