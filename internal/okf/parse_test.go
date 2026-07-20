package okf

import (
	"reflect"
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
		got, err := Parse(doc)
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
	k, err := Parse([]byte(`---
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
		if _, err := Parse([]byte(doc)); err == nil {
			t.Errorf("Parse(%q) succeeded, want error", doc)
		}
	}
}

// Free types are first-class (design doc 0005) and are stored verbatim
// (design doc 0023): a multi-word spelling is neither slugified nor
// stashed in a preservation attr, so the document round-trips because
// nothing was changed in the first place.
func TestParseFreeTypes(t *testing.T) {
	k, err := Parse([]byte("---\ntype: runbook\nid: restore-backup\ntitle: リストア手順\n---\n"))
	if err != nil {
		t.Fatal(err)
	}
	if k.Type != "runbook" || len(k.Attrs) != 0 {
		t.Errorf("one-word type: got type=%q attrs=%v", k.Type, k.Attrs)
	}

	k, err = Parse([]byte("---\ntype: Data Contract\nid: orders-contract\ntitle: 注文契約\n---\n"))
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
	k, err := Parse([]byte(`---
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
	k, err := Parse([]byte("---\ntype: insight\nid: dup\ntitle: 重複\nstatus: rejected\nstatus_note: 既存と重複\n---\n"))
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
	k, err := Parse([]byte("---\r\ntype: Glossary Term\r\nid: churn\r\ntitle: 解約\r\n---\r\n\r\n本文。\r\n"))
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
	got, err := Parse([]byte("---\ntype: Insight\n---\n\n" + body + "\n"))
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
	k, err := Parse([]byte("---\ntype: Dataset\ntitle: SO\ntags: Stack Overflow, public data, community, Q&A\n---\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Stack Overflow", "public data", "community", "Q&A"}
	if !reflect.DeepEqual(k.Tags, want) {
		t.Errorf("tags = %v, want %v", k.Tags, want)
	}
}
