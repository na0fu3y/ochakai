package okf

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// asStored is the step every write path takes between parsing a document
// and storing it: a claim that says exactly what this instance observed
// about the entry is not a claim at all, but ochakai's own export form
// handed back to it (design doc 0046 §2.2). A parse cannot make that call
// — it has no entry to compare against — so the tests that state a
// round-trip property make it here.
func asStored(k, observed *domain.Knowledge) {
	if IsObservation(k.Attrs[ClaimKey], observed) {
		DropClaim(k)
	}
}

func testEntry() *domain.Knowledge {
	at := time.Date(2026, 7, 28, 4, 12, 0, 0, time.UTC)
	return &domain.Knowledge{
		CreatedBy: domain.Actor{Kind: domain.ActorHuman, Name: "tanaka@example.co.jp"},
		UpdatedBy: domain.Actor{Kind: domain.ActorProcess, Name: "analyst@example.iam.gserviceaccount.com"},
		UpdatedAt: at,
		Verifications: []domain.Verification{
			{By: domain.Actor{Kind: domain.ActorHuman, Name: "tanaka@example.co.jp"}, At: at},
		},
	}
}

// The document a writer sends is the document that is stored: comments,
// key order and quoting style are the writer's, and OKF asks nothing of
// any of them (design doc 0046 §2.2). This is the property the whole
// change exists for, so it is asserted on a document that would look
// nothing like the canonical rendering.
func TestParseKeepsTheDocumentAsWritten(t *testing.T) {
	in := "---\n" +
		"# what this entry is for\n" +
		"title: 'Revenue'   # single quotes, and a trailing comment\n" +
		"type: Metric\n" + // type after title: not the canonical order
		"tags:\n- finance\n- 売上\n" + // sequence at the parent's indentation
		"stale_after: 2026-12-31\n" +
		"question: どうやって売上を数えるか\n" +
		"---\n\n本文。\n"
	d, _, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if d.Doc != in {
		t.Errorf("the stored document is not what was written:\n--- want ---\n%s\n--- got ---\n%s", in, d.Doc)
	}
	// The index derived from it still reads every key.
	if d.Title != "Revenue" || d.Type != domain.TypeMetrics || d.StaleAfter != "2026-12-31" ||
		len(d.Tags) != 2 || d.Attrs["question"] != "どうやって売上を数えるか" {
		t.Errorf("the index does not reproduce the document: %+v", d.Knowledge)
	}
}

// Only the line endings and the trailing newline are touched.
func TestNormalizeText(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"---\r\ntype: Metric\r\n---\r\n", "---\ntype: Metric\n---\n"},
		{"a", "a\n"},
		{"a\n\n\n", "a\n"},
		{"\ufeff---\n", "---\n"},
		{"", ""},
		// A document ending in a bare CR: the trailing carriage return goes
		// with the trailing newlines, or the newline appended after it would
		// make the CRLF this same function says is not stored.
		{"a\r", "a\n"},
		{"a\r\r\n\r", "a\n"},
		// A carriage return run up against a line ending goes with it: what
		// one pass of ReplaceAll leaves behind is another CRLF.
		{"a\r\r\nb\n", "a\nb\n"},
		{"a\rb\r", "a\rb\n"}, // a CR inside a line is the writer's, and stays
	} {
		got := string(NormalizeText([]byte(tc.in)))
		if got != tc.want {
			t.Errorf("NormalizeText(%q) = %q, want %q", tc.in, got, tc.want)
		}
		// And normalizing the normalized changes nothing. A writer PUTting
		// back the bytes a read handed it is doing exactly that, and the
		// answer it is owed is `unchanged` rather than a second revision.
		if again := string(NormalizeText([]byte(got))); again != got {
			t.Errorf("NormalizeText is not idempotent on %q: %q then %q", tc.in, got, again)
		}
	}
}

