package okf

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// set spells the values as the wire spells them — JSON, in the order
// the caller wrote it, because that order is what reaches the document.
func set(kv ...string) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for i := 0; i+1 < len(kv); i += 2 {
		out[kv[i]] = json.RawMessage(kv[i+1])
	}
	return out
}

const fmDoc = `---
type: Metric
# the resource is the table this counts over
resource: bigquery://p/d/orders
title: 売上
tags:
  - sales
  - daily
sources:
  - resource: bigquery://p/d/orders
    title: Orders
threshold: 5
---

Revenue is the sum of ` + "`net_amount`" + `.
`

func TestStructureOfReadsKeysInDocumentOrder(t *testing.T) {
	s, err := StructureOf([]byte(fmDoc))
	if err != nil {
		t.Fatalf("StructureOf: %v", err)
	}
	want := []string{"type", "resource", "title", "tags", "sources", "threshold"}
	if strings.Join(s.Keys, ",") != strings.Join(want, ",") {
		t.Errorf("keys = %v, want %v", s.Keys, want)
	}
	if s.Values["title"] != "売上" {
		t.Errorf("title = %v", s.Values["title"])
	}
	// A producer key is carried like any other: the face is the
	// document's frontmatter, not ochakai's envelope (design doc 0130).
	if s.Values["threshold"] != 5 {
		t.Errorf("threshold = %#v, want 5", s.Values["threshold"])
	}
	src, ok := s.Values["sources"].([]any)
	if !ok || len(src) != 1 {
		t.Fatalf("sources = %#v", s.Values["sources"])
	}
	if m, _ := src[0].(map[string]any); m["title"] != "Orders" {
		t.Errorf("sources[0] = %#v", src[0])
	}
}

// A date keeps the spelling its writer chose. Decoded to time.Time and
// rendered back it would come out as an instant, which is an edit to a
// key the caller never named.
func TestStructureOfKeepsADateADate(t *testing.T) {
	s, err := StructureOf([]byte("---\ntype: Metric\nstale_after: 2026-12-31\n---\n"))
	if err != nil {
		t.Fatalf("StructureOf: %v", err)
	}
	if s.Values["stale_after"] != "2026-12-31" {
		t.Errorf("stale_after = %#v", s.Values["stale_after"])
	}
}

func TestStructureOfRefusesWhatIsNotADocument(t *testing.T) {
	if _, err := StructureOf([]byte("just prose\n")); !errors.Is(err, ErrNoFrontmatter) {
		t.Errorf("err = %v, want ErrNoFrontmatter", err)
	}
	if _, err := StructureOf([]byte("---\n- a\n- b\n---\n")); !errors.Is(err, ErrFrontmatterNotAMapping) {
		t.Errorf("err = %v, want ErrFrontmatterNotAMapping", err)
	}
}

// The whole point of the surface: everything the caller did not name is
// where it was, comments and key order included.
func TestSetFrontmatterKeysLeavesEveryOtherByte(t *testing.T) {
	out, err := SetFrontmatterKeys([]byte(fmDoc), set("title", `"Monthly revenue"`), nil)
	if err != nil {
		t.Fatalf("SetFrontmatterKeys: %v", err)
	}
	got := string(out)
	for _, want := range []string{
		"# the resource is the table this counts over\n",
		"resource: bigquery://p/d/orders\n",
		"threshold: 5\n",
		"Revenue is the sum of `net_amount`.\n",
		"title: Monthly revenue\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "title: 売上") {
		t.Errorf("old title survived:\n%s", got)
	}
	// Order is the document's, not the setter's.
	if i, j := strings.Index(got, "resource:"), strings.Index(got, "title:"); i > j {
		t.Errorf("keys reordered:\n%s", got)
	}
}

