package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"golang.org/x/oauth2"
)

// fakeBigQuery answers a dry run with the statement type it is given and
// records every request that reached it.
type fakeBigQuery struct {
	mu        sync.Mutex
	statement string
	dryStatus int
	calls     []string
	bodies    []map[string]any
	auth      []string
}

func (f *fakeBigQuery) start(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.calls = append(f.calls, r.Method+" "+r.URL.RequestURI())
		f.bodies = append(f.bodies, body)
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/jobs"):
			if f.dryStatus != 0 {
				w.WriteHeader(f.dryStatus)
				_, _ = io.WriteString(w, `{"error":{"message":"Syntax error"}}`)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"statistics": map[string]any{"query": map[string]any{"statementType": f.statement}}})
		default:
			_, _ = io.WriteString(w, `{"jobComplete":true,"rows":[]}`)
		}
	}))
	t.Cleanup(srv.Close)
	old := bigQueryUpstream
	bigQueryUpstream = srv.URL
	t.Cleanup(func() { bigQueryUpstream = old })
}

func postQuery(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8098/bigquery/v2/projects/billing/queries", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

var queryTokens = oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "person"})

// A SELECT runs, as the person, with the cap imposed here and nothing
// the proxy does not know passed on.
func TestQueryProxyRunsASelect(t *testing.T) {
	f := &fakeBigQuery{statement: "SELECT"}
	f.start(t)
	rec := postQuery(t, queryHandler(queryTokens),
		`{"query":"SELECT 1","maximumBytesBilled":"999999999999999","timeoutMs":20000,"createSession":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if len(f.calls) != 2 || f.calls[0] != "POST /bigquery/v2/projects/billing/jobs" || f.calls[1] != "POST /bigquery/v2/projects/billing/queries" {
		t.Fatalf("calls = %v, want a dry run and then the query", f.calls)
	}
	dry := f.bodies[0]["configuration"].(map[string]any)
	if dry["dryRun"] != true {
		t.Errorf("the first call was not a dry run: %v", dry)
	}
	q := f.bodies[1]
	if q["maximumBytesBilled"] != strconv.Itoa(maxBytesBilled) {
		t.Errorf("maximumBytesBilled = %v, want the proxy's cap", q["maximumBytesBilled"])
	}
	if _, ok := q["createSession"]; ok {
		t.Error("a field the proxy does not know was passed on")
	}
	if q["timeoutMs"] != float64(20000) || q["query"] != "SELECT 1" {
		t.Errorf("forwarded body = %v", q)
	}
	for _, a := range f.auth {
		if a != "Bearer person" {
			t.Errorf("Authorization = %q", a)
		}
	}
}

// What the popup's read-only scope guaranteed: nothing but a SELECT runs.
func TestQueryProxyRefusesAnythingButASelect(t *testing.T) {
	for _, kind := range []string{"INSERT", "DELETE", "CREATE_TABLE", "SCRIPT", "EXPORT_DATA"} {
		f := &fakeBigQuery{statement: kind}
		f.start(t)
		rec := postQuery(t, queryHandler(queryTokens), `{"query":"whatever"}`)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), kind) {
			t.Errorf("%s: status %d, body %s", kind, rec.Code, rec.Body)
		}
		if len(f.calls) != 1 {
			t.Errorf("%s: calls = %v, want the dry run only", kind, f.calls)
		}
	}
}

// A statement BigQuery cannot read fails with BigQuery's own words, and
// nothing runs.
func TestQueryProxyRelaysADryRunError(t *testing.T) {
	f := &fakeBigQuery{dryStatus: http.StatusBadRequest}
	f.start(t)
	rec := postQuery(t, queryHandler(queryTokens), `{"query":"SELEC 1"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Syntax error") {
		t.Errorf("status %d, body %s", rec.Code, rec.Body)
	}
	if len(f.calls) != 1 {
		t.Errorf("calls = %v", f.calls)
	}
}

func TestQueryProxyReadsResults(t *testing.T) {
	f := &fakeBigQuery{}
	f.start(t)
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8098/bigquery/v2/projects/billing/queries/job_1?location=US&maxResults=50", nil)
	rec := httptest.NewRecorder()
	queryHandler(queryTokens).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || len(f.calls) != 1 || f.calls[0] != "GET /bigquery/v2/projects/billing/queries/job_1?location=US&maxResults=50" {
		t.Errorf("status %d, calls %v", rec.Code, f.calls)
	}
}

// Nothing else of BigQuery is reachable through the proxy.
func TestQueryProxyServesOnlyTheTwoCalls(t *testing.T) {
	f := &fakeBigQuery{statement: "SELECT"}
	f.start(t)
	h := queryHandler(queryTokens)
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/bigquery/v2/projects/billing/jobs"},
		{http.MethodDelete, "/bigquery/v2/projects/billing/datasets/d"},
		{http.MethodGet, "/bigquery/v2/projects/billing/datasets"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(c.method, "http://127.0.0.1:8098"+c.path, strings.NewReader("{}")))
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: status %d", c.method, c.path, rec.Code)
		}
	}
	if len(f.calls) != 0 {
		t.Errorf("calls = %v", f.calls)
	}
}

// The page learns from its own markup whether the proxy runs queries,
// and another site cannot make it run one.
func TestUIRunsQueriesOnlyWhenItCan(t *testing.T) {
	f := &fakeBigQuery{statement: "SELECT"}
	f.start(t)
	for _, c := range []struct {
		queries oauth2.TokenSource
		want    string
	}{
		{nil, `<meta name="ochakai-runs-queries" content="">`},
		{queryTokens, `<meta name="ochakai-runs-queries" content="proxy">`},
	} {
		h, err := uiHandler("http://ochakai.internal", nil, c.queries)
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8098/", nil))
		if !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("page does not carry %s", c.want)
		}
	}
	h, _ := uiHandler("http://ochakai.internal", nil, queryTokens)
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8098/bigquery/v2/projects/billing/queries", strings.NewReader(`{"query":"SELECT 1"}`))
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || len(f.calls) != 0 {
		t.Errorf("a cross-site query: status %d, calls %v", rec.Code, f.calls)
	}
}
