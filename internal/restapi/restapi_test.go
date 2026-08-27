package restapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/service"
	"github.com/na0fu3y/ochakai/internal/store"
)

// TestWriteErrorStatuses pins the error classification: client-input
// failures are 400s (via service.InvalidInputError, no string matching),
// everything unrecognized is a 500.
func TestWriteErrorStatuses(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"not found", store.ErrNotFound, http.StatusNotFound},
		{"already exists", store.ErrAlreadyExists, http.StatusConflict},
		{"not deleted", store.ErrNotDeleted, http.StatusConflict},
		{"no rejection to withdraw", store.ErrNoRejection, http.StatusConflict},
		{"if-match conflict", store.ErrConflict, http.StatusPreconditionFailed},
		{"invalid input", service.Invalidf("title is required"), http.StatusBadRequest},
		{"unsupported", service.Unsupportedf("files need GCS"), http.StatusNotImplemented},
		{"unknown", errors.New("connection reset"), http.StatusInternalServerError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeError(rec, c.err)
			if rec.Code != c.want {
				t.Errorf("writeError(%v) = %d, want %d", c.err, rec.Code, c.want)
			}
		})
	}
}

// TestWriteErrorHidesInternalDetailsOn500 pins the one status whose
// message a caller does not get verbatim: everything else above is text
// the service deliberately wrote for a caller to read, but an unmapped
// error can be a driver or SQL message, and a 500 must not put that on
// the wire (issue #365).
func TestWriteErrorHidesInternalDetailsOn500(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, errors.New("pq: password authentication failed for user \"ochakai\" at 10.0.0.5:5432"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q is not JSON: %v", rec.Body, err)
	}
	if strings.Contains(body["error"], "10.0.0.5") || strings.Contains(body["error"], "password") {
		t.Errorf(`error = %q, leaks the underlying error`, body["error"])
	}
}

// TestUnmatchedRoutesAnswerJSON pins the other half of the same envelope
// promise: a path nothing serves and a path served under a different
// method both used to fall through to net/http's own plain-text 404/405
// instead of the {"error": "..."} shape every other response here uses
// (issue #365).
func TestUnmatchedRoutesAnswerJSON(t *testing.T) {
	h := Handler(&service.Service{})
	cases := []struct {
		name, method, url string
		want              int
		wantErr           string
	}{
		{"unknown path", http.MethodGet, "/api/v1/nope", http.StatusNotFound, "not found"},
		{"wrong method", http.MethodPost, "/api/v1/search", http.StatusMethodNotAllowed, "method not allowed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(c.method, c.url, nil))
			if rec.Code != c.want {
				t.Fatalf("%s %s = %d, want %d (body: %s)", c.method, c.url, rec.Code, c.want, rec.Body)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("body %q is not JSON: %v", rec.Body, err)
			}
			if body["error"] != c.wantErr {
				t.Errorf("error = %q, want %q", body["error"], c.wantErr)
			}
		})
	}
}

