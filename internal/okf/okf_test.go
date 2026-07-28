package okf

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/na0fu3y/ochakai/internal/domain"
)

func sample() []domain.Knowledge {
	verifiedAt := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	return []domain.Knowledge{
		{
			Type: domain.TypeInsights, ID: "insights/revenue-seasonality",
			Title: "売上の季節性", Description: "12月は繁忙期",
			Tags: []string{"sales"}, Status: domain.StatusStable,
			StaleAfter:    "2026-12-31",
			CreatedBy:     domain.Actor{Kind: "process", Name: "claude-code"},
			UpdatedBy:     domain.Actor{Kind: "process", Name: "claude-code"},
			Verifications: []domain.Verification{{By: domain.Actor{Kind: "human", Name: "na0"}, At: verifiedAt}},
			Attrs:         map[string]any{"kind": "seasonality"},
			Body:          "12月は+40%が通常。[売上](/metrics/revenue.md) の話である。",
			UpdatedAt:     verifiedAt,
		},
		{
			Type: domain.TypeTables, ID: "tables/orders",
			Title: "orders", Status: domain.StatusDraft,
			CreatedBy: domain.Actor{Kind: "human", Name: "na0"},
			UpdatedBy: domain.Actor{Kind: "human", Name: "na0", Via: "process:insightflow"},
			Resource:  "myproject.shop.orders",
			Attrs:     map[string]any{"model": "sales_analytics"},
			UpdatedAt: verifiedAt,
		},
	}
}

// frontmatterOf decodes a rendered document's frontmatter.
func frontmatterOf(t *testing.T, doc []byte) map[string]any {
	t.Helper()
	parts := strings.SplitN(string(doc), "---\n", 3)
	if len(parts) != 3 {
		t.Fatalf("bad document structure:\n%s", doc)
	}
	var fm map[string]any
	if err := yaml.Unmarshal([]byte(parts[1]), &fm); err != nil {
		t.Fatalf("frontmatter is not valid YAML: %v", err)
	}
	return fm
}

func TestDocumentFrontmatterAndBody(t *testing.T) {
	entries := sample()
	doc, err := Document(&entries[0])
	if err != nil {
		t.Fatal(err)
	}
	s := string(doc)
	if !strings.HasPrefix(s, "---\n") {
		t.Fatalf("missing frontmatter delimiter:\n%s", s)
	}
	parts := strings.SplitN(s, "---\n", 3)
	if len(parts) != 3 {
		t.Fatalf("bad document structure:\n%s", s)
	}
	var fm map[string]any
	if err := yaml.Unmarshal([]byte(parts[1]), &fm); err != nil {
		t.Fatalf("frontmatter is not valid YAML: %v", err)
	}
	// v0.2 spellings only: timestamp became generated.at, and the flat
	// verified_by/verified_at became the verified list (SPEC §13.1). A
	// verified entry is stable plus a verification, not status: verified
	// (design doc 0036 §3.4).
	for key, want := range map[string]any{
		"type":        "Insight",
		"title":       "売上の季節性",
		"status":      "stable",
		"stale_after": "2026-12-31",
		"created_by":  "process:claude-code",
	} {
		if fm[key] != want {
			t.Errorf("frontmatter %s = %v, want %v", key, fm[key], want)
		}
	}
	for _, gone := range []string{"timestamp", "verified_by", "verified_at"} {
		if _, ok := fm[gone]; ok {
			t.Errorf("v0.1 key %s must not be written any more:\n%s", gone, parts[1])
		}
	}
	generated, _ := fm["generated"].(map[string]any)
	if generated["by"] != "process:claude-code" || generated["at"] != "2026-07-01T00:00:00Z" {
		t.Errorf("generated = %v, want the last content author and change time", fm["generated"])
	}
	verified, _ := fm["verified"].([]any)
	if len(verified) != 1 {
		t.Fatalf("verified = %v, want a one-element list", fm["verified"])
	}
	if v, _ := verified[0].(map[string]any); v["by"] != "human:na0" || v["at"] != "2026-07-01T00:00:00Z" {
		t.Errorf("verified[0] = %v, want the human reviewer and time", verified[0])
	}
	if _, ok := fm["id"]; ok {
		t.Errorf("id must not export — the path is the id (design doc 0016):\n%s", parts[1])
	}
	if !strings.Contains(parts[2], "12月は+40%が通常。") {
		t.Errorf("body missing:\n%s", parts[2])
	}
	// Links live in the body and nowhere else (design doc 0024): the link
	// is there because the author wrote it, and Document adds no section
	// of its own that a re-import would read a second time.
	if !strings.Contains(parts[2], "[売上](/metrics/revenue.md)") {
		t.Errorf("body link missing:\n%s", parts[2])
	}
	if strings.Contains(parts[2], "# Links") {
		t.Errorf("Document must not generate a links section (design doc 0024):\n%s", parts[2])
	}
}

