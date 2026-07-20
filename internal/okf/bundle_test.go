package okf

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// The id's segments become nested directories, and every level gets its
// own index.md. The layout is the user's (design doc 0016): a domain
// directory and a type directory sit side by side as equals.
func TestBundleNestedDirectories(t *testing.T) {
	now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	entries := []domain.Knowledge{
		{Type: domain.TypeQueries, ID: "queries/sales/monthly-revenue", Title: "月次売上",
			CreatedBy: domain.Actor{Kind: "human", Name: "na0"}, Status: domain.StatusDraft, UpdatedAt: now},
		{Type: domain.TypeQueries, ID: "queries/sales/refunds/by-region", Title: "地域別返金",
			CreatedBy: domain.Actor{Kind: "human", Name: "na0"}, Status: domain.StatusDraft, UpdatedAt: now},
		{Type: "runbook", ID: "ops/restore", Title: "リストア手順",
			CreatedBy: domain.Actor{Kind: "human", Name: "na0"}, Status: domain.StatusDraft, UpdatedAt: now},
	}
	files, err := Bundle(entries)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"queries/sales/monthly-revenue.md", "queries/sales/refunds/by-region.md", "ops/restore.md",
		"index.md", "queries/index.md", "queries/sales/index.md", "queries/sales/refunds/index.md", "ops/index.md",
	} {
		if _, ok := files[path]; !ok {
			t.Errorf("bundle missing %s", path)
		}
	}
	if idx := string(files["queries/sales/index.md"]); !strings.Contains(idx, "[refunds/](refunds/index.md) - 1 concept") ||
		!strings.Contains(idx, "[月次売上](monthly-revenue.md)") {
		t.Errorf("nested index wrong:\n%s", idx)
	}
	// Every index level is alphabetical — no recommended-type priority at
	// the root (design doc 0016 §4.8): "ops" sorts before "query" even
	// though query is a built-in type directory.
	root := string(files["index.md"])
	if strings.Index(root, "ops/") > strings.Index(root, "queries/") {
		t.Errorf("root index must be alphabetical:\n%s", root)
	}
}

// Bundle → FromBundle is lossless for everything a bundle carries
// (server-owned provenance intentionally travels outside the payload).
func TestBundleRoundTrip(t *testing.T) {
	now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	want := []domain.Knowledge{
		// A free type keeps its authored spelling with no preservation
		// attr: the type is stored verbatim (design doc 0023).
		{Type: "Data Contract", ID: "contracts/orders", Title: "注文契約", Status: domain.StatusDraft,
			CreatedBy: domain.Actor{Kind: "human", Name: "na0"},
			Attrs:     map[string]any{"owner": "sales"}, UpdatedAt: now},
		{Type: domain.TypeQueries, ID: "queries/sales/monthly-revenue", Title: "月次売上", Status: domain.StatusVerified,
			Description: "月ごとの売上",
			CreatedBy:   domain.Actor{Kind: "human", Name: "na0"},
			Tags:        []string{"sales"},
			Links:       []domain.Link{{Rel: "measures", Target: "metrics/revenue"}},
			Attrs:       map[string]any{"sql": "SELECT 1"},
			Body:        "本文。", UpdatedAt: now},
	}
	files, err := Bundle(want)
	if err != nil {
		t.Fatal(err)
	}
	got, _, skipped := FromBundle(files)
	if len(skipped) != 0 {
		t.Fatalf("skipped %v", skipped)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d", len(got), len(want))
	}
	for i := range want {
		w, g := want[i], got[i]
		if g.Type != w.Type || g.ID != w.ID || g.Title != w.Title || g.Description != w.Description ||
			g.Status != w.Status || g.Body != w.Body {
			t.Errorf("entry %d envelope: got %+v, want %+v", i, g, w)
		}
		if !reflect.DeepEqual(g.Links, w.Links) {
			t.Errorf("entry %d links = %v, want %v", i, g.Links, w.Links)
		}
		if !reflect.DeepEqual(g.Attrs, w.Attrs) {
			t.Errorf("entry %d attrs = %v, want %v", i, g.Attrs, w.Attrs)
		}
	}
}

