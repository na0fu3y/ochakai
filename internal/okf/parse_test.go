package okf

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
)

func TestParseRoundTrip(t *testing.T) {
	entries := sample()
	for i := range entries {
		want := entries[i]
		doc, err := Document(&want)
		if err != nil {
			t.Fatal(err)
		}
		got, _, err := Parse(doc)
		if err != nil {
			t.Fatalf("Parse(%s): %v", want.URI(), err)
		}
		asStored(&got.Knowledge, &want)
		// The id is the entry's path, not part of the document (design doc
		// 0016): the exported frontmatter carries no id key, and the parsed
		// document leaves ID for the caller's address to fill in.
		if got.ID != "" {
			t.Errorf("Parse invented an id: %q", got.ID)
		}
		if got.Type != want.Type || got.Title != want.Title ||
			got.Description != want.Description || got.Status != want.Status {
			t.Errorf("envelope mismatch: got %+v, want %+v", got, want)
		}
		if !reflect.DeepEqual(got.Tags, want.Tags) {
			t.Errorf("tags = %v, want %v", got.Tags, want.Tags)
		}
		// Parse reads no links: they are derived from the body when the
		// entry is written (design doc 0024).
		if got.Links != nil {
			t.Errorf("Parse invented links: %v", got.Links)
		}
		if got.Resource != want.Resource {
			t.Errorf("resource = %q, want %q", got.Resource, want.Resource)
		}
		if !reflect.DeepEqual(got.Attrs, want.Attrs) {
			t.Errorf("attrs = %v, want %v", got.Attrs, want.Attrs)
		}
		if wantBody := "12月は+40%が通常。[売上](/metrics/revenue.md) の話である。"; want.Body != "" && got.Body != wantBody {
			t.Errorf("body = %q, want %q", got.Body, wantBody)
		}
	}
}

func TestParseAcceptsRawTypeAndHandWrittenDoc(t *testing.T) {
	k, _, err := Parse([]byte(`---
type: Attested Computation
title: 月次売上
status: draft
runtime: bigquery
question: 月ごとの売上は？
sql: SELECT 1
---

Body text here.
`))
	if err != nil {
		t.Fatal(err)
	}
	if k.Type != domain.TypeComputations {
		t.Errorf("got %s", k.Type)
	}
	if k.Attrs["sql"] != "SELECT 1" {
		t.Errorf("attrs = %v", k.Attrs)
	}
	if k.Body != "Body text here." {
		t.Errorf("body = %q", k.Body)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	for _, doc := range []string{
		"just markdown, no frontmatter",
		"---\ntype: metric\n",                   // unterminated
		"---\ntitle: x\n---\n",                  // no type at all
		"---\ntype: \ntitle: x\n---\n",          // empty
		"---\ntype: [a]\ntitle: x\n---\n",       // type is not a string
		"---\ntype: \"a\\nb\"\ntitle: x\n---\n", // more than one line
	} {
		if _, _, err := Parse([]byte(doc)); err == nil {
			t.Errorf("Parse(%q) succeeded, want error", doc)
		}
	}
}

// Free types are first-class (design doc 0005) and are stored verbatim
// (design doc 0023): a multi-word spelling is neither slugified nor
// stashed in a preservation attr, so the document round-trips because
// nothing was changed in the first place.
func TestParseFreeTypes(t *testing.T) {
	k, _, err := Parse([]byte("---\ntype: runbook\ntitle: リストア手順\n---\n"))
	if err != nil {
		t.Fatal(err)
	}
	if k.Type != "runbook" || len(k.Attrs) != 0 {
		t.Errorf("one-word type: got type=%q attrs=%v", k.Type, k.Attrs)
	}

	k, _, err = Parse([]byte("---\ntype: Data Contract\ntitle: 注文契約\n---\n"))
	if err != nil {
		t.Fatal(err)
	}
	if k.Type != "Data Contract" || len(k.Attrs) != 0 {
		t.Errorf("spelled type: got type=%q attrs=%v", k.Type, k.Attrs)
	}

	// A namespaced type is legal OKF — SPEC §4.1 registers no taxonomy and
	// requires a consumer to tolerate unknown types, and §11 forbids
	// rejecting a bundle over one. ochakai used to answer 400 and, on
	// import, demote the document to a loose bundle file (design doc 0064).
	ns, _, err := Parse([]byte("---\ntype: acme/Table\n---\n"))
	if err != nil {
		t.Fatal(err)
	}
	if ns.Type != "acme/Table" {
		t.Errorf("namespaced type: got %q, want acme/Table", ns.Type)
	}

	doc, err := Document(&k.Knowledge)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(doc), "type: Data Contract") {
		t.Errorf("original spelling lost on export:\n%s", doc)
	}
	if strings.Contains(string(doc), "okf_type") {
		t.Errorf("no preservation attr should exist any more:\n%s", doc)
	}
}