// TestBadRequestValidation exercises the parameter checks that fail before
// any store access: malformed numbers are rejected instead of silently
// treated as unset, q cannot be combined with sort (matching the CLI), and
// a search without a query is a 400, not zero-fragment SQL handed to
// Postgres.
func TestBadRequestValidation(t *testing.T) {
	h := Handler(&service.Service{})
	cases := []struct {
		name, url, wantSubstr string
	}{
		{"invalid sort", "/api/v1/search?sort=created_at", "invalid sort"},
		{"sort with query", "/api/v1/search?sort=verified_at&q=revenue", "cannot be combined"},
		{"usage sort with query", "/api/v1/search?sort=usage&q=revenue", "cannot be combined"},
		{"failed sort with query", "/api/v1/search?sort=failed&q=revenue", "cannot be combined"},
		{"bad search limit", "/api/v1/search?limit=abc", "invalid limit"},
		{"bad log limit", "/api/v1/bundle/metrics/log.md?limit=abc", "invalid limit"},
		{"bad links_to limit", "/api/v1/search?links_to=metrics/revenue&limit=1.5", "invalid limit"},
		{"bad index prefix", "/api/v1/bundle/..%2Fescape/index.md", "invalid prefix"},
		// A limit in range is a well-formed number that is still refused:
		// design doc 0064 unifies every endpoint on reject-over-clamp, the
		// policy `days` already had.
		{"search limit too large", "/api/v1/search?q=x&limit=51", "between 1 and 50"},
		{"log limit too large", "/api/v1/bundle/metrics/log.md?limit=1001", "between 1 and 1000"},
		// A concept's ?history is 50/200 whatever the Accept says, because
		// there is one representation of it now (design doc 0102). It
		// used to answer 200 as markdown and this 400 as JSON — the same
		// request with two answers, which 0064 §14.1 had to write down
		// rather than fix.
		{"revisions limit too large", "/api/v1/bundle/metrics/revenue.md?history&limit=1001", "between 1 and 200"},
		// "fm." carries the OKF keys nothing else asks about (design
		// doc 0047). The five with a column behind them, and every key
		// OKF does not define, are refused on both surfaces that take a
		// filter, searching or listing, before any store access.
		{"fm.status", "/api/v1/search?q=x&fm.status=stable", "use status="},
		{"fm.tags while listing", "/api/v1/search?sort=usage&fm.tags=core", "use tag="},
		{"fm.sources", "/api/v1/search?q=x&fm.sources=bq://t", "use source=URI"},
		{"fm.stale_after", "/api/v1/search?q=x&fm.stale_after=2026-12-31", "use sort=stale_after"},
		{"producer key", "/api/v1/search?q=x&fm.owner=finance", "OKF does not define"},
		{"producer key while listing", "/api/v1/search?sort=usage&fm.owner=finance", "OKF does not define"},
		// A scalar key sent twice is a contradiction, not a question
		// (design doc 0064 §20.2). It used to be answered silently and in
		// two directions: the first value won everywhere except fm.{key},
		// where the last one did.
		{"repeated q", "/api/v1/search?q=a&q=b", `\"q\" was sent 2 times`},
		{"repeated limit", "/api/v1/search?q=x&limit=10&limit=50", `\"limit\" was sent 2 times`},
		{"repeated sort", "/api/v1/search?sort=usage&sort=failed", `\"sort\" was sent 2 times`},
		{"repeated fm key", "/api/v1/search?q=x&fm.owner=a&fm.owner=b", `\"fm.owner\" was sent 2 times`},
		{"repeated days", "/api/v1/stats?days=7&days=30", `\"days\" was sent 2 times`},
		{"repeated history limit", "/api/v1/bundle/metrics/log.md?limit=1&limit=2", `\"limit\" was sent 2 times`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.url, nil))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("GET %s = %d, want 400 (body: %s)", c.url, rec.Code, rec.Body)
			}
			if !strings.Contains(rec.Body.String(), c.wantSubstr) {
				t.Errorf("GET %s body %q does not mention %q", c.url, rec.Body, c.wantSubstr)
			}
		})
	}
}

// restOperations pairs each REST operation with a concrete address
// that reaches it through Handler's router and the api/openapi.yaml
// operation (method + path template) it implements. Shared by every
// test below that needs to address all of them, so there is one list of
// URLs to keep in sync with the router rather than one per test.
var restOperations = []struct{ name, method, url, spec string }{
	{"search", http.MethodGet, "/api/v1/search", "GET /api/v1/search"},
	{"bundle get", http.MethodGet, "/api/v1/bundle/metrics/revenue.md", "GET /api/v1/bundle/{path}"},
	{"bundle put", http.MethodPut, "/api/v1/bundle/metrics/revenue.md", "PUT /api/v1/bundle/{path}"},
	{"bundle delete", http.MethodDelete, "/api/v1/bundle/metrics/revenue.md", "DELETE /api/v1/bundle/{path}"},
	{"review", http.MethodPost, "/api/v1/review/metrics/revenue", "POST /api/v1/review/{id}"},
	{"usage get", http.MethodGet, "/api/v1/usage/metrics/revenue", "GET /api/v1/usage/{id}"},
	{"usage post", http.MethodPost, "/api/v1/usage/metrics/revenue", "POST /api/v1/usage/{id}"},
	{"stats", http.MethodGet, "/api/v1/stats", "GET /api/v1/stats"},
	{"move", http.MethodPost, "/api/v1/move", "POST /api/v1/move"},
	{"frontmatter", http.MethodPost, "/api/v1/frontmatter", "POST /api/v1/frontmatter"},
	{"reembed", http.MethodPost, "/api/v1/reembed", "POST /api/v1/reembed"},
	{"access get", http.MethodGet, "/api/v1/access", "GET /api/v1/access"},
	{"access put", http.MethodPut, "/api/v1/access", "PUT /api/v1/access"},
}

