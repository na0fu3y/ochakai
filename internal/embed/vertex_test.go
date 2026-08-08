package embed

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newTestVertex points a Vertex at a fake API and captures request bodies.
func newTestVertex(t *testing.T, model string, embedContent bool, handler http.HandlerFunc) *Vertex {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Vertex{
		model:        model,
		dim:          4,
		base:         srv.URL + "/models/" + model,
		client:       srv.Client(),
		embedContent: embedContent,
	}
}

// The gemini-embedding-2 dialect: one :embedContent call per text, task
// folded into the prompt, outputDimensionality top-level (design doc
// 0020 §2.3 — wire shape verified against the live API on 2026-07-20).
func TestVertexEmbedContentDialect(t *testing.T) {
	var paths []string
	var bodies []embedContentRequest
	v := newTestVertex(t, "gemini-embedding-2", true, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		var req embedContentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		bodies = append(bodies, req)
		fmt.Fprintf(w, `{"embedding":{"values":[%d,0,0,0]}}`, len(bodies))
	})

	vecs, err := v.Embed(context.Background(), TaskQuery, []string{"売上とは", "利益とは"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 || vecs[0][0] != 1 || vecs[1][0] != 2 {
		t.Errorf("vecs = %v, want one vector per text in order", vecs)
	}
	for _, p := range paths {
		if p != "/models/gemini-embedding-2:embedContent" {
			t.Errorf("path = %q, want :embedContent", p)
		}
	}
	if got := bodies[0].Content.Parts[0].Text; got != "task: search result | query: 売上とは" {
		t.Errorf("query prompt = %q, want task-framed", got)
	}
	if bodies[0].OutputDimensionality != 4 {
		t.Errorf("outputDimensionality = %d, want 4", bodies[0].OutputDimensionality)
	}

	if _, err := v.Embed(context.Background(), TaskDocument, []string{"受注合計"}); err != nil {
		t.Fatal(err)
	}
	if got := bodies[2].Content.Parts[0].Text; got != "title: none | text: 受注合計" {
		t.Errorf("document prompt = %q, want title/text-framed", got)
	}
}

func TestVertexEmbedFile(t *testing.T) {
	data := []byte("\x89PNG fake bytes")
	var body embedContentRequest
	v := newTestVertex(t, "gemini-embedding-2", true, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		fmt.Fprint(w, `{"embedding":{"values":[1,2,3,4]}}`)
	})
	vec, err := v.EmbedFile(context.Background(), "chart.png", "image/png", data)
	if err != nil {
		t.Fatal(err)
	}
	if len(vec) != 4 {
		t.Errorf("vec = %v, want 4 dims", vec)
	}
	parts := body.Content.Parts
	if len(parts) != 2 || parts[0].Text != "title: chart.png" {
		t.Fatalf("parts = %+v, want filename text part then file part", parts)
	}
	if parts[1].InlineData == nil || parts[1].InlineData.MimeType != "image/png" ||
		parts[1].InlineData.Data != base64.StdEncoding.EncodeToString(data) {
		t.Errorf("file part = %+v, want base64 inline_data", parts[1].InlineData)
	}
}

// The :predict dialect is unchanged for earlier models, and file input
// reports unsupported instead of calling out.
func TestVertexPredictDialect(t *testing.T) {
	var body vertexRequest
	var path string
	v := newTestVertex(t, "gemini-embedding-001", false, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		fmt.Fprint(w, `{"predictions":[{"embeddings":{"values":[1,2,3,4]}}]}`)
	})
	if _, err := v.Embed(context.Background(), TaskDocument, []string{"受注合計"}); err != nil {
		t.Fatal(err)
	}
	if path != "/models/gemini-embedding-001:predict" {
		t.Errorf("path = %q, want :predict", path)
	}
	if len(body.Instances) != 1 || body.Instances[0].TaskType != string(TaskDocument) ||
		body.Instances[0].Content != "受注合計" {
		t.Errorf("instances = %+v, want raw content with task_type", body.Instances)
	}

	if _, err := v.EmbedFile(context.Background(), "chart.png", "image/png", []byte("x")); !errors.Is(err, ErrFileEmbeddingUnsupported) {
		t.Errorf("EmbedFile on text-only model = %v, want ErrFileEmbeddingUnsupported", err)
	}
}

func TestVertexHost(t *testing.T) {
	for loc, want := range map[string]string{
		"us-central1": "us-central1-aiplatform.googleapis.com",
		"global":      "aiplatform.googleapis.com",
		"us":          "aiplatform.googleapis.com",
		"eu":          "aiplatform.googleapis.com",
	} {
		if got := vertexHost(loc); got != want {
			t.Errorf("vertexHost(%q) = %q, want %q", loc, got, want)
		}
	}
}

// The width belongs to the model and is carried in code, not in a
// setting (design doc 0080 §3). Two things are pinned here: that every
// model ochakai names is one it has a width for, and that the width is
// still 768 — every vector any deployment has ever stored is 768 wide,
// and a change here silently strands all of them until `ochakai reembed`
// pays to rewrite them.
func TestModelWidths(t *testing.T) {
	for _, model := range Models() {
		dim, ok := Dimension(model)
		if !ok {
			t.Errorf("Models() names %q, which Dimension does not know", model)
		}
		if dim != 768 {
			t.Errorf("Dimension(%q) = %d, want 768: changing a stored width invalidates "+
				"every vector written at the old one", model, dim)
		}
	}
	if _, ok := Dimension("gemini-embedding-99"); ok {
		t.Error("a model nobody added has a width; the guess is what 0080 §3 refuses")
	}
}

func TestVertexDimensionMismatch(t *testing.T) {
	v := newTestVertex(t, "gemini-embedding-2", true, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"embedding":{"values":[1,2]}}`)
	})
	if _, err := v.Embed(context.Background(), TaskQuery, []string{"x"}); err == nil {
		t.Error("dimension mismatch should error")
	}
}

// A transient failure does not merely fail the call — search degrades to
// lexical and a write leaves vector search — so a 5xx is retried before
// the caller hears about it, and a 4xx is not: it is the request's own
// fault and would fail the same way three times.
func TestVertexRetriesTransientFailures(t *testing.T) {
	old := vertexBackoff
	vertexBackoff = time.Millisecond
	t.Cleanup(func() { vertexBackoff = old })

	calls := 0
	v := newTestVertex(t, "gemini-embedding-2", true, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls < 3 {
			http.Error(w, "backend overloaded", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, `{"embedding":{"values":[1,2,3,4]}}`)
	})
	if _, err := v.Embed(context.Background(), TaskQuery, []string{"x"}); err != nil {
		t.Fatalf("two 503s then success should succeed, got %v", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}

	calls = 0
	exhausted := newTestVertex(t, "gemini-embedding-2", true, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		http.Error(w, "backend overloaded", http.StatusServiceUnavailable)
	})
	if _, err := exhausted.Embed(context.Background(), TaskQuery, []string{"x"}); err == nil {
		t.Error("a 503 on every attempt should error")
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3 attempts before giving up", calls)
	}

	calls = 0
	badRequest := newTestVertex(t, "gemini-embedding-2", true, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		http.Error(w, "invalid argument", http.StatusBadRequest)
	})
	if _, err := badRequest.Embed(context.Background(), TaskQuery, []string{"x"}); err == nil {
		t.Error("a 400 should error")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want no retry on a 400", calls)
	}
}
