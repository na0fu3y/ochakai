package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/na0fu3y/ochakai/internal/apiclient"
	"github.com/na0fu3y/ochakai/internal/domain"
)

func TestParseArgsAllowsFlagsAfterPositionals(t *testing.T) {
	fs := flag.NewFlagSet("", flag.ContinueOnError)
	f := fs.String("f", "", "")
	pos, err := parseArgs(fs, []string{"metrics/revenue", "-f", "x.md"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(pos, []string{"metrics/revenue"}) || *f != "x.md" {
		t.Errorf("pos = %v, f = %q", pos, *f)
	}
}

func TestParseRef(t *testing.T) {
	for in, want := range map[string]string{
		"metrics/revenue":           "metrics/revenue",
		"ochakai://metrics/revenue": "metrics/revenue",
		"revenue":                   "revenue", // root-level ids are entries too
	} {
		id, err := parseRef(in)
		if err != nil || id != want {
			t.Errorf("parseRef(%q) = %q, %v; want %q", in, id, err, want)
		}
	}
	for _, bad := range []string{"", "ochakai://"} {
		if _, err := parseRef(bad); err == nil {
			t.Errorf("parseRef(%q) succeeded, want error", bad)
		}
	}
}

func TestDecodeEntryDetectsFormat(t *testing.T) {
	fromJSON, err := decodeEntry([]byte(`{"type":"metric","id":"revenue","title":"売上"}`))
	if err != nil || fromJSON.ID != "revenue" {
		t.Fatalf("json: %v, %+v", err, fromJSON)
	}
	fromOKF, err := decodeEntry([]byte("\n---\ntype: metric\nid: revenue\ntitle: 売上\n---\n\nbody\n"))
	if err != nil || fromOKF.ID != "revenue" || fromOKF.Body != "body" {
		t.Fatalf("okf: %v, %+v", err, fromOKF)
	}
	if _, err := decodeEntry([]byte("plain text")); err == nil {
		t.Error("garbage decoded without error")
	}
}

func TestParseFilter(t *testing.T) {
	got, err := parseFilter("orders.status = shipped and packed")
	if err != nil {
		t.Fatal(err)
	}
	want := apiclient.Filter{Field: "orders.status", Op: "=", Value: "shipped and packed"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}

	got, err = parseFilter("orders.qty >= 10")
	if err != nil || got.Value != int64(10) {
		t.Errorf("numeric value = %#v (%v)", got.Value, err)
	}

	got, err = parseFilter("orders.region in tokyo, osaka")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Value, []any{"tokyo", "osaka"}) {
		t.Errorf("in list = %#v", got.Value)
	}

	if _, err := parseFilter("no-op-here"); err == nil {
		t.Error("bad filter parsed without error")
	}
}

func TestParseGrain(t *testing.T) {
	tg, err := parseGrain("orders.created_at:month")
	if err != nil || tg.Field != "orders.created_at" || tg.Grain != "month" {
		t.Errorf("tg = %+v, err = %v", tg, err)
	}
	for _, bad := range []string{"orders.created_at", ":month", "orders.created_at:"} {
		if _, err := parseGrain(bad); err == nil {
			t.Errorf("parseGrain(%q) succeeded, want error", bad)
		}
	}
}

func tarGz(t *testing.T, files map[string]string) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf
}

func TestExtractTarGzWritesFiles(t *testing.T) {
	dir := t.TempDir()
	n, err := extractTarGz(dir, tarGz(t, map[string]string{"metrics/revenue.md": "hello", "index.md": "idx"}))
	if err != nil || n != 2 {
		t.Fatalf("n = %d, err = %v", n, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "metrics", "revenue.md"))
	if err != nil || string(data) != "hello" {
		t.Errorf("extracted content = %q, %v", data, err)
	}
}

func TestExtractTarGzRefusesEscapes(t *testing.T) {
	for _, evil := range []string{"../evil.md", "/abs.md"} {
		dir := t.TempDir()
		if _, err := extractTarGz(dir, tarGz(t, map[string]string{evil: "x"})); err == nil {
			t.Errorf("extractTarGz accepted %q", evil)
		}
		if _, err := os.Stat(filepath.Join(dir, "..", "evil.md")); err == nil {
			t.Errorf("escaped file was written for %q", evil)
		}
	}
}

func TestScalar(t *testing.T) {
	for in, want := range map[string]any{
		"10": int64(10), "1.5": 1.5, "true": true, "false": false, "shipped": "shipped",
	} {
		if got := scalar(in); got != want {
			t.Errorf("scalar(%q) = %#v, want %#v", in, got, want)
		}
	}
}

