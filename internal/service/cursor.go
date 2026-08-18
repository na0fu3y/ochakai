package service

import (
	"encoding/base64"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/store"
)

// Listing is one page of the query surface: the hits, and where the next
// page resumes when the order has not run out (design doc 0050 §2.1).
// Cursor is empty on the last page — its absence is how a caller learns
// there is nothing behind this one, which is the only count the surface
// gives (§2.3).
//
// A search fills Hits and never Cursor: a ranking has no next page
// (§2.2).
type Listing struct {
	Hits   []domain.SearchHit `json:"hits"`
	Cursor string             `json:"cursor,omitempty"`
	// Degraded says this ranking is worse than this deployment ordinarily
	// gives: the query could not be embedded, so it was ranked lexically
	// alone (design doc 0114). Only a search can carry it — a listing is
	// a total order with nothing to embed — and it is absent rather than
	// false when the answer is the whole answer.
	Degraded bool `json:"degraded,omitempty"`
}

// cursorVersion prefixes the encoded position so a later change of shape
// is a 400 with a sentence rather than a page starting somewhere
// surprising.
const cursorVersion = "1"

// The markers that tell an absent ordering value from an empty one. A
// listing ordered ASC NULLS LAST ends in the entries nobody has verified,
// and walking off that tail is the ordinary case, not an error.
const (
	cursorValue = '='
	cursorNull  = '~'
)

// encodeCursor packs a position in a listing's order: which listing it
// belongs to, that listing's ordering keys, and the id that breaks their
// ties. It is opaque on purpose (design doc 0050 §2.1) — the caller hands
// back what it received, and the shape stays ours to change.
//
// Not signed: a tampered cursor can only start a listing somewhere odd,
// and it moves neither the filter nor who may read (design doc 0002). A
// key that guards nothing is a key the deployment would have to hold
// (design doc 0003).
func encodeCursor(sort string, keys []*string, id string) string {
	parts := make([]string, 0, len(keys)+3)
	parts = append(parts, cursorVersion, sort)
	for _, k := range keys {
		if k == nil {
			parts = append(parts, string(cursorNull))
			continue
		}
		parts = append(parts, string(cursorValue)+*k)
	}
	parts = append(parts, id)
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join(parts, "\x00")))
}

// decodeCursor reads a cursor back as a store position, refusing one that
// belongs to a different listing: the keys are that listing's ordering
// columns, so a cursor from another one is not a position here. want is
// how many keys this listing orders by, the id aside.
func decodeCursor(enc, sort string, want int) (*store.After, error) {
	if enc == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return nil, malformedCursor()
	}
	parts := strings.Split(string(raw), "\x00")
	if len(parts) < 3 || parts[0] != cursorVersion {
		return nil, malformedCursor()
	}
	// The listing is checked before the key count, because a cursor from
	// another listing usually has a different number of keys and the
	// caller is better served by being told which listing it came from
	// than by being told it is malformed. Only a listing this server has
	// is named back: the rest is the caller's own bytes, and quoting them
	// says nothing true about the request.
	if parts[1] != sort {
		if slices.Contains(domain.ListSorts, parts[1]) || parts[1] == sortBySource ||
			parts[1] == sortByLinksTo || parts[1] == browseListing {
			return nil, Invalidf("cursor belongs to the %s listing; it cannot resume the %s one", parts[1], sort)
		}
		return nil, malformedCursor()
	}
	if len(parts) != want+3 {
		return nil, malformedCursor()
	}
	after := &store.After{Keys: make([]*string, 0, want), ID: parts[len(parts)-1]}
	for _, p := range parts[2 : len(parts)-1] {
		switch {
		case p == string(cursorNull):
			after.Keys = append(after.Keys, nil)
		case strings.HasPrefix(p, string(cursorValue)):
			v := p[1:]
			after.Keys = append(after.Keys, &v)
		default:
			return nil, malformedCursor()
		}
	}
	return after, nil
}

func malformedCursor() error {
	return Invalidf("cursor is not one this server handed out; pass back the cursor from the previous page")
}

// cursorKeys reads the ordering values of the last hit on a page — the
// same columns the feed's ORDER BY names, in the same order. They come
// off the hit rather than out of a second query because a listing already
// carries what it ranks by: the verification time the canary feed reads,
// the counts the usage feeds show the reviewer.
func cursorKeys(sort string, h domain.SearchHit) []*string {
	switch sort {
	case "verified_at":
		return []*string{cursorTime(h.VerifiedAt)}
	case "stale_after":
		return []*string{cursorText(h.StaleAfter)}
	case "usage":
		u := usageOf(h)
		return []*string{cursorCount(u.Recent.Fetches), cursorCount(u.Fetches), cursorTime(&h.CreatedAt)}
	case "failed":
		u := usageOf(h)
		return []*string{
			cursorCount(u.Recent.Failed), cursorCount(u.Failed),
			cursorCount(u.Worked), cursorTime(h.VerifiedAt),
		}
	default: // the reverse lookups order by id alone
		return nil
	}
}

// cursorSortKeys is how many ordering keys each listing has, which is
// what decodeCursor checks a cursor against before the store sees it.
func cursorSortKeys(sort string) int { return len(cursorKeys(sort, domain.SearchHit{})) }

func usageOf(h domain.SearchHit) domain.Usage {
	if h.Usage == nil {
		return domain.Usage{}
	}
	return *h.Usage
}

// cursorTime renders a timestamp the way PostgreSQL reads it back, to the
// precision it stores: a position that rounded would either repeat the
// last row of the previous page or skip the first of the next.
func cursorTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339Nano)
	return &s
}

func cursorCount(n int64) *string {
	s := strconv.FormatInt(n, 10)
	return &s
}

// cursorText carries a text ordering key. An empty one is still a value,
// not a NULL — the feeds that order by text only list rows that have it.
func cursorText(s string) *string { return &s }

// nextCursor returns the cursor for the page after hits, or "" when the
// listing ended. more says whether the store had a row beyond the page:
// a listing asks for one more than it hands out, so the last page is
// recognized as it is read rather than by a second request coming back
// empty.
func nextCursor(sort string, hits []domain.SearchHit, more bool) string {
	if !more || len(hits) == 0 {
		return ""
	}
	return encodeCursor(sort, cursorKeys(sort, hits[len(hits)-1]), hits[len(hits)-1].ID)
}

// searchBoundError is the refusal a cursor on a search gets: relevance
// has no page two (design doc 0050 §2.2). It names the way forward,
// because the message is the caller's next request.
func searchBoundError() error {
	return Invalidf("a search is bounded by limit and has no next page: its ranking is a fused window, "+
		"not an order to resume from — narrow it with filters, or walk a listing with sort=%s",
		strings.Join(domain.ListSorts, "|"))
}
