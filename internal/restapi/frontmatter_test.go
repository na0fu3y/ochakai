package restapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/service"
)

// POST /api/v1/frontmatter is a function over a document (design doc
// 0130): no store, no id, nothing recorded — so these run against a
// Service with no database behind it, through checkedServer, which holds
// every request and response here to api/openapi.yaml.

type fmAnswer struct {
	Document string         `json:"document"`
	Keys     []string       `json:"keys"`
	Values   map[string]any `json:"values"`
}

func postFrontmatter(t *testing.T, body string) (int, fmAnswer) {
	t.Helper()
	srv := checkedServer(t, Handler(&service.Service{}))
	res, err := srv.Client().Post(srv.URL+"/api/v1/frontmatter", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/v1/frontmatter: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	var out fmAnswer
	if res.StatusCode == http.StatusOK {
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			t.Fatalf("decoding answer: %v", err)
		}
	}
	return res.StatusCode, out
}

func TestFrontmatterReadsAsStructure(t *testing.T) {
	status, got := postFrontmatter(t, `{"document":"---\ntype: Metric\ntitle: Revenue\ntags:\n  - finance\nthreshold: 5\n---\n\nbody\n"}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if strings.Join(got.Keys, ",") != "type,title,tags,threshold" {
		t.Errorf("keys = %v", got.Keys)
	}
	if got.Values["title"] != "Revenue" {
		t.Errorf("title = %#v", got.Values["title"])
	}
	// A producer key rides beside the ones the spec defines (SPEC §4.1):
	// the face is the document's frontmatter, not ochakai's envelope.
	if got.Values["threshold"] != float64(5) {
		t.Errorf("threshold = %#v", got.Values["threshold"])
	}
	// A read hands the same document back, byte for byte.
	if !strings.Contains(got.Document, "threshold: 5\n") || !strings.HasSuffix(got.Document, "\nbody\n") {
		t.Errorf("document changed on a read:\n%s", got.Document)
	}
}

func TestFrontmatterSetsAndUnsetsKeys(t *testing.T) {
	status, got := postFrontmatter(t, `{"document":"---\ntype: Metric\n# why this one\ntitle: Revenue\nstale_after: 2026-12-31\n---\n\nbody\n",`+
		`"set":{"sources":[{"resource":"bigquery://p/d/orders","title":"Orders"}]},"unset":["stale_after"]}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if strings.Contains(got.Document, "stale_after") {
		t.Errorf("unset key survived:\n%s", got.Document)
	}
	if !strings.Contains(got.Document, "# why this one\n") {
		t.Errorf("a comment was lost:\n%s", got.Document)
	}
	if !strings.Contains(got.Document, "sources:\n") || !strings.Contains(got.Document, "title: Orders") {
		t.Errorf("sources not written:\n%s", got.Document)
	}
	// values are read back out of the document that is going back, so the
	// form and the textarea below it cannot disagree.
	src, ok := got.Values["sources"].([]any)
	if !ok || len(src) != 1 {
		t.Fatalf("sources = %#v", got.Values["sources"])
	}
	if strings.Join(got.Keys, ",") != "type,title,sources" {
		t.Errorf("keys = %v", got.Keys)
	}
}

// Provenance is what this instance observed, never a document's claim
// (design doc 0009). Offering to write it from a form would be offering
// a field the write path ignores.
func TestFrontmatterRefusesServerOwnedKeys(t *testing.T) {
	for _, body := range []string{
		`{"document":"---\ntype: Metric\n---\n","set":{"verified":[{"by":"human:someone"}]}}`,
		`{"document":"---\ntype: Metric\n---\n","unset":["created_by"]}`,
	} {
		status, _ := postFrontmatter(t, body)
		if status != http.StatusBadRequest {
			t.Errorf("status = %d, want 400 for %s", status, body)
		}
	}
}

func TestFrontmatterRefusesWhatIsNotADocument(t *testing.T) {
	status, _ := postFrontmatter(t, `{"document":"just prose\n"}`)
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", status)
	}
}

// A key outside document/set/unset is a 400 naming it (design doc 0064
// §2, applied to the body the way readJSON applies it everywhere else).
func TestFrontmatterRefusesAnUnknownField(t *testing.T) {
	status, _ := postFrontmatter(t, `{"document":"---\ntype: Metric\n---\n","replace":{"title":"x"}}`)
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", status)
	}
}