// Stripping takes out the keys this instance owns — in every block shape
// YAML allows for them — and nothing else. A producer key whose name
// merely starts like an owned one stays: the match is on the key, not on
// a prefix.
func TestStripServerKeysRemovesOnlyTheInstanceKeys(t *testing.T) {
	in := "---\n" +
		"type: Metric\n" +
		"generated:\n    by: process:x\n    at: 2026-07-28T04:12:00Z\n" +
		"verified:\n- by: human:tanaka\n  at: 2026-07-27T00:00:00Z\n" +
		"\n" +
		"created_by: human:tanaka\n" +
		"generated_at_source: yes\n" +
		"title: Revenue\n" +
		"---\n\n本文。\n"
	want := "---\ntype: Metric\ngenerated_at_source: yes\ntitle: Revenue\n---\n\n本文。\n"
	if got := string(StripServerKeys([]byte(in))); got != want {
		t.Errorf("StripServerKeys:\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
	// The v0.1 spellings are owned too (SPEC §13.1).
	old := "---\ntype: Metric\ntimestamp: 2026-01-01T00:00:00Z\nverified_by: human:x\nverified_at: 2026-01-02T00:00:00Z\n---\n"
	if got := string(StripServerKeys([]byte(old))); got != "---\ntype: Metric\n---\n" {
		t.Errorf("v0.1 spellings survived stripping: %q", got)
	}
}

// The export form is the stored document plus this instance's keys, and
// taking them off again gets the stored document back — which is what
// makes get → edit → put a round trip that changes only what the editor
// changed.
func TestServerKeysRoundTrip(t *testing.T) {
	stored := "---\n# a comment\ntitle: 'Revenue'\ntype: Metric\n---\n\n本文。\n"
	out, err := WithServerKeys([]byte(stored), testEntry())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"generated:", "verified:", "created_by: human:tanaka@example.co.jp"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("the export form is missing %q:\n%s", want, out)
		}
	}
	if got := string(StripServerKeys(out)); got != stored {
		t.Errorf("round trip:\n--- want ---\n%s\n--- got ---\n%s", stored, got)
	}
	// And what comes back parses as the same entry it went in as. The
	// parse alone cannot know the keys are ours — it keeps them as a
	// claim (design doc 0046 §2.2) — so the round trip closes where the
	// entry is at hand: the claim is recognized as this instance's own
	// observation, and taking it off leaves the stored bytes.
	d, _, err := Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if !IsObservation(d.Attrs[ClaimKey], testEntry()) {
		t.Fatalf("our own export form was read as a foreign claim: %#v", d.Attrs[ClaimKey])
	}
	DropClaim(&d.Knowledge)
	if d.Doc != stored {
		t.Errorf("re-parsing the export form stored %q, want %q", d.Doc, stored)
	}
	if d.Attrs != nil {
		t.Errorf("dropping the claim left attrs behind: %#v", d.Attrs)
	}
}

// A document from somewhere else is the other half: nothing about it is
// this instance's observation, so what it says about itself is kept —
// under a key that cannot collide with what this instance appends, and
// out of every ledger.
func TestForeignTrustFamilyBecomesAClaim(t *testing.T) {
	in := "---\ntype: Metric\ntitle: Revenue\n" +
		"generated:\n  by: human:sato@example.co.jp\n  at: 2026-07-01T00:00:00Z\n" +
		"verified:\n- by: human:ceo@example.co.jp\n  at: 2026-07-02T00:00:00Z\n" +
		"created_by: human:sato@example.co.jp\n---\n\n本文。\n"
	d, _, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"generated", "verified", "created_by"}; !slices.Equal(d.Claimed, want) {
		t.Errorf("Claimed = %v, want %v", d.Claimed, want)
	}
	// Nothing about the entry itself moved: the ledger stays empty and
	// the claim is an ordinary key of the stored document.
	if len(d.Verifications) != 0 || d.CreatedBy != (domain.Actor{}) {
		t.Errorf("a document's claim reached a ledger: %+v", d.Knowledge)
	}
	claim, ok := d.Attrs[ClaimKey].(map[string]any)
	if !ok {
		t.Fatalf("no claim was kept: %#v", d.Attrs)
	}
	if got := claim["created_by"]; got != "human:sato@example.co.jp" {
		t.Errorf("claim.created_by = %#v", got)
	}
	if !strings.Contains(d.Doc, "received:") || strings.Contains(d.Doc, "\ncreated_by:") {
		t.Errorf("the stored document did not move the trust family:\n%s", d.Doc)
	}
	// It is not this instance's observation, whatever this instance holds.
	if IsObservation(d.Attrs[ClaimKey], testEntry()) {
		t.Error("a foreign claim was mistaken for our own observation")
	}
	// And the export form carries both without colliding: one `verified`
	// for the ledger, one under the claim.
	out, err := WithServerKeys([]byte(d.Doc), testEntry())
	if err != nil {
		t.Fatal(err)
	}
	back, _, err := Parse(out)
	if err != nil {
		t.Fatalf("the export form of an entry with a claim no longer parses: %v\n%s", err, out)
	}
	if back.Doc != d.Doc {
		t.Errorf("a claim did not survive the export form:\n--- want ---\n%s\n--- got ---\n%s", d.Doc, back.Doc)
	}
	if len(back.Claimed) != 0 {
		t.Errorf("a recorded claim was overwritten by the instance's own keys: %v", back.Claimed)
	}
}

