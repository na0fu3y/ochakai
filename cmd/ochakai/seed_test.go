package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/okf"
)

// bundleOf reads back what seed printed, as a map from path to document.
func bundleOf(t *testing.T, raw []byte) map[string]string {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("the output is not a gzip: %v", err)
	}
	out := map[string]string{}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		out[h.Name] = string(body)
	}
	return out
}

func seedBundle(t *testing.T, cols []seedColumn, project, prefix string) map[string]string {
	t.Helper()
	buf := &bytes.Buffer{}
	if err := writeSeedBundle(buf, foldShards(gatherSeedTables(cols)), project, prefix); err != nil {
		t.Fatal(err)
	}
	return bundleOf(t, buf.Bytes())
}

// What comes out has to be an OKF bundle `ochakai import` can read — the
// whole design rests on seed writing nothing itself.
func TestSeedProducesAnImportableBundle(t *testing.T) {
	files := seedBundle(t, []seedColumn{
		{Schema: "shop", Table: "orders", Column: "order_id", DataType: "STRING", IsNullable: "NO"},
		{Schema: "shop", Table: "orders", Column: "customer_id", DataType: "STRING", IsNullable: "YES",
			Comment: "null for guest checkout"},
		{Schema: "shop", Table: "items", Column: "item_id", DataType: "STRING", IsNullable: "NO"},
	}, "demo-shop", "tables")

	if len(files) != 2 {
		t.Fatalf("bundle holds %d documents, want one per table: %v", len(files), files)
	}
	doc, ok := files["tables/shop/orders.md"]
	if !ok {
		t.Fatalf("no document at the table's own address: %v", files)
	}
	parsed, _, err := okf.Parse([]byte(doc))
	if err != nil {
		t.Fatalf("seed wrote a document the parser refuses: %v\n%s", err, doc)
	}
	k := parsed.Knowledge
	if got := string(k.Type); got != "BigQuery Table" {
		t.Errorf("type = %q", got)
	}
	if k.Title != "shop.orders" {
		t.Errorf("title = %q, want the qualified name", k.Title)
	}
	if k.Resource != "bigquery://demo-shop.shop.orders" {
		t.Errorf("resource = %q", k.Resource)
	}
	// A projection is a question, not an answer: it lands in the review
	// queue, and until somebody rules on it, it says so.
	if k.Lifecycle() != "draft" {
		t.Errorf("status = %q, want draft", k.Lifecycle())
	}
	for _, want := range []string{"`order_id`", "`customer_id`", "null for guest checkout", "STRING"} {
		if !strings.Contains(k.Body, want) {
			t.Errorf("the column table is missing %q:\n%s", want, k.Body)
		}
	}
	// The description is left empty on purpose — a sentence nobody wrote
	// is a sentence everybody would trust.
	if k.Description != "" {
		t.Errorf("description = %q, want none: the schema does not know what the table is for", k.Description)
	}
}

// Without --project the account is unknown, and a resource naming the
// wrong project is worse than one naming none.
func TestSeedLeavesTheResourceOutWithoutAProject(t *testing.T) {
	files := seedBundle(t, []seedColumn{
		{Schema: "shop", Table: "orders", Column: "order_id", DataType: "STRING"},
	}, "", "tables")
	doc := files["tables/shop/orders.md"]
	if strings.Contains(doc, "resource:") {
		t.Errorf("a resource was guessed:\n%s", doc)
	}
}

// Both shapes a warehouse client prints, and rows in whatever order they
// arrive.
func TestReadSeedColumns(t *testing.T) {
	array := `[{"table_schema":"shop","table_name":"orders","column_name":"a","data_type":"STRING"},
	           {"table_schema":"shop","table_name":"orders","column_name":"b","data_type":"INT64"}]`
	lines := `{"table_schema":"shop","table_name":"orders","column_name":"a","data_type":"STRING"}
{"table_schema":"shop","table_name":"orders","column_name":"b","data_type":"INT64"}`
	for name, in := range map[string]string{"array": array, "one object per line": lines} {
		cols, err := readSeedColumns(strings.NewReader(in))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(cols) != 2 || cols[0].Column != "a" || cols[1].DataType != "INT64" {
			t.Errorf("%s: parsed %+v", name, cols)
		}
	}
	if _, err := readSeedColumns(strings.NewReader("  ")); err == nil {
		t.Error("empty input should say so rather than print an empty bundle")
	}
}

