package okf

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/na0fu3y/ochakai/internal/domain"
)

func sample() []domain.Knowledge {
	verifiedAt := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	return []domain.Knowledge{
		{
			Type: domain.TypeInsights, ID: "insights/revenue-seasonality",
			Title: "売上の季節性", Description: "12月は繁忙期",
			Tags: []string{"sales"}, Status: domain.StatusVerified,
			CreatedBy:  domain.Actor{Kind: "agent", Name: "claude-code"},
			VerifiedBy: &domain.Actor{Kind: "human", Name: "na0"}, VerifiedAt: &verifiedAt,
			Attrs:     map[string]any{"kind": "seasonality"},
			Body:      "12月は+40%が通常。[売上](/metrics/revenue.md) の話である。",
			UpdatedAt: verifiedAt,
		},
		{
			Type: domain.TypeTables, ID: "tables/orders",
			Title: "orders", Status: domain.StatusDraft,
			CreatedBy: domain.Actor{Kind: "human", Name: "na0"},
			Resource:  "myproject.shop.orders",
			Attrs:     map[string]any{"model": "sales_analytics"},
			UpdatedAt: verifiedAt,
		},
	}
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
	for key, want := range map[string]any{
		"type":        "Insight",
		"title":       "売上の季節性",
		"status":      "verified",
		"created_by":  "agent:claude-code",
		"verified_by": "human:na0",
		"timestamp":   "2026-07-01T00:00:00Z",
	} {
		if fm[key] != want {
			t.Errorf("frontmatter %s = %v, want %v", key, fm[key], want)
		}
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
		Title: "重複した知見", Status: domain.StatusRejected,
		StatusNote: "revenue-seasonality と重複",
		CreatedBy:  domain.Actor{Kind: "agent", Name: "claude-code"},
		RejectedBy: &domain.Actor{Kind: "human", Name: "na0"}, RejectedAt: &rejectedAt,
		UpdatedAt: rejectedAt,
	}
	doc, err := Document(&k)
	if err != nil {
		t.Fatal(err)
	}
	s := string(doc)
	for _, want := range []string{
		"status: rejected",
		"status_note: revenue-seasonality と重複",
		"rejected_by: human:na0",
		`rejected_at: "2026-07-16T00:00:00Z"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("document missing %q:\n%s", want, s)
		}
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
	files, err := Bundle(sample())
	if err != nil {
		t.Fatal(err)
	}
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
	if !strings.HasPrefix(root, "---\nokf_version: \"0.1\"\n---\n") {
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
	files, err := Bundle(sample())
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := WriteTarGz(&buf, files, time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
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
