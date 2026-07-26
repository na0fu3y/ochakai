package domain

// Property tests for the rules the model states as predicates rather than
// as types (design doc 0035 §3.3). ValidID, Normalize and LinksFromBody
// are each asserted about *every* input in a comment somewhere; these
// targets check the ones other code actually leans on.

import (
	"strings"
	"testing"
)

// FuzzNormalizeIsStable pins the two things every boundary assumes when
// it normalizes an id before comparing it to stored text (design doc
// 0022): normalizing twice is normalizing once, so a value that has been
// through one boundary is not changed again by the next; and NFC never
// invents or destroys a path separator, so an id's segment structure
// survives it. Without the second, a normalized id could address a
// different entry than the one written.
func FuzzNormalizeIsStable(f *testing.F) {
	f.Add("metrics/revenue")
	f.Add("メトリクス/売上")
	f.Add("café/résumé") // NFD: combining acutes
	f.Add("ガ/ヷ")            // compositions that NFC rewrites
	f.Add("")
	f.Add("///")

	f.Fuzz(func(t *testing.T, s string) {
		n := Normalize(s)
		if nn := Normalize(n); nn != n {
			t.Errorf("Normalize is not idempotent: %q -> %q -> %q", s, n, nn)
		}
		if got, want := strings.Count(n, "/"), strings.Count(s, "/"); got != want {
			t.Errorf("Normalize changed the segment structure of %q: %d separators -> %d", s, want, got)
		}
	})
}

// FuzzValidIDAgreesWithPrefixes ties ValidID to what browse assumes about
// it (design docs 0014, 0017). Browsing walks an id one segment at a
// time, so every ancestor of a reachable entry has to be a valid prefix —
// otherwise an entry exists at an address no one can navigate to. The
// entry's own id must be a valid prefix too: ValidID is the stricter of
// the two everywhere except the reserved "index"/"log" filenames, which
// only name a directory when they lead a prefix.
func FuzzValidIDAgreesWithPrefixes(f *testing.F) {
	f.Add("metrics/revenue")
	f.Add("a/b/c/d")
	f.Add("index")
	f.Add("queries/index/monthly")
	f.Add("queries/log")
	f.Add(".hidden/x")
	f.Add("a//b")
	f.Add("")
	f.Add(strings.Repeat("x", 600))

	f.Fuzz(func(t *testing.T, id string) {
		if !ValidID(id) {
			return
		}
		if !ValidIDPrefix(id) {
			t.Errorf("ValidID(%q) but not ValidIDPrefix: nothing could browse to it", id)
		}
		segs := strings.Split(id, "/")
		for i := 1; i < len(segs); i++ {
			ancestor := strings.Join(segs[:i], "/")
			if !ValidIDPrefix(ancestor) {
				t.Errorf("ValidID(%q) but its ancestor %q is not a valid prefix", id, ancestor)
			}
		}
	})
}

// FuzzLinksFromBody asserts what the store is handed when it writes an
// entry's edges. Links are derived, never authored (design doc 0024), so
// nothing between the body and the database checks them: a self-link
// would make an entry its own backlink, an empty target would key a row
// to nothing, and a duplicate would double-count an edge that prose
// mentioned once.
func FuzzLinksFromBody(f *testing.F) {
	f.Add("metrics/revenue", "See [reading](ochakai://insights/revenue-reading).")
	f.Add("metrics/revenue", "Self [me](ochakai://metrics/revenue) and <ochakai://a/b>.")
	f.Add("a/b", "Relative [x](./c.md) and absolute [y](/d/e.md).")
	f.Add("a/b", "Bare ochakai://tables/orders を参照。 And http://example.com/x.md.")
	f.Add("a/b", "![img](b/weekly.png) is an attachment, not a link.")
	f.Add("a/b", "```\n[fenced](ochakai://x/y)\n```\nand `[code](ochakai://z)`.")
	f.Add("", "")

	f.Fuzz(func(t *testing.T, id, body string) {
		// Every write path validates the id before deriving links (the
		// service's create/update run validate, then deriveLinks), so a
		// malformed id is not an input link derivation ever sees — and
		// relative targets resolve against it, so its shape is what the
		// targets inherit.
		if !ValidID(id) {
			t.Skip()
		}
		links := LinksFromBody(id, body)
		seen := map[Link]bool{}
		for _, l := range links {
			if l.Target == "" {
				t.Errorf("empty link target from body %q", body)
			}
			if l.Target == id {
				t.Errorf("self-link to %q from its own body", id)
			}
			if Normalize(l.Target) != l.Target {
				t.Errorf("link target %q is not normalized", l.Target)
			}
			if seen[l] {
				t.Errorf("duplicate link %+v", l)
			}
			seen[l] = true
			// DisplayText is what every surface renders; an edge with no
			// readable name is a dead entry in a "linked from" list.
			if l.DisplayText() == "" {
				t.Errorf("link %+v renders as nothing", l)
			}
		}
		// Deriving twice gives the same edges: the body is the only input,
		// so a second write of unchanged prose must not churn the rows.
		if again := LinksFromBody(id, body); len(again) != len(links) {
			t.Errorf("LinksFromBody is not deterministic: %d then %d", len(links), len(again))
		}
	})
}

// TestOKFStatusRoundTrip states design doc 0034 §3.4's mapping over the
// whole status vocabulary rather than the examples: exporting and
// re-importing returns the status, except for the one direction the
// design calls lossy on purpose. A status added to Statuses without a
// place in the OKF lifecycle fails here.
func TestOKFStatusRoundTrip(t *testing.T) {
	for _, s := range Statuses {
		okf := OKFStatus(s)
		switch okf {
		case OKFStatusDraft, OKFStatusStable, OKFStatusDeprecated:
		default:
			t.Errorf("OKFStatus(%q) = %q, which is not an OKF lifecycle value", s, okf)
		}
		// ochakai's own exports write a verified key alongside stable, and
		// only alongside stable.
		got, known := StatusFromOKF(okf, s == StatusVerified)
		if !known {
			t.Errorf("our own export of %q reads back as unknown", s)
		}
		want := s
		if s == StatusRejected {
			want = StatusDeprecated // OKF has no "was never accepted" (0034 §3.4)
		}
		if got != want {
			t.Errorf("%q exported as %q read back as %q, want %q", s, okf, got, want)
		}
	}
}