func TestDocumentRejectedProvenance(t *testing.T) {
	rejectedAt := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	k := domain.Knowledge{
		Type: domain.TypeInsights, ID: "insights/dup-insight",
		Title: "重複した知見", Status: domain.StatusDraft,
		StatusNote: "revenue-seasonality と重複",
		CreatedBy:  domain.Actor{Kind: "process", Name: "claude-code"},
		Rejection: &domain.Rejection{
			By: domain.Actor{Kind: "human", Name: "na0"}, At: rejectedAt, Note: "重複",
		},
		UpdatedAt: rejectedAt,
	}
	doc, err := Document(&k)
	if err != nil {
		t.Fatal(err)
	}
	s := string(doc)
	// The entry's real lifecycle value goes out. A rejection used to be
	// folded onto deprecated, which asserts the opposite thing about the
	// entry ("was correct, no longer current"); it is this instance's
	// ruling now, so it travels as an extension key beside the true
	// status and import never reads it back (design doc 0043 §3.3).
	for _, want := range []string{
		"status: draft",
		"status_note: revenue-seasonality と重複",
		"rejected_by: human:na0",
		`rejected_at: "2026-07-16T00:00:00Z"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("document missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "status: deprecated") {
		t.Errorf("a rejection must not be folded onto deprecated:\n%s", s)
	}
	if strings.Contains(s, "verified:") {
		t.Errorf("an unverified entry must carry no verified key — its absence is the trust tier (SPEC §5.3):\n%s", s)
	}
}

// The ledger is plural, so a re-checked entry exports every confirmation
// rather than only the last (SPEC §5.2, design doc 0043 §3.2).
func TestDocumentWritesEveryVerification(t *testing.T) {
	first := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	second := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	k := domain.Knowledge{
		Type: domain.TypeInsights, ID: "insights/seasonality", Status: domain.StatusStable,
		CreatedBy: domain.Actor{Kind: "human", Name: "na0"},
		Verifications: []domain.Verification{
			{By: domain.Actor{Kind: "human", Name: "na0"}, At: first},
			{By: domain.Actor{Kind: "human", Name: "tanaka"}, At: second},
		},
		UpdatedAt: second,
	}
	doc, err := Document(&k)
	if err != nil {
		t.Fatal(err)
	}
	fm := frontmatterOf(t, doc)
	verified, _ := fm["verified"].([]any)
	if len(verified) != 2 {
		t.Fatalf("verified = %v, want both confirmations", fm["verified"])
	}
	for i, want := range []string{"human:na0", "human:tanaka"} {
		v, _ := verified[i].(map[string]any)
		if v["by"] != want {
			t.Errorf("verified[%d].by = %v, want %q (oldest first)", i, v["by"], want)
		}
	}
}

// The delegating caller travels as a sibling of by, so by stays a bare
// actor and the human: prefix a consumer keys trust off stays readable
// (design docs 0027, 0036 §3.2).
func TestDocumentDelegatedActor(t *testing.T) {
	entries := sample()
	doc, err := Document(&entries[1])
	if err != nil {
		t.Fatal(err)
	}
	var fm map[string]any
	if err := yaml.Unmarshal([]byte(strings.SplitN(string(doc), "---\n", 3)[1]), &fm); err != nil {
		t.Fatal(err)
	}
	generated, _ := fm["generated"].(map[string]any)
	if generated["by"] != "human:na0" || generated["via"] != "process:insightflow" {
		t.Errorf("generated = %v, want by and via kept apart", fm["generated"])
	}
}

func TestDocumentResourceForTable(t *testing.T) {
	entries := sample()
	doc, err := Document(&entries[1])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(doc), "resource: myproject.shop.orders") {
		t.Errorf("table resource missing:\n%s", doc)
	}
}

func TestBundleLayoutAndIndexes(t *testing.T) {
	files := bundle(t, sample())
	for _, path := range []string{"insights/revenue-seasonality.md", "tables/orders.md", "index.md", "insights/index.md", "tables/index.md"} {
		if _, ok := files[path]; !ok {
			t.Errorf("bundle missing %s (have %d files)", path, len(files))
		}
	}
	// Index files list their contents with relative links and no
	// frontmatter (SPEC §6); the root index alone declares okf_version
	// (§11).
	typeIdx := string(files["insights/index.md"])
	if !strings.Contains(typeIdx, "[売上の季節性](revenue-seasonality.md)") {
		t.Errorf("type index missing concept link:\n%s", typeIdx)
	}
	if strings.HasPrefix(typeIdx, "---") {
		t.Errorf("non-root index must not carry frontmatter:\n%s", typeIdx)
	}
	root := string(files["index.md"])
	if !strings.Contains(root, "[insights/](insights/index.md)") {
		t.Errorf("root index missing type link:\n%s", root)
	}
	if !strings.HasPrefix(root, "---\nokf_version: \"0.2\"\n---\n") {
		t.Errorf("root index must declare okf_version:\n%s", root)
	}
}

// Extension attrs export as producer-defined top-level frontmatter keys
// (SPEC §4.1), not nested under an attrs map, so foreign consumers see
// them and foreign bundles round-trip in place.
func TestDocumentFlattensAttrs(t *testing.T) {
	entries := sample()
	doc, err := Document(&entries[0]) // attrs: {kind: seasonality}
	if err != nil {
		t.Fatal(err)
	}
	var fm map[string]any
	if err := yaml.Unmarshal([]byte(strings.SplitN(string(doc), "---\n", 3)[1]), &fm); err != nil {
		t.Fatal(err)
	}
	if fm["kind"] != "seasonality" {
		t.Errorf("extension key not at top level: %v", fm)
	}
	if _, ok := fm["attrs"]; ok {
		t.Errorf("attrs must not nest:\n%s", doc)
	}

	// An attr whose name collides with an envelope key must not clobber it.
	k := entries[0]
	k.Attrs = map[string]any{"title": "偽タイトル", "kind": "seasonality"}
	doc, err = Document(&k)
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal([]byte(strings.SplitN(string(doc), "---\n", 3)[1]), &fm); err != nil {
		t.Fatalf("colliding attr broke the frontmatter: %v\n%s", err, doc)
	}
	if fm["title"] != "売上の季節性" {
		t.Errorf("envelope must win over a colliding attr: %v", fm["title"])
	}
}

func TestWriteTarGzRoundTrip(t *testing.T) {
	files := bundle(t, sample())
	var buf bytes.Buffer
	writeTarGz(t, &buf, files, time.Unix(0, 0))
	gz, err := gzip.NewReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	got := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got[hdr.Name] = true
	}
	if len(got) != len(files) {
		t.Errorf("tar has %d entries, want %d", len(got), len(files))
	}
}

// TestDocumentKeepsLeadingNewlines is why the YAML library moved to the
// maintained fork. gopkg.in/yaml.v3 v3.0.1 emitted a value that begins
// with a newline as a block scalar that dropped exactly that newline —
// its own output did not read back as what went in, so an export of such
// an entry silently lost a character on re-import. The bug is fixed
// upstream in go.yaml.in/yaml, and this pins the fix: it fails against
// the old library.
//
// Not a general claim about round-tripping. A value whose first line
// starts with a tab still emits frontmatter the parser rejects, in every
// yaml version tried; see the fuzz target's skip for that one.
func TestDocumentKeepsLeadingNewlines(t *testing.T) {
	for _, title := range []string{
		"\nleading",
		"\n\nleading",
		"\n",
		"trailing\n",
		"first\nsecond",
		"tab\tmid\nsecond",
	} {
		doc, err := Document(&domain.Knowledge{Type: domain.TypeMetrics, Title: title})
		if err != nil {
			t.Fatalf("Document(title=%q): %v", title, err)
		}
		got, _, err := Parse(doc)
		if err != nil {
			t.Errorf("title=%q: our own document does not parse: %v\n%s", title, err, doc)
			continue
		}
		if got.Title != title {
			t.Errorf("title %q came back as %q\n%s", title, got.Title, doc)
		}
	}
}

// TestDocumentSurvivesIndentedFirstLines pins the shapes that used to
// export into a bundle ochakai could not read back. A value that spans
// lines and opens with whitespace went out as a block scalar with the
// wrong indentation indicator, and Parse rejected the document outright —
// so the entry was skipped on import (design doc 0019) rather than
// round-tripping. Found by FuzzDocumentRoundTrip; see the text type.
func TestDocumentSurvivesIndentedFirstLines(t *testing.T) {
	for _, v := range []string{
		" space\nsecond",
		"\ttab\nsecond",
		"\nnewline\nsecond",
		"  \n  ",
		"ordinary\nprose",
		"trailing\n",
		"mid\ttab\nsecond",
	} {
		for _, c := range []struct {
			where string
			k     *domain.Knowledge
			got   func(*domain.Knowledge) string
		}{
			{"title", &domain.Knowledge{Type: domain.TypeMetrics, Title: v},
				func(k *domain.Knowledge) string { return k.Title }},
			{"description", &domain.Knowledge{Type: domain.TypeMetrics, Description: v},
				func(k *domain.Knowledge) string { return k.Description }},
			{"tag", &domain.Knowledge{Type: domain.TypeMetrics, Tags: []string{v}},
				func(k *domain.Knowledge) string {
					if len(k.Tags) != 1 {
						return ""
					}
					return k.Tags[0]
				}},
			{"attr", &domain.Knowledge{Type: domain.TypeMetrics, Attrs: map[string]any{"note": v}},
				func(k *domain.Knowledge) string {
					s, _ := k.Attrs["note"].(string)
					return s
				}},
		} {
			doc, err := Document(c.k)
			if err != nil {
				t.Errorf("%s=%q: Document: %v", c.where, v, err)
				continue
			}
			back, _, err := Parse(doc)
			if err != nil {
				t.Errorf("%s=%q: our own document does not parse: %v\n%s", c.where, v, err, doc)
				continue
			}
			if got := c.got(&back.Knowledge); got != v {
				t.Errorf("%s=%q came back as %q\n%s", c.where, v, got, doc)
			}
		}
	}
}

// TestDocumentKeepsBlockScalarsForProse guards the cost side of that fix:
// only the awkward shapes are quoted. An exported bundle is read in Git
// diffs (design doc 0009), where a multi-line description as one escaped
// line is a real loss.
func TestDocumentKeepsBlockScalarsForProse(t *testing.T) {
	doc, err := Document(&domain.Knowledge{
		Type: domain.TypeInsights, Description: "First line.\nSecond line.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(doc), "description: |-\n    First line.\n    Second line.\n") {
		t.Errorf("ordinary multi-line prose should stay a block scalar:\n%s", doc)
	}
}

// Canonical is the stored form: the text a writer authored, with none of
// the keys this instance owns. The content hash is taken over it (design
// doc 0043 §§3.4, 3.7), so anything that leaks in here would make a
// verification — or a rejection, or another instance's created_by —
// change the entry's version and invalidate an editor's precondition.
func TestCanonicalOmitsServerOwnedKeys(t *testing.T) {
	at := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	k := domain.Knowledge{
		Type: domain.TypeInsights, ID: "insights/seasonality", Title: "季節性",
		Status: domain.StatusStable, Body: "12月は+40%。",
		CreatedBy: domain.Actor{Kind: "human", Name: "na0"},
		UpdatedBy: domain.Actor{Kind: "human", Name: "tanaka"},
		Verifications: []domain.Verification{
			{By: domain.Actor{Kind: "human", Name: "na0"}, At: at},
		},
		Rejection: &domain.Rejection{By: domain.Actor{Kind: "human", Name: "na0"}, At: at},
		UpdatedAt: at,
	}
	canon, err := Canonical(&k)
	if err != nil {
		t.Fatal(err)
	}
	for _, owned := range []string{"generated:", "verified:", "created_by:", "rejected_by:", "rejected_at:"} {
		if strings.Contains(string(canon), owned) {
			t.Errorf("canonical form carries the server-owned key %q:\n%s", owned, canon)
		}
	}
	// What the writer wrote is all there.
	for _, want := range []string{"type: Insight", "title: 季節性", "status: stable", "12月は+40%。"} {
		if !strings.Contains(string(canon), want) {
			t.Errorf("canonical form is missing %q:\n%s", want, canon)
		}
	}
	// And it parses back as itself: the stored text is a valid document.
	back, notes, err := Parse(canon)
	if err != nil {
		t.Fatalf("the canonical form does not parse: %v\n%s", err, canon)
	}
	if len(notes) != 0 {
		t.Errorf("the canonical form was reinterpreted on read: %v", notes)
	}
	back.ID = k.ID
	if !back.SameContent(&k) {
		t.Errorf("canonical round trip changed the entry:\n got %+v\nwant %+v", back.Knowledge, k)
	}

	// A ruling must not move the hash. This is the invariant that lets
	// verify leave a held If-Match alone.
	bare := k
	bare.Verifications, bare.Rejection = nil, nil
	bareCanon, err := Canonical(&bare)
	if err != nil {
		t.Fatal(err)
	}
	if string(bareCanon) != string(canon) {
		t.Errorf("rulings changed the canonical form:\n%s\nvs\n%s", canon, bareCanon)
	}
}

// A producer key inside a structured object survives the round trip, in
// place (SPEC §4.1, design doc 0043 §3.6). It was dropped with a note
// until 0.16, on the reasoning that four surfaces should be able to
// describe one shape — a premise that went when the document became the
// stored form, since a key discarded on the way in is a key no later
// release can recover.
func TestProducerKeysInsideStructuredObjectsRoundTrip(t *testing.T) {
	const in = `---
type: Attested Computation
title: 月次売上
runtime: bigquery
sources:
  - resource: policies/revenue.md
    id: rev
    license: CC-BY
    reviewed_by: finance
parameters:
  - name: year
    type: integer
    ui_hint: dropdown
executor:
  resource: run.md
  receipt: [job_id]
  timeout_s: 300
attester:
  resource: check.py
  language: python
---

本文。
`
	d, notes, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 0 {
		t.Errorf("keeping a key is not a reinterpretation, so nothing should be reported: %v", notes)
	}
	if got := d.Sources[0].Extra; got["license"] != "CC-BY" || got["reviewed_by"] != "finance" {
		t.Errorf("source producer keys = %v", got)
	}
	if got := d.Parameters[0].Extra; got["ui_hint"] != "dropdown" {
		t.Errorf("parameter producer keys = %v", got)
	}
	if got := d.Executor.Extra; got["timeout_s"] != 300 {
		t.Errorf("executor producer keys = %v", got)
	}
	if got := d.Attester.Extra; got["language"] != "python" {
		t.Errorf("attester producer keys = %v", got)
	}

	// They come back out beside the keys the spec defines, not nested
	// under one of ochakai's own.
	out, err := Canonical(&d.Knowledge)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"license: CC-BY", "reviewed_by: finance",
		"ui_hint: dropdown", "timeout_s: 300", "language: python"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("re-export lost %q:\n%s", want, out)
		}
	}
	if strings.Contains(string(out), "extra:") {
		t.Errorf("producer keys must ride inline, not under an ochakai key:\n%s", out)
	}

	// And the entry is unchanged by the trip, so a re-import writes
	// nothing — the property the whole round trip is stated in.
	back, _, err := Parse(out)
	if err != nil {
		t.Fatalf("our own output does not parse: %v\n%s", err, out)
	}
	back.ID = d.ID
	if !back.SameContent(&d.Knowledge) {
		t.Errorf("round trip changed the entry:\n got %+v\nwant %+v", back.Knowledge, d.Knowledge)
	}
}
