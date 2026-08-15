package okf

import (
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// bundle renders entries into a path→content map, the inverse of
// FromBundle. Export streams a bundle a file at a time (Indexes, then
// Document per entry, into a TarGzWriter) and never materializes one, so
// this lives here: the round-trip tests want the whole map at once, and
// nothing in the server does.
// knowledgeOf projects parsed documents back to the knowledge they carry,
// so a round-trip test can hand FromBundle's output straight to bundle.
func knowledgeOf(docs []Doc) []domain.Knowledge {
	out := make([]domain.Knowledge, len(docs))
	for i := range docs {
		out[i] = docs[i].Knowledge
	}
	return out
}

func bundle(t *testing.T, entries []domain.Knowledge) map[string][]byte {
	t.Helper()
	files := Indexes(entries, nil)
	for i := range entries {
		doc, err := Document(&entries[i])
		if err != nil {
			t.Fatalf("render %s: %v", entries[i].URI(), err)
		}
		files[entries[i].ID+".md"] = doc
	}
	return files
}

// writeTarGz collects a whole bundle into one archive, in sorted path
// order. Export adds files as it produces them; only the tests need the
// collected form.
func writeTarGz(t *testing.T, w io.Writer, files map[string][]byte, modTime time.Time) {
	t.Helper()
	paths := slices.Sorted(maps.Keys(files))
	tgz := NewTarGzWriter(w, modTime)
	for _, p := range paths {
		if err := tgz.Add(p, files[p]); err != nil {
			t.Fatalf("add %s: %v", p, err)
		}
	}
	if err := tgz.Close(); err != nil {
		t.Fatal(err)
	}
}

// The id's segments become nested directories, and every level gets its
// own index.md. The layout is the user's (design doc 0016): a domain
// directory and a type directory sit side by side as equals.
func TestBundleNestedDirectories(t *testing.T) {
	now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	entries := []domain.Knowledge{
		{Type: domain.TypeComputations, ID: "queries/sales/monthly-revenue", Title: "月次売上",
			CreatedBy: domain.Actor{Kind: "human", Name: "na0"}, Status: domain.StatusDraft, UpdatedAt: now},
		{Type: domain.TypeComputations, ID: "queries/sales/refunds/by-region", Title: "地域別返金",
			CreatedBy: domain.Actor{Kind: "human", Name: "na0"}, Status: domain.StatusDraft, UpdatedAt: now},
		{Type: "runbook", ID: "ops/restore", Title: "リストア手順",
			CreatedBy: domain.Actor{Kind: "human", Name: "na0"}, Status: domain.StatusDraft, UpdatedAt: now},
	}
	files := bundle(t, entries)
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
	// the root (design doc 0017 §4.8): "ops" sorts before "query" even
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
		{Type: domain.TypeComputations, ID: "queries/sales/monthly-revenue", Title: "月次売上", Status: domain.StatusStable,
			Description: "月ごとの売上",
			CreatedBy:   domain.Actor{Kind: "human", Name: "na0"},
			Tags:        []string{"sales"},
			Attrs:       map[string]any{"sql": "SELECT 1"},
			Body:        "本文。", UpdatedAt: now},
	}
	files := bundle(t, want)
	got, _, _, skipped, _ := FromBundle(files)
	if len(skipped) != 0 {
		t.Fatalf("skipped %v", skipped)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d", len(got), len(want))
	}
	for i := range want {
		w, g := want[i], got[i]
		// The bundle was written by this instance, so the trust family in
		// it is its own observation coming home rather than a claim — the
		// call a write path makes with the entry at hand (design doc 0046
		// §2.2).
		asStored(&g.Knowledge, &w)
		if g.Type != w.Type || g.ID != w.ID || g.Title != w.Title || g.Description != w.Description ||
			g.Status != w.Status || g.Body != w.Body {
			t.Errorf("entry %d envelope: got %+v, want %+v", i, g, w)
		}
		// Import reads no links; the write path derives them from the body
		// (design doc 0024).
		if g.Links != nil {
			t.Errorf("entry %d: import invented links %v", i, g.Links)
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
	entries, _, _, skipped, _ := FromBundle(files)
	if len(skipped) != 0 {
		t.Fatalf("skipped %v", skipped)
	}
	if len(entries) != 1 || entries[0].ID != "insights/サンプル" || entries[0].Title != "" {
		t.Fatalf("entries = %+v, want one titleless insights/サンプル", entries)
	}
	out := bundle(t, knowledgeOf(entries))
	doc := string(out["insights/サンプル.md"])
	if strings.Contains(doc, "title:") {
		t.Errorf("re-export must omit the empty title:\n%s", doc)
	}
	if idx := string(out["insights/index.md"]); !strings.Contains(idx, "[サンプル](サンプル.md)") {
		t.Errorf("index link must fall back to the filename:\n%s", idx)
	}
}

// macOS filesystems hand paths back NFD-decomposed; the bundle path,
// the body link, and the file file must all converge on the same
// NFC spelling (design doc 0022).
func TestFromBundleNFCPaths(t *testing.T) {
	nfd := "サンプル" // "サンプル" with the handakuten decomposed
	files := map[string][]byte{
		"insights/" + nfd + ".md": []byte("---\ntype: Insight\ntitle: t\n---\n\n" +
			"![chart](" + nfd + "/chart.png)\n"),
		"insights/" + nfd + "/chart.png": pngBytes(),
	}
	entries, atts, _, skipped, _ := FromBundle(files)
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
// the reserved index.md / log.md files are skipped silently, and a
// non-markdown file or a typeless document — neither of which is a
// concept — is kept as a file of the bundle (design doc 0046 §3.2).
// The typeless one cannot keep a concept's address, so it arrives at
// `.markdown` and the note says so (design doc 0100 §3).
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
	entries, _, loose, skipped, notes := FromBundle(files)
	// A document with no type is not a concept (SPEC §11) and not a
	// mistake either: it is a markdown file, and it goes back — at a
	// spelling that does not claim to be a concept, since `.md` is an
	// address SPEC §3.1 reserves for one. The HTML nobody references
	// keeps its own name: it never claimed anything.
	var kept []string
	for _, f := range loose {
		kept = append(kept, f.Path)
	}
	if !reflect.DeepEqual(kept, []string{"notes/2026/q3.markdown", "viz.html"}) {
		t.Errorf("kept = %v, want the renamed typeless document and viz.html", kept)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, want nothing", skipped)
	}
	// Renaming is a loss of a kind — the bundle that comes back is not
	// byte-identical to the one that went in — so it is said, not done
	// quietly. --strict reads the note as the refusal it is.
	var renamed string
	for _, n := range notes {
		if strings.Contains(n, "notes/2026/q3.md") {
			renamed = n
		}
	}
	if !strings.Contains(renamed, "notes/2026/q3.markdown") {
		t.Errorf("note about the renamed file = %q, want it to name the address it landed at", renamed)
	}
	byURI := map[string]domain.Knowledge{}
	for _, e := range entries {
		byURI[e.URI()] = e.Knowledge
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
	out := bundle(t, knowledgeOf(entries))
	if doc := string(out["tables/users.md"]); !strings.Contains(doc, "type: Table\n") {
		t.Errorf("re-export did not reproduce the authored type:\n%s", doc)
	}
}

// testdata/foreign-bundle mirrors the shape of the OKF sample bundles
// (GoogleCloudPlatform/knowledge-catalog): nested type directories,
// top-level resource/timestamp/custom keys, reserved index.md and log.md,
// a non-markdown viz.html, and — since design doc 0079 — a computation
// whose producer wrote unquoted scalars where ochakai wanted strings and
// an executor with no receipt. Importing it must keep every frontmatter
// key a bundle owns and re-export it at the same path with the same
// spelling, and every one of those documents must stay a *concept*: SPEC
// §11 makes unparseable frontmatter and a missing type the only reasons
// to refuse one.
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

	entries, _, loose, skipped, notes := FromBundle(files)
	if len(loose) != 1 || loose[0].Path != "viz.html" {
		t.Errorf("loose = %+v, want only viz.html", loose)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, want nothing", skipped)
	}
	if len(entries) != 4 {
		t.Fatalf("entries = %d, want 4", len(entries))
	}
	byURI := map[string]domain.Knowledge{}
	for _, e := range entries {
		byURI[e.URI()] = e.Knowledge
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

	out := bundle(t, knowledgeOf(entries))
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

	// The producer wrote `title: 2026`, `description: 3.14`, `status: 3`
	// and `tags: [1, revenue]`. SPEC §4.1 gives none of those keys a YAML
	// type and §11 forbids refusing a document for one, so each is read
	// as the text it was written as, with a note — and the document stays
	// a concept instead of being demoted to a loose markdown file, which
	// is what happened silently before design doc 0079.
	q := byURI["ochakai://computations/quarterly_revenue"]
	if q.Type != domain.TypeComputations {
		t.Fatalf("computations/quarterly_revenue was not read as a concept: %+v", q)
	}
	if q.Title != "2026" || q.Description != "3.14" || !slices.Equal(q.Tags, []string{"1", "revenue"}) {
		t.Errorf("loose scalars read as %q / %q / %v", q.Title, q.Description, q.Tags)
	}
	// SPEC §10.2 marks only runtime REQUIRED and describes receipt with
	// no requirement word; §11 forbids rejecting a concept for a missing
	// optional field, so an executor naming code to run is a contract
	// whether or not it declares the evidence.
	if q.Executor == nil || q.Executor.Resource != "../references/skills/run-on-bq.md" {
		t.Errorf("executor without a receipt was dropped: %+v", q.Executor)
	}
	for _, want := range []string{
		`title is not a string; read as "2026"`,
		`description is not a string; read as "3.14"`,
		`tags[0] is not a string; read as "1"`,
		// The reserved files are regenerated rather than stored (SPEC
		// §3.1), and this is the note that stops a hand-written one
		// disappearing without a word.
		"index.md is a filename OKF reserves",
		"log.md is a filename OKF reserves",
	} {
		if !slices.ContainsFunc(notes, func(n string) bool { return strings.Contains(n, want) }) {
			t.Errorf("no note mentions %q: %v", want, notes)
		}
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
	entries, _, _, skipped, _ := FromBundle(wrapped)
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

// What enters the bundle leaves it (design doc 0046 §3.2). The reading
// that FromBundle used to take — anything that is not a concept and not
// an file is reported and dropped — meant a producer's bundle came
// back smaller than it went in, which is the one thing a bundle store
// must not do. The only paths still reported are the ones that cannot be
// stored at all.
func TestFromBundleKeepsWhatItCannotAttribute(t *testing.T) {
	files := map[string][]byte{
		// The concepts, and a file each of them claims.
		"metrics/revenue.md":        []byte("---\ntype: Metric\ntitle: 売上\n---\n\n![chart](revenue/chart.png)\n"),
		"metrics/revenue/chart.png": pngBytes(),

		// Nobody's, all of it kept.
		"seeds/orders.csv":  []byte("a,b\n1,2\n"),
		"notes/meeting.md":  []byte("# 議事録\n\ntype なし。\n"),      // not a concept (SPEC §11); lands at .markdown
		"vendor/report.zip": append([]byte("PK\x03\x04"), 0, 0), // never was on any allowlist
		"diagrams/er.svg":   []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`),

		// Generated, and hidden: skipped in silence, as before.
		"index.md":          []byte("# bundle\n"),
		"metrics/log.md":    []byte("# log\n"),
		".git/config":       []byte("[core]\n"),
		"._AppleDouble":     []byte("junk"),
		"metrics/.DS_Store": []byte("junk"),

		// The one thing that cannot be kept: nothing to store.
		"empty.txt": {},
	}
	entries, atts, loose, skipped, _ := FromBundle(files)
	if len(entries) != 1 || len(atts) != 1 {
		t.Fatalf("entries = %d, files = %d, want 1 and 1", len(entries), len(atts))
	}
	var kept []string
	for _, f := range loose {
		kept = append(kept, f.Path)
	}
	// notes/meeting.md keeps its bytes and loses its claim to a concept's
	// address (design doc 0100 §3); everything else keeps its own name.
	want := []string{"diagrams/er.svg", "notes/meeting.markdown", "seeds/orders.csv", "vendor/report.zip"}
	if !reflect.DeepEqual(kept, want) {
		t.Errorf("kept = %v, want %v", kept, want)
	}
	if len(skipped) != 1 || !strings.Contains(skipped[0], "empty.txt") {
		t.Errorf("skipped = %v, want only empty.txt", skipped)
	}
	// Every path the bundle carried is accounted for exactly once.
	seen := map[string]int{}
	for _, e := range entries {
		seen[e.ID+".md"]++
	}
	for _, a := range atts {
		seen[a.Path]++
	}
	for _, f := range loose {
		seen[f.Path]++
	}
	for p, n := range seen {
		if n != 1 {
			t.Errorf("%s was taken %d times", p, n)
		}
	}
}