// A top-level `id` is one of those producer keys and nothing more. SPEC
// v0.2 defines no such field — a concept's id is its path (§2, design doc
// 0075), and `sources[].id` is a different key — so the envelope does not
// read one: it stays in attrs, exports back where its writer put it, and
// never decides where the concept lives.
//
// It used to be an envelope key that filled Doc.ID. The bundle importer
// overwrote it from the path and REST overwrote it from the address, so
// the one door it reached was `ochakai put` with no argument; everywhere
// else it was a producer's key quietly taken away.
func TestParseDoesNotReadAnIDFromFrontmatter(t *testing.T) {
	k, _, err := Parse([]byte("---\ntype: Metric\nid: acme-42\ntitle: 売上\n---\n\nBody.\n"))
	if err != nil {
		t.Fatal(err)
	}
	if k.ID != "" {
		t.Errorf("Parse read an id out of frontmatter: %q", k.ID)
	}
	if k.Attrs["id"] != "acme-42" {
		t.Errorf("a producer's id key was not kept: %v", k.Attrs)
	}
	doc, err := Document(&k.Knowledge)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(doc), "id: acme-42") {
		t.Errorf("re-export lost the producer's id key:\n%s", doc)
	}
}

// Unknown top-level frontmatter keys are producer-defined extensions
// (SPEC §4.1): they land in attrs as-is and export back to the top level,
// so a foreign bundle's resource/owner keys survive a round-trip in place.
func TestParseKeepsUnknownKeys(t *testing.T) {
	k, _, err := Parse([]byte(`---
type: Reference
title: Average Pageviews
resource: https://developers.google.com/analytics/bigquery/basic-queries
owner: analytics-team
timestamp: '2026-05-28T22:51:43+00:00'
created_by: 'agent:someone'
---

Body.
`))
	if err != nil {
		t.Fatal(err)
	}
	if k.Resource != "https://developers.google.com/analytics/bigquery/basic-queries" {
		t.Errorf("resource not extracted: %q", k.Resource)
	}
	if k.Attrs["owner"] != "analytics-team" {
		t.Errorf("unknown keys not kept: %v", k.Attrs)
	}
	for _, serverOwned := range []string{"timestamp", "created_by"} {
		if _, ok := k.Attrs[serverOwned]; ok {
			t.Errorf("server-owned key %s leaked into attrs: %v", serverOwned, k.Attrs)
		}
	}
	if k.CreatedBy.Name != "" {
		t.Errorf("created_by must come from authentication, not the payload: %v", k.CreatedBy)
	}

	doc, err := Document(&k.Knowledge)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"resource: https://developers.google.com/analytics/bigquery/basic-queries",
		"owner: analytics-team",
	} {
		if !strings.Contains(string(doc), want) {
			t.Errorf("re-export missing %q:\n%s", want, doc)
		}
	}
	if strings.Contains(string(doc), "attrs:") {
		t.Errorf("re-export must not nest attrs:\n%s", doc)
	}
}

func TestParseStatusNoteRoundTrip(t *testing.T) {
	k, _, err := Parse([]byte("---\ntype: insight\ntitle: 重複\nstatus: deprecated\nstatus_note: 既存と重複\n---\n"))
	if err != nil {
		t.Fatal(err)
	}
	if k.StatusNote != "既存と重複" {
		t.Errorf("status_note = %q", k.StatusNote)
	}
	if _, ok := k.Attrs["status_note"]; ok {
		t.Errorf("status_note is an envelope key, not an attr: %v", k.Attrs)
	}
}

