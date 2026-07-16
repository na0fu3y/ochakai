package apiclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// newTestPair returns a client wired to a test server. Plain http means
// no token source is resolved — exactly the local-development path.
func newTestPair(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(context.Background(), srv.URL+"/") // trailing slash must be tolerated
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSearchBuildsQueryAndDecodesHits(t *testing.T) {
	var got url.Values
	c := newTestPair(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/knowledge" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		got = r.URL.Query()
		_ = json.NewEncoder(w).Encode(map[string]any{"hits": []domain.SearchHit{
			{Knowledge: domain.Knowledge{Type: domain.TypeMetric, ID: "revenue", Title: "売上"}, Score: 0.9},
		}})
	})
	hits, err := c.Search(context.Background(), "revenue", []string{"metric", "term"}, []string{"verified"}, []string{"core"}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ID != "revenue" || hits[0].Score != 0.9 {
		t.Errorf("hits = %+v", hits)
	}
	if got.Get("q") != "revenue" || len(got["type"]) != 2 || got.Get("status") != "verified" ||
		got.Get("tag") != "core" || got.Get("limit") != "5" {
		t.Errorf("query = %v", got)
	}
}

func TestErrorResponsesBecomeAPIErrors(t *testing.T) {
	c := newTestPair(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found: metric/nope"})
	})
	_, err := c.Get(context.Background(), "metric", "nope")
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("err = %T %v, want *APIError", err, err)
	}
	if apiErr.StatusCode != http.StatusNotFound || apiErr.Message != "not found: metric/nope" {
		t.Errorf("apiErr = %+v", apiErr)
	}
}

func TestCompileRefusalIs422(t *testing.T) {
	c := newTestPair(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "metric x is not in the model"})
	})
	_, err := c.Compile(context.Background(), CompileRequest{})
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("err = %T %v, want 422 *APIError", err, err)
	}
}

func TestCreateSendsJSONBodyAndDelete204(t *testing.T) {
	c := newTestPair(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			if ct := r.Header.Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q", ct)
			}
			var k domain.Knowledge
			if err := json.NewDecoder(r.Body).Decode(&k); err != nil || k.ID != "revenue" {
				t.Errorf("body decode: %v, k=%+v", err, k)
			}
			k.Status = domain.StatusDraft
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(k)
		case http.MethodDelete:
			if r.URL.Path != "/api/v1/knowledge/metric/revenue" {
				t.Errorf("path = %s", r.URL.Path)
			}
			w.WriteHeader(http.StatusNoContent)
		}
	})
	created, err := c.Create(context.Background(), &domain.Knowledge{Type: domain.TypeMetric, ID: "revenue", Title: "売上"})
	if err != nil || created.Status != domain.StatusDraft {
		t.Fatalf("create: %v, %+v", err, created)
	}
	if err := c.Delete(context.Background(), "metric", "revenue"); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestExportStreamsBody(t *testing.T) {
	c := newTestPair(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/export" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte("tarball-bytes"))
	})
	rc, err := c.Export(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	if string(data) != "tarball-bytes" {
		t.Errorf("body = %q", data)
	}
}

func TestNewRejectsBadURLs(t *testing.T) {
	for _, bad := range []string{"", "not-a-url", "ftp://x", "localhost:8080"} {
		if _, err := New(context.Background(), bad); err == nil {
			t.Errorf("New(%q) succeeded, want error", bad)
		}
	}
}