// Title is optional (design doc 0022): a titleless document imports as
// a titleless entry and re-exports without a title line — the filename
// is the name, and the document round-trips unchanged. The generated
// index.md falls back to the filename for its link text.
func TestBundleTitleOptional(t *testing.T) {
	files := map[string][]byte{
		"insights/サンプル.md": []byte("---\ntype: Insight\n---\n\n本文。\n"),
	}
	entries, _, skipped := FromBundle(files)
	if len(skipped) != 0 {
		t.Fatalf("skipped %v", skipped)
	}
	if len(entries) != 1 || entries[0].ID != "insights/サンプル" || entries[0].Title != "" {
		t.Fatalf("entries = %+v, want one titleless insights/サンプル", entries)
	}
	out, err := Bundle(entries)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(out["insights/サンプル.md"])
	if strings.Contains(doc, "title:") {
		t.Errorf("re-export must omit the empty title:\n%s", doc)
	}
	if idx := string(out["insights/index.md"]); !strings.Contains(idx, "[サンプル](サンプル.md)") {
		t.Errorf("index link must fall back to the filename:\n%s", idx)
	}
}

// macOS filesystems hand paths back NFD-decomposed; the bundle path,
// the body link, and the attachment file must all converge on the same
// NFC spelling (design doc 0022).
func TestFromBundleNFCPaths(t *testing.T) {
	nfd := "サンプル" // "サンプル" with the handakuten decomposed
	files := map[string][]byte{
		"insights/" + nfd + ".md": []byte("---\ntype: Insight\ntitle: t\n---\n\n" +
			"![chart](" + nfd + "/chart.png)\n"),
		"insights/" + nfd + "/chart.png": pngBytes(),
	}
	entries, atts, skipped := FromBundle(files)
	if len(skipped) != 0 {
		t.Fatalf("skipped %v", skipped)
	}
	if len(entries) != 1 || entries[0].ID != "insights/サンプル" {
		t.Fatalf("entries = %+v, want the NFC id insights/サンプル", entries)
	}
	if len(atts) != 1 || atts[0].ID != "insights/サンプル" || atts[0].Name != "chart.png" {
		t.Fatalf("atts = %+v, want chart.png attributed to the NFC id", atts)
	}
}

// A foreign OKF bundle — free layout, spelled frontmatter types,
// non-markdown extras — imports with structure preserved: the path is the
// id verbatim, the frontmatter alone names the type (design doc 0016),
// the reserved index.md / log.md files are skipped silently, and
// non-markdown files and typeless documents are skipped with a report.
func TestFromBundleForeign(t *testing.T) {
	files := map[string][]byte{
		"index.md":         []byte("# my bundle\n"),
		"log.md":           []byte("# Update Log\n\n## 2026-05-22\n* **Creation**: initial import.\n"),
		"tables/index.md":  []byte("# tables\n"),
		"tables/log.md":    []byte("# tables log\n"),
		"tables/users.md":  []byte("---\ntype: Table\ntitle: users\n---\n\nUser table.\n"),
		"datasets/ga4.md":  []byte("---\ntype: dataset\ntitle: GA4\n---\n"),
		"notes/2026/q3.md": []byte("---\ntitle: Q3 notes\n---\n"), // no frontmatter type at all
		"viz.html":         []byte("<html></html>"),
	}
	entries, _, skipped := FromBundle(files)
	if len(skipped) != 2 || !strings.Contains(skipped[0], "notes/2026/q3.md") || !strings.Contains(skipped[1], "viz.html") {
		t.Errorf("skipped = %v, want the typeless document and viz.html", skipped)
	}
	byURI := map[string]domain.Knowledge{}
	for _, e := range entries {
		byURI[e.URI()] = e
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %v", byURI)
	}
	// There are no type aliases (design doc 0023 §3.5): a foreign
	// spelling is neither rejected nor rewritten, it simply is the type.
	// "Table" is a free type — distinct from the recommended "BigQuery
	// Table" — and the entry stays at tables/users either way.
	users := byURI["ochakai://tables/users"]
	if users.Title != "users" || users.Type != domain.Type("Table") || len(users.Attrs) != 0 {
		t.Errorf("tables/users: %+v", users)
	}
	if ga4, ok := byURI["ochakai://datasets/ga4"]; !ok || ga4.Type != domain.Type("dataset") {
		t.Errorf("datasets/ga4 missing or mistyped: %v", byURI)
	}

	// Import and export are identity on the type key: what came in as
	// "Table" goes back out as "Table", at the original path, with no
	// preservation attr in between.
	out, err := Bundle(entries)
	if err != nil {
		t.Fatal(err)
	}
	if doc := string(out["tables/users.md"]); !strings.Contains(doc, "type: Table\n") {
		t.Errorf("re-export did not reproduce the authored type:\n%s", doc)
	}
}

