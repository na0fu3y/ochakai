package restapi

import (
	"bytes"
	"errors"
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
		{"if-match conflict", store.ErrConflict, http.StatusPreconditionFailed},
		{"invalid input", service.Invalidf("title is required"), http.StatusBadRequest},
		{"unsupported", service.Unsupportedf("attachments need GCS"), http.StatusNotImplemented},
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
		{"invalid sort", "/api/v1/knowledge?sort=created_at", "invalid sort"},
		{"neither q nor sort", "/api/v1/knowledge", "needs a query"},
		{"whitespace query", "/api/v1/knowledge?q=%20%09", "needs a query"},
		{"sort with query", "/api/v1/knowledge?sort=verified_at&q=revenue", "cannot be combined"},
		{"usage sort with query", "/api/v1/knowledge?sort=usage&q=revenue", "cannot be combined"},
		{"failed sort with query", "/api/v1/knowledge?sort=failed&q=revenue", "cannot be combined"},
		{"bad search limit", "/api/v1/knowledge?limit=abc", "invalid limit"},
		{"bad revisions limit", "/api/v1/revisions/metrics/revenue?limit=abc", "invalid limit"},
		{"bad backlinks limit", "/api/v1/backlinks/metrics/revenue?limit=1.5", "invalid limit"},
		{"bad context limit", "/api/v1/context?q=x&limit=1.5", "invalid limit"},
		{"bad min_score", "/api/v1/context?q=x&min_score=high", "invalid min_score"},
		{"browse bad prefix", "/api/v1/browse?prefix=..%2Fescape", "invalid prefix"},
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

// TestAttachWithoutBlobStore pins the 501: on an instance without GCS
// (design doc 0013), attaching fails before any validation or DB access
// with the config hint.
func TestAttachWithoutBlobStore(t *testing.T) {
	h := Handler(&service.Service{Store: &store.Store{}})
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 16)...)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut,
		"/api/v1/attachments/insights/revenue/weekly.png", bytes.NewReader(png)))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("PUT attachment without GCS = %d, want 501 (body: %s)", rec.Code, rec.Body)
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
		{"attachment", http.MethodPut, "/api/v1/attachments/insights/revenue/weekly.png",
			domain.MaxAttachmentSize + 1, "attachment exceeds"},
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
// the entry's content hash, and parseIfMatch reads it (and "*", and
// absence) back — so a value from a response header is accepted verbatim
// on the next request.
func TestETagRoundTrip(t *testing.T) {
	const hash = "9f2c1b0d4e5a6789abcdef0123456789abcdef0123456789abcdef0123456789"
	k := &domain.Knowledge{ID: "metrics/x", ContentHash: hash}
	etag := etagOf(k)
	if etag != `"`+hash+`"` {
		t.Fatalf("etagOf = %s", etag)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/v1/knowledge/metrics/x", nil)
	req.Header.Set("If-Match", etag)
	got := parseIfMatch(req)
	if got == nil || *got != hash {
		t.Errorf("parseIfMatch(%s) = %v, want %q", etag, got, hash)
	}

	// Absent and "*" carry no version precondition.
	for _, v := range []string{"", "*"} {
		r := httptest.NewRequest(http.MethodPut, "/x", nil)
		if v != "" {
			r.Header.Set("If-Match", v)
		}
		if p := parseIfMatch(r); p != nil {
			t.Errorf("parseIfMatch(If-Match:%q) = %v; want nil", v, p)
		}
	}

	// The version is an opaque token now, so there is no format to
	// reject: a value that never matched anything fails the precondition
	// with a 412, which is what a merely stale one does too. Rejecting it
	// at parse time would only tell a client that its own ETag looked
	// wrong to us, which is not something we can know.
	r := httptest.NewRequest(http.MethodPut, "/x", nil)
	r.Header.Set("If-Match", `"not-a-hash"`)
	if p := parseIfMatch(r); p == nil || *p != "not-a-hash" {
		t.Errorf("parseIfMatch of an unknown token = %v, want it passed through", p)
	}
}

// An oversized JSON body is a 413, matching what the attachment path
// (readBody) already answers. Calling it "invalid JSON" sent the caller
// hunting for a syntax error in a payload that merely did not fit.
func TestOversizedJSONBodyIsTooLarge(t *testing.T) {
	h := Handler(&service.Service{})
	body := `{"type":"metrics","id":"metrics/x","body":"` + strings.Repeat("a", 5<<20) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", w.Code, http.StatusRequestEntityTooLarge)
	}
	if got := w.Body.String(); !strings.Contains(got, "exceeds") {
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
		name, method, url, wantSubstr string
	}{
		{"purge=1", http.MethodDelete, "/api/v1/knowledge/metrics/revenue?purge=1", "invalid purge"},
		{"purge=True", http.MethodDelete, "/api/v1/knowledge/metrics/revenue?purge=True", "invalid purge"},
		{"purge=yes", http.MethodDelete, "/api/v1/knowledge/metrics/revenue?purge=yes", "invalid purge"},
		{"attachments=0", http.MethodGet, "/api/v1/export?attachments=0", "invalid attachments"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(c.method, c.url, nil))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%s %s = %d, want 400 (body: %s)", c.method, c.url, rec.Code, rec.Body)
			}
			if !strings.Contains(rec.Body.String(), c.wantSubstr) {
				t.Errorf("body %q does not mention %q", rec.Body, c.wantSubstr)
			}
		})
	}
}
