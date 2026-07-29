package apiclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
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
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/search" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		got = r.URL.Query()
		_ = json.NewEncoder(w).Encode(map[string]any{"hits": []domain.SearchHit{
			{Summary: domain.Summary{Type: domain.TypeMetrics, ID: "revenue", Title: "売上"}, Score: 0.9},
		}})
	})
	page, err := c.Search(context.Background(), SearchParams{
		Query: "revenue", Types: []string{"metrics", "terms"}, Statuses: []string{"stable"},
		Tags: []string{"core"}, Limit: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Hits) != 1 || page.Hits[0].ID != "revenue" || page.Hits[0].Score != 0.9 {
		t.Errorf("hits = %+v", page.Hits)
	}
	if got.Get("q") != "revenue" || len(got["type"]) != 2 || got.Get("status") != "stable" ||
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
			{Type: domain.TypeComputations, ID: "monthly-revenue", Title: "月次売上"},
		}})
	})
	page, err := c.Search(context.Background(), SearchParams{Sort: "verified_at", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Hits) != 1 || page.Hits[0].ID != "monthly-revenue" || page.Hits[0].Score != 0 {
		t.Errorf("hits = %+v", page.Hits)
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
			{Summary: domain.Summary{Type: domain.TypeInsights, ID: "draft-a", Title: "草案"},
				Usage: &domain.Usage{SearchHits: 7, Fetches: 2}},
		}})
	})
	page, err := c.Search(context.Background(), SearchParams{Sort: "usage", Statuses: []string{"draft"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("sort") != "usage" || got.Get("status") != "draft" || got.Has("q") {
		t.Errorf("query = %v", got)
	}
	if len(page.Hits) != 1 || page.Hits[0].Usage == nil || page.Hits[0].Usage.SearchHits != 7 {
		t.Errorf("usage did not decode: %+v", page.Hits)
	}
}

// Browsing reads the JSON representation of a directory's index.md
// (design doc 0046 §3.7), so the path carries the directory rather than
// a query parameter.
func TestBrowseReadsTheDirectorysIndex(t *testing.T) {
	var path string
	c := newTestPair(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if a := r.Header.Get("Accept"); a != "application/json" {
			t.Errorf("Accept = %q, want the structured representation", a)
		}
		path = r.URL.Path
		_ = json.NewEncoder(w).Encode(BrowseResult{
			Dirs:    []BrowseDir{{Name: "sales", Count: 4}},
			Entries: []BrowseEntry{{Type: "queries", ID: "monthly-revenue", Title: "月次売上", Status: domain.StatusStable}},
		})
	})
	res, err := c.Browse(context.Background(), "queries/")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/api/v1/bundle/queries/index.md" {
		t.Errorf("path = %s", path)
	}
	if len(res.Dirs) != 1 || res.Dirs[0].Name != "sales" ||
		len(res.Entries) != 1 || res.Entries[0].ID != "monthly-revenue" {
		t.Errorf("res = %+v", res)
	}

	// The root's own index.
	if _, err := c.Browse(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if path != "/api/v1/bundle/index.md" {
		t.Errorf("root path = %s", path)
	}
}

func TestRevisionsHitsCanonicalPathAndSendsLimit(t *testing.T) {
	var got url.Values
	c := newTestPair(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/bundle/queries/sales/monthly-revenue/log.md" {
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

// Backlinks is the links_to reverse lookup on the search face now
// (design doc 0046 §3.5): one filter, no path of its own, and hits
// rather than an entries array.
func TestBacklinksAsksTheSearchFaceWithLinksTo(t *testing.T) {
	c := newTestPair(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/search" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("links_to"); got != "metrics/revenue" {
			t.Errorf("links_to = %q", got)
		}
		if r.URL.Query().Has("q") {
			t.Error("a reverse lookup sent a query; it has no text to rank by")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"hits": []domain.SearchHit{
			{Summary: domain.Summary{Type: domain.TypeInsights, ID: "revenue-reading", Title: "売上の読み方"}},
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
		_ = json.NewEncoder(w).Encode(domain.Usage{SearchHits: 12, Fetches: 4, Worked: 2})
	})
	u, err := c.Usage(context.Background(), "queries/sales/monthly-revenue")
	if err != nil {
		t.Fatal(err)
	}
	if u.SearchHits != 12 || u.Fetches != 4 || u.Worked != 2 {
		t.Errorf("usage = %+v", u)
	}
}

// The window travels only when the caller asked for one, so the server
// stays the one place the default lives (design doc 0051 §3.5).
func TestStatsSendsTheWindowOnlyWhenSet(t *testing.T) {
	for _, tc := range []struct {
		days int
		want string
	}{{0, ""}, {7, "7"}} {
		c := newTestPair(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/stats" {
				t.Errorf("path = %s", r.URL.Path)
			}
			if got := r.URL.Query().Get("days"); got != tc.want {
				t.Errorf("days = %q, want %q", got, tc.want)
			}
			_ = json.NewEncoder(w).Encode(domain.Stats{
				WindowDays: 30,
				Entries:    domain.StatsEntries{Total: 3, Status: map[string]int64{"draft": 1}},
				Misses: domain.StatsMisses{Recording: true, Count: 2,
					Queries: []domain.MissedQuery{{Query: "解約率の定義", Count: 2}}},
			})
		})
		st, err := c.Stats(context.Background(), tc.days)
		if err != nil {
			t.Fatal(err)
		}
		if st.Entries.Total != 3 || st.Entries.Status["draft"] != 1 ||
			!st.Misses.Recording || len(st.Misses.Queries) != 1 {
			t.Errorf("stats = %+v", st)
		}
	}
}

func TestErrorResponsesBecomeAPIErrors(t *testing.T) {
	c := newTestPair(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found: metrics/nope"})
	})
	_, err := c.Get(context.Background(), "metrics/nope")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %T %v, want *APIError", err, err)
	}
	if apiErr.StatusCode != http.StatusNotFound || apiErr.Message != "not found: metrics/nope" {
		t.Errorf("apiErr = %+v", apiErr)
	}
}