// A table's columns keep the order the rows arrived in, because a listing
// ordered by ordinal_position is the order somebody reading the table
// expects — and the tables themselves come out sorted, so two runs of the
// same query produce the same bundle.
func TestSeedKeepsColumnOrderAndSortsTables(t *testing.T) {
	files := seedBundle(t, []seedColumn{
		{Schema: "shop", Table: "zeta", Column: "z1", DataType: "STRING"},
		{Schema: "shop", Table: "alpha", Column: "first", DataType: "STRING"},
		{Schema: "shop", Table: "alpha", Column: "second", DataType: "STRING"},
	}, "", "tables")
	doc := files["tables/shop/alpha.md"]
	if strings.Index(doc, "`first`") > strings.Index(doc, "`second`") {
		t.Errorf("columns were reordered:\n%s", doc)
	}
	if _, ok := files["tables/shop/zeta.md"]; !ok {
		t.Errorf("bundle: %v", files)
	}
}

// A date-sharded table is one table to whoever queries it. Projected a
// shard at a time, a GA4 export is hundreds of identical entries that tie
// on every search they match and push the rest off the page — so the
// shards fold into one entry, addressed by the wildcard they are queried
// through, carrying the latest shard's columns.
func TestSeedFoldsDateShardedTables(t *testing.T) {
	var cols []seedColumn
	for _, day := range []string{"20260101", "20260102", "20260103"} {
		cols = append(cols, seedColumn{Schema: "ga", Table: "events_" + day, Column: "event_name", DataType: "STRING"})
	}
	cols = append(cols,
		// The latest shard gained a column: it is the schema the wildcard reads.
		seedColumn{Schema: "ga", Table: "events_20260103", Column: "is_active_user", DataType: "BOOL"},
		// Two stems in one dataset stay two entries.
		seedColumn{Schema: "ga", Table: "events_intraday_20260103", Column: "event_name", DataType: "STRING"},
		seedColumn{Schema: "ga", Table: "events_intraday_20260104", Column: "event_name", DataType: "STRING"},
		// One dated table alone may be a snapshot somebody named.
		seedColumn{Schema: "shop", Table: "orders_20250331", Column: "order_id", DataType: "STRING"},
		// Eight digits that are not a date, and a date glued to a digit.
		seedColumn{Schema: "shop", Table: "batch_12345678", Column: "id", DataType: "STRING"},
		seedColumn{Schema: "shop", Table: "batch_87654321", Column: "id", DataType: "STRING"},
		seedColumn{Schema: "shop", Table: "t_120260101", Column: "id", DataType: "STRING"},
		seedColumn{Schema: "shop", Table: "t_120260102", Column: "id", DataType: "STRING"},
		// A stem that is a table of its own keeps its address.
		seedColumn{Schema: "shop", Table: "sales", Column: "id", DataType: "STRING"},
		seedColumn{Schema: "shop", Table: "sales20260101", Column: "id", DataType: "STRING"},
		seedColumn{Schema: "shop", Table: "sales20260102", Column: "id", DataType: "STRING"},
	)
	files := seedBundle(t, cols, "proj", "wh")

	var paths []string
	for p := range files {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	want := []string{
		"wh/ga/events_.md", "wh/ga/events_intraday_.md",
		"wh/shop/batch_12345678.md", "wh/shop/batch_87654321.md",
		"wh/shop/orders_20250331.md",
		"wh/shop/sales.md", "wh/shop/sales20260101.md", "wh/shop/sales20260102.md",
		"wh/shop/t_120260101.md", "wh/shop/t_120260102.md",
	}
	if !slices.Equal(paths, want) {
		t.Fatalf("bundle paths:\n got %v\nwant %v", paths, want)
	}

	parsed, _, err := okf.Parse([]byte(files["wh/ga/events_.md"]))
	if err != nil {
		t.Fatal(err)
	}
	k := parsed.Knowledge
	if k.Title != "ga.events_*" {
		t.Errorf("title = %q, want the wildcard the shards are queried through", k.Title)
	}
	if k.Resource != "bigquery://proj.ga.events_*" {
		t.Errorf("resource = %q", k.Resource)
	}
	for _, s := range []string{"3 tables", "`events_20260101` to `events_20260103`", "`is_active_user`", "_TABLE_SUFFIX"} {
		if !strings.Contains(k.Body, s) {
			t.Errorf("body lacks %q:\n%s", s, k.Body)
		}
	}
}

// The pipeline the help text promises, end to end: rows in, drafts in a
// knowledge base. seed writing nothing itself is the whole design, so
// what it prints has to be exactly what import takes.
func TestSeedPipesIntoImport(t *testing.T) {
	dir := t.TempDir()
	rows := `[{"table_schema":"shop","table_name":"orders","column_name":"order_id","data_type":"STRING","is_nullable":"NO"},
	          {"table_schema":"shop","table_name":"orders","column_name":"total_price","data_type":"NUMERIC","is_nullable":"NO"}]`
	in := filepath.Join(dir, "cols.json")
	if err := os.WriteFile(in, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "seeded.tar.gz")

	// seed prints the bundle to stdout, as a pipeline expects.
	out, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = out
	err = cmdSeed(context.Background(), []string{in, "--project", "demo-shop"})
	os.Stdout = orig
	out.Close()
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	var written []string
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/bundle/{path...}", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		d, _, err := okf.Parse(body)
		if err != nil {
			t.Errorf("import sent a document seed produced and the parser refused: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		k := d.Knowledge
		k.ID = strings.TrimSuffix(r.PathValue("path"), ".md")
		written = append(written, k.ID)
		if string(k.Type) != "BigQuery Table" || k.Lifecycle() != "draft" {
			t.Errorf("imported %s as type %q status %q", k.ID, k.Type, k.Lifecycle())
		}
		_ = json.NewEncoder(w).Encode(k)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	if err := cmdImport(context.Background(), []string{archive, "--url", srv.URL}); err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(written) != 1 || written[0] != "tables/shop/orders" {
		t.Errorf("wrote %v, want the one table at its own address", written)
	}
}

// The failure this path cannot see from the inside: `bq query` stops at
// 100 rows unless told otherwise, and a catalog cut there is a valid
// bundle of the tables that fit. Nothing downstream can tell it from a
// small warehouse, so the count itself is the only thing left to say
// something about — and it says it as a note, because 100 rows is also
// just 100 rows.
func TestSeedNotesAnInputCutAtAClientsDefault(t *testing.T) {
	note := seedTruncationNote(clientDefaultRows)
	if note == "" {
		t.Fatalf("%d rows drew no note: the truncation a reporter lost a day to passes in silence", clientDefaultRows)
	}
	if !strings.Contains(note, "--max_rows") {
		t.Errorf("the note does not name the flag that fixes it:\n%s", note)
	}
	for _, rows := range []int{0, 1, clientDefaultRows - 1, clientDefaultRows + 1, 1000} {
		if got := seedTruncationNote(rows); got != "" {
			t.Errorf("%d rows drew a note: %s", rows, got)
		}
	}
}

// seedGolden is what the projection makes of testdata/seed-columns.json,
// and the web UI's copy of it (internal/webui/static/js/seed.js) is held
// to the same file by internal/webui/jstest/seed.test.js. Two
// implementations of one projection drift the day one of them learns a
// rule the other did not (design doc 0148 §3); this file is where that
// day fails instead. `go test ./cmd/ochakai -run TestSeedGolden -update`
// rewrites it from the Go side.
type seedGolden struct {
	Project  string `json:"project"`
	Prefix   string `json:"prefix"`
	Concepts []struct {
		ID       string `json:"id"`
		Document string `json:"document"`
	} `json:"concepts"`
}

func TestSeedGolden(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "seed-columns.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cols, err := readSeedColumns(f)
	if err != nil {
		t.Fatal(err)
	}
	got := seedGolden{Project: "proj", Prefix: "tables"}
	for _, tb := range foldShards(gatherSeedTables(cols)) {
		got.Concepts = append(got.Concepts, struct {
			ID       string `json:"id"`
			Document string `json:"document"`
		}{seedID(tb, got.Prefix), seedDocument(tb, got.Project)})
	}
	want := filepath.Join("testdata", "seed-golden.json")
	if *updateGolden {
		b, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(want, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	raw, err := os.ReadFile(want)
	if err != nil {
		t.Fatal(err)
	}
	var w seedGolden
	if err := json.Unmarshal(raw, &w); err != nil {
		t.Fatal(err)
	}
	if len(w.Concepts) != len(got.Concepts) {
		t.Fatalf("%d concepts, the golden file has %d; rerun with -update if the change is intended", len(got.Concepts), len(w.Concepts))
	}
	for i := range got.Concepts {
		if got.Concepts[i] != w.Concepts[i] {
			t.Errorf("concept %d:\n got %q\n%s\nwant %q\n%s\nrerun with -update if the change is intended",
				i, got.Concepts[i].ID, got.Concepts[i].Document, w.Concepts[i].ID, w.Concepts[i].Document)
		}
	}
}
