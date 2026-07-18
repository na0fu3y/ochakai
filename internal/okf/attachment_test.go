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
func TestFromBundleAttachments(t *testing.T) {
	png := pngBytes()
	files := map[string][]byte{
		"insight/revenue-reading.md": []byte("---\ntype: Insight\ntitle: 売上の読み方\n---\n\n" +
			"正常時は ![週次の正常形](revenue-reading/weekly.png) のような形。\n" +
			"外部画像 ![ext](https://example.com/x.png) は無視される。\n"),
		"insight/revenue-reading/weekly.png": png,
		"table/orders.md": []byte("---\ntype: Table\ntitle: orders\n---\n\n" +
			"ER 図: ![er](/diagrams/er.png)\n"), // bundle-root absolute (SPEC §5 recommended form)
		"diagrams/er.png":  png,
		"unreferenced.png": png,
		"term/broken.md": []byte("---\ntype: Glossary Term\ntitle: x\n---\n\n" +
			"![missing](nowhere/gone.png)\n"), // broken links are tolerated, never an error
	}
	entries, atts, skipped := FromBundle(files)
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(entries))
	}
	got := map[string]string{}
	for _, a := range atts {
		got[string(a.Type)+"/"+a.ID+"/"+a.Name] = a.Path
	}
	want := map[string]string{
		"insight/revenue-reading/weekly.png": "insight/revenue-reading/weekly.png",
		"table/orders/er.png":                "diagrams/er.png",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("attachments = %v, want %v", got, want)
	}
	// The unreferenced image stays in the skip report; the referenced ones
	// must not appear there.
	if len(skipped) != 1 || !strings.Contains(skipped[0], "unreferenced.png") {
		t.Errorf("skipped = %v, want only unreferenced.png", skipped)
	}
}

// A concept at the bundle root resolves relative links against the root,
// not against the canonical type directory it will move to on re-export.
func TestFromBundleRootConceptAttachment(t *testing.T) {
	files := map[string][]byte{
		"overview.md":   []byte("---\ntype: Insight\ntitle: overview\n---\n\n![chart](img/chart.png)\n"),
		"img/chart.png": pngBytes(),
	}
	_, atts, _ := FromBundle(files)
	if len(atts) != 1 || atts[0].Path != "img/chart.png" || atts[0].ID != "overview" {
		t.Fatalf("atts = %+v", atts)
	}
}

// Markdown links must not attach concept documents, oversized files, or
// files that resolve nowhere; orphans outside any entry's namespace stay
// in the skip report.
func TestFromBundleAttachmentAllowlist(t *testing.T) {
	files := map[string][]byte{
		"term/a.md": []byte("---\ntype: Glossary Term\ntitle: a\n---\n\n" +
			"[別エントリ](/term/b.md) と [ログ](data.csv) と ![big](big.png)\n"),
		"term/b.md": []byte("---\ntype: Glossary Term\ntitle: b\n---\n"),
		"data.csv":  []byte("a,b,c"),
		"big.png":   append(pngBytes(), make([]byte, domain.MaxAttachmentSize)...),
	}
	_, atts, skipped := FromBundle(files)
	if len(atts) != 0 {
		t.Errorf("nothing should attach, got %+v", atts)
	}
	// data.csv and big.png stay reported; term/b.md imported as an entry.
	if len(skipped) != 2 {
		t.Errorf("skipped = %v", skipped)
	}
}

// Non-image files in the allowlist (design doc 0013) attach like images:
// by body reference wherever they sit, and PDFs and plain text both pass
// the sniffer.
func TestFromBundleFileAttachments(t *testing.T) {
	files := map[string][]byte{
		"table/orders.md": []byte("---\ntype: Table\ntitle: orders\n---\n\n" +
			"シード: [seeds](/data/seeds.txt)、仕様: [spec](orders/spec.pdf)\n"),
		"data/seeds.txt":        []byte("https://example.com/schema\nhttps://example.com/docs\n"),
		"table/orders/spec.pdf": []byte("%PDF-1.7 fake pdf body"),
	}
	_, atts, skipped := FromBundle(files)
	got := map[string]string{}
	for _, a := range atts {
		got[string(a.Type)+"/"+a.ID+"/"+a.Name] = a.Path
	}
	want := map[string]string{
		"table/orders/seeds.txt": "data/seeds.txt",
		"table/orders/spec.pdf":  "table/orders/spec.pdf",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("attachments = %v, want %v", got, want)
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
		"table/orders.md": []byte("---\ntype: Table\ntitle: orders\n---\n\n" +
			"![er](orders/er.png)\n"), // referenced: claims table/orders/er.png
		"table/orders/er.png":    pngBytes(),
		"table/orders/seeds.txt": []byte("seed data, referenced by no body\n"),
		"orphan/seeds.txt":       []byte("no entry named orphan\n"),
	}
	entries, atts, skipped := FromBundle(files)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	got := map[string]string{}
	for _, a := range atts {
		got[string(a.Type)+"/"+a.ID+"/"+a.Name] = a.Path
	}
	want := map[string]string{
		"table/orders/er.png":    "table/orders/er.png",
		"table/orders/seeds.txt": "table/orders/seeds.txt",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("attachments = %v, want %v", got, want)
	}
	if len(skipped) != 1 || !strings.Contains(skipped[0], "orphan/seeds.txt") {
		t.Errorf("skipped = %v, want only orphan/seeds.txt", skipped)
	}
}

// Namespace attribution follows hierarchical IDs: the entry's directory
// is its full canonical path, not just one segment deep.
func TestFromBundleNamespaceHierarchicalID(t *testing.T) {
	files := map[string][]byte{
		"query/sales/monthly.md":           []byte("---\ntype: Golden Query\ntitle: monthly\n---\n"),
		"query/sales/monthly/expected.txt": []byte("month,revenue\n2026-01,100\n"),
	}
	_, atts, skipped := FromBundle(files)
	if len(atts) != 1 || atts[0].ID != "sales/monthly" || atts[0].Name != "expected.txt" {
		t.Fatalf("atts = %+v", atts)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, want none", skipped)
	}
}

func TestAttachmentPath(t *testing.T) {
	native := &domain.Attachment{Name: "weekly.png"}
	if p := AttachmentPath(domain.TypeInsight, "sales/revenue-reading", native); p != "insight/sales/revenue-reading/weekly.png" {
		t.Errorf("canonical path = %q", p)
	}
	foreign := &domain.Attachment{Name: "er.png", OKFPath: "diagrams/er.png"}
	if p := AttachmentPath(domain.TypeTable, "orders", foreign); p != "diagrams/er.png" {
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
		{"insight", "revenue-reading/weekly.png", "insight/revenue-reading/weekly.png", true},
		{"insight", "/diagrams/er.png", "diagrams/er.png", true},
		{".", "img/chart.png", "img/chart.png", true},
		{"insight", "../shared.png", "shared.png", true}, // stays inside the bundle
		{"insight", "../../escape.png", "", false},
		{"insight", "https://example.com/x.png", "", false},
		{"insight", "data:image/png;base64,xxx", "", false},
		{"insight", "#section", "", false},
	} {
		got, ok := resolveTarget(tc.docDir, tc.target)
		if ok != tc.ok || got != tc.want {
			t.Errorf("resolveTarget(%q, %q) = %q, %v; want %q, %v", tc.docDir, tc.target, got, ok, tc.want, tc.ok)
		}
	}
}
