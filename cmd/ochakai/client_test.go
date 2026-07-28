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
	"github.com/na0fu3y/ochakai/internal/okf"
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

// TestCmdSearchNeedsQueryOrSort pins the client-side check mirroring the
// server rule: exactly one of the query and --sort is required, and both
// checks fire before any connection is made.
func TestCmdSearchNeedsQueryOrSort(t *testing.T) {
	if err := cmdSearch(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "needs a query") {
		t.Errorf("no query and no --sort: got %v, want a needs-a-query error", err)
	}
	if err := cmdSearch(context.Background(), []string{" ", "\t"}); err == nil || !strings.Contains(err.Error(), "needs a query") {
		t.Errorf("whitespace query: got %v, want a needs-a-query error", err)
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

// Guard: the client dispatch table and domain types stay in sync with the
// commands documented in usage().
func TestClientCommandsCoverDesignDoc(t *testing.T) {
	for _, name := range []string{"search", "browse", "context", "get", "create", "update", "verify", "reject", "delete", "purge", "reembed", "move", "attach", "detach", "usage", "report", "revisions", "backlinks", "export", "import", "use", "whoami", "ui", "mcp-stdio", "completion"} {
		if _, ok := clientCommands[name]; !ok {
			t.Errorf("missing client command %q", name)
		}
	}
	if len(clientCommands) != 25 {
		t.Errorf("unexpected extra client commands: %d", len(clientCommands))
	}
	_ = domain.Types // keep the import honest
}

func TestRenderContext(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	human := domain.Actor{Kind: domain.ActorHuman, Name: "na0"}
	res := &apiclient.ContextResult{
		Hits: []domain.ContextRank{
			{Type: domain.TypeComputations, ID: "queries/monthly-revenue", Status: domain.StatusStable, Title: "Monthly revenue", Score: 0.9},
			{Type: domain.TypeTerms, ID: "terms/arr", Status: domain.StatusDraft, Title: "ARR", Score: 0.1},
		},
		Entries: []domain.Knowledge{
			{
				Type: domain.TypeComputations, ID: "queries/monthly-revenue", Status: domain.StatusStable,
				Title:         "Monthly revenue",
				CreatedBy:     domain.Actor{Kind: domain.ActorProcess, Name: "claude"},
				Verifications: []domain.Verification{{By: human, At: now}},
				Attrs:         map[string]any{"question": "Revenue by month?", "sql": "SELECT 1\n"},
				Body:          "Prefer this over writing new SQL.",
			},
			{
				Type: domain.TypeInsights, ID: "insights/revenue-seasonality", Status: domain.StatusDraft,
				Title: "Seasonality", CreatedBy: domain.Actor{Kind: domain.ActorProcess, Name: "claude"},
				Body: "Q4 peaks ~40% above baseline.",
			},
		},
	}

	var out bytes.Buffer
	renderContext(&out, res, 0)
	s := out.String()
	for _, want := range []string{
		"## ochakai://queries/monthly-revenue (stable) — Monthly revenue",
		"verified by human:na0 on 2026-06-01; created by process:claude",
		"Q: Revenue by month?",
		"```sql\nSELECT 1\n```",
		"Prefer this over writing new SQL.",
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
	mux.HandleFunc("PUT /api/v1/knowledge/{id...}", func(w http.ResponseWriter, r *http.Request) {
		// The payload is a document now (design doc 0043 §3.5), and the
		// import loop makes one call per entry rather than create-then-
		// update: whether the id was free is not something a bundle says.
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
			t.Errorf("Content-Type = %q, want text/markdown", ct)
		}
		body, _ := io.ReadAll(r.Body)
		d, _, err := okf.Parse(body)
		if err != nil {
			t.Errorf("bad PUT payload: %v\n%s", err, body)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		k := d.Knowledge
		k.ID = r.PathValue("id")
		if k.ID == "metrics/same" {
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

// `ochakai update --if-match` sends the version as the If-Match header,
// and a 412 comes back as an actionable conflict message (re-read, redo,
// retry) rather than the raw server error.
func TestUpdateIfMatchSendsHeaderAndExplainsConflict(t *testing.T) {
	var gotIfMatch string
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/knowledge/{id...}", func(w http.ResponseWriter, r *http.Request) {
		gotIfMatch = r.Header.Get("If-Match")
		w.WriteHeader(http.StatusPreconditionFailed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "knowledge changed since it was read"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	doc := filepath.Join(t.TempDir(), "revenue.md")
	if err := os.WriteFile(doc, []byte("---\ntype: metric\ntitle: 売上\n---\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := cmdUpdate(context.Background(), []string{"metrics/revenue",
		"-f", doc, "--if-match", "2026-07-01T00:00:00.123456789Z", "--url", srv.URL})
	if gotIfMatch != `"2026-07-01T00:00:00.123456789Z"` {
		t.Errorf("If-Match on the wire = %q", gotIfMatch)
	}
	if err == nil {
		t.Fatal("stale --if-match succeeded, want a conflict error")
	}
	for _, want := range []string{"conflict", "ochakai get metrics/revenue"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("conflict error misses %q: %v", want, err)
		}
	}
}

// A verification's whole product is a timestamp and a name, and the only
// way to read them back was a second call: verify printed one line and
// dropped the entry the server had already returned. --json hands the
// same response every other write verb hands over, so a canary can stamp
// and record verified_at in one round trip.
func TestVerifyJSONPrintsTheEntry(t *testing.T) {
	verified := time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC)
	human := domain.Actor{Kind: domain.ActorHuman, Name: "na0"}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/verify/{id...}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(domain.Knowledge{
			Type: domain.TypeComputations, ID: r.PathValue("id"), Status: domain.StatusStable,
			Title:         "Monthly revenue",
			Verifications: []domain.Verification{{By: human, At: verified}},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	orig := os.Stdout
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = pw
	verifyErr := cmdVerify(context.Background(), []string{"queries/monthly-revenue", "--json", "--url", srv.URL})
	pw.Close()
	os.Stdout = orig
	out, err := io.ReadAll(pr)
	if err != nil {
		t.Fatal(err)
	}
	if verifyErr != nil {
		t.Fatal(verifyErr)
	}
	var got domain.Knowledge
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("--json did not print JSON (%v): %s", err, out)
	}
	last := got.LastVerified()
	if got.ID != "queries/monthly-revenue" || last == nil || !last.At.Equal(verified) {
		t.Errorf("verified entry = %+v, want the server's response with its verification", got)
	}
}
