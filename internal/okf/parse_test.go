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
type: Golden Query
id: monthly-revenue
title: 月次売上
status: draft
question: 月ごとの売上は？
sql: SELECT 1
---

Body text here.
`))
	if err != nil {
		t.Fatal(err)
	}
	if k.Type != domain.TypeQueries || k.ID != "monthly-revenue" {
		t.Errorf("got %s/%s", k.Type, k.ID)
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
		"---\ntype: metric\n",             // unterminated
		"---\ntitle: x\n---\n",            // no type at all
		"---\ntype: a/b\ntitle: x\n---\n", // reads as an address
		"---\ntype: \ntitle: x\n---\n",    // empty
		"---\ntype: [a]\ntitle: x\n---\n", // type is not a string
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
	k, _, err := Parse([]byte("---\ntype: runbook\nid: restore-backup\ntitle: リストア手順\n---\n"))
	if err != nil {
		t.Fatal(err)
	}
	if k.Type != "runbook" || len(k.Attrs) != 0 {
		t.Errorf("one-word type: got type=%q attrs=%v", k.Type, k.Attrs)
	}

	k, _, err = Parse([]byte("---\ntype: Data Contract\nid: orders-contract\ntitle: 注文契約\n---\n"))
	if err != nil {
		t.Fatal(err)
	}
	if k.Type != "Data Contract" || len(k.Attrs) != 0 {
		t.Errorf("spelled type: got type=%q attrs=%v", k.Type, k.Attrs)
	}

	doc, err := Document(k)
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

	doc, err := Document(k)
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
	k, _, err := Parse([]byte("---\ntype: insight\nid: dup\ntitle: 重複\nstatus: rejected\nstatus_note: 既存と重複\n---\n"))
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
	k, _, err := Parse([]byte("---\r\ntype: Glossary Term\r\nid: churn\r\ntitle: 解約\r\n---\r\n\r\n本文。\r\n"))
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
		wantNote          bool
	}{
		{"stable with a verification is verified", "status: stable\n" + verified, domain.StatusVerified, false},
		{"stable alone is not a review", "status: stable\n", domain.StatusDraft, false},
		{"a bare verified mapping counts too (SPEC §5.2)", "status: stable\nverified: { by: human:na0, at: 2026-07-01T00:00:00Z }\n", domain.StatusVerified, false},
		{"draft stays draft", "status: draft\n", domain.StatusDraft, false},
		{"deprecated stays deprecated", "status: deprecated\n", domain.StatusDeprecated, false},
		{"an absent status stays unset for the write path to default", "", "", false},
		{"an absent status with a verification is a confirmation", verified, domain.StatusVerified, false},
		// The values ochakai wrote before v0.2 are unrecognized now: OKF's
		// vocabulary is the three above (design doc 0036 §3.5). A 0.12
		// bundle still imports its verified entries as verified, but
		// through the verified key SPEC §5.3 makes authoritative, not
		// through an ochakai-only status alias.
		{"ochakai 0.12 wrote status: verified", "status: verified\n" + verified, domain.StatusVerified, true},
		{"a legacy status with no verification is a draft", "status: verified\n", domain.StatusDraft, true},
		{"ochakai 0.12 wrote status: rejected", "status: rejected\n", domain.StatusDraft, true},
		{"an unknown value is a draft, not a rejected document", "status: retired\n", domain.StatusDraft, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k, notes, err := Parse([]byte("---\ntype: Insight\n" + tc.frontmatter + "---\n"))
			if err != nil {
				t.Fatal(err)
			}
			if k.Status != tc.want {
				t.Errorf("status = %q, want %q", k.Status, tc.want)
			}
			if got := len(notes) > 0; got != tc.wantNote {
				t.Errorf("notes = %v, want a note: %v", notes, tc.wantNote)
			}
			// The trust family is read for its presence only; the actors
			// and times stay outside the payload (design doc 0009).
			if k.VerifiedBy != nil || k.VerifiedAt != nil {
				t.Errorf("a bundle must not set verification provenance: %v %v", k.VerifiedBy, k.VerifiedAt)
			}
			for _, key := range []string{"verified", "generated", "status"} {
				if _, ok := k.Attrs[key]; ok {
					t.Errorf("%s leaked into attrs: %v", key, k.Attrs)
				}
			}
		})
	}
}

// A v0.1 document keeps importing: the renamed keys are read (and, being
// server-owned, ignored) rather than landing in attrs where a re-export
// would write them beside their v0.2 replacements (SPEC §13.1).
func TestParseV01Document(t *testing.T) {
	k, _, err := Parse([]byte(`---
type: Insight
title: 売上の季節性
status: verified
timestamp: '2026-05-28T22:51:43+00:00'
created_by: 'agent:claude-code'
verified_by: 'human:na0'
verified_at: '2026-06-01T00:00:00Z'
---

Body.
`))
	if err != nil {
		t.Fatal(err)
	}
	if k.Status != domain.StatusVerified {
		t.Errorf("status = %q, want verified", k.Status)
	}
	if len(k.Attrs) != 0 {
		t.Errorf("v0.1 trust keys must not become attrs: %v", k.Attrs)
	}
	doc, err := Document(k)
	if err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"timestamp:", "verified_by:", "verified_at:"} {
		if strings.Contains(string(doc), gone) {
			t.Errorf("re-export wrote the v0.1 key %q:\n%s", gone, doc)
		}
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
	doc, err := Document(k)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(doc), "stale_after: \"2026-12-31\"") {
		t.Errorf("stale_after lost on export:\n%s", doc)
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

// Every key OKF v0.2 defines is an envelope field, not an attr: the spec's
// schema is the shape ochakai keeps (design doc 0036 §2). Only producer
// extension keys ride through attrs.
func TestParseReadsOKFFamiliesIntoTheEnvelope(t *testing.T) {
	doc := []byte(`---
type: Attested Computation
title: Revenue for fiscal year
tags: [finance]
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

	out, err := Document(k)
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
	for _, key := range []string{"\ntype:", "\ntags:", "\nsources:", "\nusage_window:", "\ngenerated:", "\nstatus:", "\nruntime:", "\nparameters:", "\nexecutor:", "\nattester:", "\ncreated_by:"} {
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
executor: { resource: run.md }
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
	// A half-stated contract is dropped rather than stored: both of the
	// executor's keys are required within it (SPEC §10.2).
	if k.UsageWindow != nil || k.Executor != nil || k.Attester != nil || len(k.Parameters) != 0 {
		t.Errorf("unusable values must be dropped: %+v %+v %+v %+v",
			k.UsageWindow, k.Executor, k.Attester, k.Parameters)
	}
	for _, want := range []string{
		"last_modified", "names no resource", "dropped keys OKF does not define: license",
		"usage_count", "usage_window", "parameters[0]", "executor", "attester",
	} {
		if !slices.ContainsFunc(notes, func(n string) bool { return strings.Contains(n, want) }) {
			t.Errorf("no note mentions %q: %v", want, notes)
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
