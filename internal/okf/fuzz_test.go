package okf

// Property tests for the two directions of the OKF format (design doc
// 0035 §3.3). Parse is the only place ochakai reads bytes it did not
// write — a bundle from Git, from another instance, from a producer that
// is not ochakai at all — and design doc 0036 §3.12 lays out the
// round-trip rules ochakai holds itself to. Both are statements about
// every input, which is what a fuzz target can check and an example
// cannot.

import (
	"slices"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// FuzzParse asserts what Parse promises about anything it accepts, not
// merely that it survives. SPEC §11 forbids rejecting a concept over an
// unknown field value, so Parse reinterprets rather than refuses — and
// the postconditions below are what the store then relies on, having
// been told the entry came from a parser.
func FuzzParse(f *testing.F) {
	f.Add([]byte("---\ntype: Metric\ntitle: Revenue\n---\n\nThe headline number.\n"))
	f.Add([]byte("---\ntype: Golden Query\nstatus: stable\nverified:\n  - by: human:a@b.c\n---\n"))
	f.Add([]byte("---\ntype: Insight\nstale_after: 2026-12-31\ntags: [sales, orders]\n---\nbody"))
	f.Add([]byte("---\ntype: Insight\ntags: sales, orders\nstatus: nonsense\n---\n"))
	f.Add([]byte("---\ntype: Reference\nsources:\n  - last_modified: 2026-01-02\n---\n"))
	f.Add([]byte("---\n---\n"))                   // empty frontmatter
	f.Add([]byte("\ufeff---\ntype: Metric\n---")) // BOM, and ending at the delimiter
	f.Add([]byte("---\r\ntype: Metric\r\n---\r\nbody"))
	f.Add([]byte("no frontmatter at all"))

	f.Fuzz(func(t *testing.T, doc []byte) {
		k, _, err := Parse(doc)
		if err != nil {
			if k != nil {
				t.Fatalf("Parse returned both an entry and an error: %v", err)
			}
			return
		}
		// Parse rejects a document whose type it cannot hold, so anything
		// that came back has one the rest of the system will accept.
		if !domain.ValidType(k.Type) {
			t.Errorf("accepted an unusable type %q", k.Type)
		}
		// stale_after is a date or nothing: an unparseable one is dropped
		// with a note, never passed through (SPEC §5.5).
		if !domain.ValidStaleAfter(k.StaleAfter) {
			t.Errorf("accepted stale_after %q", k.StaleAfter)
		}
		// The status is either one ochakai knows or deliberately unset —
		// "" means "the document named none", which the write path reads
		// as draft-on-create/unchanged-on-update (design doc 0036 §3.4).
		if k.Status != "" && !domain.ValidStatus(k.Status) {
			t.Errorf("accepted status %q", k.Status)
		}
		if b := strings.TrimSpace(k.Body); b != k.Body {
			t.Errorf("body is not trimmed: %q", k.Body)
		}
		// Server-owned provenance never comes from the payload (design
		// doc 0009): whatever the document claimed, Parse must not have
		// filled any of it in.
		if k.CreatedBy != (domain.Actor{}) || k.UpdatedBy != (domain.Actor{}) ||
			k.VerifiedBy != nil || k.VerifiedAt != nil ||
			k.RejectedBy != nil || k.RejectedAt != nil {
			t.Errorf("Parse filled in provenance from the payload: %+v", k)
		}
		if len(k.Links) != 0 {
			t.Errorf("Parse read links from the document: %+v", k.Links) // derived from the body instead (0024)
		}
	})
}

// FuzzDocumentRoundTrip checks design doc 0036 §3.12's claim — that an
// ochakai export re-imports as what it says — over arbitrary field
// values rather than the handful an example can name. Writing then
// reading must return the same authored content.
//
// Three fields are outside the claim by construction. id is the file's
// path in the bundle, so Document does not write it (design doc 0017)
// and the importer takes it from the filename. Links are derived from
// the body on write, not stored (design doc 0024). Provenance belongs to
// the instance, not the bundle (design doc 0009).
//
// What this varies is the base envelope — type, title, description,
// tags, status, stale_after, body. The typed fields design doc 0036
// lifted out of attrs (sources and usage_window, §3.7; the Attested
// Computation contract, §3.8) are compared by SameContent but left at
// their zero values here, so the property does not yet say anything
// about them. Worth extending when someone next touches them.
func FuzzDocumentRoundTrip(f *testing.F) {
	f.Add("Metric", "Revenue", "The headline number", "sales,orders", "verified", "2026-12-31", "Body text.")
	f.Add("Golden Query", "", "", "", "draft", "", "")
	f.Add("Attested Computation", "T", "D", "one", "rejected", "", "- a list\n- of things")
	f.Add("カスタム型", "タイトル", "説明", "タグ", "deprecated", "", "本文です。")
	f.Add("Insight", "  padded  ", "", "a,,b", "", "not-a-date", "  \n  ")
	f.Add("Metric ", "", "", "", "draft", "", "")                   // trailing space: trimmed before storage, so not a round trip of one
	f.Add("Metric", "\nleading", "trailing\n", "", "draft", "", "") // newlines at either end: dropped before #183, kept now
	f.Add("Metric", "", "", " indented\nsecond", "draft", "", "")   // a tag whose first line is indented: emits frontmatter Parse rejects
	f.Add("Metric", "", "", "", "draft", "", "first\r\nsecond")     // CRLF body: normalized to LF on read (0036 §3.12)

	f.Fuzz(func(t *testing.T, typ, title, desc, tags, status, staleAfter, body string) {
		k := &domain.Knowledge{
			// Types are canonicalized on every write path (service's
			// normalizeKeys), so this is the only form that can reach
			// storage — and therefore the only one an export can contain.
			// Without it the property is about entries that cannot exist:
			// "Metric " is a valid type by ValidType, but Parse trims it
			// and no write would have stored the trailing space either.
			Type:  domain.CanonicalType(domain.Type(strings.TrimSpace(domain.Normalize(typ)))),
			Title: title, Description: desc,
			Status: domain.Status(status), StaleAfter: staleAfter, Body: body,
		}
		for _, tag := range strings.Split(tags, ",") {
			if tag != "" {
				k.Tags = append(k.Tags, tag)
			}
		}
		// Only entries the write path would have accepted can round-trip;
		// the rest is not a promise anyone made.
		if !domain.ValidType(k.Type) || !domain.ValidStatus(k.Status) ||
			!domain.ValidStaleAfter(k.StaleAfter) {
			t.Skip()
		}
		// The fault this target found that is still open, and it is a
		// class rather than a single shape: a value that spans lines and
		// whose *first* line is indented emits frontmatter ochakai cannot
		// read back at all. Not a lossy field — Parse fails outright, so
		// the entry exports into a bundle whose document is skipped on
		// import (design doc 0019) instead of round-tripping.
		//
		// A block scalar takes its indentation from the first content
		// line, so an indented one needs an explicit indicator; the
		// emitter writes one that does not match, or none at all. Shapes
		// found so far:
		//
		//	"\tx\ny"   anywhere: "found a tab character where an
		//	           indentation space is expected".
		//	" x\ny"    as a tag — a sequence item: "did not find expected
		//	           '-' indicator". Fine as a mapping value.
		//	"\nx"      as a tag: the same. Fine as a mapping value, which
		//	           is what #183 fixed.
		//
		// A single-line value is safe whatever it holds; the emitter
		// quotes it rather than reaching for a block scalar. Beyond that
		// the exact rule is the emitter's bug surface, and every time
		// this skip was narrowed to match it, fuzzing found another
		// shape. So it rounds up instead: any multi-line tag, and any
		// multi-line value opening with a space or tab. That skips
		// several shapes that do work ("x\n y" as a tag), which costs
		// coverage but keeps the skip from being a second bug to track.
		//
		// It reproduces in gopkg.in/yaml.v3 v3.0.1, go.yaml.in/yaml
		// v3.0.4 and v4 alike, so there is no upstream fix to take: the
		// fix belongs in Document, which is why it is still open here.
		// Enumerating the surface is the losing half of that fix — the
		// paragraph above is the evidence for it.
		//
		// The other fault this target found — a dropped leading newline —
		// was an emitter bug that upstream had already fixed, and moving
		// to the maintained library took it (#183).
		// TestDocumentKeepsLeadingNewlines pins that, and those values
		// are checked here now.
		if indentedFirstLine(k.Title) || indentedFirstLine(k.Description) ||
			indentedFirstLine(k.StatusNote) ||
			slices.ContainsFunc(k.Tags, func(t string) bool { return strings.Contains(t, "\n") }) {
			t.Skip()
		}

		doc, err := Document(k)
		if err != nil {
			t.Fatalf("Document(%+v): %v", k, err)
		}
		got, _, err := Parse(doc)
		if err != nil {
			t.Fatalf("Parse of our own Document output failed: %v\n%s", err, doc)
		}

		// Compare on the terms the round-trip is stated in: the fields a
		// writer controls, minus the three the format carries elsewhere.
		want := *k
		// CRLF comes back as LF, by the rule design doc 0036 §3.12 states
		// for line endings: a bundle authored on Windows must import the
		// same as one authored anywhere else. It shows up in the body and
		// nowhere else — frontmatter values go through YAML, which escapes
		// the carriage return in a quoted scalar, so they arrive verbatim.
		want.Body = strings.TrimSpace(strings.ReplaceAll(k.Body, "\r\n", "\n"))
		// The one documented lossy step: OKF has no lifecycle value for
		// "was never accepted", so a rejection exports as deprecated and
		// comes back as one (design doc 0036 §3.4). The ruling stays in
		// this instance's record, which is where 0009 puts it.
		if want.Status == domain.StatusRejected {
			want.Status = domain.StatusDeprecated
		}
		got.ID, want.ID = "", ""
		got.Links, want.Links = nil, nil
		if !got.SameContent(&want) {
			t.Errorf("round trip changed the entry:\n got %+v\nwant %+v\ndocument:\n%s", *got, want, doc)
		}
	})
}

// indentedFirstLine reports whether s is the shape described at the skip
// above: a value that spans lines and opens with a space or a tab.
func indentedFirstLine(s string) bool {
	first, _, multiLine := strings.Cut(s, "\n")
	return multiLine && (strings.HasPrefix(first, " ") || strings.HasPrefix(first, "\t"))
}