// TestUnknownQueryParamsAreRejected pins design doc 0064's strict
// allowlist: after the freeze, an unrecognized query parameter is a 400
// naming it rather than a silent no-op — the asymmetry with an unknown
// fm.* key (already checked against the concept's own vocabulary, see
// TestBadRequestValidation) that made the dry_run false-green possible in
// the first place. Covers every one of REST's 11 operations with a key
// that appears in no allowlist at all; TestUnknownQueryParamsMatchSpec
// below covers the sharper case, a key that is real on some operation and
// not this one.
func TestUnknownQueryParamsAreRejected(t *testing.T) {
	h := Handler(&service.Service{})
	for _, op := range restOperations {
		t.Run(op.name, func(t *testing.T) {
			url := op.url + "?typo=1"
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(op.method, url, nil))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%s %s = %d, want 400 (body: %s)", op.method, url, rec.Code, rec.Body)
			}
			if !strings.Contains(rec.Body.String(), "typo") {
				t.Errorf("%s %s body %q does not name the unknown parameter", op.method, url, rec.Body)
			}
		})
	}
}

// TestFrontmatterParamsAreNotUnknown confirms fm.* stays exempt from the
// allowlist above — it is validated by content against the concept's own
// vocabulary (checkedFilter), not by a fixed list of names.
func TestFrontmatterParamsAreNotUnknown(t *testing.T) {
	h := Handler(&service.Service{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/search?q=x&fm.owner=finance", nil))
	if strings.Contains(rec.Body.String(), "unknown query parameter") {
		t.Errorf("fm.owner was treated as an unknown top-level parameter: %s", rec.Body)
	}
}

// TestFrontmatterEmptyKeyIsRejected pins the third silent no-op issue
// #409 found once the first two were closed: "fm." itself — an empty
// frontmatter key — has "fm." as a prefix of itself, so the fm.*
// exemption in rejectUnknownParams waved it through; it is also the
// sentinel a caller passes in allowed to grant that exemption, so a
// plain membership check waved it through a second way. Either path
// handed it to frontmatterFilter, which drops a key of "" without
// telling anyone — a client asking `?fm.=x` got a 200 that quietly
// dropped the filter it asked for.
func TestFrontmatterEmptyKeyIsRejected(t *testing.T) {
	h := Handler(&service.Service{})
	for _, url := range []string{"/api/v1/search?q=x&fm.=x"} {
		t.Run(url, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("GET %s = %d, want 400 (body: %s)", url, rec.Code, rec.Body)
			}
			if !strings.Contains(errMessage(t, rec), `unknown query parameter "fm."`) {
				t.Errorf("GET %s body %q does not name fm. as unknown", url, rec.Body)
			}
		})
	}
}

// errMessage decodes a writeError response's {"error": "..."} envelope,
// so a check against its text is not defeated by JSON's own quoting: the
// wire form of `unknown query parameter "fm."` is
// `unknown query parameter \"fm.\"`, and a literal %q substring never
// matches that escaped form.
func errMessage(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q is not JSON: %v", rec.Body, err)
	}
	return body["error"]
}

// TestUnknownQueryParamNamedIsDeterministic pins that naming the
// offending key in a 400 does not depend on Go's randomized map
// iteration: with two unknown keys, the same one is named every time.
func TestUnknownQueryParamNamedIsDeterministic(t *testing.T) {
	h := Handler(&service.Service{})
	for i := range 20 {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/stats?zzz=1&aaa=1", nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("run %d: = %d, want 400 (body: %s)", i, rec.Code, rec.Body)
		}
		if !strings.Contains(rec.Body.String(), "aaa") {
			t.Errorf("run %d: body %q does not name the lexicographically first unknown key", i, rec.Body)
		}
	}
}