// A multi-line key is replaced as a block, not as its first line — the
// failure a regex on "^key:" has, and the reason the ranges come from
// the parse.
func TestSetFrontmatterKeysReplacesAWholeBlock(t *testing.T) {
	out, err := SetFrontmatterKeys([]byte(fmDoc), set("sources", `[{"resource":"https://example.com/a","title":"A"}]`), nil)
	if err != nil {
		t.Fatalf("SetFrontmatterKeys: %v", err)
	}
	got := string(out)
	if strings.Contains(got, "bigquery://p/d/orders\n    title: Orders") {
		t.Errorf("old sources block survived:\n%s", got)
	}
	// Indented as the bundle writer (Canonical) indents one, so a block
	// written from a field and one written by export read the same.
	if !strings.Contains(got, "sources:\n    - resource: https://example.com/a\n      title: A\n") {
		t.Errorf("new sources block not rendered as Canonical would write it:\n%s", got)
	}
	// And the key after it is still there, at the same level.
	if !strings.Contains(got, "\nthreshold: 5\n") {
		t.Errorf("the key after the block was eaten:\n%s", got)
	}
	// What went in comes back out.
	s, err := StructureOf(out)
	if err != nil {
		t.Fatalf("StructureOf: %v", err)
	}
	src, _ := s.Values["sources"].([]any)
	if len(src) != 1 {
		t.Fatalf("sources = %#v", s.Values["sources"])
	}
	if m, _ := src[0].(map[string]any); m["resource"] != "https://example.com/a" {
		t.Errorf("round trip = %#v", src[0])
	}
}

func TestSetFrontmatterKeysAppendsAKeyTheDocumentLacks(t *testing.T) {
	out, err := SetFrontmatterKeys([]byte("---\ntype: Metric\n---\n\nbody\n"),
		set("status", `"stable"`, "runtime", `"bigquery"`), nil)
	if err != nil {
		t.Fatalf("SetFrontmatterKeys: %v", err)
	}
	// Sorted, so two runs of one request produce the same bytes — the
	// content hash is taken over them.
	if string(out) != "---\ntype: Metric\nruntime: bigquery\nstatus: stable\n---\n\nbody\n" {
		t.Errorf("got:\n%s", out)
	}
}

func TestSetFrontmatterKeysRemoves(t *testing.T) {
	out, err := SetFrontmatterKeys([]byte(fmDoc), nil, []string{"tags", "threshold"})
	if err != nil {
		t.Fatalf("SetFrontmatterKeys: %v", err)
	}
	got := string(out)
	if strings.Contains(got, "tags:") || strings.Contains(got, "threshold:") ||
		strings.Contains(got, "- daily") {
		t.Errorf("removed keys survived:\n%s", got)
	}
	if !strings.Contains(got, "sources:") {
		t.Errorf("sources went with them:\n%s", got)
	}
}

// Provenance is what this instance observed (design doc 0009). A face
// that took `verified` here would be offering to write a key the write
// path ignores.
func TestSetFrontmatterKeysRefusesServerOwnedKeys(t *testing.T) {
	for _, key := range []string{"generated", "verified", "created_by", "verified_at"} {
		if _, err := SetFrontmatterKeys([]byte(fmDoc), set(key, `"x"`), nil); err == nil {
			t.Errorf("set %s: no error", key)
		}
		if _, err := SetFrontmatterKeys([]byte(fmDoc), nil, []string{key}); err == nil {
			t.Errorf("unset %s: no error", key)
		}
	}
}

func TestSetFrontmatterKeysRefusesWhatIsNotADocument(t *testing.T) {
	if _, err := SetFrontmatterKeys([]byte("prose\n"), set("type", `"Metric"`), nil); !errors.Is(err, ErrNoFrontmatter) {
		t.Errorf("err = %v, want ErrNoFrontmatter", err)
	}
}

