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
			{Knowledge: domain.Knowledge{Type: domain.TypeMetrics, ID: "revenue", Title: "売上"}, Score: 0.9},
		}})
	})
	hits, err := c.Search(context.Background(), SearchParams{
		Query: "revenue", Types: []string{"metrics", "terms"}, Statuses: []string{"verified"},
		Tags: []string{"core"}, Limit: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ID != "revenue" || hits[0].Score != 0.9 {
		t.Errorf("hits = %+v", hits)
	}
	if got.Get("q") != "revenue" || len(got["type"]) != 2 || got.Get("status") != "verified" ||
		got.Get("tag") != "core" || got.Get("limit") != "5" || got.Has("sort") {
		t.Errorf("query = %v", got)
	}
}

func TestSearchSortSendsSortParam(t *testing.T) {
	var got url.Values
	c := newTestPair(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		// The verified_at feed returns entries without scores.
		_ = json.NewEncoder(w).Encode(map[string]any{"hits": []domain.Knowledge{
			{Type: domain.TypeQueries, ID: "monthly-revenue", Title: "月次売上"},
		}})
	})
	hits, err := c.Search(context.Background(), SearchParams{Sort: "verified_at", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ID != "monthly-revenue" || hits[0].Score != 0 {
		t.Errorf("hits = %+v", hits)
	}
	if got.Get("sort") != "verified_at" || got.Get("limit") != "100" || got.Has("q") {
		t.Errorf("query = %v", got)
	}
}

// The sort=usage feed (draft review) sends sort=usage and decodes the
// per-hit usage object the plain search and verified_at feeds omit.
func TestSearchUsageSortDecodesUsage(t *testing.T) {
	var got url.Values
	c := newTestPair(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		_ = json.NewEncoder(w).Encode(map[string]any{"hits": []domain.SearchHit{
			{Knowledge: domain.Knowledge{Type: domain.TypeInsights, ID: "draft-a", Title: "草案"},
				Usage: &domain.Usage{SearchHits: 7, Fetches: 2}},
		}})
	})
	hits, err := c.Search(context.Background(), SearchParams{Sort: "usage", Statuses: []string{"draft"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("sort") != "usage" || got.Get("status") != "draft" || got.Has("q") {
		t.Errorf("query = %v", got)
	}
	if len(hits) != 1 || hits[0].Usage == nil || hits[0].Usage.SearchHits != 7 {
		t.Errorf("usage did not decode: %+v", hits)
	}
}

func TestBrowseBuildsQueryAndDecodesLevels(t *testing.T) {
	var got url.Values
	c := newTestPair(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/browse" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		got = r.URL.Query()
		_ = json.NewEncoder(w).Encode(BrowseResult{
			Dirs:    []BrowseDir{{Name: "sales", Count: 4}},
			Entries: []BrowseEntry{{Type: "queries", ID: "monthly-revenue", Title: "月次売上", Status: domain.StatusVerified}},
		})
	})
	res, err := c.Browse(context.Background(), "queries/")
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("prefix") != "queries/" {
		t.Errorf("query = %v", got)
	}
	if len(res.Dirs) != 1 || res.Dirs[0].Name != "sales" ||
		len(res.Entries) != 1 || res.Entries[0].ID != "monthly-revenue" {
		t.Errorf("res = %+v", res)
	}

	// Root level: no parameters at all.
	if _, err := c.Browse(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("root query = %v, want empty", got)
	}
}

func TestRevisionsHitsCanonicalPathAndSendsLimit(t *testing.T) {
	var got url.Values
	c := newTestPair(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/revisions/queries/sales/monthly-revenue" {
			t.Errorf("path = %s", r.URL.Path)
		}
		got = r.URL.Query()
		_ = json.NewEncoder(w).Encode(map[string]any{"revisions": []domain.Revision{
			{Rev: 2, Change: "update", ChangedBy: domain.Actor{Kind: domain.ActorHuman, Name: "na0"}},
		}})
	})
	revs, err := c.Revisions(context.Background(), "queries/sales/monthly-revenue", 10)
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("limit") != "10" {
		t.Errorf("query = %v", got)
	}
	if len(revs) != 1 || revs[0].Rev != 2 || revs[0].Change != "update" {
		t.Errorf("revs = %+v", revs)
	}

	// limit 0 = server default: no limit parameter on the wire.
	if _, err := c.Revisions(context.Background(), "queries/sales/monthly-revenue", 0); err != nil {
		t.Fatal(err)
	}
	if got.Has("limit") {
		t.Errorf("default query = %v, want no limit", got)
	}
}

func TestBacklinksHitsCanonicalPathAndDecodesEntries(t *testing.T) {
	c := newTestPair(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/backlinks/metrics/revenue" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"entries": []domain.Knowledge{
			{Type: domain.TypeInsights, ID: "revenue-reading", Title: "売上の読み方"},
		}})
	})
	entries, err := c.Backlinks(context.Background(), "metrics/revenue", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID != "revenue-reading" {
		t.Errorf("entries = %+v", entries)
	}
}

