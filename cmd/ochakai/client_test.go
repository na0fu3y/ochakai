package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
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
		"revenue":                   "revenue", // root-level ids are concepts too
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
	fromJSON, _, err := decodeEntry([]byte(`{"type":"metric","id":"revenue","title":"売上"}`))
	if err != nil || fromJSON.ID != "revenue" {
		t.Fatalf("json: %v, %+v", err, fromJSON)
	}
	fromOKF, _, err := decodeEntry([]byte("\n---\ntype: metric\nid: revenue\ntitle: 売上\n---\n\nbody\n"))
	if err != nil || fromOKF.ID != "revenue" || fromOKF.Body != "body" {
		t.Fatalf("okf: %v, %+v", err, fromOKF)
	}
	if _, _, err := decodeEntry([]byte("plain text")); err == nil {
		t.Error("garbage decoded without error")
	}
	// A document that says who generated and confirmed it is naming a
	// claim, not this instance's provenance (design doc 0046 §2.2): the
	// keys come back so the write can report what it kept.
	_, claimed, err := decodeEntry([]byte("---\ntype: metric\ngenerated:\n  by: human:sato\n---\n"))
	if err != nil || len(claimed) != 1 || claimed[0] != "generated" {
		t.Fatalf("claimed = %v, err = %v", claimed, err)
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

// Guard: `ochakai help` names every command the binary dispatches, and no
// others. The earlier form of this test compared the dispatch table
// against a list written out here — a copy of usage() rather than a
// reading of it, which is how `reject` came to run without ever being
// named in the help. docs/cli.md renders the same text, so a command
// missing here is missing from the reference too.
func TestUsageNamesEveryCommand(t *testing.T) {
	var b strings.Builder
	usage(&b)

	documented := map[string]bool{}
	for _, line := range strings.Split(b.String(), "\n") {
		// Command lines are indented two spaces; a description
		// continued on the next line is indented to its column.
		if !strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "   ") {
			continue
		}
		if f := strings.Fields(line); len(f) > 0 {
			documented[f[0]] = true
		}
	}

	// These three are how the binary is run rather than something
	// ochakai knows, so they are not client commands and docs/surface.md
	// does not count them either.
	for _, name := range []string{"serve", "serve-ui", "version"} {
		if !documented[name] {
			t.Errorf("usage() no longer documents %q", name)
		}
		delete(documented, name)
	}
	for name := range clientCommands {
		if !documented[name] {
			t.Errorf("`ochakai %s` runs, but `ochakai help` never names it", name)
		}
		delete(documented, name)
	}
	for name := range documented {
		t.Errorf("usage() documents %q, which the binary does not dispatch", name)
	}
	_ = domain.Types // keep the import honest
}

