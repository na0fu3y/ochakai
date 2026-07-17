package okf

import (
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
	if idx := string(files["query/sales/index.md"]); !strings.Contains(idx, "[refunds/](/query/sales/refunds/index.md) — 1 concepts") ||
		!strings.Contains(idx, "[月次売上](/query/sales/monthly-revenue.md)") {
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
	got, skipped := FromBundle(files)
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
// original type spelling survives in attrs, index.md and non-markdown
// files are skipped.
func TestFromBundleForeign(t *testing.T) {
	files := map[string][]byte{
		"index.md":         []byte("---\ntype: Index\n---\n\n# my bundle\n"),
		"tables/index.md":  []byte("---\ntype: Index\n---\n\n# tables\n"),
		"tables/users.md":  []byte("---\ntype: Table\ntitle: users\n---\n\nUser table.\n"),
		"datasets/ga4.md":  []byte("---\ntype: dataset\ntitle: GA4\n---\n"),
		"notes/2026/q3.md": []byte("---\ntitle: Q3 notes\n---\n"), // no frontmatter type at all
		"viz.html":         []byte("<html></html>"),
	}
	entries, skipped := FromBundle(files)
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
