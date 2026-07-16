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
			Type: domain.TypeInsight, ID: "revenue-seasonality",
			Title: "売上の季節性", Description: "12月は繁忙期",
			Tags: []string{"sales"}, Status: domain.StatusVerified,
			CreatedBy:  domain.Actor{Kind: "agent", Name: "claude-code"},
			VerifiedBy: &domain.Actor{Kind: "human", Name: "na0"}, VerifiedAt: &verifiedAt,
			Links:     []domain.Link{{Rel: "about", Target: "metric/revenue"}},
			Attrs:     map[string]any{"kind": "seasonality"},
			Body:      "12月は+40%が通常。",
			UpdatedAt: verifiedAt,
		},
		{
			Type: domain.TypeTable, ID: "orders",
			Title: "orders", Status: domain.StatusDraft,
			CreatedBy: domain.Actor{Kind: "human", Name: "na0"},
			Attrs:     map[string]any{"source": "myproject.shop.orders", "model": "sales_analytics"},
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
	if !strings.Contains(parts[2], "12月は+40%が通常。") {
		t.Errorf("body missing:\n%s", parts[2])
	}
	if !strings.Contains(parts[2], "- about: [metric/revenue](/metric/revenue.md)") {
		t.Errorf("links section missing:\n%s", parts[2])
	}
}

func TestDocumentRejectedProvenance(t *testing.T) {
	rejectedAt := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	k := domain.Knowledge{
		Type: domain.TypeInsight, ID: "dup-insight",
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
	for _, path := range []string{"insight/revenue-seasonality.md", "table/orders.md", "index.md", "insight/index.md", "table/index.md"} {
		if _, ok := files[path]; !ok {
			t.Errorf("bundle missing %s (have %d files)", path, len(files))
		}
	}
	if !strings.Contains(string(files["insight/index.md"]), "[売上の季節性](/insight/revenue-seasonality.md)") {
		t.Errorf("type index missing concept link:\n%s", files["insight/index.md"])
	}
	if !strings.Contains(string(files["index.md"]), "[insight/](/insight/index.md)") {
		t.Errorf("root index missing type link:\n%s", files["index.md"])
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
