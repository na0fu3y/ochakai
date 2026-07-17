package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"flag"
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
	pos, err := parseArgs(fs, []string{"metric/revenue", "-f", "x.md"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(pos, []string{"metric/revenue"}) || *f != "x.md" {
		t.Errorf("pos = %v, f = %q", pos, *f)
	}
}

func TestSplitRef(t *testing.T) {
	for in, want := range map[string][2]string{
		"metric/revenue":           {"metric", "revenue"},
		"ochakai://metric/revenue": {"metric", "revenue"},
	} {
		typ, id, err := splitRef(in)
		if err != nil || typ != want[0] || id != want[1] {
			t.Errorf("splitRef(%q) = %s/%s, %v", in, typ, id, err)
		}
	}
	for _, bad := range []string{"revenue", "metric/", "/x"} {
		if _, _, err := splitRef(bad); err == nil {
			t.Errorf("splitRef(%q) succeeded, want error", bad)
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
	n, err := extractTarGz(dir, tarGz(t, map[string]string{"metric/revenue.md": "hello", "index.md": "idx"}))
	if err != nil || n != 2 {
		t.Fatalf("n = %d, err = %v", n, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "metric", "revenue.md"))
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
	for _, name := range []string{"search", "context", "get", "create", "update", "delete", "compile", "export", "import", "import-ossie", "use", "whoami", "ui", "completion"} {
		if _, ok := clientCommands[name]; !ok {
			t.Errorf("missing client command %q", name)
		}
	}
	if len(clientCommands) != 14 {
		t.Errorf("unexpected extra client commands: %d", len(clientCommands))
	}
	_ = domain.Types // keep the import honest
}

func TestRenderContext(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	human := domain.Actor{Kind: domain.ActorHuman, Name: "na0"}
	res := &apiclient.ContextResult{
		Hits: []domain.SearchHit{
			{Knowledge: domain.Knowledge{Type: domain.TypeQuery, ID: "monthly-revenue", Status: domain.StatusVerified, Title: "Monthly revenue"}, Score: 0.9},
			{Knowledge: domain.Knowledge{Type: domain.TypeTerm, ID: "arr", Status: domain.StatusDraft, Title: "ARR"}, Score: 0.1},
		},
		Entries: []domain.Knowledge{
			{
				Type: domain.TypeQuery, ID: "monthly-revenue", Status: domain.StatusVerified,
				Title:      "Monthly revenue",
				CreatedBy:  domain.Actor{Kind: domain.ActorAgent, Name: "claude"},
				VerifiedBy: &human, VerifiedAt: &now,
				Attrs: map[string]any{"question": "Revenue by month?", "sql": "SELECT 1\n"},
				Body:  "Use this over compile.",
			},
			{
				Type: domain.TypeInsight, ID: "revenue-seasonality", Status: domain.StatusDraft,
				Title: "Seasonality", CreatedBy: domain.Actor{Kind: domain.ActorAgent, Name: "claude"},
				Body: "Q4 peaks ~40% above baseline.",
			},
		},
	}

	var out bytes.Buffer
	renderContext(&out, res, 0)
	s := out.String()
	for _, want := range []string{
		"## ochakai://query/monthly-revenue (verified) — Monthly revenue",
		"verified by human:na0 on 2026-06-01; created by agent:claude",
		"Q: Revenue by month?",
		"```sql\nSELECT 1\n```",
		"Use this over compile.",
		"## ochakai://insight/revenue-seasonality (draft) — Seasonality",
		"Also relevant",
		"- ochakai://term/arr (draft) — ARR",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("output misses %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "ochakai://query/monthly-revenue (verified) — Monthly revenue\n- ") {
		t.Error("rendered entries must not repeat in the Also relevant list")
	}

	// A tiny budget still renders the first entry, then reports the rest.
	out.Reset()
	renderContext(&out, res, 10)
	s = out.String()
	if !strings.Contains(s, "## ochakai://query/monthly-revenue") {
		t.Errorf("first entry must render regardless of budget:\n%s", s)
	}
	if strings.Contains(s, "## ochakai://insight/revenue-seasonality") {
		t.Errorf("budget must drop later entries:\n%s", s)
	}
	if !strings.Contains(s, "1 more entries beyond --budget") {
		t.Errorf("omitted entries must be reported:\n%s", s)
	}
}