// A claim is what the document asserted, down to how it spelled its
// timestamps: YAML types an unquoted date-time, and rendering that back
// used to hand a midnight instant out as a bare date and the
// space-separated form out as the T form (design doc 0075 §3.1).
func TestAClaimKeepsTheTimestampsAsWritten(t *testing.T) {
	in := "---\ntype: Metric\ntitle: Revenue\n" +
		"generated:\n  by: human:sato@example.co.jp\n  at: 2026-07-01T00:00:00Z\n" +
		"verified:\n- by: human:ceo@example.co.jp\n  at: 2026-07-02 09:30:00\n" +
		"- by: human:cfo@example.co.jp\n  at: 2026-07-03\n---\n\n本文。\n"
	d, _, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	claim, ok := d.Attrs[ClaimKey].(map[string]any)
	if !ok {
		t.Fatalf("no claim was kept: %#v", d.Attrs)
	}
	generated, _ := claim["generated"].(map[string]any)
	if got := generated["at"]; got != "2026-07-01T00:00:00Z" {
		t.Errorf("claim.generated.at = %#v, want the instant the document wrote", got)
	}
	verified, _ := claim["verified"].([]any)
	if len(verified) != 2 {
		t.Fatalf("claim.verified = %#v", claim["verified"])
	}
	for i, want := range []string{"2026-07-02 09:30:00", "2026-07-03"} {
		e, _ := verified[i].(map[string]any)
		if got := e["at"]; got != want {
			t.Errorf("claim.verified[%d].at = %#v, want %q", i, got, want)
		}
	}
	// And the stored document says the same thing as the map beside it,
	// through as many round trips as the bundle is carried.
	back, _, err := Parse([]byte(d.Doc))
	if err != nil {
		t.Fatal(err)
	}
	if back.Doc != d.Doc {
		t.Errorf("a claim did not survive re-import:\n--- want ---\n%s\n--- got ---\n%s", d.Doc, back.Doc)
	}
}

