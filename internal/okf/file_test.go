package okf

import (
	"reflect"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
)

func pngBytes() []byte { return append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 16)...) }

// Attribution is reference-driven (design doc 0008): an image belongs to
// the entry whose body links to it, wherever it sits — the canonical
// entry-named directory, a foreign _assets-style layout, or the bundle
// root. Relative and /-rooted link forms both resolve.
func TestFromBundleFilesResolveByReference(t *testing.T) {
	png := pngBytes()
	files := map[string][]byte{
		"insights/revenue-reading.md": []byte("---\ntype: Insight\ntitle: 売上の読み方\n---\n\n" +
			"正常時は ![週次の正常形](revenue-reading/weekly.png) のような形。\n" +
			"外部画像 ![ext](https://example.com/x.png) は無視される。\n"),
		"insights/revenue-reading/weekly.png": png,
		"tables/orders.md": []byte("---\ntype: Table\ntitle: orders\n---\n\n" +
			"ER 図: ![er](/diagrams/er.png)\n"), // bundle-root absolute (SPEC §5 recommended form)
		"diagrams/er.png":  png,
		"unreferenced.png": png,
		"terms/broken.md": []byte("---\ntype: Glossary Term\ntitle: x\n---\n\n" +
			"![missing](nowhere/gone.png)\n"), // broken links are tolerated, never an error
	}
	entries, atts, loose, skipped, _ := FromBundle(files)
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(entries))
	}
	got := map[string]string{}
	for _, a := range atts {
		got[a.ID+"/"+a.Name] = a.Path
	}
	want := map[string]string{
		"insights/revenue-reading/weekly.png": "insights/revenue-reading/weekly.png",
		"tables/orders/er.png":                "diagrams/er.png",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("files = %v, want %v", got, want)
	}
	// The unreferenced image belongs to no entry, and is kept anyway: it
	// is the bundle's file, at the path it arrived at (design doc 0046
	// §3.2). Nothing is skipped — a bundle that came in whole goes back
	// out whole.
	if len(loose) != 1 || loose[0].Path != "unreferenced.png" {
		t.Errorf("loose files = %v, want only unreferenced.png", loose)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, want nothing", skipped)
	}
}

// A concept at the bundle root resolves relative links against the root
// — where it also stays on re-export (design doc 0016).
func TestFromBundleRootConceptFile(t *testing.T) {
	files := map[string][]byte{
		"overview.md":   []byte("---\ntype: Insight\ntitle: overview\n---\n\n![chart](img/chart.png)\n"),
		"img/chart.png": pngBytes(),
	}
	_, atts, _, _, _ := FromBundle(files)
	if len(atts) != 1 || atts[0].Path != "img/chart.png" || atts[0].ID != "overview" {
		t.Fatalf("atts = %+v", atts)
	}
}

// Markdown links must not attach concept documents, oversized files, or
// files that resolve nowhere. A file nobody claims is kept as the
// bundle's own (design doc 0046 §3.2); only what cannot be stored at all
// is reported.
func TestFromBundleFileAllowlist(t *testing.T) {
	files := map[string][]byte{
		"terms/a.md": []byte("---\ntype: Glossary Term\ntitle: a\n---\n\n" +
			"[別エントリ](/terms/b.md) と [ログ](data.csv) と ![big](big.png)\n"),
		"terms/b.md": []byte("---\ntype: Glossary Term\ntitle: b\n---\n"),
		"data.csv":   []byte("a,b,c"),
		"big.png":    append(pngBytes(), make([]byte, domain.MaxFileSize)...),
	}
	_, atts, loose, skipped, _ := FromBundle(files)
	if len(atts) != 0 {
		t.Errorf("nothing should attach, got %+v", atts)
	}
	// terms/b.md is imported as an entry. data.csv resolves nowhere from
	// a.md's directory, so it is nobody's file and is kept as the
	// bundle's. big.png is past the size limit, which is the one thing
	// that cannot be kept — and is therefore the only thing reported.
	if len(loose) != 1 || loose[0].Path != "data.csv" {
		t.Errorf("loose = %+v, want only data.csv", loose)
	}
	if len(skipped) != 1 || !strings.Contains(skipped[0], "big.png") {
		t.Errorf("skipped = %v, want only big.png", skipped)
	}
}

