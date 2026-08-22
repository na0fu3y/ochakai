package service

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/store"
)

// A cursor is opaque, which makes its round trip the only thing holding
// the walk together: what comes back has to be the position that went out
// — including the absent one, since a verification-age listing ends in
// entries nobody has verified (design doc 0050 §2.1).
func TestCursorRoundTrip(t *testing.T) {
	at := "2026-07-14T02:33:41.019778Z"
	for _, tc := range []struct {
		sort string
		keys []*string
		id   string
	}{
		{sort: "verified_at", keys: []*string{&at}, id: "metrics/revenue"},
		{sort: "verified_at", keys: []*string{nil}, id: "glossary/completed-order"},
		{sort: "usage", keys: []*string{ptr("74"), &at}, id: "insights/seasonality"},
		{sort: "failed", keys: []*string{ptr("3"), ptr("0"), nil}, id: "queries/broken"},
		{sort: "source", keys: nil, id: "tables/shop-orders"},
		// An id with the separator the encoding joins on, and one with a
		// character base64url has to survive.
		{sort: "stale_after", keys: []*string{ptr("2026-12-31")}, id: "a/b?c+d"},
		// An empty ordering value is a value, not an absent one.
		{sort: "stale_after", keys: []*string{ptr("")}, id: "x"},
	} {
		enc := encodeCursor(tc.sort, tc.keys, tc.id)
		got, err := decodeCursor(enc, tc.sort, len(tc.keys))
		if err != nil {
			t.Fatalf("%s: %v", tc.sort, err)
		}
		if got.ID != tc.id {
			t.Errorf("%s: id = %q, want %q", tc.sort, got.ID, tc.id)
		}
		if len(got.Keys) != len(tc.keys) {
			t.Fatalf("%s: %d keys came back, %d went out", tc.sort, len(got.Keys), len(tc.keys))
		}
		for i := range tc.keys {
			switch {
			case tc.keys[i] == nil && got.Keys[i] != nil:
				t.Errorf("%s: key %d came back as %q, want the absent one", tc.sort, i, *got.Keys[i])
			case tc.keys[i] != nil && got.Keys[i] == nil:
				t.Errorf("%s: key %d came back absent, want %q", tc.sort, i, *tc.keys[i])
			case tc.keys[i] != nil && *got.Keys[i] != *tc.keys[i]:
				t.Errorf("%s: key %d = %q, want %q", tc.sort, i, *got.Keys[i], *tc.keys[i])
			}
		}
	}
}

// Everything a cursor can be wrong about is a client error with a
// sentence, never a page that starts somewhere arbitrary: the keys are
// one listing's ordering columns, so a cursor from another listing is not
// a position here.
func TestCursorRefusals(t *testing.T) {
	var inputErr *InvalidInputError
	usage := encodeCursor("usage", []*string{ptr("74"), ptr("2026-07-14T02:33:41Z")}, "insights/seasonality")

	for _, tc := range []struct {
		name, cursor, sort, want string
		keys                     int
	}{
		{name: "another listing", cursor: usage, sort: "failed", keys: 3, want: "belongs to the usage listing"},
		{name: "not base64", cursor: "not a cursor!", sort: "usage", keys: 2, want: "not one this server handed out"},
		{name: "wrong key count", cursor: usage, sort: "usage", keys: 1, want: "not one this server handed out"},
		{name: "unmarked key", cursor: encodeRaw("1", "usage", "74", "x"), sort: "usage", keys: 1,
			want: "not one this server handed out"},
		{name: "another version", cursor: encodeRaw("9", "usage", "=74", "x"), sort: "usage", keys: 1,
			want: "not one this server handed out"},
	} {
		_, err := decodeCursor(tc.cursor, tc.sort, tc.keys)
		if !errors.As(err, &inputErr) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want an InvalidInputError saying %q", tc.name, err, tc.want)
		}
	}

	// The empty cursor is the first page, not a malformed one.
	if got, err := decodeCursor("", "usage", 2); err != nil || got != nil {
		t.Errorf("no cursor: got (%v, %v), want the first page", got, err)
	}
}

// A ranking has no next page, and the refusal has to name the way
// forward: the caller reads the message and makes one more request
// (design doc 0050 §2.2). Nothing here touches the store — the check runs
// before any of it, as the mode checks beside it do.
func TestSearchRefusesACursor(t *testing.T) {
	s := &Service{}
	_, err := s.SearchOrList(context.Background(), "revenue", "", "MQBhbnl0aGluZw", store.Filter{}, 0)
	if _, ok := errors.AsType[*InvalidInputError](err); !ok {
		t.Fatalf("a cursor on a search: got %v, want an InvalidInputError", err)
	}
	for _, sort := range domain.ListSorts {
		if !strings.Contains(err.Error(), sort) {
			t.Errorf("the refusal never mentions sort=%s, so it does not say what to do instead: %v", sort, err)
		}
	}
}