func TestPutSendsADocumentAndDelete204(t *testing.T) {
	const doc = "---\ntype: Metric\ntitle: 売上\n---\n\n本文。\n"
	c := newTestPair(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
				t.Errorf("Content-Type = %q, want text/markdown", ct)
			}
			if got := r.Header.Get("If-None-Match"); got != "*" {
				t.Errorf("If-None-Match = %q, want * for a create-only write", got)
			}
			body, _ := io.ReadAll(r.Body)
			if string(body) != doc {
				t.Errorf("body = %q, want the document verbatim", body)
			}
			w.Header().Add("Ochakai-Note", "status \"retired\" is not an OKF lifecycle value")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(domain.View{ID: "metrics/revenue",
				Summary: domain.Summary{Type: domain.TypeMetrics, ID: "metrics/revenue",
					Title: "売上", Status: domain.StatusDraft}})
		case http.MethodDelete:
			if r.URL.Path != "/api/v1/bundle/metrics/revenue.md" {
				t.Errorf("path = %s", r.URL.Path)
			}
			w.WriteHeader(http.StatusNoContent)
		}
	})
	got, created, changed, notes, err := c.Put(context.Background(), "metrics/revenue", []byte(doc), "", true)
	if err != nil || got.Summary.Status != domain.StatusDraft {
		t.Fatalf("put: %v, %+v", err, got)
	}
	if !created || !changed {
		t.Errorf("created = %v, changed = %v; a 201 is both", created, changed)
	}
	// A reinterpretation comes back rather than being swallowed.
	if len(notes) != 1 {
		t.Errorf("notes = %v, want the one the server reported", notes)
	}
	if err := c.Delete(context.Background(), "metrics/revenue"); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

// An update is the same call without the create-only precondition, and a
// server that wrote nothing says so in a header rather than in the body.
func TestPutReportsAnUnchangedWrite(t *testing.T) {
	c := newTestPair(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("If-Match"); got != `"abc123"` {
			t.Errorf("If-Match = %q, want the quoted version", got)
		}
		if r.Header.Get("If-None-Match") != "" {
			t.Error("an update must not send If-None-Match")
		}
		w.Header().Set("Ochakai-Unchanged", "true")
		_ = json.NewEncoder(w).Encode(domain.View{ID: "metrics/revenue"})
	})
	_, created, changed, _, err := c.Put(context.Background(), "metrics/revenue",
		[]byte("---\ntype: Metric\n---\n"), "abc123", false)
	if err != nil {
		t.Fatal(err)
	}
	if created || changed {
		t.Errorf("created = %v, changed = %v; a 200 with Ochakai-Unchanged is neither", created, changed)
	}
}

