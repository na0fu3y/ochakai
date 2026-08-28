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
	"slices"
	"strings"
	"sync"
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
	for line := range strings.SplitSeq(b.String(), "\n") {
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

func TestEachConcurrently(t *testing.T) {
	var mu sync.Mutex
	ran := map[int]int{}
	if err := eachConcurrently(context.Background(), 100, func(_ context.Context, i int) error {
		mu.Lock()
		defer mu.Unlock()
		ran[i]++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 100 {
		t.Errorf("ran %d distinct indices, want 100", len(ran))
	}
	for i, n := range ran {
		if n != 1 {
			t.Errorf("index %d ran %d times", i, n)
		}
	}

	boom := errors.New("boom")
	started := 0
	err := eachConcurrently(context.Background(), 1000, func(ctx context.Context, i int) error {
		mu.Lock()
		started++
		mu.Unlock()
		if i == 0 {
			return boom
		}
		<-ctx.Done() // the error must reach later calls as cancellation
		return ctx.Err()
	})
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want the first real error, not a cancellation", err)
	}
	if started >= 1000 {
		t.Error("an early error should stop new work from starting")
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

// `ochakai put --only-if-new` sends If-None-Match "*", and a 412 comes
// back blaming the id being taken, not a stale --if-match nobody passed
// (issue #428).
func TestPutOnlyIfNewExplainsConflictWithoutBlamingIfMatch(t *testing.T) {
	var gotIfNoneMatch, gotIfMatch string
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/bundle/{path...}", func(w http.ResponseWriter, r *http.Request) {
		gotIfNoneMatch = r.Header.Get("If-None-Match")
		gotIfMatch = r.Header.Get("If-Match")
		w.WriteHeader(http.StatusPreconditionFailed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "knowledge already exists"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	doc := filepath.Join(t.TempDir(), "revenue.md")
	if err := os.WriteFile(doc, []byte("---\ntype: metric\ntitle: 売上\n---\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := cmdPut(context.Background(), []string{"metrics/revenue",
		"-f", doc, "--only-if-new", "--url", srv.URL})
	if gotIfNoneMatch != "*" {
		t.Errorf("If-None-Match on the wire = %q, want \"*\"", gotIfNoneMatch)
	}
	if gotIfMatch != "" {
		t.Errorf("If-Match on the wire = %q, want none — --if-match was never passed", gotIfMatch)
	}
	if err == nil {
		t.Fatal("--only-if-new onto a taken id succeeded, want a conflict error")
	}
	if strings.Contains(err.Error(), "--if-match") {
		t.Errorf("conflict error blames --if-match, which was never passed: %v", err)
	}
	for _, want := range []string{"conflict", "already exists", "--only-if-new"} {
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

	// A server that declares the scope it counted wins over the flag:
	// a caller with grants gets their own numbers whether or not they
	// asked for a prefix, and the hint has to name what was counted
	// (design doc 0123).
	t.Run("the declared scope names the hint", func(t *testing.T) {
		declared := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"at":"2026-08-23T00:00:00Z","window_days":30,`+
				`"scope":["teams/sales"],`+
				`"concepts":{"total":1,"status":{},"trust":{}},`+
				`"queues":{"drafts":1,"failed":0,"stale_after":0},`+
				`"review":{"verifications":0},"outcomes":{"worked":0,"failed":0},`+
				`"misses":{"recording":true,"withheld":true,"count":0,"queries":[]},`+
				`"embedding":{"enabled":false},"dropped":{"events":0,"misses":0}}`)
		}))
		defer declared.Close()
		out := captureStdout(t, func() {
			_ = cmdStats(context.Background(), []string{"--url", declared.URL})
		})
		for _, want := range []string{
			"scope\tteams/sales\n",
			"drafts\t1\tochakai list usage --status draft --prefix teams/sales\n",
			"misses\twithheld\n",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("output misses %q:\n%s", want, out)
			}
		}
	})

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
	// The note names the concept it was true of: a bundle's notes are
	// grouped by text, and a group of one is spelled as the line it
	// replaced with the id in front of it.
	if !strings.Contains(kept, "note: ochakai://metrics/foreign: generated, verified is not") &&
		!strings.Contains(kept, "note: ochakai://metrics/foreign: generated, verified are not") {
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

// **A merge is not a verify** (design doc 0009 §3.2): an import writes
// documents and rules on nothing, so a `verified` key in the bundle is a
// claim the concept carries and never a verification this instance made.
//
// This is a regression test with a story. The import used to call
// `POST /api/v1/review/{id}` for every document that carried the key,
// recording whoever ran it as the verifier — correct under 0009 §3.2 as
// it read when that code was written ("verifier は取り込んだ者"), and
// forbidden by the same section after the record was settled and its
// body replaced. Nothing failed in between, because no test watched the
// ruling surface during an import. This one does.
func TestImportDoesNotRuleOnWhatItCarries(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "metrics"), 0o755); err != nil {
		t.Fatal(err)
	}
	const claimed = "---\ntype: metric\ntitle: Claimed\n" +
		"verified:\n  - by: human:ceo@example.co.jp\n    at: 2026-07-02T00:00:00Z\n" +
		"---\n\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "metrics", "claimed.md"), []byte(claimed), 0o644); err != nil {
		t.Fatal(err)
	}

	var rulings int
	var mu sync.Mutex
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/bundle/{path...}", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(domain.View{Document: string(body)})
	})
	mux.HandleFunc("POST /api/v1/review/{id...}", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		rulings++
		mu.Unlock()
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
	mu.Lock()
	defer mu.Unlock()
	if rulings != 0 {
		t.Errorf("import made %d ruling(s) on the concepts it wrote; a merge is not a verify (design doc 0009 §3.2)\noutput:\n%s", rulings, out)
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
// `ochakai list` (design doc 0062) — and a search with no query is
// refused with a sentence pointing at the other half, because a reader
// who reached for the wrong one learns the split there or nowhere. The
// listing half refuses nothing: with no feed and no reverse lookup it
// enumerates, which is the answer a filter alone asks for.
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
	// The other direction is not a refusal: a listing with no feed and no
	// reverse lookup is the plain enumeration, so it asks for no mode at
	// all and the server answers in address order.
	if err := cmdList(context.Background(), []string{"--url", srv.URL}); err != nil {
		t.Fatalf("list with no feed: %v", err)
	}
	if strings.Contains(got, "sort=") || strings.Contains(got, "q=") {
		t.Errorf("query = %q, want neither a feed nor a query on it", got)
	}
	if err := cmdList(context.Background(), []string{"--prefix", "metrics", "--url", srv.URL}); err != nil {
		t.Fatalf("list --prefix: %v", err)
	}
	if !strings.Contains(got, "prefix=metrics") || strings.Contains(got, "sort=") {
		t.Errorf("query = %q, want the prefix and no mode", got)
	}
	if err := cmdList(context.Background(), []string{"nonsense", "--url", srv.URL}); err == nil {
		t.Error("an unknown feed was accepted")
	}
}

// `ochakai put` writes both kinds of object at one address space: the
// bytes say which, exactly as they do on the wire (design docs 0075 §1,
// 0077). A concept keeps the id spelling every other command uses and
// lands at "<id>.md"; a file lands at the path it was given, untouched.
func TestPutWritesAConceptOrAFileByItsBytes(t *testing.T) {
	var gotPath, gotType string
	var gotBody []byte
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/bundle/{path...}", func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotType = r.URL.Path, r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		if strings.HasSuffix(gotPath, ".png") {
			_ = json.NewEncoder(w).Encode(domain.File{Name: "weekly.png", MediaType: "image/png", Size: 4})
			return
		}
		_ = json.NewEncoder(w).Encode(domain.View{ID: "insights/reading-revenue"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dir := t.TempDir()
	doc := filepath.Join(dir, "concept.md")
	if err := os.WriteFile(doc, []byte("---\ntype: Insight\n---\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	png := filepath.Join(dir, "weekly.png")
	pngBytes := []byte("\x89PNG")
	if err := os.WriteFile(png, pngBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := cmdPut(context.Background(),
		[]string{"insights/reading-revenue", "-f", doc, "--url", srv.URL}); err != nil {
		t.Fatalf("put a concept: %v", err)
	}
	if gotPath != "/api/v1/bundle/insights/reading-revenue.md" {
		t.Errorf("a concept went to %s, want its own address", gotPath)
	}
	if !strings.HasPrefix(gotType, "text/markdown") {
		t.Errorf("a concept travelled as %q, want the OKF document type", gotType)
	}

	if err := cmdPut(context.Background(),
		[]string{"insights/reading-revenue/weekly.png", "-f", png, "--url", srv.URL}); err != nil {
		t.Fatalf("put a file: %v", err)
	}
	if gotPath != "/api/v1/bundle/insights/reading-revenue/weekly.png" {
		t.Errorf("a file went to %s, want the path it was given", gotPath)
	}
	if !bytes.Equal(gotBody, pngBytes) {
		t.Errorf("the file's bytes were rewritten on the way out: %q", gotBody)
	}

	// A file has no version, so the concept preconditions are refused
	// rather than dropped on the floor.
	err := cmdPut(context.Background(),
		[]string{"insights/reading-revenue/weekly.png", "-f", png, "--only-if-new", "--url", srv.URL})
	if err == nil || !strings.Contains(err.Error(), "--only-if-new") {
		t.Errorf("--only-if-new on a file = %v, want a refusal naming the flag", err)
	}
}

// The hint `ochakai attach` printed is what a file's attribution now
// rests on: nothing derives it from where the file sits, so the relative
// markdown link to paste into a body is the whole of the help (design
// doc 0075 §5). It goes to stderr, where the file hints already go.
func TestPutPrintsTheBodyLinkForAFile(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/bundle/{path...}", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(domain.File{Name: "weekly.png", MediaType: "image/png", Size: 4})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	png := filepath.Join(t.TempDir(), "weekly.png")
	if err := os.WriteFile(png, []byte("\x89PNG"), 0o644); err != nil {
		t.Fatal(err)
	}
	stderr := captureStderr(t, func() {
		if err := cmdPut(context.Background(),
			[]string{"insights/reading-revenue/weekly.png", "-f", png, "--url", srv.URL}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(stderr, "![weekly.png](reading-revenue/weekly.png)") {
		t.Errorf("stderr = %q, want the relative link to paste into the body", stderr)
	}
}

// `ochakai delete` removes either kind, and the argument's spelling
// picks which address it asks first. The other one is tried only when
// the first holds nothing, so a dotted id and a file whose name carries
// no extension both stay reachable and neither can shadow an object
// that is there (design doc 0077).
func TestDeleteAddressesBothKindsOfObject(t *testing.T) {
	var asked []string
	live := map[string]bool{}
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/v1/bundle/{path...}", func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.PathValue("path"))
		if !live[r.PathValue("path")] {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	for _, tc := range []struct {
		name, arg string
		there     string
		want      []string
	}{
		{"a concept by its id", "terms/obsolete-kpi", "terms/obsolete-kpi.md", []string{"terms/obsolete-kpi.md"}},
		{"a file by its path", "insights/reading/weekly.png", "insights/reading/weekly.png", []string{"insights/reading/weekly.png"}},
		{"a concept whose id has a dot", "metrics/revenue.v2", "metrics/revenue.v2.md",
			[]string{"metrics/revenue.v2", "metrics/revenue.v2.md"}},
		{"a file with no extension", "tables/orders/Makefile", "tables/orders/Makefile",
			[]string{"tables/orders/Makefile.md", "tables/orders/Makefile"}},
	} {
		asked, live = nil, map[string]bool{tc.there: true}
		if err := cmdDelete(context.Background(), []string{tc.arg, "--url", srv.URL}); err != nil {
			t.Errorf("%s: %v", tc.name, err)
		}
		if !reflect.DeepEqual(asked, tc.want) {
			t.Errorf("%s: asked %v, want %v", tc.name, asked, tc.want)
		}
	}
}

// captureStderr runs f with os.Stderr redirected, and returns what was
// written to it.
func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stderr
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		var b bytes.Buffer
		_, _ = io.Copy(&b, r)
		done <- b.String()
	}()
	f()
	os.Stderr = saved
	_ = w.Close()
	out := <-done
	_ = r.Close()
	return out
}

// The document the CLI sends is the document it was given. The server
// stores the bytes it receives (design doc 0046 §2.2), so a rendering
// here is a rewrite there — and `ochakai put` rendered okf.Canonical
// from the parsed fields until design doc 0079, which dropped the
// producer's comments, key order and scalar style, and everything the
// family readers had no field for.
//
// SPEC §4.1 asks a consumer to preserve unknown keys when round-tripping;
// preserving the bytes is how ochakai does it, and it is the only way the
// CLI and the REST face store the same thing for the same file.
func TestPutSendsTheDocumentItWasGiven(t *testing.T) {
	const doc = "---\n" +
		"# the producer's note to the next reader\n" +
		"type: Attested Computation\n" +
		"runtime: bigquery\n" +
		"title: 'Quarterly revenue'\n" +
		"executor:\n" +
		"  resource: references/skills/run-on-bq.md\n" +
		"attester:\n" +
		"  resource: ''\n" +
		"quarter_start_month: 4\n" +
		"---\n" +
		"\n" +
		"# Computation\n" +
		"\n" +
		"    SELECT 1\n"

	var sent string
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/bundle/{path...}", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		sent = string(body)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(domain.View{ID: strings.TrimSuffix(r.PathValue("path"), ".md"), Document: sent})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "quarterly.md")
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmdPut(context.Background(),
		[]string{"computations/quarterly", "-f", path, "--url", srv.URL}); err != nil {
		t.Fatal(err)
	}
	if sent != doc {
		t.Errorf("`ochakai put` rewrote the document it was handed:\ngot:\n%s\nwant:\n%s", sent, doc)
	}

	// JSON input carries no document, so the canonical rendering is still
	// what it turns into (design doc 0043 §5).
	jsonPath := filepath.Join(t.TempDir(), "k.json")
	if err := os.WriteFile(jsonPath, []byte(`{"type":"Metric","title":"Revenue"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmdPut(context.Background(),
		[]string{"metrics/revenue", "-f", jsonPath, "--url", srv.URL}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sent, "type: Metric") || !strings.Contains(sent, "title: Revenue") {
		t.Errorf("JSON input did not render as an OKF document:\n%s", sent)
	}
}

// A document the write path refuses does not enter the knowledge base,
// and design doc 0079 §1 says why that stays the answer: the source
// bundle is untouched, so the refusal is reported rather than worked
// around, and storing the bytes as a file instead would have meant a
// rule frozen into the wire forever (design doc 0064).
//
// What the refusal must not do is spread. A file the refused concept's
// body pointed at is an object of the bundle at its own path, and
// attribution is derived rather than stored (design doc 0075 §5) — it
// used to be dropped along with the concept, so one refusal cost every
// file the document named. It is written now, and the line says nobody
// claims it.
func TestImportReportsARefusedConceptAndKeepsItsFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "computations"), 0o755); err != nil {
		t.Fatal(err)
	}
	doc := "---\ntype: Attested Computation\ntitle: Quarterly revenue\n---\n\n" +
		"See [the chart](/computations/quarterly/chart.png).\n"
	if err := os.WriteFile(filepath.Join(dir, "computations", "quarterly.md"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "computations", "quarterly"), 0o755); err != nil {
		t.Fatal(err)
	}
	chart := []byte("\x89PNG\r\n\x1a\nnot really a png")
	if err := os.WriteFile(filepath.Join(dir, "computations", "quarterly", "chart.png"), chart, 0o644); err != nil {
		t.Fatal(err)
	}

	var stored []string
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/bundle/{path...}", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		path := r.PathValue("path")
		// The write path refuses an Attested Computation with no runtime
		// (SPEC §10.2, design doc 0036 §3.4) — at every Content-Type,
		// because the bytes decide what an object is and nothing about a
		// refusal is negotiable (design doc 0079 §1).
		if strings.HasSuffix(path, "quarterly.md") {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "an Attested Computation needs a runtime (e.g. bigquery, postgres, dbt, python)"})
			return
		}
		stored = append(stored, path)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(domain.File{Path: path, Size: int64(len(body))})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	out, errOut := captureOutput(t, func() error {
		return cmdImport(context.Background(), []string{dir, "--url", srv.URL})
	})
	if !slices.Contains(stored, "computations/quarterly/chart.png") {
		t.Errorf("a refused concept took its file down with it; stored = %v", stored)
	}
	if slices.Contains(stored, "computations/quarterly.md") {
		t.Errorf("the refused document was stored anyway; stored = %v", stored)
	}
	for _, want := range []string{
		"refused as a concept and not stored",
		"needs a runtime",
		"still in the bundle you imported from",
	} {
		if !strings.Contains(errOut, want) {
			t.Errorf("the skip does not say %q:\n%s", want, errOut)
		}
	}
	if want := "imported 0 concepts (0 created, 0 updated, 0 unchanged, 0 attributed, 1 loose, 1 skipped, 1 notes)"; !strings.Contains(out, want) {
		t.Errorf("summary = %q, want %q", out, want)
	}

	// --strict is the posture for a sync nobody watches, and a document
	// the knowledge base does not hold is exactly what it must catch —
	// including through --dry-run, which has to reach the same verdict
	// (design doc 0061).
	if _, _, err := captureRun(t, func() error {
		return cmdImport(context.Background(), []string{dir, "--strict", "--url", srv.URL})
	}); err == nil {
		t.Error("--strict accepted a bundle with a refused concept")
	}
}

// captureOutput runs fn with stdout and stderr redirected, failing the
// test if fn does.
func captureOutput(t *testing.T, fn func() error) (out, errOut string) {
	t.Helper()
	out, errOut, err := captureRun(t, fn)
	if err != nil {
		t.Fatalf("%v\nstdout:\n%s\nstderr:\n%s", err, out, errOut)
	}
	return out, errOut
}

func captureRun(t *testing.T, fn func() error) (out, errOut string, err error) {
	t.Helper()
	outR, outW, perr := os.Pipe()
	if perr != nil {
		t.Fatal(perr)
	}
	errR, errW, perr := os.Pipe()
	if perr != nil {
		t.Fatal(perr)
	}
	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	err = fn()
	os.Stdout, os.Stderr = origOut, origErr
	outW.Close()
	errW.Close()
	o, _ := io.ReadAll(outR)
	e, _ := io.ReadAll(errR)
	return string(o), string(e), err
}

// A sandbox is announced where its numbers are, because the numbers are
// the thing it qualifies: they are restored away on a schedule, and so
// is whatever the reader writes (design doc 0087 §§3-4). The bundled web
// UI reads the same field for its banner; a caller who came by CLI, or a
// deployment serving no pages at all, is told here or nowhere.
func TestStatsAnnouncesADisposableSandbox(t *testing.T) {
	var body map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/stats", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(body)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	run := func(t *testing.T) string {
		t.Helper()
		orig := os.Stdout
		pr, pw, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		os.Stdout = pw
		err = cmdStats(context.Background(), []string{"--url", srv.URL})
		pw.Close()
		os.Stdout = orig
		if err != nil {
			t.Fatal(err)
		}
		out, err := io.ReadAll(pr)
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}

	body = map[string]any{"sandbox": true}
	out := run(t)
	if !strings.Contains(out, "sandbox\ttrue\t") {
		t.Errorf("a sandbox did not say so:\n%s", out)
	}
	if !strings.Contains(out, "disposable") {
		t.Errorf("the sandbox line does not say what it costs the reader:\n%s", out)
	}

	// Off the two announced postures there is nothing to announce, and a
	// line that appears on every ordinary deployment would be noise
	// rather than a warning.
	body = map[string]any{"insecure_dev": true}
	if out := run(t); !strings.Contains(out, "insecure_dev\ttrue\t") || strings.Contains(out, "sandbox\t") {
		t.Errorf("dev deployment announced wrongly:\n%s", out)
	}
	body = map[string]any{}
	if out := run(t); strings.Contains(out, "sandbox") || strings.Contains(out, "insecure_dev") {
		t.Errorf("ordinary deployment announced a posture:\n%s", out)
	}
}

// A hit says nothing about a ruling — the columns are the concept's —
// and there are no turned-down rows to count any more: a rejection is a
// deletion (design doc 0135), so what a curator declined is not in the
// ranking at all.
func TestPrintHitsKeepsTheRowToTheConcept(t *testing.T) {
	page := &apiclient.SearchResult{Hits: []domain.SearchHit{
		{ID: "metrics/a", Status: domain.StatusDraft, Title: "A", Score: 1},
		{ID: "metrics/b", Status: domain.StatusStable, Title: "B", Score: 0.5},
	}}
	stdout, stderr := capture(t, func() { printHits(page, "") })
	for _, want := range []string{"ochakai://metrics/a\tdraft\tA", "ochakai://metrics/b\tstable\tB"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the row format must not change:\n%s", stdout)
		}
	}
	if strings.Contains(stdout, "rejected") || strings.Contains(stderr, "turned down") {
		t.Errorf("a ruling must not reach the output:\n%s\n%s", stdout, stderr)
	}

	// And an empty result says so, rather than leaving a reader unable to
	// tell a miss from a command that did not run.
	_, stderr = capture(t, func() { printHits(&apiclient.SearchResult{}, "") })
	if !strings.Contains(stderr, "no concept matched") {
		t.Errorf("an empty ranking must say so:\n%s", stderr)
	}
	_, stderr = capture(t, func() { printHits(&apiclient.SearchResult{}, "failed") })
	if !strings.Contains(stderr, "the failed feed is empty") {
		t.Errorf("an empty feed must name itself:\n%s", stderr)
	}
}

// The first column is the key the reader asked to be ordered by, so only a
// feed has one. A search's score is not that key — its scale depends on
// whether the deployment embeds and the line cannot say which, which is
// what took `min_score` off the wire (0068 §3) — and a reverse lookup
// orders by address and carries a score of 0 (design doc 0110).
func TestPrintHitsLeadsWithTheOrderingKeyOnlyWhenThereIsOne(t *testing.T) {
	verified := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	hit := domain.SearchHit{
		ID: "metrics/revenue", Status: domain.StatusStable, Title: "売上", StaleAfter: "2026-12-31", VerifiedAt: &verified,
		// A lexical deployment's score, well above RRF's ~1/60 ceiling:
		// whichever scale it is on, it must not reach the line.
		Score: 1.73,
		Usage: &domain.Usage{SearchHits: 42, Failed: 7},
	}
	page := &apiclient.SearchResult{Hits: []domain.SearchHit{hit}}

	// A search and a reverse lookup both arrive with no feed, and both
	// lead with the address.
	for _, name := range []string{"search", "reverse lookup"} {
		stdout, _ := capture(t, func() { printHits(page, "") })
		line := strings.TrimSuffix(stdout, "\n")
		if got, want := strings.Count(line, "\t"), 2; got != want {
			t.Errorf("%s: want %d tabs (uri, status, title), got %d:\n%s", name, want, got, line)
		}
		if !strings.HasPrefix(line, "ochakai://metrics/revenue\t") {
			t.Errorf("%s: the address must be the first field:\n%s", name, line)
		}
		if strings.Contains(line, "1.73") || strings.Contains(line, "0.000") {
			t.Errorf("%s: a score must not reach the line:\n%s", name, line)
		}
	}

	// A feed still leads with its own key, which is a number or a date the
	// reader can compare against the row below it.
	for feed, want := range map[string]string{
		"verified_at": "2026-08-01T09:00:00Z\t",
		"usage":       "42\t",
		"failed":      "7\t",
		"stale_after": "2026-12-31\t",
	} {
		stdout, _ := capture(t, func() { printHits(page, feed) })
		if !strings.HasPrefix(stdout, want+"ochakai://metrics/revenue\t") {
			t.Errorf("the %s feed must lead with %q:\n%s", feed, want, stdout)
		}
	}
}

// capture runs fn with stdout and stderr redirected, and returns what it
// wrote to each.
func capture(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	fn()
	outW.Close()
	errW.Close()
	os.Stdout, os.Stderr = origOut, origErr
	out, err := io.ReadAll(outR)
	if err != nil {
		t.Fatal(err)
	}
	errs, err := io.ReadAll(errR)
	if err != nil {
		t.Fatal(err)
	}
	return string(out), string(errs)
}

// A note is about how a document was read, so a bundle written by
// another instance produces the same sentence once per concept in it.
// Printing them as they arrive means the demo bundle's ten `created`
// lines come back buried under ten identical forty-word notes, and a
// seeded catalog buries them under one per table. Grouping keeps the
// sentence and the concepts it was true of, and costs one line each.
func TestNoteTallyPrintsARepeatedNoteOnce(t *testing.T) {
	var tally noteTally
	for _, id := range []string{"c", "a", "b", "e", "d", "g", "f"} {
		tally.add("ochakai://metrics/"+id, []string{"a claim was kept"})
	}
	tally.add("ochakai://metrics/a", []string{"stale_after was dropped"})
	if tally.n != 8 {
		t.Errorf("tally.n = %d, want 8: --strict counts notes, not groups", tally.n)
	}
	_, stderr := capture(t, tally.report)
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	if len(lines) != 3 {
		t.Errorf("want three lines — two groups, one of them with its concepts:\n%s", stderr)
	}
	// The group of one keeps the shape it had before there were groups.
	if !strings.Contains(stderr, "note: ochakai://metrics/a: stale_after was dropped") {
		t.Errorf("a note true of one concept must name it inline:\n%s", stderr)
	}
	if !strings.Contains(stderr, "7 concepts: ochakai://metrics/a, ochakai://metrics/b") {
		t.Errorf("a group must be counted and listed in id order:\n%s", stderr)
	}
	if !strings.Contains(stderr, "and 2 more") {
		t.Errorf("a group longer than five must say how many it did not list:\n%s", stderr)
	}
}

// An unknown command answers with the one thing that is wrong, not with
// the command list: forty lines for a mistyped letter said nothing about
// the letter, and the same forty for a flag put before the command said
// nothing about the flag.
func TestUnknownCommandSaysWhatIsWrong(t *testing.T) {
	for _, tc := range []struct {
		cmd   string
		want  string
		avoid string
	}{
		{cmd: "serach", want: `Did you mean "search"?`},
		{cmd: "improt", want: `Did you mean "import"?`},
		{cmd: "--url", want: "A flag belongs to a command", avoid: "Did you mean"},
		{cmd: "frobnicate", want: "unknown command", avoid: "Did you mean"},
	} {
		var b strings.Builder
		unknownCommand(&b, tc.cmd)
		if !strings.Contains(b.String(), tc.want) {
			t.Errorf("%q: missing %q:\n%s", tc.cmd, tc.want, b.String())
		}
		if tc.avoid != "" && strings.Contains(b.String(), tc.avoid) {
			t.Errorf("%q: must not say %q:\n%s", tc.cmd, tc.avoid, b.String())
		}
		// Never the whole list: that is the thing this replaced.
		if strings.Contains(b.String(), "Client commands (talk to a server") {
			t.Errorf("%q: printed the whole usage:\n%s", tc.cmd, b.String())
		}
	}
}

// TestGetDownloadRefusesAnEscapingName pins the one place a client writes
// files where the server chose the names. A server holds every file name
// to a single path segment, so this can only fire for an answer that did
// not come from an ochakai — which is exactly when believing it would put
// bytes outside the directory the caller named.
func TestGetDownloadRefusesAnEscapingName(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/bundle/{path...}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(domain.View{
			ID: "metrics/revenue", Document: "---\ntype: Metric\n---\n\nbody\n",
			Files: []domain.File{{
				Name: "../escaped.txt", Path: "metrics/note.txt",
				MediaType: "text/plain", Size: 3,
			}},
		})
	})
	mux.HandleFunc("GET /api/v1/bundle/metrics/note.txt", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("pwn"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dir := t.TempDir()
	into := filepath.Join(dir, "files")
	err := cmdGet(context.Background(), []string{"metrics/revenue", "--download", into, "--url", srv.URL})
	if err == nil {
		t.Fatal("cmdGet saved a file the server named ../escaped.txt, want a refusal")
	}
	if !strings.Contains(err.Error(), "one path segment") {
		t.Errorf("error does not say what is wrong: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "escaped.txt")); !os.IsNotExist(err) {
		t.Errorf("escaped.txt was written outside the download directory")
	}
}