// A string that would go out as a block scalar the parser cannot read
// back would turn a stored document into one that no longer parses —
// the reason every renderer in this package textifies.
func TestSetFrontmatterKeysSurvivesAwkwardValues(t *testing.T) {
	for _, v := range []string{" leading space\nsecond line", "---\n", ": not a key", "2026-12-31"} {
		out, err := SetFrontmatterKeys([]byte(fmDoc), set("description", string(mustJSON(v))), nil)
		if err != nil {
			t.Fatalf("SetFrontmatterKeys(%q): %v", v, err)
		}
		s, err := StructureOf(out)
		if err != nil {
			t.Fatalf("StructureOf after %q: %v\n%s", v, err, out)
		}
		if s.Values["description"] != v {
			t.Errorf("round trip of %q = %#v", v, s.Values["description"])
		}
	}
}

// A document naming one key twice is one yaml.v3 parses. Replacing only
// the first occurrence would leave a second `title:` under a form that
// reported it set the title once.
func TestSetFrontmatterKeysReplacesEveryOccurrence(t *testing.T) {
	doc := "---\ntype: Metric\ntitle: first\ntags:\n  - a\ntitle: second\n---\n\nbody\n"
	out, err := SetFrontmatterKeys([]byte(doc), set("title", `"only"`), nil)
	if err != nil {
		t.Fatalf("SetFrontmatterKeys: %v", err)
	}
	got := string(out)
	if strings.Count(got, "title:") != 1 {
		t.Errorf("title appears %d times:\n%s", strings.Count(got, "title:"), got)
	}
	if !strings.Contains(got, "title: only\n") || !strings.Contains(got, "- a\n") {
		t.Errorf("got:\n%s", got)
	}
	// And the same for a removal.
	gone, err := SetFrontmatterKeys([]byte(doc), nil, []string{"title"})
	if err != nil {
		t.Fatalf("SetFrontmatterKeys: %v", err)
	}
	if strings.Contains(string(gone), "title:") {
		t.Errorf("a duplicate survived the removal:\n%s", gone)
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// A JSON object is ordered where a Go map is not, and that order is what
// the document gets: a source alphabetized on the way through would read
// differently from one the bundle writer emitted, in the same file.
func TestSetFrontmatterKeysKeepsTheOrderTheCallerWrote(t *testing.T) {
	out, err := SetFrontmatterKeys([]byte("---\ntype: Metric\n---\n"),
		set("sources", `[{"resource":"bigquery://p/d/t","title":"Orders","usage_count":12,"author":"human:a@b.c"}]`), nil)
	if err != nil {
		t.Fatalf("SetFrontmatterKeys: %v", err)
	}
	want := "sources:\n    - resource: bigquery://p/d/t\n      title: Orders\n      usage_count: 12\n      author: human:a@b.c\n"
	if !strings.Contains(string(out), want) {
		t.Errorf("got:\n%s\nwant it to contain:\n%s", out, want)
	}
}

// A number stays a number and a string that looks like one stays a
// string — the round trip a form depends on, since it hands back what it
// just wrote and redraws the fields from it.
func TestSetFrontmatterKeysKeepsJSONTypes(t *testing.T) {
	out, err := SetFrontmatterKeys([]byte("---\ntype: Metric\n---\n"),
		set("sources", `[{"usage_count":12,"id":"12","last_modified":"2026-01-02"}]`,
			"parameters", `[{"name":"year","type":"integer","required":true}]`), nil)
	if err != nil {
		t.Fatalf("SetFrontmatterKeys: %v", err)
	}
	s, err := StructureOf(out)
	if err != nil {
		t.Fatalf("StructureOf: %v\n%s", err, out)
	}
	src := s.Values["sources"].([]any)[0].(map[string]any)
	if src["usage_count"] != 12 {
		t.Errorf("usage_count = %#v, want 12", src["usage_count"])
	}
	if src["id"] != "12" {
		t.Errorf("id = %#v, want the string \"12\"", src["id"])
	}
	if src["last_modified"] != "2026-01-02" {
		t.Errorf("last_modified = %#v", src["last_modified"])
	}
	p := s.Values["parameters"].([]any)[0].(map[string]any)
	if p["required"] != true {
		t.Errorf("required = %#v, want true", p["required"])
	}
}