// TestReviewRefusesRulingsItCannotRecord pins the two ways a ruling is
// refused before any store access (design doc 0055 §§3.2-3.3): a value
// outside the vocabulary, and a note where nothing can carry one.
//
// A note is the case worth a test. Dropping it would be the quiet
// failure: a reviewer who wrote down why, and finds later that nowhere
// kept it, is worse off than one told at the time. It belongs to
// `rejected` alone — a verification says the concept is right as it
// stands, and anything more to say about it belongs in the concept.
//
// These live here rather than beside the integration test because the
// checked server validates every request against the spec, and a request
// meant to be refused is one the spec is right to reject.
func TestReviewRefusesRulingsItCannotRecord(t *testing.T) {
	h := Handler(&service.Service{})
	cases := []struct {
		name, body, wantSubstr string
	}{
		{"unknown ruling", `{"ruling":"approved"}`, "ruling must be"},
		{"no ruling", `{}`, "ruling must be"},
		{"note on a verification", `{"ruling":"verified","note":"looks right"}`, "carries no note"},
		{"note on a withdrawal", `{"ruling":"withdrawn","note":"changed my mind"}`, "carries no note"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/review/metrics/revenue", strings.NewReader(c.body))
			req.Header.Set("Content-Type", "application/json")
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("POST %s = %d, want 400 (body: %s)", c.body, rec.Code, rec.Body)
			}
			if !strings.Contains(rec.Body.String(), c.wantSubstr) {
				t.Errorf("POST %s body %q does not mention %q", c.body, rec.Body, c.wantSubstr)
			}
		})
	}
}

// TestBundleAddressesRefuseByPath pins the one refusal that is a
// property of the address rather than of what is stored at it: the two
// names OKF reserves per directory are generated from the bundle, so a
// write to one is a conflict with what the address *is* (409, design doc
// 0046 §§3.5, 3.7-3.8) — never a 404, and never the write path taking
// the bytes and storing a file that would then be shadowed by the
// generated document forever.
//
// Every other path is an object of the bundle and is read and written
// like one; that is the integration test's, since it needs a store.
//
// Through checkedServer, so each status is one the contract declares:
// the spec only covers what a test exercises, and until this one there
// was no request here to cover the refusals at all (issue #270).
func TestBundleAddressesRefuseByPath(t *testing.T) {
	srv := checkedServer(t, Handler(&service.Service{}))
	defer srv.Close()

	cases := []struct {
		name, method, path string
		want               int
		wantSubstr         string
	}{
		{"put index.md", http.MethodPut, "index.md", http.StatusConflict, "generated from the bundle"},
		{"put nested log.md", http.MethodPut, "metrics/log.md", http.StatusConflict, "log.md is generated"},
		{"delete index.md", http.MethodDelete, "index.md", http.StatusConflict, "index.md is generated"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req, err := http.NewRequest(c.method, srv.URL+"/api/v1/bundle/"+c.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			// A PUT at a `.md` path is a concept write, so it names the
			// media type the spec declares for one. It used to be
			// excluded from the validator entirely, because a body with
			// no `type` was a file write and a file's bytes are opaque —
			// which is exactly the conflation design doc 0100 removed.
			if c.method == http.MethodPut {
				req.Header.Set("Content-Type", "text/markdown")
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != c.want {
				t.Fatalf("%s %s = %d, want %d (body: %s)", c.method, c.path, resp.StatusCode, c.want, body)
			}
			if !strings.Contains(string(body), c.wantSubstr) {
				t.Errorf("%s %s said %q, which does not mention %q", c.method, c.path, body, c.wantSubstr)
			}
		})
	}
}

// TestBundleParamsAreRejectedOutsideTheirMode pins design doc 0064 §2's
// completion (issue #470): history, limit and files are declared for the
// whole GET /api/v1/bundle/{path} address, but each is read in only some
// of its modes. Sent to a mode that does not read it, each is now a 400
// naming the mode, rather than a silently ignored value.
func TestBundleParamsAreRejectedOutsideTheirMode(t *testing.T) {
	h := Handler(&service.Service{})
	cases := []struct {
		name, url, accept string
	}{
		{"limit on index.md", "/api/v1/bundle/index.md?limit=5", ""},
		{"limit on a concept", "/api/v1/bundle/metrics/revenue.md?limit=5", ""},
		{"files on index.md", "/api/v1/bundle/index.md?files=false", ""},
		{"files on log.md", "/api/v1/bundle/metrics/log.md?files=false", ""},
		{"files on a concept", "/api/v1/bundle/metrics/revenue.md?files=false", "application/json"},
		{"history with the archive", "/api/v1/bundle/metrics/revenue.md?history", "application/gzip"},
		{"limit with the archive", "/api/v1/bundle/?limit=5", "application/gzip"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, c.url, nil)
			if c.accept != "" {
				req.Header.Set("Accept", c.accept)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("GET %s = %d, want 400 (body: %s)", c.url, rec.Code, rec.Body)
			}
			if !strings.Contains(rec.Body.String(), "not meaningful") {
				t.Errorf("body %q does not say the parameter is out of mode", rec.Body)
			}
		})
	}
}