// Guard: the client dispatch table and domain types stay in sync with the
// commands documented in usage().
func TestClientCommandsCoverDesignDoc(t *testing.T) {
	for _, name := range []string{"search", "browse", "context", "get", "create", "update", "delete", "attach", "detach", "usage", "report", "revisions", "backlinks", "compile", "export", "import", "use", "whoami", "ui", "completion"} {
		if _, ok := clientCommands[name]; !ok {
			t.Errorf("missing client command %q", name)
		}
	}
	if len(clientCommands) != 20 {
		t.Errorf("unexpected extra client commands: %d", len(clientCommands))
	}
	_ = domain.Types // keep the import honest
}

func TestRenderContext(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	human := domain.Actor{Kind: domain.ActorHuman, Name: "na0"}
	res := &apiclient.ContextResult{
		Hits: []domain.SearchHit{
			{Knowledge: domain.Knowledge{Type: domain.TypeQueries, ID: "queries/monthly-revenue", Status: domain.StatusVerified, Title: "Monthly revenue"}, Score: 0.9},
			{Knowledge: domain.Knowledge{Type: domain.TypeTerms, ID: "terms/arr", Status: domain.StatusDraft, Title: "ARR"}, Score: 0.1},
		},
		Entries: []domain.Knowledge{
			{
				Type: domain.TypeQueries, ID: "queries/monthly-revenue", Status: domain.StatusVerified,
				Title:      "Monthly revenue",
				CreatedBy:  domain.Actor{Kind: domain.ActorAgent, Name: "claude"},
				VerifiedBy: &human, VerifiedAt: &now,
				Attrs: map[string]any{"question": "Revenue by month?", "sql": "SELECT 1\n"},
				Body:  "Use this over compile.",
			},
			{
				Type: domain.TypeInsights, ID: "insights/revenue-seasonality", Status: domain.StatusDraft,
				Title: "Seasonality", CreatedBy: domain.Actor{Kind: domain.ActorAgent, Name: "claude"},
				Body: "Q4 peaks ~40% above baseline.",
			},
		},
	}

	var out bytes.Buffer
	renderContext(&out, res, 0)
	s := out.String()
	for _, want := range []string{
		"## ochakai://queries/monthly-revenue (verified) — Monthly revenue",
		"verified by human:na0 on 2026-06-01; created by agent:claude",
		"Q: Revenue by month?",
		"```sql\nSELECT 1\n```",
		"Use this over compile.",
		"## ochakai://insights/revenue-seasonality (draft) — Seasonality",
		"Also relevant",
		"- ochakai://terms/arr (draft) — ARR",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("output misses %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "ochakai://queries/monthly-revenue (verified) — Monthly revenue\n- ") {
		t.Error("rendered entries must not repeat in the Also relevant list")
	}

	// A tiny budget still renders the first entry, then reports the rest.
	out.Reset()
	renderContext(&out, res, 10)
	s = out.String()
	if !strings.Contains(s, "## ochakai://queries/monthly-revenue") {
		t.Errorf("first entry must render regardless of budget:\n%s", s)
	}
	if strings.Contains(s, "## ochakai://insights/revenue-seasonality") {
		t.Errorf("budget must drop later entries:\n%s", s)
	}
	if !strings.Contains(s, "1 more entries beyond --budget") {
		t.Errorf("omitted entries must be reported:\n%s", s)
	}
}

// TestImportReportsUnchanged pins the import summary against a fake
// server: an existing entry whose PUT answers with Ochakai-Unchanged
// counts (and prints) as unchanged, everything else as before. Servers
// without the header (absent on the second PUT) keep reporting updated.
func TestImportReportsUnchanged(t *testing.T) {
	dir := t.TempDir()
	for name, doc := range map[string]string{
		"same.md": "---\ntype: metric\ntitle: Same\n---\n\nbody\n",
		"diff.md": "---\ntype: metric\ntitle: Diff\n---\n\nbody\n",
	} {
		if err := os.MkdirAll(filepath.Join(dir, "metrics"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "metrics", name), []byte(doc), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/knowledge", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "already exists"})
	})
	mux.HandleFunc("PUT /api/v1/knowledge/{id...}", func(w http.ResponseWriter, r *http.Request) {
		var k domain.Knowledge
		if err := json.NewDecoder(r.Body).Decode(&k); err != nil {
			t.Errorf("bad PUT payload: %v", err)
		}
		if r.PathValue("id") == "metrics/same" {
			w.Header().Set("Ochakai-Unchanged", "true")
		}
		_ = json.NewEncoder(w).Encode(k)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	orig := os.Stdout
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = pw
	importErr := cmdImport(context.Background(), []string{dir, "--url", srv.URL})
	pw.Close()
	os.Stdout = orig
	out, err := io.ReadAll(pr)
	if err != nil {
		t.Fatal(err)
	}
	if importErr != nil {
		t.Fatalf("cmdImport: %v\noutput:\n%s", importErr, out)
	}
	for _, want := range []string{
		"unchanged ochakai://metrics/same\n",
		"updated ochakai://metrics/diff\n",
		"imported 2 entries (0 created, 1 updated, 1 unchanged, 0 attachments, 0 skipped)\n",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("output misses %q:\n%s", want, out)
		}
	}
}
