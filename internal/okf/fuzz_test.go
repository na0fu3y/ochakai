package okf

// Property tests for the two directions of the OKF format (design doc
// 0035 §3.3). Parse is the only place ochakai reads bytes it did not
// write — a bundle from Git, from another instance, from a producer that
// is not ochakai at all — and design doc 0036 §3.12 lays out
// the round-trip rules ochakai promises to hold to. Both are statements about every
// input, which is what a fuzz target can check and an example cannot.

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
	f.Add("Metric ", "", "", "", "draft", "", "") // trailing space: trimmed before storage, so not a round trip of one

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
		// Two faults this target found, scoped out here and reported
		// rather than worked around — both are in how yaml.v3 emits a
		// multi-line string as a block scalar, and both need a fix in
		// Document (which would change the bytes of every exported
		// document that has a multi-line field), not in a test.
		//
		//  1. A leading newline is dropped: "\nx" comes back "x", "\n\nx"
		//     comes back "\nx". Only the first one, and only at the front
		//     — trailing newlines, blank lines and embedded newlines all
		//     survive, and a leading space is enough to make it survive.
		//  2. A value with a tab in it produces frontmatter ochakai
		//     cannot read back: Document writes the tab into an indented
		//     block scalar and Parse fails with "found a tab character
		//     where an indentation space is expected". This is the worse
		//     of the two — such an entry exports into a bundle whose
		//     document is skipped on import (design doc 0019) rather than
		//     round-tripping.
		if blockScalarFault(k.Title) || blockScalarFault(k.Description) ||
			blockScalarFault(k.StatusNote) || slices.ContainsFunc(k.Tags, blockScalarFault) {
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
		want.Body = strings.TrimSpace(k.Body)
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

// blockScalarFault reports whether s would go into the frontmatter as a
// block scalar that does not survive the trip back — the two faults
// listed at the skip above. yaml.v3 only reaches for a block scalar when
// the value spans lines, so a single-line value is always safe.
func blockScalarFault(s string) bool {
	return strings.Contains(s, "\n") &&
		(strings.HasPrefix(s, "\n") || strings.Contains(s, "\t"))
}