// TestAttachWithoutBlobStore pins the 501: on an instance without GCS
// (design doc 0013), attaching fails before any validation or DB access
// with the config hint.
func TestAttachWithoutBlobStore(t *testing.T) {
	h := Handler(&service.Service{Store: &store.Store{}})
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 16)...)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut,
		"/api/v1/bundle/insights/revenue/weekly.png", bytes.NewReader(png)))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("PUT file without GCS = %d, want 501 (body: %s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "OCHAKAI_GCS_BUCKET") {
		t.Errorf("body %q does not carry the config hint", rec.Body)
	}
}

// TestOversizedBodies pins the 413 classification: only a body that
// exceeds the limit is 413 — one byte over is enough, and the check
// fires before any service call.
func TestOversizedBodies(t *testing.T) {
	h := Handler(&service.Service{})
	cases := []struct {
		name, method, url string
		size              int
		wantSubstr        string
	}{
		{"file", http.MethodPut, "/api/v1/bundle/insights/revenue/weekly.png",
			domain.MaxFileSize + 1, "object exceeds"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(c.method, c.url, bytes.NewReader(make([]byte, c.size)))
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("%s %s (%d bytes) = %d, want 413 (body: %s)", c.method, c.url, c.size, rec.Code, rec.Body)
			}
			if !strings.Contains(rec.Body.String(), c.wantSubstr) {
				t.Errorf("body %q does not mention %q", rec.Body, c.wantSubstr)
			}
		})
	}
}