// viewOf renders a concept the way a read does, so a rendering test works
// on the shape the CLI actually receives.
func viewOf(t *testing.T, k domain.Knowledge) domain.View {
	t.Helper()
	v, err := okf.ViewOf(&k)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestRenderContext(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	human := domain.Actor{Kind: domain.ActorHuman, Name: "na0"}
	res := &apiclient.ContextResult{
		Hits: []domain.ContextRank{
			{Type: domain.TypeComputations, ID: "queries/monthly-revenue", Status: domain.StatusStable, Title: "Monthly revenue", Score: 0.9},
			{Type: domain.TypeTerms, ID: "terms/arr", Status: domain.StatusDraft, Title: "ARR", Score: 0.1},
		},
		Entries: []domain.View{
			viewOf(t, domain.Knowledge{
				Type: domain.TypeComputations, ID: "queries/monthly-revenue", Status: domain.StatusStable,
				Title:         "Monthly revenue",
				CreatedBy:     domain.Actor{Kind: domain.ActorProcess, Name: "claude"},
				Verifications: []domain.Verification{{By: human, At: now}},
				Attrs:         map[string]any{"question": "Revenue by month?"},
				Body:          "Prefer this over writing new SQL.",
			}),
			viewOf(t, domain.Knowledge{
				Type: domain.TypeInsights, ID: "insights/revenue-seasonality", Status: domain.StatusDraft,
				Title: "Seasonality", CreatedBy: domain.Actor{Kind: domain.ActorProcess, Name: "claude"},
				Body: "Q4 peaks ~40% above baseline.",
			}),
		},
	}

	var out bytes.Buffer
	renderContext(&out, res, 0)
	s := out.String()
	for _, want := range []string{
		"## ochakai://queries/monthly-revenue (stable) — Monthly revenue",
		"verified by human:na0 on 2026-06-01; created by process:claude",
		// The concept is its document: the producer key and the body come
		// out as written, without this renderer knowing about either.
		"question: Revenue by month?",
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
		t.Error("rendered concepts must not repeat in the Also relevant list")
	}

	// A tiny budget still renders the first concept, then reports the rest.
	out.Reset()
	renderContext(&out, res, 10)
	s = out.String()
	if !strings.Contains(s, "## ochakai://queries/monthly-revenue") {
		t.Errorf("first concept must render regardless of budget:\n%s", s)
	}
	if strings.Contains(s, "## ochakai://insights/revenue-seasonality") {
		t.Errorf("budget must drop later concepts:\n%s", s)
	}
	if !strings.Contains(s, "1 more concepts beyond --budget") {
		t.Errorf("omitted concepts must be reported:\n%s", s)
	}
}

// TestImportReportsUnchanged pins the import summary against a fake
// server: an existing concept whose PUT answers with Ochakai-Unchanged
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
	mux.HandleFunc("PUT /api/v1/bundle/{path...}", func(w http.ResponseWriter, r *http.Request) {
		// The payload is a document now (design doc 0043 §3.5), and the
		// import loop makes one call per concept rather than create-then-
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
		// A concept lives at its id plus `.md` (design doc 0046 §3.5).
		k.ID = strings.TrimSuffix(r.PathValue("path"), ".md")
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
		"imported 2 concepts (0 created, 1 updated, 1 unchanged, 0 attributed, 0 loose, 0 skipped, 0 notes)\n",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("output misses %q:\n%s", want, out)
		}
	}
}

// TestImportStrictRefusesBeforeWriting pins the whole point of --strict:
// a note is a report by default and a concept carrying one still imports,
// but under --strict the same bundle fails — and fails before the first
// PUT, so a sync that would drift lands nothing at all. Without the flag
// the identical bundle is written, which is what makes this a posture and
// not a parser change.
func TestImportStrictRefusesBeforeWriting(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "metrics"), 0o755); err != nil {
		t.Fatal(err)
	}
	// "verified" was never an OKF lifecycle value, so the parser reads it
	// as a draft and says so. That reinterpretation is exactly the one an
	// unattended import must not absorb quietly.
	doc := "---\ntype: Metric\ntitle: Revenue\nstatus: verified\n---\n\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "metrics", "revenue.md"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	puts := 0
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/bundle/{path...}", func(w http.ResponseWriter, r *http.Request) {
		puts++
		body, _ := io.ReadAll(r.Body)
		d, _, err := okf.Parse(body)
		if err != nil {
			t.Errorf("bad PUT payload: %v\n%s", err, body)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		k := d.Knowledge
		// A concept lives at its id plus `.md` (design doc 0046 §3.5).
		k.ID = strings.TrimSuffix(r.PathValue("path"), ".md")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(k)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	err := cmdImport(context.Background(), []string{dir, "--strict", "--url", srv.URL})
	if err == nil {
		t.Fatal("--strict imported a bundle with a note, want an error")
	}
	if !strings.Contains(err.Error(), "nothing was written") {
		t.Errorf("--strict error = %v, want it to say nothing was written", err)
	}
	if puts != 0 {
		t.Errorf("--strict wrote %d concepts before failing, want 0", puts)
	}

	// --dry-run --strict is the CI gate: same verdict, still no writes.
	if err := cmdImport(context.Background(), []string{dir, "--dry-run", "--strict", "--url", srv.URL}); err == nil {
		t.Error("--dry-run --strict accepted a bundle with a note, want an error")
	}

	if err := cmdImport(context.Background(), []string{dir, "--url", srv.URL}); err != nil {
		t.Fatalf("without --strict the same bundle must import: %v", err)
	}
	if puts != 1 {
		t.Errorf("import without --strict wrote %d concepts, want 1", puts)
	}
}

