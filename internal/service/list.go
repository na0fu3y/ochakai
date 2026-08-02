// Listings: the four review feeds and the reverse lookups, paged with an
// opaque cursor. A listing is a total order and a search is not, which is
// why they page differently and are asked for differently (design docs
// 0050, 0062).
package service

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/store"
)

// SearchOrList answers the query surface REST (GET /api/v1/search) and
// MCP (search_concepts) both expose, so the two cannot drift: one place
// decides whether a request searches or lists, validates the mode, and
// runs it. A sort mode names one of the listing feeds (see list); with no
// sort, a source filter and no query is the reverse lookup; otherwise it
// is a search.
//
// Sort and query are mutually exclusive rather than combined: a feed is a
// queue a reviewer works through, and ranking it by relevance to a query
// would leave the reviewer with neither the whole queue nor the best
// matches. The CLI refuses the same pair before it ever gets here.
//
// cursor resumes a listing where the previous page ended, and is refused
// on a search: a listing has a total order to resume from, a ranking has
// a window (design doc 0050 §2.2).
func (s *Service) SearchOrList(ctx context.Context, query, sort, cursor string, f store.Filter, limit int) (*Listing, error) {
	if sort != "" {
		if !domain.ValidListSort(sort) {
			return nil, Invalidf("invalid sort %q (valid: %s)", sort, strings.Join(domain.ListSorts, ", "))
		}
		if strings.TrimSpace(query) != "" {
			return nil, Invalidf("sort=%s lists concepts; it cannot be combined with a search query", sort)
		}
		return s.list(ctx, sort, cursor, f, limit)
	}
	// A reverse lookup with nothing to search by lists rather than
	// searches: "what derives from this material" (design doc 0037 §2.3)
	// and "what links at this concept" both have a set for an answer and no
	// text to rank it by. Both list in address order, and the mode is
	// named so a cursor from one cannot resume the other.
	if strings.TrimSpace(query) == "" {
		switch {
		case f.Source != "":
			return s.list(ctx, sortBySource, cursor, f, limit)
		case f.LinksTo != "":
			return s.list(ctx, sortByLinksTo, cursor, f, limit)
		}
	}
	if cursor != "" {
		return nil, searchBoundError()
	}
	hits, err := s.Search(ctx, query, f, limit)
	if err != nil {
		return nil, err
	}
	return &Listing{Hits: hits}, nil
}

// The listing modes a reverse-lookup filter with no query selects.
// Neither is one of domain.ListSorts: no caller can ask for them by name,
// because the filter already says what is wanted. They are distinct so
// that a cursor names the listing it came from.
const (
	sortBySource  = "source"
	sortByLinksTo = "links_to"
)

// askableKeys are the frontmatter keys "fm." answers, and askableHint is
// the list a refusal shows. Both come from the domain vocabulary, so the
// server, the OpenAPI text, the MCP schemas and the CLI help cannot
// disagree about what is askable.
var (
	askableKeys = domain.AskableFrontmatterKeys()
	askableHint = strings.Join(askableKeys, ", ")
)

// checkedFilter returns f with its path scopes normalized, rejecting one
// that could not lead an id, and refuses a frontmatter filter naming a key
// "fm." does not answer. Every surface hands its filter through here — the
// contract lives on the server (design doc 0015 §2), so REST, MCP and the
// CLI cannot disagree about whether "teams/growth/" is the same scope as
// "teams/growth", nor about which spellings of a question exist.
//
// An empty prefix is the root, which narrows nothing: it drops out rather
// than erroring, because asking for everything is a request the server can
// answer, not a mistake to report.
//
// This is not an access check. A caller may pass any prefix or none, and
// none returns the whole base — reading is bounded by who can reach the
// service, not by what they ask for (design doc 0002, and 0041 §4).
func checkedFilter(f store.Filter) (store.Filter, error) {
	// Sorted, so a request naming two bad keys reports the same one every
	// time rather than whichever the map handed over first.
	for _, key := range slices.Sorted(maps.Keys(f.Frontmatter)) {
		if use, ok := domain.FilterOwnedKeys[key]; ok {
			return f, Invalidf("fm.%s is not a filter: ochakai reads %s through a column of its own, "+
				"which does not answer what the frontmatter says — use %s", key, key, use)
		}
		if !slices.Contains(askableKeys, key) {
			return f, Invalidf("fm.%s is not a filter: OKF does not define %q, and the filter "+
				"vocabulary is OKF's — a producer's own key is stored and handed back exactly as "+
				"written, not asked for. Askable: %s", key, key, askableHint)
		}
	}
	if len(f.Prefixes) == 0 {
		return f, nil
	}
	scoped := make([]string, 0, len(f.Prefixes))
	for _, p := range f.Prefixes {
		clean, err := normalizePrefix(p)
		if err != nil {
			return f, err
		}
		if clean != "" {
			scoped = append(scoped, clean)
		}
	}
	f.Prefixes = scoped
	return f, nil
}