// TestUnknownBodyFieldsAreRejected pins design doc 0064 §2's completion
// (issue #470): a JSON body key none of these three operations declare is
// a 400 naming it, the same as an unrecognized query parameter, rather
// than a 200 that silently dropped it — closing the body's version of the
// hole that let ?dry_run= go unenforced.
func TestUnknownBodyFieldsAreRejected(t *testing.T) {
	h := Handler(&service.Service{})
	cases := []struct {
		name, method, url, body string
	}{
		{"move", http.MethodPost, "/api/v1/move", `{"from":"a","to":"b","force":true}`},
		{"review", http.MethodPost, "/api/v1/review/metrics/revenue", `{"ruling":"verified","rulling":"verified"}`},
		{"usage", http.MethodPost, "/api/v1/usage/metrics/revenue", `{"outcome":"worked","notes":"x"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(c.method, c.url, strings.NewReader(c.body)))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%s %s = %d, want 400 (body: %s)", c.method, c.url, rec.Code, rec.Body)
			}
			if !strings.Contains(rec.Body.String(), "unknown field") {
				t.Errorf("body %q does not name the unknown field", rec.Body)
			}
		})
	}
}

// TestMisspeltCaseIsAnUnknownField pins the half of design doc 0064 §2
// that was written and not kept until 0122: encoding/json v1 matched a
// field name without regard to case, so a body naming RULING reached a
// handler that had declared ruling and was answered 200. The rule was
// always that a key the operation does not declare is a 400 naming it,
// and "ruling" spelt another way is not a key it declares.
func TestMisspeltCaseIsAnUnknownField(t *testing.T) {
	h := Handler(&service.Service{})
	for _, body := range []string{
		`{"RULING":"verified"}`,
		`{"Ruling":"verified"}`,
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/review/metrics/revenue", strings.NewReader(body)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("POST review %s = %d, want 400 (body: %s)", body, rec.Code, rec.Body)
		}
		if !strings.Contains(rec.Body.String(), "unknown field") {
			t.Errorf("body %q does not name the unknown field", rec.Body)
		}
	}
}

// TestRepeatedBodyFieldIsRejected pins 0122's other refusal: a name
// given twice used to be answered with the last of them, which decided
// silently which of two values the caller meant. Naming the field is
// what makes the answer actionable, so the message carries it.
func TestRepeatedBodyFieldIsRejected(t *testing.T) {
	h := Handler(&service.Service{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/review/metrics/revenue",
		strings.NewReader(`{"ruling":"verified","ruling":"rejected"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST review with a repeated field = %d, want 400 (body: %s)", rec.Code, rec.Body)
	}
	// The quotes around the name are escaped in the JSON error body, so
	// the assertion looks for the name and the reason, not the spelling
	// of the message.
	if !strings.Contains(rec.Body.String(), "ruling") || !strings.Contains(rec.Body.String(), "twice") {
		t.Errorf("body %q does not say which field appeared twice", rec.Body)
	}
}

// TestTrailingBodyContentIsRejected pins the other half of readJSON's
// strictness: content after the JSON value is a 400, not silently
// discarded.
func TestTrailingBodyContentIsRejected(t *testing.T) {
	h := Handler(&service.Service{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/move",
		strings.NewReader(`{"from":"a","to":"b"}{"from":"c","to":"d"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST move with trailing content = %d, want 400 (body: %s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "single JSON value") {
		t.Errorf("body %q does not say why", rec.Body)
	}
}

// TestReportOutcomeBadRequests pins the outcome endpoint's client-error
// paths, which all fire before any store access: malformed JSON and an
// unknown outcome are 400s.
func TestReportOutcomeBadRequests(t *testing.T) {
	h := Handler(&service.Service{})
	cases := []struct {
		name, body, wantSubstr string
	}{
		{"invalid JSON", "not json", "invalid JSON"},
		{"unknown outcome", `{"outcome":"misleading"}`, "invalid outcome"},
		{"missing outcome", `{}`, "invalid outcome"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/usage/queries/monthly-revenue", strings.NewReader(c.body))
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("POST usage = %d, want 400 (body: %s)", rec.Code, rec.Body)
			}
			if !strings.Contains(rec.Body.String(), c.wantSubstr) {
				t.Errorf("body %q does not mention %q", rec.Body, c.wantSubstr)
			}
		})
	}
}

// TestETagRoundTrip pins the ETag/If-Match wire format: etagOf quotes
// the concept's content hash, and parseIfMatch reads it (and absence)
// back — so a value from a response header is accepted verbatim on the
// next request.
func TestETagRoundTrip(t *testing.T) {
	const hash = "9f2c1b0d4e5a6789abcdef0123456789abcdef0123456789abcdef0123456789"
	k := &domain.Knowledge{ID: "metrics/x", ContentHash: hash}
	etag := etagOf(k)
	if etag != `"`+hash+`"` {
		t.Fatalf("etagOf = %s", etag)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/v1/bundle/metrics/x.md", nil)
	req.Header.Set("If-Match", etag)
	got, err := parseIfMatch(req)
	if err != nil || got == nil || *got != hash {
		t.Errorf("parseIfMatch(%s) = %v, %v, want %q", etag, got, err, hash)
	}

	// Absence carries no version precondition.
	r := httptest.NewRequest(http.MethodPut, "/x", nil)
	if p, err := parseIfMatch(r); p != nil || err != nil {
		t.Errorf("parseIfMatch of an absent If-Match = %v, %v; want nil, nil", p, err)
	}

	// "*" is the one value refused. RFC 9110 §13.1.1 gives it a meaning
	// ochakai does not implement ("only if a representation exists"), and
	// reading it as no precondition at all silently created for a caller
	// who sent it to prevent exactly that. A 400 can still be given RFC's
	// meaning after the freeze; either behaviour, frozen, could not
	// (design doc 0064 §20.1).
	r = httptest.NewRequest(http.MethodPut, "/x", nil)
	r.Header.Set("If-Match", "*")
	p, err := parseIfMatch(r)
	if p != nil || err == nil {
		t.Errorf(`parseIfMatch(If-Match:"*") = %v, %v; want nil and a 400`, p, err)
	}

	// The version is an opaque token now, so there is no format to
	// reject: a value that never matched anything fails the precondition
	// with a 412, which is what a merely stale one does too. Rejecting it
	// at parse time would only tell a client that its own ETag looked
	// wrong to us, which is not something we can know.
	r = httptest.NewRequest(http.MethodPut, "/x", nil)
	r.Header.Set("If-Match", `"not-a-hash"`)
	if p, err := parseIfMatch(r); err != nil || p == nil || *p != "not-a-hash" {
		t.Errorf("parseIfMatch of an unknown token = %v, %v, want it passed through", p, err)
	}
}

// An oversized document is a 413, matching what the file path
// (readBody) already answers. Calling it a parse failure sent the caller
// hunting for a syntax error in a payload that merely did not fit.
func TestOversizedDocumentIsTooLarge(t *testing.T) {
	h := Handler(&service.Service{})
	body := "---\ntype: Metric\n---\n\n" + strings.Repeat("a", 6<<20)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/bundle/metrics/x.md", strings.NewReader(body))
	req.Header.Set("Content-Type", "text/markdown")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", w.Code, http.StatusRequestEntityTooLarge)
	}
	if got := w.Body.String(); !strings.Contains(got, "exceeds") {
		t.Errorf("body = %s", got)
	}
}

// A JSON body is refused with the reason rather than a parse error: the
// caller is written against the typed write surface design doc 0043 §3.5
// removed, and needs to be told that — not told its document has no
// frontmatter.
func TestJSONWriteSaysToSendADocument(t *testing.T) {
	h := Handler(&service.Service{})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/bundle/metrics/x.md",
		strings.NewReader(`{"type":"Metric","id":"metrics/x"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnsupportedMediaType)
	}
	if got := w.Body.String(); !strings.Contains(got, "OKF document") {
		t.Errorf("body = %s", got)
	}
}

// A boolean query parameter refuses what it cannot read, like the numeric
// ones. "?purge=1" used to fall through to a soft delete and answer 204,
// telling the caller an id had been freed that was still taken -- and the
// two calls of a purge are meant to be deliberate (design doc 0031).
func TestBooleanQueryParamsRejectUnreadableValues(t *testing.T) {
	h := Handler(&service.Service{})
	cases := []struct {
		name, method, url, accept, wantSubstr string
	}{
		{"purge=1", http.MethodDelete, "/api/v1/bundle/metrics/revenue.md?purge=1", "", "invalid purge"},
		{"purge=True", http.MethodDelete, "/api/v1/bundle/metrics/revenue.md?purge=True", "", "invalid purge"},
		{"purge=yes", http.MethodDelete, "/api/v1/bundle/metrics/revenue.md?purge=yes", "", "invalid purge"},
		// The archive of the whole bundle: the parameter is read before
		// the snapshot is opened, which is what lets this run against a
		// service with no store at all.
		{"files=0", http.MethodGet, "/api/v1/bundle/?files=0", "application/gzip", "invalid files"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(c.method, c.url, nil)
			if c.accept != "" {
				req.Header.Set("Accept", c.accept)
			}
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%s %s = %d, want 400 (body: %s)", c.method, c.url, rec.Code, rec.Body)
			}
			if !strings.Contains(rec.Body.String(), c.wantSubstr) {
				t.Errorf("body %q does not mention %q", rec.Body, c.wantSubstr)
			}
		})
	}
}

// TestAcceptIsReadAsASetOfNames pins the representation-selection rule
// design doc 0064 §22 wrote down: an address has one default and at most
// two media types that select another, so Accept is read as a set of
// names rather than as a ranking. Substring matching agreed with that
// rule only by luck — it answered a type that merely contained a token,
// and it was case-sensitive where RFC 9110 is not.
func TestAcceptIsReadAsASetOfNames(t *testing.T) {
	cases := []struct {
		name, accept string
		json, doc    bool
	}{
		{"absent", "", false, false},
		{"anything", "*/*", false, false},
		{"json", "application/json", true, false},
		{"markdown", "text/markdown", false, true},
		{"with a charset", "text/markdown; charset=utf-8", false, true},
		{"uppercase", "TEXT/MARKDOWN", false, true},
		{"among several", "application/xml, text/markdown, */*", false, true},
		{"browser-shaped", "text/html,application/xhtml+xml,*/*;q=0.8", false, false},
		// A ranking is not read, so a weight neither selects nor excludes:
		// the name is what selects. A caller that wants the default asks
		// for it by not naming the token.
		{"a weight does not exclude", "text/markdown;q=0", false, true},
		{"a weight does not select", "application/json;q=0.9", true, false},
		// Exactness: a longer type that merely contains the token is a
		// different type, and a subtype prefix is not the token either.
		{"a type containing the token", "text/markdown-x", false, false},
		{"a structured suffix", "application/vnd.acme+json", false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/x", nil)
			if c.accept != "" {
				r.Header.Set("Accept", c.accept)
			}
			if got := wantsJSON(r); got != c.json {
				t.Errorf("wantsJSON(Accept: %q) = %v, want %v", c.accept, got, c.json)
			}
			if got := wantsDocument(r); got != c.doc {
				t.Errorf("wantsDocument(Accept: %q) = %v, want %v", c.accept, got, c.doc)
			}
		})
	}
}

// An archive's filename is one of the two places design doc 0127 has it
// say which part of a base it is, and it is the one that lands before
// anybody opens anything. An id is any valid UTF-8, so for this
// product's first audience the subtree is usually spelled in Japanese —
// and folding those characters away took the filename's half of 0127
// back out in exactly that case.
func TestAnArchiveNamesTheSubtreeItCarriesInAnySpelling(t *testing.T) {
	// The two shapes that made this a defect rather than an ugly name:
	// a name that says nothing, and a name that says something wider.
	whole := archiveFilename("")
	for _, prefix := range []string{"営業", "営業/売上", "日本語", "_", "-"} {
		got := archiveFilename(prefix)
		if got == whole || got == "ochakai-okf-.tar.gz" {
			t.Errorf("archiveFilename(%q) = %q, which a whole-base backup is kept under", prefix, got)
		}
	}
	for _, c := range []struct{ prefix, wider string }{
		{"teams/成長", "teams"},
		{"チーム/growth", "growth"},
	} {
		if got := archiveFilename(c.prefix); got == archiveFilename(c.wider) {
			t.Errorf("archiveFilename(%q) = %q, the name of %q — an archive of one directory "+
				"claiming to be the archive of every directory beside it",
				c.prefix, got, c.wider)
		}
	}

	// The exact name travels for the clients that read RFC 6266, and
	// the ASCII half stays unambiguous for the ones that do not.
	disp := archiveDisposition("teams/成長")
	if !strings.Contains(disp, `filename*=UTF-8''ochakai-okf-teams-%E6%88%90%E9%95%B7.tar.gz`) {
		t.Errorf("Content-Disposition carries no exact name: %q", disp)
	}
	if !strings.Contains(disp, `filename="ochakai-okf-teams-%E6%88%90%E9%95%B7.tar.gz"`) {
		t.Errorf("the ASCII half is not the subtree it carries: %q", disp)
	}

	// What shipped for the whole base and for a subtree the ASCII name
	// already spelled exactly is what keeps shipping, byte for byte:
	// this fixes the case that was broken and moves nothing else. A
	// prefix the fold only changed the case of gains the exact spelling
	// beside it, which is the same rule and not a second one.
	for _, c := range []struct{ prefix, want string }{
		{"", `attachment; filename="ochakai-okf.tar.gz"`},
		{"teams/growth", `attachment; filename="ochakai-okf-teams-growth.tar.gz"`},
		{"teams/growth-eu", `attachment; filename="ochakai-okf-teams-growth-eu.tar.gz"`},
		{"Teams", `attachment; filename="ochakai-okf-teams.tar.gz"; filename*=UTF-8''ochakai-okf-Teams.tar.gz`},
	} {
		if got := archiveDisposition(c.prefix); got != c.want {
			t.Errorf("archiveDisposition(%q) = %q, want %q", c.prefix, got, c.want)
		}
	}
}