// `ochakai put --if-match` sends the version as the If-Match header,
// and a 412 comes back as an actionable conflict message (re-read, redo,
// retry) rather than the raw server error.
func TestPutIfMatchSendsHeaderAndExplainsConflict(t *testing.T) {
	var gotIfMatch string
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/bundle/{path...}", func(w http.ResponseWriter, r *http.Request) {
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
	err := cmdPut(context.Background(), []string{"metrics/revenue",
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
// dropped the concept the server had already returned. --json hands the
// same response every other write verb hands over, so a canary can stamp
// and record verified_at in one round trip.
func TestVerifyJSONPrintsTheEntry(t *testing.T) {
	verified := time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC)
	human := domain.Actor{Kind: domain.ActorHuman, Name: "na0"}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/review/{id...}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(domain.View{
			ID: r.PathValue("id"),
			Summary: domain.Summary{Type: domain.TypeComputations, ID: r.PathValue("id"),
				Status: domain.StatusStable, Title: "Monthly revenue", Trust: domain.TrustHuman},
			Observed: domain.Observed{Verified: []domain.Verification{{By: human, At: verified}}},
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
	var got domain.View
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("--json did not print JSON (%v): %s", err, out)
	}
	last := got.Observed.LastVerified()
	if got.ID != "queries/monthly-revenue" || last == nil || !last.At.Equal(verified) {
		t.Errorf("verified concept = %+v, want the server's response with its verification", got)
	}
}

// TestStatsPrintsEachQueueWithTheCommandThatListsIt pins the two halves
// of what makes the queue lines a nudge rather than a statistic: every
// one carries the command that opens that queue (with the scope it was
// asked under), and --exit-code turns "somebody owes a review" into an
// exit status a scheduler can watch (design doc 0049).
func TestStatsPrintsEachQueueWithTheCommandThatListsIt(t *testing.T) {
	var gotPrefixes []string
	counts := domain.QueueCounts{Drafts: 3, Failed: 1, StaleAfter: 0}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/stats", func(w http.ResponseWriter, r *http.Request) {
		gotPrefixes = r.URL.Query()["prefix"]
		_ = json.NewEncoder(w).Encode(map[string]any{"queues": counts})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	orig := os.Stdout
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = pw
	statsErr := cmdStats(context.Background(), []string{"--prefix", "teams/growth", "--exit-code", "--url", srv.URL})
	pw.Close()
	os.Stdout = orig
	out, err := io.ReadAll(pr)
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(statsErr, errWorkPending) {
		t.Errorf("--exit-code with 4 waiting = %v, want errWorkPending (exit 2)", statsErr)
	}
	if len(gotPrefixes) != 1 || gotPrefixes[0] != "teams/growth" {
		t.Errorf("server saw prefix %v, want [teams/growth]", gotPrefixes)
	}
	for _, want := range []string{
		"drafts\t3\tochakai list usage --status draft --prefix teams/growth\n",
		"failed\t1\tochakai list failed --prefix teams/growth\n",
		"stale_after\t0\tochakai list stale_after --prefix teams/growth\n",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("output misses %q:\n%s", want, out)
		}
	}

	// All three empty is the case the exit status exists to make
	// visible: it prints the zeros and exits 0, so a quiet queue and an
	// empty one stop looking alike.
	counts = domain.QueueCounts{}
	if err := cmdStats(context.Background(), []string{"--exit-code", "--url", srv.URL}); err != nil {
		t.Errorf("--exit-code on an empty base = %v, want nil (exit 0)", err)
	}
}

// A bundle from another instance arrives with a trust family. The import
// parses it, so the move to a claim happens here rather than on the
// server (design doc 0046 §2.2) — and the note has to follow the claim,
// which means asking what was actually stored. A claim the server kept is
// reported and counts against --strict; one it dropped as its own
// observation coming home is neither.
func TestImportReportsAKeptClaim(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "metrics"), 0o755); err != nil {
		t.Fatal(err)
	}
	const foreign = "---\ntype: metric\ntitle: Foreign\n" +
		"generated:\n  by: human:sato@example.co.jp\n  at: 2026-07-01T00:00:00Z\n" +
		"verified:\n  - by: human:ceo@example.co.jp\n    at: 2026-07-02T00:00:00Z\n" +
		"---\n\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "metrics", "foreign.md"), []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}

	// keep says whether this server stores the claim or recognizes it as
	// what it already observed; both answers travel back the same way, in
	// the stored document.
	run := func(t *testing.T, keep bool) string {
		t.Helper()
		mux := http.NewServeMux()
		mux.HandleFunc("PUT /api/v1/bundle/{path...}", func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			if !keep {
				body = []byte(strings.Split(string(body), "received:")[0] + "---\n\nbody\n")
			}
			_ = json.NewEncoder(w).Encode(domain.View{Document: string(body)})
		})
		mux.HandleFunc("POST /api/v1/review/{id...}", func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(domain.View{})
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()

		orig, origErr := os.Stdout, os.Stderr
		pr, pw, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		os.Stdout, os.Stderr = pw, pw
		importErr := cmdImport(context.Background(), []string{dir, "--url", srv.URL})
		pw.Close()
		os.Stdout, os.Stderr = orig, origErr
		out, err := io.ReadAll(pr)
		if err != nil {
			t.Fatal(err)
		}
		if importErr != nil {
			t.Fatalf("cmdImport: %v\noutput:\n%s", importErr, out)
		}
		return string(out)
	}

	kept := run(t, true)
	if !strings.Contains(kept, "note: generated, verified is not") &&
		!strings.Contains(kept, "note: generated, verified are not") {
		t.Errorf("a kept claim was not reported:\n%s", kept)
	}
	if !strings.Contains(kept, "1 notes)") {
		t.Errorf("a kept claim did not reach the summary count:\n%s", kept)
	}

	// The same bundle against a server that recognizes the keys as its own
	// is the Git review loop (design doc 0009 §3.2), which must stay quiet.
	dropped := run(t, false)
	if strings.Contains(dropped, "note:") || !strings.Contains(dropped, "0 notes)") {
		t.Errorf("our own export form coming home was reported as a claim:\n%s", dropped)
	}
}