func TestParseNormalizesCRLF(t *testing.T) {
	k, _, err := Parse([]byte("---\r\ntype: Glossary Term\r\ntitle: 解約\r\n---\r\n\r\n本文。\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if k.Type != domain.TypeTerms || k.Title != "解約" || k.Body != "本文。" {
		t.Errorf("CRLF document mangled: %+v", k)
	}
}

func TestParseKeepsLinksSectionAsBody(t *testing.T) {
	// A "# Links" section used to be folded out of the body into
	// structured links. It is ordinary prose now (design doc 0024) — the
	// links inside it are found by the same extraction as any other.
	body := "Intro.\n\n# Links\n\nSee [the dashboard](https://example.com) for details."
	got, _, err := Parse([]byte("---\ntype: Insight\n---\n\n" + body + "\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Body != body {
		t.Errorf("body = %q, want %q", got.Body, body)
	}
	if got.Links != nil {
		t.Errorf("Parse invented links: %v", got.Links)
	}
}

// The knowledge-catalog reference bundles sometimes write tags as one
// comma-separated string; permissive consumption (SPEC §9) accepts it.
func TestParseScalarTags(t *testing.T) {
	k, _, err := Parse([]byte("---\ntype: Dataset\ntitle: SO\ntags: Stack Overflow, public data, community, Q&A\n---\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Stack Overflow", "public data", "community", "Q&A"}
	if !reflect.DeepEqual(k.Tags, want) {
		t.Errorf("tags = %v, want %v", k.Tags, want)
	}
}

// The v0.2 lifecycle vocabulary is three values where ochakai has four,
// and "a human confirmed this" is the verified key, not a status
// (SPEC §5.3-5.4, design doc 0036 §3.4).
func TestParseStatusMapping(t *testing.T) {
	const verified = "verified:\n  - { by: human:na0, at: 2026-07-01T00:00:00Z }\n"
	for _, tc := range []struct {
		name, frontmatter string
		want              domain.Status
		wantVerified      bool
		wantNote          bool
	}{
		{"stable with a verification", "status: stable\n" + verified, domain.StatusStable, true, false},
		// The row that used to be reported. ochakai had no state for
		// "ready for consumption, unconfirmed", so this landed as a draft
		// — a lifecycle demotion the producer never wrote, which 0036
		// §3.4 required a note about. The ledger holds the state now, so
		// the value arrives unaltered and there is nothing to report
		// (design doc 0043 §3.2).
		{"stable alone keeps its lifecycle value", "status: stable\n", domain.StatusStable, false, false},
		{"a bare verified mapping counts too (SPEC §5.2)", "status: stable\nverified: { by: human:na0, at: 2026-07-01T00:00:00Z }\n", domain.StatusStable, true, false},
		{"draft stays draft", "status: draft\n", domain.StatusDraft, false, false},
		{"a verified draft is both", "status: draft\n" + verified, domain.StatusDraft, true, false},
		{"deprecated stays deprecated", "status: deprecated\n", domain.StatusDeprecated, false, false},
		{"an absent status stays unset for the write path to default", "", "", false, false},
		{"an absent status with a verification stays unset too", verified, "", true, false},
		// The values ochakai wrote before 0.16 are unrecognized: OKF's
		// vocabulary is the three above. A 0.12 bundle's verified entries
		// still import as verified, but through the verified key SPEC
		// §5.3 makes authoritative, not through an ochakai-only alias.
		{"ochakai 0.12 wrote status: verified", "status: verified\n" + verified, domain.StatusDraft, true, true},
		{"a legacy status with no verification is an unverified draft", "status: verified\n", domain.StatusDraft, false, true},
		{"ochakai 0.12 wrote status: rejected", "status: rejected\n", domain.StatusDraft, false, true},
		{"an unknown value is a draft, not a rejected document", "status: retired\n", domain.StatusDraft, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, notes, err := Parse([]byte("---\ntype: Insight\n" + tc.frontmatter + "---\n"))
			if err != nil {
				t.Fatal(err)
			}
			if d.Status != tc.want {
				t.Errorf("status = %q, want %q", d.Status, tc.want)
			}
			if d.Verified != tc.wantVerified {
				t.Errorf("verified = %v, want %v", d.Verified, tc.wantVerified)
			}
			if got := len(notes) > 0; got != tc.wantNote {
				t.Errorf("notes = %v, want a note: %v", notes, tc.wantNote)
			}
			// The trust family is read for its presence only; the actors
			// and times stay outside the payload (design doc 0009).
			if len(d.Verifications) != 0 {
				t.Errorf("a bundle must not set rulings: %v", d.Verifications)
			}
			for _, key := range []string{"verified", "generated", "status"} {
				if _, ok := d.Attrs[key]; ok {
					t.Errorf("%s leaked into attrs: %v", key, d.Attrs)
				}
			}
		})
	}
}

// A v0.1 document keeps importing: the renamed keys are read as the
// document's claim rather than landing in attrs at top level, where a
// re-export would write them beside their v0.2 replacements (SPEC §13.1).
// Kept, though — a retired spelling is still what the producer wrote, and
// dropping it would be the one loss no later release can undo.
func TestParseV01Document(t *testing.T) {
	k, _, err := Parse([]byte(`---
type: Insight
title: 売上の季節性
status: verified
timestamp: '2026-05-28T22:51:43+00:00'
created_by: 'process:claude-code'
verified_by: 'human:na0'
verified_at: '2026-06-01T00:00:00Z'
---

Body.
`))
	if err != nil {
		t.Fatal(err)
	}
	// "verified" was never an OKF lifecycle value, so it reads as a draft
	// — but verified_at is v0.1's spelling of the trust key, and SPEC
	// §5.3 keeps the two signals independent, so the document still
	// arrives confirmed.
	if k.Status != domain.StatusDraft || !k.Verified {
		t.Errorf("status = %q verified = %v", k.Status, k.Verified)
	}
	claim, _ := k.Attrs[ClaimKey].(map[string]any)
	if len(k.Attrs) != 1 || len(claim) != 4 {
		t.Errorf("v0.1 trust keys must be a claim and nothing else: %v", k.Attrs)
	}
	doc, err := Document(&k.Knowledge)
	if err != nil {
		t.Fatal(err)
	}
	// Not at top level, where they would contradict the v0.2 keys this
	// instance appends: a claim is nested, so the two never compete.
	for _, gone := range []string{"\ntimestamp:", "\nverified_by:", "\nverified_at:"} {
		if strings.Contains(string(doc), gone) {
			t.Errorf("re-export wrote the v0.1 key %q at top level:\n%s", gone, doc)
		}
	}
	if !strings.Contains(string(doc), "verified_at: ") {
		t.Errorf("the v0.1 claim was dropped instead of kept:\n%s", doc)
	}
}

func TestParseStaleAfter(t *testing.T) {
	k, notes, err := Parse([]byte("---\ntype: Insight\nstale_after: 2026-12-31\n---\n"))
	if err != nil {
		t.Fatal(err)
	}
	if k.StaleAfter != "2026-12-31" || len(notes) != 0 {
		t.Errorf("stale_after = %q, notes = %v", k.StaleAfter, notes)
	}
	if _, ok := k.Attrs["stale_after"]; ok {
		t.Errorf("stale_after is an envelope key, not an attr: %v", k.Attrs)
	}
	// The export is the document as written, so the writer's own bare
	// date comes back (design doc 0046 §2.2) — while the canonical
	// rendering, which composes a date rather than passing one through,
	// quotes it so YAML does not type it as a timestamp on the way back
	// in (design doc 0036 §3.7).
	doc, err := Document(&k.Knowledge)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(doc), "stale_after: 2026-12-31\n") {
		t.Errorf("stale_after lost on export:\n%s", doc)
	}
	canon, err := Canonical(&k.Knowledge)
	if err != nil {
		t.Fatal(err)
	}
	// The canonical form is ochakai composing rather than keeping, so the
	// moment is spelled the way SPEC §5 defines one — while Document
	// above kept the writer's bare date, its bytes being the writer's
	// (design doc 0139 §3.2, §3.4).
	if !strings.Contains(string(canon), "stale_after: \"2026-12-31T00:00:00Z\"") {
		t.Errorf("stale_after not spelled as an instant in the canonical form:\n%s", canon)
	}

	// A malformed date is dropped with a note — the document still imports
	// (SPEC §11).
	k, notes, err = Parse([]byte("---\ntype: Insight\nstale_after: soon\n---\n"))
	if err != nil {
		t.Fatal(err)
	}
	if k.StaleAfter != "" || len(notes) != 1 {
		t.Errorf("stale_after = %q, notes = %v", k.StaleAfter, notes)
	}
}

// The moment-valued keys of SPEC §5 read as moments, which is the
// spelling the spec's own worked example uses. This is what used to be
// dropped: a `date` was the only form ochakai took, so Appendix A's
// concept arrived with three of its keys silently emptied and a note
// nobody reads on an import of hundreds (design doc 0133).
//
// Verbatim from the spec, trimmed to the keys under test — if OKF v0.2's
// example stops parsing here, ochakai has stopped consuming OKF v0.2.
func TestParseTakesTheSpecsOwnInstants(t *testing.T) {
	k, notes, err := Parse([]byte(`---
type: Attested Computation
title: Revenue for fiscal year
runtime: bigquery
stale_after: 2026-12-31T18:30:00Z
sources:
  - id: rev-policy
    resource: https://wiki.acme/finance/revenue-recognition
    last_modified: 2026-04-02T09:15:00Z
usage_window: { from: 2026-06-01T00:00:00Z, to: 2026-06-30T23:59:59Z }
---

# Computation

    SELECT 1
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 0 {
		t.Errorf("the spec's own example was read with complaints: %v", notes)
	}
	if k.StaleAfter != "2026-12-31T18:30:00Z" {
		t.Errorf("stale_after = %q, want the instant the document declared", k.StaleAfter)
	}
	if len(k.Sources) != 1 || k.Sources[0].LastModified != "2026-04-02T09:15:00Z" {
		t.Errorf("sources = %+v, want last_modified kept", k.Sources)
	}
	if k.UsageWindow == nil || k.UsageWindow.To != "2026-06-30T23:59:59Z" {
		t.Errorf("usage_window = %+v, want both bounds kept", k.UsageWindow)
	}
	// Parsing reads the text the producer wrote, unquoted timestamps
	// included: YAML resolves one to a time.Time, which no longer says
	// which spelling it arrived in, so the node is retagged a string
	// before it is decoded (frontmatterMap). Both spellings survive the
	// read as written; choosing one is the store's job, on the way back
	// out of the column that holds stale_after as an instant.
	for _, written := range []string{"2026-12-31T00:00:00Z", "2026-12-31"} {
		k, _, err = Parse([]byte("---\ntype: Insight\nstale_after: " + written + "\n---\n"))
		if err != nil {
			t.Fatal(err)
		}
		if k.StaleAfter != written {
			t.Errorf("stale_after written %q read as %q", written, k.StaleAfter)
		}
	}
	// An offset is what makes a datetime absolute; without one it is not
	// a moment and is dropped the way "soon" is.
	_, notes, err = Parse([]byte("---\ntype: Insight\nstale_after: \"2026-12-31T18:30:00\"\n---\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 {
		t.Errorf("a datetime with no offset was taken as absolute: %v", notes)
	}
}

// Every key OKF v0.2 defines is an envelope field, not an attr: the spec's
// schema is the shape ochakai keeps (design doc 0036 §2). Only producer
// extension keys ride through attrs.
func TestParseReadsOKFFamiliesIntoTheEnvelope(t *testing.T) {
	doc := []byte(`---
type: Attested Computation
title: Revenue for fiscal year
tags: [finance]
status: stable
runtime: bigquery
parameters:
  - { name: year, type: integer, required: true }
executor:
  resource: references/skills/run-on-bq.md
  receipt: [job_id, executed_sql, result]
attester:
  resource: references/attesters/revenue.py
computation: queries/revenue.sql
sources:
  - id: rev-policy
    resource: https://wiki.example/finance/revenue-recognition
    title: Revenue recognition policy
    author: human:jsmith@acme
    usage_count: 0
    last_modified: 2026-06-15
    usage_window: { from: 2026-05-01, to: 2026-05-31 }
usage_window: { from: 2026-06-01, to: 2026-06-30 }
dialect: standard-sql
---

# Computation

    SELECT SUM(amount) FROM finance.recognized_revenue WHERE fiscal_year = @year
`)
	k, notes, err := Parse(doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 0 {
		t.Errorf("nothing here needs reinterpreting: %v", notes)
	}
	if k.Type != domain.TypeComputations {
		t.Errorf("type = %q, want %q", k.Type, domain.TypeComputations)
	}
	if k.Runtime != "bigquery" || k.Computation != "queries/revenue.sql" {
		t.Errorf("runtime = %q, computation = %q", k.Runtime, k.Computation)
	}
	want := []domain.Parameter{{Name: "year", Type: "integer", Required: true}}
	if !reflect.DeepEqual(k.Parameters, want) {
		t.Errorf("parameters = %+v, want %+v", k.Parameters, want)
	}
	if k.Executor == nil || k.Executor.Resource != "references/skills/run-on-bq.md" ||
		!reflect.DeepEqual(k.Executor.Receipt, []string{"job_id", "executed_sql", "result"}) {
		t.Errorf("executor = %+v", k.Executor)
	}
	if k.Attester == nil || k.Attester.Resource != "references/attesters/revenue.py" {
		t.Errorf("attester = %+v", k.Attester)
	}
	if len(k.Sources) != 1 {
		t.Fatalf("sources = %+v", k.Sources)
	}
	s := k.Sources[0]
	// A date the producer wrote without a clock stays a date: YAML types
	// it as a timestamp, and letting that through would hand back a day
	// nobody wrote (design doc 0036 §3.7).
	if s.LastModified != "2026-06-15" {
		t.Errorf("last_modified = %q, want the bare date", s.LastModified)
	}
	// Zero uses is a count, not the absence of one (design doc 0036 §3.1).
	if s.UsageCount == nil || *s.UsageCount != 0 {
		t.Errorf("usage_count = %v, want a pointer to 0", s.UsageCount)
	}
	if s.UsageWindow == nil || s.UsageWindow.From != "2026-05-01" {
		t.Errorf("per-source usage_window = %+v", s.UsageWindow)
	}
	if k.UsageWindow == nil || k.UsageWindow.To != "2026-06-30" {
		t.Errorf("usage_window = %+v", k.UsageWindow)
	}
	// Only the producer's own key is left in attrs.
	if !reflect.DeepEqual(k.Attrs, map[string]any{"dialect": "standard-sql"}) {
		t.Errorf("attrs = %v, want only the producer extension key", k.Attrs)
	}

	// The canonical form is what ochakai writes when it composes a
	// document itself; it is also what the index has to be able to
	// reproduce, so the spellings and the key order are asserted on it
	// rather than on the export, which now hands back the writer's bytes
	// (design doc 0046 §2.2).
	out, err := Canonical(&k.Knowledge)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"runtime: bigquery", "attester:", "sources:", "usage_window:", "# Computation",
		"usage_count: 0", `last_modified: "2026-06-15"`, "dialect: standard-sql",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("re-export missing %q:\n%s", want, out)
		}
	}
	// The frontmatter follows the spec's own family order (design doc
	// 0036 §3.6), so a document reads the way SPEC introduces it.
	var last int
	for _, key := range []string{"\ntype:", "\ntags:", "\nsources:", "\nusage_window:", "\nstatus:", "\nruntime:", "\nparameters:", "\nexecutor:", "\nattester:"} {
		at := strings.Index("\n"+string(out), key)
		if at < 0 {
			t.Fatalf("key %q missing from the export:\n%s", key, out)
		}
		if at < last {
			t.Errorf("key %q is out of the spec's family order:\n%s", key, out)
		}
		last = at
	}
}

// A malformed value inside an OKF family costs the value and a note, never
// the document: SPEC's conformance rule tells consumers to take the
// concept (design doc 0036 §3.4).
func TestParseReportsUnusableOKFValues(t *testing.T) {
	k, notes, err := Parse([]byte(`---
type: Insight
sources:
  - resource: policies/revenue.md
    last_modified: soon
    license: CC-BY
  - id: nameless
  - resource: ok/other.md
    usage_count: many
usage_window: [2026-06-01, 2026-06-30]
parameters:
  - { type: integer }
executor: { receipt: [job_id] }
attester: {}
---
`))
	if err != nil {
		t.Fatal(err)
	}
	// The two sources that name a resource survive; the one that names
	// nothing does not (SPEC §5.1 requires resource within an entry).
	if len(k.Sources) != 2 {
		t.Errorf("sources = %+v, want the two that name a resource", k.Sources)
	}
	if len(k.Sources) > 0 && k.Sources[0].LastModified != "" {
		t.Errorf("an unparseable last_modified must be dropped, got %q", k.Sources[0].LastModified)
	}
	// An executor naming nothing to run says nothing, so it is dropped.
	// A receipt is not what makes it a contract: SPEC §10.2 marks only
	// runtime REQUIRED and §11 forbids rejecting a concept over a missing
	// optional field, so `executor: { resource: run.md }` — the case that
	// used to be dropped here — now survives (design doc 0079,
	// TestExecutorWithoutReceiptIsKept).
	if k.UsageWindow != nil || k.Executor != nil || k.Attester != nil || len(k.Parameters) != 0 {
		t.Errorf("unusable values must be dropped: %+v %+v %+v %+v",
			k.UsageWindow, k.Executor, k.Attester, k.Parameters)
	}
	for _, want := range []string{
		"last_modified", "names no resource",
		"usage_count", "usage_window", "parameters[0]", "executor", "attester",
	} {
		if !slices.ContainsFunc(notes, func(n string) bool { return strings.Contains(n, want) }) {
			t.Errorf("no note mentions %q: %v", want, notes)
		}
	}
	// The producer key is kept, and reported by nothing: a note is for a
	// value read differently than written (design doc 0036 §3.4), and a
	// key carried through untouched is not one (design doc 0043 §3.6).
	if len(k.Sources) > 0 && k.Sources[0].Extra["license"] != "CC-BY" {
		t.Errorf("sources[0] lost its producer key: %+v", k.Sources[0].Extra)
	}
	for _, n := range notes {
		if strings.Contains(n, "license") {
			t.Errorf("a kept key was reported as dropped: %q", n)
		}
	}
}

// An attr shadowing an envelope key is not exported twice — the envelope
// wins, as it has since 0005, and the write path now refuses the payload
// outright (design doc 0036 §3.5).
func TestDocumentDoesNotReExportShadowedEnvelopeAttrs(t *testing.T) {
	out, err := Document(&domain.Knowledge{
		Type:    domain.TypeInsights,
		ID:      "i",
		Sources: []domain.Source{{Resource: "policies/revenue.md"}},
		Attrs:   map[string]any{"sources": []any{map[string]any{"resource": "ghost.md"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "ghost.md") {
		t.Errorf("a shadowed attr must not be re-exported:\n%s", out)
	}
}

// A foreign bundle that declares OKF's stable lifecycle without a
// verification now survives the round trip intact. It did not until 0.16:
// with confirmation held as a status, ochakai had nowhere to put "stable
// and unverified", so the entry was demoted to a draft and re-exported as
// one — a lifecycle value the producer never wrote. The predecessor of
// this test asserted that demotion and said it should start failing if
// ochakai ever gained somewhere to keep the producer's lifecycle. Design
// doc 0043 §3.2 is that somewhere.
func TestStableWithoutVerificationRoundTrips(t *testing.T) {
	const in = "---\ntype: Metric\ntitle: Average order value\nstatus: stable\n---\n\nThe average revenue per order.\n"
	d, notes, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != domain.StatusStable {
		t.Fatalf("status = %q, want stable — the producer's lifecycle value", d.Status)
	}
	if d.Verified {
		t.Error("stable alone is not a review: SPEC §5.3 puts human review in `verified`")
	}
	if len(notes) != 0 {
		t.Errorf("nothing was reinterpreted, so there is nothing to report; notes = %v", notes)
	}

	out, err := Document(&d.Knowledge)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "status: stable") {
		t.Errorf("re-export lost the producer's lifecycle value:\n%s", out)
	}
	if strings.Contains(string(out), "verified:") {
		t.Errorf("re-export must not assert a review nobody made:\n%s", out)
	}
}

// SPEC §11 lists exactly three conformance conditions, and says a
// consumer "MUST NOT reject a bundle because of" unknown keys, unknown
// types, missing optional fields, broken links or a missing index.md.
// SPEC §4.1 gives title, description, resource, status, status_note,
// stale_after, runtime and computation no YAML type at all, so an
// unquoted 2026 is a title a producer wrote, not a broken document.
//
// ochakai refused the whole document over each of these until design doc
// 0079 — and on import that refusal was silent: the file was demoted to
// a loose markdown object with no note and nothing in `skipped`, so it
// kept its bytes and stopped being knowledge.
func TestParseTakesNonStringScalars(t *testing.T) {
	for _, tc := range []struct {
		name, doc, note string
		check           func(*Doc) string
	}{
		{"an unquoted year is a title", "---\ntype: Metric\ntitle: 2026\n---\n", `title is not a string; read as "2026"`,
			func(d *Doc) string { return d.Title }},
		{"a float is a description", "---\ntype: Metric\ndescription: 3.14\n---\n", `description is not a string`,
			func(d *Doc) string { return d.Description }},
		{"a bool is a status_note", "---\ntype: Metric\nstatus_note: true\n---\n", `status_note is not a string`,
			func(d *Doc) string { return d.StatusNote }},
		{"a number is a runtime", "---\ntype: Metric\nruntime: 3\n---\n", `runtime is not a string`,
			func(d *Doc) string { return d.Runtime }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, notes, err := Parse([]byte(tc.doc))
			if err != nil {
				t.Fatalf("SPEC §11 makes this document conformant: %v", err)
			}
			if got := tc.check(d); got == "" {
				t.Errorf("the value was dropped rather than read: %+v", d.Knowledge)
			}
			if !slices.ContainsFunc(notes, func(n string) bool { return strings.Contains(n, tc.note) }) {
				t.Errorf("a reinterpretation is never silent (design doc 0036 §3.4); notes = %v", notes)
			}
		})
	}

	// A list of numbers is a list of tags; a mapping where a scalar
	// belongs has no text to be read as, so it is dropped — with a note,
	// never with the document.
	d, notes, err := Parse([]byte("---\ntype: Metric\ntags: [1, 2]\nstatus: {a: b}\n---\n"))
	if err != nil {
		t.Fatalf("SPEC §11 makes this document conformant: %v", err)
	}
	if !slices.Equal(d.Tags, []string{"1", "2"}) {
		t.Errorf("tags = %v, want the two the producer wrote", d.Tags)
	}
	if !slices.ContainsFunc(notes, func(n string) bool { return strings.Contains(n, "status is not a scalar; dropped") }) {
		t.Errorf("a dropped mapping must be reported; notes = %v", notes)
	}

	// The two refusals SPEC §11 does license: frontmatter that will not
	// parse, and a missing or empty type.
	for _, bad := range []string{
		"---\ntype: Metric\n  bad indent: [\n---\n",
		"---\ntitle: no type here\n---\n",
		"---\ntype: {a: b}\n---\n",
	} {
		if _, _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("SPEC §11 makes this non-conformant, want an error:\n%s", bad)
		}
	}
}

// SPEC §10.2 marks exactly one field REQUIRED for an Attested
// Computation: runtime. Of the executor it says "`resource` names run
// instructions or code" and "`receipt` declares the fields a run must
// return" — no requirement word on either — and §11 forbids rejecting a
// concept for a missing optional field.
//
// ochakai dropped the whole executor without a receipt, in a note that
// cited §10.2 for a rule §10.2 does not state (design doc 0079). The
// resource is still the whole content of the key: an executor naming
// nothing to run says nothing.
func TestExecutorWithoutReceiptIsKept(t *testing.T) {
	d, notes, err := Parse([]byte("---\ntype: Attested Computation\nruntime: bigquery\n" +
		"executor:\n  resource: references/skills/run-on-bq.md\n---\n"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Executor == nil || d.Executor.Resource != "references/skills/run-on-bq.md" {
		t.Fatalf("executor = %+v, want the contract the producer wrote", d.Executor)
	}
	if len(notes) != 0 {
		t.Errorf("nothing was reinterpreted; notes = %v", notes)
	}
	// A key its writer left out is not written back (design doc 0046
	// §3.9): the re-export says what the document said, and no more.
	out, err := Canonical(&d.Knowledge)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "receipt:") {
		t.Errorf("re-export invented a receipt the producer never wrote:\n%s", out)
	}

	// An executor with a receipt and nothing to run is still dropped.
	d, notes, err = Parse([]byte("---\ntype: Attested Computation\nruntime: bigquery\n" +
		"executor:\n  receipt: [job_id]\n---\n"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Executor != nil {
		t.Errorf("executor = %+v, want it dropped: it names nothing to run", d.Executor)
	}
	if !slices.ContainsFunc(notes, func(n string) bool { return strings.Contains(n, "executor needs a resource") }) {
		t.Errorf("notes = %v, want one naming the resource", notes)
	}
}

// A zero actor is no actor. OKF's convention (SPEC §7) has no form for
// one, and Actor.String renders it as a bare ":" — a created_by value
// nothing can read back. Unreachable for a stored row since migration
// 0022_actor_process.sql; an entry composed in memory still reaches the
// renderer (design doc 0079).
func TestZeroActorWritesNoCreatedBy(t *testing.T) {
	out, err := Document(&domain.Knowledge{Type: domain.TypeMetrics, Body: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "created_by:") {
		t.Errorf("an actorless entry must write no created_by:\n%s", out)
	}
}

// The fm. index says what the document says, timestamps included. YAML
// resolves an unquoted date or date-time to a time.Time, and giving one a
// spelling again is choosing a spelling the producer may not have
// written — which is what the index cannot do, since `fm.stale_after` is
// an exact match against the text a reader sees in the document (design
// doc 0047, design doc 0074 §4.1).
func TestFrontmatterIndexesTheSpellingTheDocumentWrote(t *testing.T) {
	for _, written := range []string{
		"2026-12-31",           // a bare date
		"2026-12-31T00:00:00Z", // the midnight the collapse used to eat
		"2026-12-31T18:30:00Z", // an instant with a clock
		"2026-12-31T09:00:00Z", // another
	} {
		doc := []byte("---\ntype: Insight\nstale_after: " + written + "\nreviewed_on: " + written + "\n---\n\nbody\n")
		fm := Frontmatter(doc)
		for _, key := range []string{"stale_after", "reviewed_on"} {
			if got := fm[key]; got != written {
				t.Errorf("document wrote %s: %s, index holds %#v", key, written, got)
			}
		}
	}
}