// A move rewrites a referring entry's body. Its frontmatter is not what
// changed, and must come out byte for byte (design doc 0046 §2.2).
func TestReplaceBodyLeavesFrontmatterAlone(t *testing.T) {
	in := "---\n# a comment\ntitle: 'Revenue'\ntype: Metric\n---\n\nold body\n"
	got := string(ReplaceBody([]byte(in), "new [body](/metrics/net.md)"))
	want := "---\n# a comment\ntitle: 'Revenue'\ntype: Metric\n---\n\nnew [body](/metrics/net.md)\n"
	if got != want {
		t.Errorf("ReplaceBody:\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
	if got := string(ReplaceBody([]byte(in), "")); got != "---\n# a comment\ntitle: 'Revenue'\ntype: Metric\n---\n" {
		t.Errorf("an emptied body left something behind: %q", got)
	}
}

// A frontmatter mapping may be indented — unusual YAML, and valid YAML.
// Then column 0 is *outside* it, and the keys this instance appends have
// to be written at the mapping's own column or they end it instead of
// extending it. That produced an export form that no longer parsed, and
// whose keys StripServerKeys could then no longer find; on the way in it
// stored a document that no longer parsed and dropped the claim it had
// just taken out of it.
func TestAnIndentedFrontmatterIsAppendedAtItsOwnColumn(t *testing.T) {
	in := "---\n  type: Metric\n  generated: {by: human:x}\n---\n\n本文。\n"
	d, _, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	// The claim came out of the document and went back under ClaimKey, at
	// the mapping's indentation, leaving a document that still parses.
	if d.Attrs[ClaimKey] == nil {
		t.Fatalf("the claim was dropped: %q", d.Doc)
	}
	if !strings.Contains(d.Doc, "\n  received:\n") {
		t.Errorf("the claim was not written at the mapping's column: %q", d.Doc)
	}
	back, _, err := Parse([]byte(d.Doc))
	if err != nil {
		t.Fatalf("the stored document no longer parses: %v\n%s", err, d.Doc)
	}
	if back.Doc != d.Doc {
		t.Errorf("the stored document did not survive re-import:\n--- want ---\n%s\n--- got ---\n%s", d.Doc, back.Doc)
	}
	// And so does the export form, which is where the failure was found.
	out, err := WithServerKeys([]byte(d.Doc), testEntry())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Parse(out); err != nil {
		t.Fatalf("the export form no longer parses: %v\n%s", err, out)
	}
	if got := string(StripServerKeys(out)); got != d.Doc {
		t.Errorf("round trip lost bytes:\n--- want ---\n%s\n--- got ---\n%s", d.Doc, got)
	}
}

// The line surgery cuts a frontmatter into lines and takes keys out of
// it by line. Three shapes cannot be read that way, and it declines all
// three rather than damage them — each of them used to lose a key the
// writer owned, which is worse than losing one of ours. What they get
// instead is what the bundle promises anything it holds: the bytes come
// back out (design doc 0075 §1), with no surgery in either direction.
func TestTheSurgeryDeclinesAFrontmatterItCannotAddress(t *testing.T) {
	for _, tc := range []struct{ why, in string }{
		// A flow mapping's keys do not own their lines: one line holds two
		// of them, so removing "this key's lines" took the neighbour.
		{"flow mapping", "---\n{type: Metric, generated: x}\n---\n"},
		{"flow mapping over two lines", "---\n{type: Metric,\n generated: x}\n---\n"},
		// The parser counts a bare carriage return as a line break and this
		// file counts only "\n", so the key line numbers and the lines being
		// cut were out of step: a different line was removed than the one
		// the key was read on.
		{"a line break only the parser counts", "---\n\rgenerated: x\ntype: Metric\n---\n"},
		{"a Unicode line separator", "---\ntype: Metric\u2028title: x\ngenerated: y\n---\n"},
		// A document marker ends the mapping before the block ends, so a
		// key appended after it joins nothing — and a removal the export
		// cannot undo is what leaves a stored document holding one of our
		// keys.
		{"an end-of-document marker", "---\ntype: Metric\ngenerated: x\ntitle: t\n...\n---\n"},
		{"a start-of-document marker", "---\ntype: Metric\n--- \ntitle: t\n---\n"},
	} {
		if got := string(StripServerKeys([]byte(tc.in))); got != tc.in {
			t.Errorf("%s: the strip cut lines it cannot address:\n--- want ---\n%q\n--- got ---\n%q", tc.why, tc.in, got)
		}
		d, _, err := Parse([]byte(tc.in))
		if err != nil {
			t.Fatalf("%s: Parse(%q): %v", tc.why, tc.in, err)
		}
		if d.Type != domain.TypeMetrics {
			t.Errorf("%s: the writer's own key was lost: %+v", tc.why, d.Knowledge)
		}
		if d.Doc != tc.in {
			t.Errorf("%s: the document was rewritten:\n--- want ---\n%q\n--- got ---\n%q", tc.why, tc.in, d.Doc)
		}
		out, err := WithServerKeys([]byte(d.Doc), testEntry())
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != tc.in {
			t.Errorf("%s: keys were appended where they cannot be taken off again:\n%q", tc.why, out)
		}
		if got := string(StripServerKeys(out)); got != tc.in {
			t.Errorf("%s: round trip lost bytes:\n--- want ---\n%q\n--- got ---\n%q", tc.why, tc.in, got)
		}
	}
}

// The property the two operations owe each other, over whatever the
// fuzzer can produce: appending this instance's keys and taking them off
// again is the identity on any document ochakai stores (design doc 0035 —
// invariants the type system cannot carry are checked from outside).
func FuzzServerKeyRoundTrip(f *testing.F) {
	f.Add("---\ntype: Metric\n---\n")
	f.Add("---\ntype: Metric\ntags:\n- a\n---\n\nbody\n")
	f.Add("---\n# comment\ntype: Metric\n\ntitle: x\n---\n")
	f.Add("---\ntype: Metric\ngenerated: {by: x}\n---\n")
	f.Add("---\ntype: Metric\ngenerated: {by: x}\nverified: [{by: human:y, at: 2026-07-01T00:00:00Z}]\n---\n")
	f.Add("---\ntype: Metric\nreceived:\n  verified:\n  - by: human:x\n---\n") // a claim already recorded
	f.Add("---\ntype: Metric\nreceived: not a mapping\ncreated_by: human:x\n---\n")
	f.Add("---\n---\nbody only\n")
	f.Add("not a document at all\n")
	// A mapping indented by one space: column 0 is outside it, so keys
	// appended there end the frontmatter rather than extend it.
	f.Add("---\n type: 0\n---")
	f.Add("---\n  type: Metric\n  generated: {by: x}\n---\n")
	// Flow mappings, whose keys do not own their lines at all.
	f.Add("---\n{type: Metric, generated: x}\n---\n")
	f.Add("---\n{type: Metric,\n generated: x}\n---\n")
	f.Add("---\ntype: Metric\ngenerated: [1,\n 2]\n---\n")         // a flow *value* stays addressable
	f.Add("---\ntype: Metric\n...\n---\n")                         // frontmatter that ends before its delimiter
	f.Add("---\ntype: Metric\ngenerated: x\ntitle: t\n...\n---\n") // and one with a key to take out first
	f.Add("---\ntype: Metric\n--- \ntitle: t\n---\n")
	f.Add("---\ntype: Metric\n---\nbody\r") // a bare CR at the end (NormalizeText)
	// Line breaks the parser counts and "\n" splitting does not, which put
	// the key line numbers and the cut lines out of step.
	f.Add("---\n\rgenerated: x\ntype: 0\n---\n")
	f.Add("---\n\rtype:\r 0\n---")
	f.Add("---\ntype: Metric\u2028title: x\ngenerated: y\n---\n")

	f.Fuzz(func(t *testing.T, raw string) {
		// Normalizing is idempotent, so a writer sending back the bytes a
		// read handed it is sending something already stored — which is the
		// difference between `unchanged` and a second revision.
		normalized := NormalizeText([]byte(raw))
		if again := NormalizeText(normalized); !slices.Equal(again, normalized) {
			t.Fatalf("NormalizeText is not idempotent on %q: %q then %q", raw, normalized, again)
		}
		// Only documents ochakai would have stored: the write path parses
		// before it stores, so a text Parse refuses is not one this
		// property is about.
		d, _, err := Parse(normalized)
		if err != nil {
			t.Skip()
		}
		stored := d.Doc
		out, err := WithServerKeys([]byte(stored), testEntry())
		if err != nil {
			t.Fatalf("WithServerKeys(%q): %v", stored, err)
		}
		if got := string(StripServerKeys(out)); got != stored {
			t.Errorf("round trip lost bytes:\n--- stored ---\n%q\n--- back ---\n%q", stored, got)
		}
		// And the export form is still a document that reads the same —
		// once the entry it belongs to says the claim in it is its own
		// observation, which is what a write path has and a parse does not.
		back, _, err := Parse(out)
		if err != nil {
			t.Fatalf("the export form no longer parses: %v\n%s", err, out)
		}
		if asStored(&back.Knowledge, testEntry()); back.Doc != stored {
			t.Errorf("re-parsing the export form stored %q, want %q", back.Doc, stored)
		}
	})
}