// The false green --dry-run was: a bundle with nothing wrong in it as far
// as the parse could see, and a note the server raises at write time (a
// trust family kept as the document's own claim). The dry run reported it
// clean and exited 0; the import that followed reported the note and,
// under --strict, failed. Now the dry run asks the server the same
// question — with dry_run=true, so nothing is written — and reaches the
// same verdict (design doc 0061).
func TestImportDryRunAsksTheServerAndWritesNothing(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "metrics"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Parses without a single note: "verified" here is the trust family
	// OKF defines, not an unrecognized status.
	doc := "---\ntype: Metric\ntitle: Revenue\n" +
		"verified:\n  - by: human:ceo@example.com\n    at: 2026-07-02T00:00:00Z\n---\n\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "metrics", "revenue.md"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	var writes, plans int
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/bundle/{path...}", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		d, _, err := okf.Parse(body)
		if err != nil {
			t.Errorf("bad PUT payload: %v\n%s", err, body)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		k := d.Knowledge
		k.ID = strings.TrimSuffix(r.PathValue("path"), ".md")
		// The import moved the trust family under `received` before
		// sending, so the server sees no keys of its own to report on and
		// raises no note itself: what it answers is the document it would
		// have stored, and a claim still on it is the note (design doc
		// 0046 §2.2). Both modes answer the same way.
		if !strings.Contains(string(body), "received:") {
			t.Errorf("the import sent a trust family the server owns:\n%s", body)
		}
		if r.URL.Query().Get("dry_run") == "true" {
			plans++
			w.Header().Set("Ochakai-Plan", "created")
			_ = json.NewEncoder(w).Encode(domain.View{ID: k.ID, Document: string(body)})
			return
		}
		writes++
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(domain.View{ID: k.ID, Document: string(body)})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	out := captureStdout(t, func() {
		if err := cmdImport(context.Background(), []string{dir, "--dry-run", "--url", srv.URL}); err != nil {
			t.Fatalf("plain --dry-run must report rather than fail: %v", err)
		}
	})
	for _, want := range []string{
		"would create ochakai://metrics/revenue\n",
		"dry run: 1 concepts (1 created, 0 updated, 0 unchanged, 0 attributed, 0 loose, 0 skipped, 1 notes)\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dry run output misses %q:\n%s", want, out)
		}
	}
	if plans != 1 || writes != 0 {
		t.Errorf("dry run made %d plans and %d writes, want 1 and 0", plans, writes)
	}

	// The gate now fails, and still writes nothing — which is the whole
	// point of gating CI on it.
	err := cmdImport(context.Background(), []string{dir, "--dry-run", "--strict", "--url", srv.URL})
	if err == nil {
		t.Fatal("--dry-run --strict accepted a bundle the import would note")
	}
	if !strings.Contains(err.Error(), "nothing was written") {
		t.Errorf("--strict error = %v, want it to say nothing was written", err)
	}
	if writes != 0 {
		t.Errorf("the dry run wrote %d concepts", writes)
	}

	// And the verdict is the import's own: without --dry-run the same
	// bundle raises the same note, and --strict fails on it too.
	if err := cmdImport(context.Background(), []string{dir, "--strict", "--url", srv.URL}); err == nil {
		t.Error("--strict imported a bundle the dry run refused")
	}
	if writes != 1 {
		t.Errorf("the import wrote %d concepts, want 1", writes)
	}
}