func TestExportStreamsBody(t *testing.T) {
	for _, tc := range []struct {
		name        string
		attachments bool
		wantParam   string
	}{
		{"with attachments", true, ""},
		{"markdown only", false, "false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestPair(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/bundle/" {
					t.Errorf("path = %s", r.URL.Path)
				}
				if a := r.Header.Get("Accept"); a != "application/gzip" {
					t.Errorf("Accept = %q, want the archive representation", a)
				}
				if got := r.URL.Query().Get("attachments"); got != tc.wantParam {
					t.Errorf("attachments = %q, want %q", got, tc.wantParam)
				}
				_, _ = w.Write([]byte("tarball-bytes"))
			})
			rc, err := c.Export(context.Background(), tc.attachments)
			if err != nil {
				t.Fatal(err)
			}
			defer rc.Close()
			data, _ := io.ReadAll(rc)
			if string(data) != "tarball-bytes" {
				t.Errorf("body = %q", data)
			}
		})
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
			Hits: []domain.ContextRank{{Type: "metrics", ID: "revenue", Score: 0.8}},
			Entries: []domain.View{{ID: "revenue", Document: "---\ntype: metrics\n---\n",
				Summary: domain.Summary{Type: "metrics", ID: "revenue", Title: "売上"}}},
		})
	})
	res, err := c.Context(context.Background(), ContextParams{
		Query: "why did revenue drop", Types: []string{"metrics"},
		Statuses: []string{"stable"}, Tags: []string{"core"},
		Prefixes: []string{"teams/growth", "company"}, Limit: 7, MinScore: 0.5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 || len(res.Entries) != 1 || res.Entries[0].Summary.Title != "売上" {
		t.Errorf("result = %+v", res)
	}
	if got.Get("q") != "why did revenue drop" || got.Get("type") != "metrics" ||
		got.Get("status") != "stable" || got.Get("tag") != "core" ||
		got.Get("limit") != "7" || got.Get("min_score") != "0.5" {
		t.Errorf("query = %v", got)
	}
	// Every scope travels as its own repeated parameter: joining them into
	// one value would ask the server for a single path with a comma in it.
	if want := []string{"teams/growth", "company"}; !slices.Equal(got["prefix"], want) {
		t.Errorf("prefix = %v, want %v", got["prefix"], want)
	}
}

// A file goes to the address it lives at, with nothing beside it saying
// where it really lives (design doc 0046 §3.3).
func TestAttachSendsBytesToTheAddress(t *testing.T) {
	body := []byte("attachment bytes")
	c := newTestPair(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v1/bundle/insights/sales/reading/weekly.png" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.URL.RawQuery != "" {
			t.Errorf("the write carries a parameter: %q", r.URL.RawQuery)
		}
		got, _ := io.ReadAll(r.Body)
		if string(got) != string(body) {
			t.Errorf("body = %q", got)
		}
		_ = json.NewEncoder(w).Encode(domain.Attachment{Name: "weekly.png", MediaType: "image/png", Size: int64(len(body))})
	})
	att, err := c.Attach(context.Background(), "insights/sales/reading", "weekly.png", body)
	if err != nil {
		t.Fatal(err)
	}
	if att.Name != "weekly.png" || att.MediaType != "image/png" {
		t.Errorf("attachment = %+v", att)
	}
}

func TestAttachmentFetchesBytesAndMediaType(t *testing.T) {
	c := newTestPair(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/bundle/insights/reading/weekly.png" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png bytes"))
	})
	data, mediaType, err := c.Attachment(context.Background(), "insights/reading/weekly.png")
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
		if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/bundle/insights/reading/weekly.png" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	if err := c.Detach(context.Background(), "insights/reading/weekly.png"); err != nil {
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

// Put's ifMatch becomes the If-Match precondition on the wire: absent
// when "", quoted into a valid ETag when the caller passes the bare
// version (the content_hash a read returned), verbatim when already an
// ETag. A stale version surfaces as a 412 APIError.
func TestPutSendsIfMatchAndMapsConflict(t *testing.T) {
	const stale = "0000000000000000000000000000000000000000000000000000000000000000"
	var got []string
	c := newTestPair(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v1/bundle/metrics/revenue.md" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		v, sent := r.Header["If-Match"]
		if !sent {
			v = []string{"(absent)"}
		}
		got = append(got, v[0])
		if v[0] == `"`+stale+`"` {
			w.WriteHeader(http.StatusPreconditionFailed)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "knowledge changed since it was read"})
			return
		}
		_ = json.NewEncoder(w).Encode(domain.View{ID: "metrics/revenue"})
	})
	doc := []byte("---\ntype: Metric\ntitle: 売上\n---\n")
	for _, ifMatch := range []string{"", "abc123", `"abc123"`} {
		if _, _, _, _, err := c.Put(context.Background(), "metrics/revenue", doc, ifMatch, false); err != nil {
			t.Fatalf("Put(ifMatch=%q): %v", ifMatch, err)
		}
	}
	want := []string{"(absent)", `"abc123"`, `"abc123"`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("If-Match on the wire = %q, want %q", got, want)
	}

	_, _, _, _, err := c.Put(context.Background(), "metrics/revenue", doc, stale, false)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("stale version: err = %v, want a 412 *APIError", err)
	}
	if apiErr.Message != "knowledge changed since it was read" {
		t.Errorf("Message = %q", apiErr.Message)
	}
}

// A positive budget reaches the server, so --json gets the server's
// budget semantics (entries that do not fit come back as outline). 0
// sends nothing: the rendered CLI path asks for everything and caps
// while printing.
func TestContextSendsBudgetOnlyWhenSet(t *testing.T) {
	var got []string
	c := newTestPair(t, func(w http.ResponseWriter, r *http.Request) {
		v := r.URL.Query().Get("budget")
		if v == "" {
			v = "(absent)"
		}
		got = append(got, v)
		_ = json.NewEncoder(w).Encode(ContextResult{})
	})
	for _, budget := range []int{0, 4000} {
		if _, err := c.Context(context.Background(), ContextParams{Query: "q", Budget: budget}); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"(absent)", "4000"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("budget on the wire = %q, want %q", got, want)
	}
}