// The keys a page hands out are the columns its feed orders by, in that
// order — the store reads them positionally, so a listing whose ORDER BY
// grows a column and whose cursor does not would resume against the wrong
// one.
func TestCursorKeysMatchTheOrder(t *testing.T) {
	at := time.Date(2026, 7, 14, 2, 33, 41, 19778000, time.UTC)
	hit := domain.SearchHit{
		Summary: domain.Summary{ID: "metrics/revenue", StaleAfter: "2026-12-31", VerifiedAt: &at, CreatedAt: at},
		// search_hits and fetches differ on purpose: the usage feed ranks
		// by reads, and a cursor built from the listings it was ranked by
		// until now would pass every other assertion here.
		// The recent counts differ from the lifetime ones for the same
		// reason: the feeds rank on the window and break ties on the
		// total, so a cursor built from either alone passes half of this.
		Usage: &domain.Usage{SearchHits: 74, Fetches: 41, Worked: 2, Failed: 3,
			Recent: domain.RecentUsage{Days: domain.RecentUsageDays, Fetches: 9, Failed: 1}},
	}
	for sort, want := range map[string][]string{
		"verified_at": {"2026-07-14T02:33:41.019778Z"},
		"stale_after": {"2026-12-31"},
		"usage":       {"9", "41", "2026-07-14T02:33:41.019778Z"},
		"failed":      {"1", "3", "2", "2026-07-14T02:33:41.019778Z"},
		"source":      {},
	} {
		keys := cursorKeys(sort, hit)
		if len(keys) != len(want) {
			t.Errorf("%s: %d keys, want %d", sort, len(keys), len(want))
			continue
		}
		if got := cursorSortKeys(sort); got != len(want) {
			t.Errorf("%s: cursorSortKeys says %d, the feed has %d", sort, got, len(want))
		}
		for i := range want {
			if keys[i] == nil || *keys[i] != want[i] {
				t.Errorf("%s: key %d = %v, want %q", sort, i, keys[i], want[i])
			}
		}
	}

	// A feed whose ordering value is absent carries the absence, which is
	// the never-verified tail of two of them.
	unverified := domain.SearchHit{Summary: domain.Summary{ID: "x"}, Usage: &domain.Usage{}}
	if k := cursorKeys("verified_at", unverified); len(k) != 1 || k[0] != nil {
		t.Errorf("a never-verified entry's cursor key = %v, want the absent one", k)
	}
	// A listing whose hits carry no usage object at all must still produce
	// a position rather than panic: score-0 listings share one wire shape.
	if k := cursorKeys("usage", domain.SearchHit{Summary: domain.Summary{ID: "x"}}); len(k) != 3 || *k[0] != "0" {
		t.Errorf("a hit with no usage totals = %v, want a zero position", k)
	}
}

// nextCursor is what decides whether a caller sees an end: a page with
// nothing behind it must not hand one out, or every walk gains a trailing
// request that returns nothing (design doc 0050 §2.3).
func TestNextCursorOnlyWhenMoreFollows(t *testing.T) {
	hits := []domain.SearchHit{{Summary: domain.Summary{ID: "a", StaleAfter: "2026-01-01"}}}
	if got := nextCursor("stale_after", hits, false); got != "" {
		t.Errorf("the last page handed out a cursor: %q", got)
	}
	if got := nextCursor("stale_after", nil, true); got != "" {
		t.Errorf("an empty page handed out a cursor: %q", got)
	}
	if got := nextCursor("stale_after", hits, true); got == "" {
		t.Error("a page with more behind it handed out no cursor")
	}
}

func ptr(s string) *string { return &s }

// encodeRaw builds a cursor body the encoder would never produce, for the
// refusals that a well-formed one cannot reach.
func encodeRaw(parts ...string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join(parts, "\x00")))
}

// A level's cursor carries two positions and the directory it walked
// (design doc 0101): the two runs of a level end independently, and a
// cursor is a position in one listing — resuming another directory from
// it would skip rows in both.
func TestBrowseCursorCarriesBothRunsAndItsDirectory(t *testing.T) {
	lvl := &store.Level{
		Concepts:     []store.BrowseConcept{{ID: "sales/a"}, {ID: "sales/b"}},
		Files:        []store.BrowseFile{{Path: "sales/x.png"}},
		MoreConcepts: true,
		MoreFiles:    true,
	}
	enc := nextBrowseCursor("sales/", lvl)
	after, err := decodeBrowseCursor(enc, "sales/")
	if err != nil {
		t.Fatal(err)
	}
	if after.Concepts == nil || *after.Concepts != "sales/b" {
		t.Errorf("concepts resume at %v, want the last row of the page", after.Concepts)
	}
	if after.Files == nil || *after.Files != "sales/x.png" {
		t.Errorf("files resume at %v, want the last row of the page", after.Files)
	}

	// A kind that ran out travels as absent, so the next page does not
	// ask its query again.
	lvl.MoreFiles = false
	after, err = decodeBrowseCursor(nextBrowseCursor("sales/", lvl), "sales/")
	if err != nil {
		t.Fatal(err)
	}
	if after.Files != nil {
		t.Errorf("a finished kind resumes at %v, want nothing", *after.Files)
	}

	// Neither kind behind: no cursor at all, which is how a caller learns
	// the level is done (design doc 0068 §2.1).
	lvl.MoreConcepts = false
	if got := nextBrowseCursor("sales/", lvl); got != "" {
		t.Errorf("a last page handed out %q", got)
	}

	// The wrong directory, and the wrong listing.
	lvl.MoreConcepts = true
	enc = nextBrowseCursor("sales/", lvl)
	if _, err := decodeBrowseCursor(enc, "glossary/"); err == nil ||
		!strings.Contains(err.Error(), "sales") || !strings.Contains(err.Error(), "glossary") {
		t.Errorf("a cursor from another directory = %v, want a refusal naming both", err)
	}
	if _, err := decodeBrowseCursor(encodeCursor("usage", []*string{ptr("7"), ptr("x")}, "sales/"), "sales/"); err == nil {
		t.Error("a cursor from the usage feed resumed a level")
	}
	// The root has no name of its own, so the refusal gives it one rather
	// than quoting an empty string at the reader.
	if _, err := decodeBrowseCursor(enc, ""); err == nil || !strings.Contains(err.Error(), "root") {
		t.Errorf("a cursor resuming the root from elsewhere = %v, want the root named", err)
	}
}