// list runs one of the listing feeds. None of them is a search: no usage
// is recorded (reading a review queue must not inflate the very signal it
// ranks by), and every hit carries score 0, so the REST and MCP responses
// keep the exact wire shape of a search across all modes.
//
//   - verified_at — by verification age, oldest first (never-verified
//     last): the feed for canary runs over verified golden queries (see
//     docs/guides/golden-query-canary.md).
//   - usage — by demand, most-searched first, never-used drafts
//     oldest-first at the bottom: the web UI's draft review queue. Hits
//     carry their usage totals so the reviewer sees the evidence inline.
//   - failed — concepts whose failure reports are still unanswered, worst
//     first: the re-verification feed (design doc 0025), the
//     evidence-based counterpart to verified_at. Verifying (or rejecting)
//     takes a concept out of it, so a base that is kept up yields an empty
//     one. Hits carry their usage totals.
//   - stale_after — concepts whose declared expiry has passed, most
//     overdue first (design doc 0037 §2.1); only concepts stale today, a
//     date that has not come due is not work yet. The one feed verifying
//     does not empty: verified_at and failed are server observations,
//     while stale_after is what the writer declared, so clearing it means
//     editing the concept to declare a new expiry (design doc 0037 §2.2).
//   - source — the concepts citing one resource, the reverse of
//     sources[].resource (design doc 0037 §2.3).
//
// Every mode pages: each has a total order ending in the id, so a cursor
// carries a position in it and the caller walks past the cap instead of
// stopping at it (design doc 0050 §2.1). One row beyond the page is read
// to tell a full page from the last one — without it, a listing whose
// length happens to equal the limit would hand out a cursor onto nothing.
func (s *Service) list(ctx context.Context, sort, cursor string, f store.Filter, limit int) (*Listing, error) {
	limit, err := checkedLimit(limit, 100, 1000)
	if err != nil {
		return nil, err
	}
	f, err = checkedFilter(f)
	if err != nil {
		return nil, err
	}
	after, err := decodeCursor(cursor, sort, cursorSortKeys(sort))
	if err != nil {
		return nil, err
	}
	hits, err := s.listPage(ctx, sort, f, after, limit+1)
	if err != nil {
		return nil, err
	}
	more := len(hits) > limit
	if more {
		hits = hits[:limit]
	}
	return &Listing{Hits: hits, Cursor: nextCursor(sort, hits, more)}, nil
}

// listPage runs one feed's query. The two usage feeds rank by the usage
// totals and hand them back with each hit, so they are hits already; the
// rest return concepts to project.
func (s *Service) listPage(ctx context.Context, sort string, f store.Filter, after *store.After, limit int) ([]domain.SearchHit, error) {
	switch sort {
	case "usage":
		return s.Store.ListByUsage(ctx, f, after, limit)
	case "failed":
		return s.Store.ListByFailed(ctx, f, after, limit)
	}
	list := s.Store.ListByVerifiedAt
	switch sort {
	case "stale_after":
		list = s.Store.ListByStaleAfter
	case sortBySource, sortByLinksTo:
		// Both reverse lookups are the same query with a different
		// filter, in address order (design doc 0050: a listing pages).
		list = s.Store.ListByAddress
	}
	entries, err := list(ctx, f, after, limit)
	if err != nil {
		return nil, err
	}
	hits := make([]domain.SearchHit, len(entries))
	for i := range entries {
		hits[i] = domain.SearchHit{Summary: domain.SummaryOf(&entries[i])}
	}
	return hits, nil
}