func TestUsageHitsCanonicalPathWithHierarchicalID(t *testing.T) {
	c := newTestPair(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/usage/queries/sales/monthly-revenue" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(domain.Usage{SearchHits: 12, Fetches: 4, Compiles: 2})
	})
	u, err := c.Usage(context.Background(), "queries/sales/monthly-revenue")
	if err != nil {
		t.Fatal(err)
	}
	if u.SearchHits != 12 || u.Fetches != 4 || u.Compiles != 2 {
		t.Errorf("usage = %+v", u)
	}
}

func TestErrorResponsesBecomeAPIErrors(t *testing.T) {
	c := newTestPair(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found: metrics/nope"})
	})
	_, err := c.Get(context.Background(), "metrics/nope")
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("err = %T %v, want *APIError", err, err)
	}
	if apiErr.StatusCode != http.StatusNotFound || apiErr.Message != "not found: metrics/nope" {
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
			if r.URL.Path != "/api/v1/knowledge/metrics/revenue" {
				t.Errorf("path = %s", r.URL.Path)
			}
			w.WriteHeader(http.StatusNoContent)
		}
	})
	created, err := c.Create(context.Background(), &domain.Knowledge{Type: domain.TypeMetrics, ID: "revenue", Title: "売上"})
	if err != nil || created.Status != domain.StatusDraft {
		t.Fatalf("create: %v, %+v", err, created)
	}
	if err := c.Delete(context.Background(), "metrics/revenue"); err != nil {
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

func TestContextBuildsQueryAndDecodesPack(t *testing.T) {
	var got url.Values
	c := newTestPair(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/context" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		got = r.URL.Query()
		_ = json.NewEncoder(w).Encode(ContextResult{
			Hits:    []domain.SearchHit{{Knowledge: domain.Knowledge{Type: "metrics", ID: "revenue"}, Score: 0.8}},
			Entries: []domain.Knowledge{{Type: "metrics", ID: "revenue", Title: "売上"}},
		})
	})
	res, err := c.Context(context.Background(), "why did revenue drop",
		[]string{"metrics"}, []string{"verified"}, []string{"core"}, 7, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 || len(res.Entries) != 1 || res.Entries[0].Title != "売上" {
		t.Errorf("result = %+v", res)
	}
	if got.Get("q") != "why did revenue drop" || got.Get("type") != "metrics" ||
		got.Get("status") != "verified" || got.Get("tag") != "core" ||
		got.Get("limit") != "7" || got.Get("min_score") != "0.5" {
		t.Errorf("query = %v", got)
	}
}

func TestAttachSendsBytesAndOKFPath(t *testing.T) {
	body := []byte("attachment bytes")
	c := newTestPair(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v1/attachments/insights/sales/reading/weekly.png" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("okf_path") != "images/weekly.png" {
			t.Errorf("okf_path = %q", r.URL.Query().Get("okf_path"))
		}
		got, _ := io.ReadAll(r.Body)
		if string(got) != string(body) {
			t.Errorf("body = %q", got)
		}
		_ = json.NewEncoder(w).Encode(domain.Attachment{Name: "weekly.png", MediaType: "image/png", Size: int64(len(body))})
	})
	att, err := c.Attach(context.Background(), "insights/sales/reading", "weekly.png", "images/weekly.png", body)
	if err != nil {
		t.Fatal(err)
	}
	if att.Name != "weekly.png" || att.MediaType != "image/png" {
		t.Errorf("attachment = %+v", att)
	}
}

func TestAttachmentFetchesBytesAndMediaType(t *testing.T) {
	c := newTestPair(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/attachments/insights/reading/weekly.png" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png bytes"))
	})
	data, mediaType, err := c.Attachment(context.Background(), "insights/reading", "weekly.png")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "png bytes" || mediaType != "image/png" {
		t.Errorf("data = %q, mediaType = %q", data, mediaType)
	}
}

func TestDetachHitsAttachmentPath(t *testing.T) {
	called := false
	c := newTestPair(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/attachments/insights/reading/weekly.png" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	if err := c.Detach(context.Background(), "insights/reading", "weekly.png"); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("no request sent")
	}
}

func TestReportOutcomePostsAndDecodesTotals(t *testing.T) {
	c := newTestPair(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/usage/queries/monthly-revenue" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var in map[string]string
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Errorf("bad body: %v", err)
		}
		if in["outcome"] != "failed" || in["note"] != "joins dropped rows" {
			t.Errorf("payload = %v", in)
		}
		_ = json.NewEncoder(w).Encode(domain.Usage{Worked: 3, Failed: 1})
	})
	u, err := c.ReportOutcome(context.Background(), "queries/monthly-revenue", "failed", "joins dropped rows")
	if err != nil {
		t.Fatal(err)
	}
	if u.Worked != 3 || u.Failed != 1 {
		t.Errorf("usage = %+v", u)
	}
}