// testdata/foreign-bundle mirrors the shape of the OKF sample bundles
// (GoogleCloudPlatform/knowledge-catalog): nested type directories,
// top-level resource/timestamp/custom keys, reserved index.md and log.md,
// a non-markdown viz.html. Importing it must keep every frontmatter key a
// bundle owns and re-export it at the same path with the same spelling.
func TestFromBundleFixture(t *testing.T) {
	files := map[string][]byte{}
	root := filepath.Join("testdata", "foreign-bundle")
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		files[filepath.ToSlash(rel)] = data
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	entries, _, skipped := FromBundle(files)
	if len(skipped) != 1 || !strings.Contains(skipped[0], "viz.html") {
		t.Errorf("skipped = %v, want only viz.html", skipped)
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(entries))
	}
	byURI := map[string]domain.Knowledge{}
	for _, e := range entries {
		byURI[e.URI()] = e
	}
	aov := byURI["ochakai://references/metrics/avg_order_value"]
	if aov.Resource != "https://example.com/metrics/aov" {
		t.Errorf("references/metrics/avg_order_value resource = %q", aov.Resource)
	}
	// "Reference" is the built-in type's own spelling, so attrs carry
	// only the producer's extension keys — nothing is stashed there to
	// rebuild the type on the way out (design doc 0023).
	if aov.Attrs["owner"] != "analytics-team" || aov.Attrs["okf_type"] != nil {
		t.Errorf("references/metrics/avg_order_value attrs = %v", aov.Attrs)
	}
	if !strings.Contains(aov.Body, "SUM(order_total)") || !strings.Contains(aov.Body, "# Citations") {
		t.Errorf("body mangled:\n%s", aov.Body)
	}

	out, err := Bundle(entries)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(out["references/metrics/avg_order_value.md"])
	for _, want := range []string{
		"type: Reference",
		"resource: https://example.com/metrics/aov",
		"owner: analytics-team",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("re-export missing %q:\n%s", want, doc)
		}
	}
	if doc := string(out["tables/orders_.md"]); !strings.Contains(doc, "type: BigQuery Table") ||
		!strings.Contains(doc, "resource: https://bigquery.googleapis.com/v2/projects/demo/datasets/shop/tables/orders_*") {
		t.Errorf("tables/orders_ re-export wrong:\n%s", doc)
	}
}

// `tar czf ga4.tgz ga4/` wraps the bundle in one directory. There is no
// unwrapping (design doc 0017 §4.3): the wrapper is the bundle's
// namespace, so every entry imports under "ga4/" — and macOS tar noise
// (AppleDouble ._* files, .DS_Store) is still skipped silently.
func TestFromBundleWrappedArchive(t *testing.T) {
	wrapped := map[string][]byte{
		"./ga4/index.md":       []byte("# ga4\n"),
		"ga4/tables/events.md": []byte("---\ntype: BigQuery Table\ntitle: events\n---\n"),
		"._ga4":                []byte("apple double"),
		"ga4/.DS_Store":        []byte("finder noise"),
	}
	entries, _, skipped := FromBundle(wrapped)
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, want none (hidden files skip silently)", skipped)
	}
	if len(entries) != 1 || entries[0].ID != "ga4/tables/events" {
		t.Fatalf("entries = %+v, want the one entry under the ga4/ namespace", entries)
	}
	// "BigQuery Table" is the built-in table type itself — the wrapper
	// changes the namespace, never the type.
	if entries[0].Type != domain.TypeTables || len(entries[0].Attrs) != 0 {
		t.Errorf("type = %q attrs = %v", entries[0].Type, entries[0].Attrs)
	}
}