// Non-image files in the allowlist (design doc 0013) attach like images:
// by body reference wherever they sit, and PDFs and plain text both pass
// the sniffer.
func TestFromBundleNonImageFileTypes(t *testing.T) {
	files := map[string][]byte{
		"tables/orders.md": []byte("---\ntype: Table\ntitle: orders\n---\n\n" +
			"シード: [seeds](/data/seeds.txt)、仕様: [spec](orders/spec.pdf)\n"),
		"data/seeds.txt":         []byte("https://example.com/schema\nhttps://example.com/docs\n"),
		"tables/orders/spec.pdf": []byte("%PDF-1.7 fake pdf body"),
	}
	_, atts, _, skipped, _ := FromBundle(files)
	got := map[string]string{}
	for _, a := range atts {
		got[a.ID+"/"+a.Name] = a.Path
	}
	want := map[string]string{
		"tables/orders/seeds.txt": "data/seeds.txt",
		"tables/orders/spec.pdf":  "tables/orders/spec.pdf",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("files = %v, want %v", got, want)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, want none", skipped)
	}
}

// Canonical-namespace attribution (design doc 0013): an unreferenced
// non-markdown file at "<type>/<id>/<name>" attaches to that entry; a
// referenced file of the same name is not double-attached; orphans
// elsewhere stay in the skip report.
func TestFromBundleNamespaceAttribution(t *testing.T) {
	files := map[string][]byte{
		"tables/orders.md": []byte("---\ntype: Table\ntitle: orders\n---\n\n" +
			"![er](orders/er.png)\n"), // referenced: claims tables/orders/er.png
		"tables/orders/er.png":    pngBytes(),
		"tables/orders/seeds.txt": []byte("seed data, referenced by no body\n"),
		"orphan/seeds.txt":        []byte("no entry named orphan\n"),
	}
	entries, atts, loose, skipped, _ := FromBundle(files)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	got := map[string]string{}
	for _, a := range atts {
		got[a.ID+"/"+a.Name] = a.Path
	}
	want := map[string]string{
		"tables/orders/er.png":    "tables/orders/er.png",
		"tables/orders/seeds.txt": "tables/orders/seeds.txt",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("files = %v, want %v", got, want)
	}
	// A file under no entry's namespace is attributed to nobody and kept
	// anyway, at the path it arrived at (design doc 0046 §3.2).
	if len(loose) != 1 || loose[0].Path != "orphan/seeds.txt" {
		t.Errorf("loose = %+v, want only orphan/seeds.txt", loose)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, want nothing", skipped)
	}
}

// Namespace attribution follows hierarchical IDs: the entry's directory
// is its full canonical path, not just one segment deep.
func TestFromBundleNamespaceHierarchicalID(t *testing.T) {
	files := map[string][]byte{
		"queries/sales/monthly.md":           []byte("---\ntype: Attested Computation\ntitle: monthly\n---\n"),
		"queries/sales/monthly/expected.txt": []byte("month,revenue\n2026-01,100\n"),
	}
	_, atts, _, skipped, _ := FromBundle(files)
	if len(atts) != 1 || atts[0].ID != "queries/sales/monthly" || atts[0].Name != "expected.txt" {
		t.Fatalf("atts = %+v", atts)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, want none", skipped)
	}
}

func TestFilePath(t *testing.T) {
	native := &domain.File{Name: "weekly.png"}
	if p := FilePath("insights/sales/revenue-reading", native); p != "insights/sales/revenue-reading/weekly.png" {
		t.Errorf("canonical path = %q", p)
	}
	foreign := &domain.File{Name: "er.png", Path: "diagrams/er.png"}
	if p := FilePath("tables/orders", foreign); p != "diagrams/er.png" {
		t.Errorf("foreign path = %q", p)
	}
}

func TestBodyLinkTargets(t *testing.T) {
	body := "a ![x](one.png) b [y](two/three.png \"title\") c ![z](https://e.com/a.png) d [w](#frag)"
	got := bodyLinkTargets(body)
	want := []string{"one.png", "two/three.png", "https://e.com/a.png", "#frag"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("targets = %v, want %v", got, want)
	}
}

func TestResolveTarget(t *testing.T) {
	for _, tc := range []struct {
		docDir, target, want string
		ok                   bool
	}{
		{"insights", "revenue-reading/weekly.png", "insights/revenue-reading/weekly.png", true},
		{"insights", "/diagrams/er.png", "diagrams/er.png", true},
		{".", "img/chart.png", "img/chart.png", true},
		{"insights", "../shared.png", "shared.png", true}, // stays inside the bundle
		{"insights", "../../escape.png", "", false},
		{"insights", "https://example.com/x.png", "", false},
		{"insights", "data:image/png;base64,xxx", "", false},
		{"insights", "#section", "", false},
	} {
		got, ok := resolveTarget(tc.docDir, tc.target)
		if ok != tc.ok || got != tc.want {
			t.Errorf("resolveTarget(%q, %q) = %q, %v; want %q, %v", tc.docDir, tc.target, got, ok, tc.want, tc.ok)
		}
	}
}