// A server that does not know dry_run ignored the parameter and wrote the
// document. Silence is then the opposite of what it looks like, so the
// missing plan header is an error and not a clean bill (design doc 0061
// §3.2).
func TestImportDryRunRefusesAServerWithNoPlan(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "metrics"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metrics", "revenue.md"),
		[]byte("---\ntype: Metric\ntitle: Revenue\n---\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/bundle/{path...}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(domain.View{ID: strings.TrimSuffix(r.PathValue("path"), ".md")})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	err := cmdImport(context.Background(), []string{dir, "--dry-run", "--url", srv.URL})
	if err == nil {
		t.Fatal("a dry run against a server with no Ochakai-Plan reported success")
	}
	if !strings.Contains(err.Error(), "Ochakai-Plan") {
		t.Errorf("error = %v, want it to name the missing header", err)
	}
}

// captureStdout runs f with stdout on a pipe and returns what it printed.
// The import loop prints its per-object lines and its summary there, and
// asserting on them is how the counts stay the ones a reader sees.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	orig := os.Stdout
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = pw
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(pr)
		done <- string(b)
	}()
	f()
	pw.Close()
	os.Stdout = orig
	return <-done
}

// `--links-to X` on its own lists the backlinks of X, exactly as the wire
// does (design doc 0046 §3.5): a set is the answer and there is no text to
// rank it by. That makes it a listing rather than a search, so it lives on
// `ochakai list` (design doc 0062) — and the refusal each command gives
// has to point at the other one, because a reader who reached for the
// wrong half learns the split from that sentence or from nowhere.
func TestListStandsAloneOnAReverseLookup(t *testing.T) {
	var got string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/search", func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{"hits": []any{}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	if err := cmdList(context.Background(),
		[]string{"--links-to", "metrics/revenue", "--url", srv.URL}); err != nil {
		t.Fatalf("--links-to alone: %v", err)
	}
	if !strings.Contains(got, "links_to=metrics%2Frevenue") {
		t.Errorf("query = %q, want the reverse lookup", got)
	}

	// A feed is a positional argument now, and the wire still calls it sort.
	if err := cmdList(context.Background(), []string{"usage", "--url", srv.URL}); err != nil {
		t.Fatalf("list usage: %v", err)
	}
	if !strings.Contains(got, "sort=usage") {
		t.Errorf("query = %q, want sort=usage", got)
	}

	// Neither half accepts what belongs to the other, and each refusal
	// names the command that does take it.
	err := cmdSearch(context.Background(), []string{"--url", srv.URL})
	if err == nil {
		t.Fatal("a search with no query was accepted")
	}
	if !strings.Contains(err.Error(), "ochakai list") {
		t.Errorf("refusal %q does not send the reader to `ochakai list`", err)
	}
	err = cmdList(context.Background(), []string{"--url", srv.URL})
	if err == nil {
		t.Fatal("a listing with no feed and no reverse lookup was accepted")
	}
	for _, want := range append(append([]string{}, domain.ListSorts...), "--source", "--links-to") {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not name %s", err, want)
		}
	}
	if err := cmdList(context.Background(), []string{"nonsense", "--url", srv.URL}); err == nil {
		t.Error("an unknown feed was accepted")
	}
}
