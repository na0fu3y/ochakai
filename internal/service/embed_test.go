package service

import (
	"errors"
	"strings"
	"testing"
)

// A reembed cursor is opaque like a listing cursor (issue #366): it must
// round-trip its two positions, refuse anything not base64url, and
// refuse a listing's own cursor rather than silently misreading it as a
// reembed position.
func TestReembedCursorRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name              string
		entryID, filePath string
	}{
		{name: "concepts only", entryID: "metrics/revenue", filePath: ""},
		{name: "concepts and files", entryID: "metrics/revenue", filePath: "insights/q3/report.pdf"},
		// A NUL cannot occur in either field (design doc 0017), but the
		// separator character itself and other punctuation must survive.
		{name: "punctuation", entryID: "a/b?c+d", filePath: "x/y/name with spaces.txt"},
	} {
		enc := encodeReembedCursor(tc.entryID, tc.filePath)
		gotEntry, gotPath, err := decodeReembedCursor(enc)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if gotEntry != tc.entryID || gotPath != tc.filePath {
			t.Errorf("%s: got (%q, %q), want (%q, %q)",
				tc.name, gotEntry, gotPath, tc.entryID, tc.filePath)
		}
	}
}

// The empty cursor is the first pass, not a malformed one — Reembed with
// no cursor argument must decode cleanly.
func TestReembedCursorEmpty(t *testing.T) {
	entry, filePath, err := decodeReembedCursor("")
	if err != nil || entry != "" || filePath != "" {
		t.Errorf("empty cursor: got (%q, %q, %v), want the first pass", entry, filePath, err)
	}
}

func TestReembedCursorRefusals(t *testing.T) {
	var inputErr *InvalidInputError
	listingCursor := encodeCursor("usage", []*string{ptr("74"), ptr("2026-07-14T02:33:41Z")}, "insights/seasonality")

	for _, tc := range []struct {
		name, cursor string
	}{
		{name: "not base64", cursor: "not a cursor!"},
		{name: "a listing's own cursor", cursor: listingCursor},
		{name: "wrong field count", cursor: encodeRaw("1", "reembed", "metrics/revenue")},
		// The shape a release before design doc 0091 handed out: one
		// field longer, because its file half was a (concept, file) pair.
		{name: "the previous shape", cursor: encodeRaw("1", "reembed", "metrics/revenue", "insights/q3", "report.pdf")},
		{name: "another version", cursor: encodeRaw("9", "reembed", "metrics/revenue", "")},
		{name: "missing the reembed tag", cursor: encodeRaw("1", "metrics/revenue", "", "")},
	} {
		_, _, err := decodeReembedCursor(tc.cursor)
		if !errors.As(err, &inputErr) || !strings.Contains(err.Error(), "not one this server handed out") {
			t.Errorf("%s: got %v, want an InvalidInputError saying the cursor is not one this server handed out", tc.name, err)
		}
	}
}
