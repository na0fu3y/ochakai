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

// Hierarchical IDs become nested directories, and every level gets its own
// index.md (design doc 0005).
func TestBundleNestedDirectories(t *testing.T) {
	now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	entries := []domain.Knowledge{
		{Type: domain.TypeQuery, ID: "sales/monthly-revenue", Title: "月次売上",
			CreatedBy: domain.Actor{Kind: "human", Name: "na0"}, Status: domain.StatusDraft, UpdatedAt: now},
		{Type: domain.TypeQuery, ID: "sales/refunds/by-region", Title: "地域別返金",
			CreatedBy: domain.Actor{Kind: "human", Name: "na0"}, Status: domain.StatusDraft, UpdatedAt: now},
		{Type: "runbook", ID: "restore", Title: "リストア手順",
			CreatedBy: domain.Actor{Kind: "human", Name: "na0"}, Status: domain.StatusDraft, UpdatedAt: now},
	}
	files, err := Bundle(entries)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"query/sales/monthly-revenue.md", "query/sales/refunds/by-region.md", "runbook/restore.md",
		"index.md", "query/index.md", "query/sales/index.md", "query/sales/refunds/index.md", "runbook/index.md",
	} {
		if _, ok := files[path]; !ok {
			t.Errorf("bundle missing %s", path)
		}
	}
	if idx := string(files["query/sales/index.md"]); !strings.Contains(idx, "[refunds/](refunds/index.md) - 1 concept") ||
		!strings.Contains(idx, "[月次売上](monthly-revenue.md)") {
		t.Errorf("nested index wrong:\n%s", idx)
	}
	// Recommended types come first in the root index, free types after.
	root := string(files["index.md"])
	if strings.Index(root, "query/") > strings.Index(root, "runbook/") {
		t.Errorf("root index order wrong:\n%s", root)
	}
}

// Bundle → FromBundle is lossless for everything a bundle carries
// (server-owned provenance intentionally travels outside the payload).
func TestBundleRoundTrip(t *testing.T) {
	now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	want := []domain.Knowledge{
		{Type: "data-contract", ID: "orders", Title: "注文契約", Status: domain.StatusDraft,
			CreatedBy: domain.Actor{Kind: "human", Name: "na0"},
			Attrs:     map[string]any{AttrOKFType: "Data Contract", "owner": "sales"}, UpdatedAt: now},
		{Type: domain.TypeQuery, ID: "sales/monthly-revenue", Title: "月次売上", Status: domain.StatusVerified,
			Description: "月ごとの売上",
			CreatedBy:   domain.Actor{Kind: "human", Name: "na0"},
			Tags:        []string{"sales"},
			Links:       []domain.Link{{Rel: "measures", Target: "metric/revenue"}},
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

// A foreign OKF bundle — free type directories, spelled frontmatter types,
// non-markdown extras — imports with structure preserved: path wins, the
// original type spelling survives in attrs, the reserved index.md / log.md
// files are skipped silently and non-markdown files with a report.
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
	if len(skipped) != 1 || !strings.Contains(skipped[0], "viz.html") {
		t.Errorf("skipped = %v, want only viz.html", skipped)
	}
	byURI := map[string]domain.Knowledge{}
	for _, e := range entries {
		byURI[e.URI()] = e
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %v", byURI)
	}
	users := byURI["ochakai://tables/users"]
	if users.Title != "users" || users.Attrs[AttrOKFType] != "Table" {
		t.Errorf("tables/users: %+v", users)
	}
	if _, ok := byURI["ochakai://datasets/ga4"]; !ok {
		t.Errorf("datasets/ga4 missing: %v", byURI)
	}
	if notes, ok := byURI["ochakai://notes/2026/q3"]; !ok || notes.Title != "Q3 notes" {
		t.Errorf("hierarchical typeless concept missing: %v", byURI)
	}

	// Re-export writes the original spelling back at the original path.
	out, err := Bundle(entries)
	if err != nil {
		t.Fatal(err)
	}
	if doc := string(out["tables/users.md"]); !strings.Contains(doc, "type: Table") {
		t.Errorf("re-export lost spelling:\n%s", doc)
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
	if aov.Attrs["resource"] != "https://example.com/metrics/aov" ||
		aov.Attrs["owner"] != "analytics-team" ||
		aov.Attrs[AttrOKFType] != "Reference" {
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

// `tar czf bundle.tar.gz ga4/` wraps the bundle in one directory; without
// unwrapping, "ga4" would silently become every entry's type.
func TestStripWrapper(t *testing.T) {
	wrapped := map[string][]byte{
		"./ga4/index.md":       []byte("# ga4\n"),
		"ga4/tables/events.md": []byte("---\ntype: BigQuery Table\n---\n"),
		"._ga4":                []byte("apple double"), // macOS tar noise must not defeat detection
		"ga4/.DS_Store":        []byte("finder noise"),
	}
	files, root := StripWrapper(wrapped)
	if root != "ga4" {
		t.Fatalf("root = %q, want ga4", root)
	}
	if _, ok := files["tables/events.md"]; !ok {
		t.Errorf("paths not unwrapped: %v", files)
	}
	if len(files) != 2 {
		t.Errorf("hidden files must be dropped on unwrap: %v", files)
	}
	if entries, _, skipped := FromBundle(wrapped); len(skipped) != 0 || len(entries) != 1 {
		t.Errorf("hidden files must be skipped silently: skipped=%v entries=%d", skipped, len(entries))
	}

	// A bundle rooted at the top (root index.md is a top-level file) is
	// left alone.
	flat := map[string][]byte{
		"index.md":         []byte("# bundle\n"),
		"tables/events.md": []byte("---\ntype: BigQuery Table\n---\n"),
	}
	if _, root := StripWrapper(flat); root != "" {
		t.Errorf("flat bundle wrongly unwrapped (root %q)", root)
	}

	// Two top-level directories: nothing to unwrap.
	two := map[string][]byte{
		"metric/a.md": []byte("x"),
		"table/b.md":  []byte("x"),
	}
	if _, root := StripWrapper(two); root != "" {
		t.Errorf("multi-dir bundle wrongly unwrapped (root %q)", root)
	}
}
