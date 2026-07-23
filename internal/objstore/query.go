package objstore

import (
	"context"
	"sort"
	"strings"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// Filter narrows searches and feeds, mirroring store.Filter.
type Filter struct {
	Types    []domain.Type
	Statuses []domain.Status
	Tags     []string
}

func (f Filter) match(k *domain.Knowledge) bool {
	if len(f.Types) > 0 {
		hit := false
		for _, t := range f.Types {
			if domain.FoldType(t) == domain.FoldType(k.Type) {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	if len(f.Statuses) > 0 {
		hit := false
		for _, s := range f.Statuses {
			if s == k.Status {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	} else if k.Status == domain.StatusRejected {
		// Without an explicit status filter, rejected entries are excluded
		// from answers but remain queryable on request — the SQL store's
		// buildWhere rule (store.go), so the memory of "no" doesn't resurface.
		return false
	}
	if len(f.Tags) > 0 && !overlap(f.Tags, k.Tags) {
		return false
	}
	return true
}

func overlap(a, b []string) bool {
	set := map[string]bool{}
	for _, s := range a {
		set[s] = true
	}
	for _, s := range b {
		if set[s] {
			return true
		}
	}
	return false
}

// SearchLexical reproduces the SQL store's trigram-with-substring-floor
// ranking (store.SearchLexical) as an in-memory scan: trigram similarity
// over id+title+description+tags+body, a +0.3 floor when the query is a
// literal substring, and a +0.05 verified boost, keeping only score > 0.05.
// The ordering behavior is what carries over — an exact-substring hit
// dominates a fuzzy one, and verified outranks a draft tie — not bit-parity
// with pg_trgm.
func (ix *Index) SearchLexical(_ context.Context, query string, f Filter, limit int) ([]domain.SearchHit, error) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	q := strings.ToLower(strings.TrimSpace(query))
	qgrams := trigrams(q)
	var hits []domain.SearchHit
	for _, r := range ix.byID {
		k := &r.k
		if !f.match(k) {
			continue
		}
		hay := strings.ToLower(strings.Join([]string{
			k.ID, k.Title, k.Description, strings.Join(k.Tags, " "), k.Body,
		}, " "))
		score := trigramSimilarity(qgrams, trigrams(hay))
		if q != "" && strings.Contains(hay, q) {
			score += 0.3
		}
		if k.Status == domain.StatusVerified {
			score += 0.05
		}
		if score <= 0.05 {
			continue
		}
		kc := *k
		hits = append(hits, domain.SearchHit{Knowledge: kc, Score: score})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

// ListLinkingTo is the reverse edge Context needs — the backlink query the
// SQL store runs as a JSONB containment index scan (links @> …). Here it is
// a scan of the in-memory link graph, which is always derived from bodies
// (design doc 0024), so both bare and ochakai:// target forms are already
// normalized to the entry id.
func (ix *Index) ListLinkingTo(_ context.Context, id string, limit int) ([]domain.Knowledge, error) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	var out []domain.Knowledge
	for _, r := range ix.byID {
		for _, l := range r.k.Links {
			if l.Target == id {
				out = append(out, r.k)
				break
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ListModelsDefiningMetric is compile-time model resolution: the SQL store
// finds it with a JSONB path containment (attrs->'spec'->'metrics' @> …);
// here it is an in-memory walk of the same structure.
func (ix *Index) ListModelsDefiningMetric(_ context.Context, metric string) ([]domain.Knowledge, error) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	var out []domain.Knowledge
	for _, r := range ix.byID {
		k := &r.k
		if domain.FoldType(k.Type) != domain.FoldType(domain.TypeModels) {
			continue
		}
		if modelDefinesMetric(k.Attrs, metric) {
			out = append(out, *k)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func modelDefinesMetric(attrs map[string]any, metric string) bool {
	spec, ok := attrs["spec"].(map[string]any)
	if !ok {
		return false
	}
	metrics, ok := spec["metrics"].([]any)
	if !ok {
		return false
	}
	for _, m := range metrics {
		mm, ok := m.(map[string]any)
		if !ok {
			continue
		}
		if name, _ := mm["name"].(string); name == metric {
			return true
		}
	}
	return false
}

// ListByVerifiedAt is the verification-age feed: filtered entries oldest
// verified_at first, never-verified last (store.ListByVerifiedAt). An
// in-memory sort replaces the SQL ORDER BY … NULLS LAST.
func (ix *Index) ListByVerifiedAt(_ context.Context, f Filter, limit int) ([]domain.Knowledge, error) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	var out []domain.Knowledge
	for _, r := range ix.byID {
		if f.match(&r.k) {
			out = append(out, r.k)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].VerifiedAt, out[j].VerifiedAt
		switch {
		case a == nil && b == nil:
			return out[i].ID < out[j].ID
		case a == nil:
			return false // never-verified sinks last
		case b == nil:
			return true
		case a.Equal(*b):
			return out[i].ID < out[j].ID
		default:
			return a.Before(*b)
		}
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Move renames an entry and rewrites every reference to it — the markdown
// links in referrers' bodies and attrs.model (design doc 0019). The SQL
// store does this in one transaction across many tables; here there is no
// transaction, so writes are ordered for crash safety: the rename lands
// first, then each referrer. A crash mid-rewrite leaves a referrer whose
// body still points at oldID — a dangling-but-visible edge on reload, not
// corruption, because links are always re-derived from bodies (design doc
// 0026 §4). Re-running Move (or a startup repair) finishes the job.
func (ix *Index) Move(ctx context.Context, oldID, newID string) (*domain.Knowledge, error) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	src, ok := ix.byID[oldID]
	if !ok {
		return nil, ErrNotFound
	}
	if _, taken := ix.byID[newID]; taken {
		return nil, ErrAlreadyExists
	}

	// 1. Write the entry at its new key, then drop the old object.
	moved := src.k
	moved.ID = newID
	moved.UpdatedAt = nowStored()
	moved.Links = domain.LinksFromBody(newID, moved.Body)
	gen, err := ix.write(ctx, &moved, int64Ptr(0))
	if err != nil {
		return nil, err
	}
	if err := ix.os.Delete(ctx, entryKey(oldID), &src.gen); err != nil {
		return nil, err
	}
	delete(ix.byID, oldID)
	ix.byID[newID] = &record{k: moved, gen: gen}

	// 2. Rewrite referrers one object at a time (no transaction). Ordering
	// after the rename means a crash here leaves the rename done and some
	// referrers pending — the self-healing case above.
	if err := ix.rewriteReferences(ctx, oldID, newID); err != nil {
		return nil, err
	}
	return &moved, nil
}

// rewriteReferences repairs live entries that point at oldID. Candidates
// are found by scanning the in-memory link graph (and attrs.model); each
// repaired entry is rewritten through a conditional Put. Exposed for the
// startup-repair path a crash-recovered base would run.
func (ix *Index) rewriteReferences(ctx context.Context, oldID, newID string) error {
	for id, r := range ix.byID {
		k := r.k
		body := domain.RewriteBodyLinks(k.ID, k.Body, oldID, newID)
		modelMoved := false
		if m, ok := k.Attrs["model"].(string); ok && m == oldID {
			k.Attrs["model"] = newID
			modelMoved = true
		}
		if body == k.Body && !modelMoved {
			continue
		}
		k.Body = body
		k.Links = domain.LinksFromBody(k.ID, k.Body)
		k.UpdatedAt = nowStored()
		gen, err := ix.write(ctx, &k, &r.gen)
		if err != nil {
			return err
		}
		ix.byID[id] = &record{k: k, gen: gen}
	}
	return nil
}

// RecordEvents bumps in-memory usage counters (design doc 0025 §10: usage
// is best-effort). The SQL store buffers and flushes; a GCS-only store
// would periodically snapshot these counters to a single object. Kept
// purely in memory here — the point is that aggregation needs no join.
func (ix *Index) RecordEvents(_ context.Context, event string, ids []string) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	now := nowStored()
	for _, id := range ids {
		u := ix.usage[id]
		if u == nil {
			u = &domain.Usage{}
			ix.usage[id] = u
		}
		switch event {
		case "search_hit":
			u.SearchHits++
		case "fetched":
			u.Fetches++
		case "compiled":
			u.Compiles++
		case "worked":
			u.Worked++
		case "failed":
			u.Failed++
		}
		t := now
		u.LastUsedAt = &t
	}
}

// Usage returns the aggregated counters for id (zero value when unseen) —
// the SQL store's usageLateral aggregation, precomputed in memory.
func (ix *Index) Usage(_ context.Context, id string) *domain.Usage {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	if u := ix.usage[id]; u != nil {
		cp := *u
		return &cp
	}
	return &domain.Usage{}
}
