package restapi

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/na0fu3y/ochakai/internal/compiler"
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
		{"compile refusal", &compiler.Error{Reason: "outside the subset"}, http.StatusUnprocessableEntity},
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
// any service call: malformed numbers are rejected instead of silently
// treated as unset, and q cannot be combined with sort (matching the CLI).
func TestBadRequestValidation(t *testing.T) {
	h := Handler(&service.Service{})
	cases := []struct {
		name, url, wantSubstr string
	}{
		{"invalid sort", "/api/v1/knowledge?sort=created_at", "invalid sort"},
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

// TestETagRoundTrip pins the ETag/If-Match wire format: etagOf quotes the
// updated_at, and parseIfMatch reads it (and "*", and absence) back — so a
// value from a response header is accepted verbatim on the next request.
func TestETagRoundTrip(t *testing.T) {
	updated := time.Date(2026, 7, 24, 1, 2, 3, 456789000, time.UTC)
	k := &domain.Knowledge{ID: "metrics/x", UpdatedAt: updated}
	etag := etagOf(k)
	if etag != `"2026-07-24T01:02:03.456789Z"` {
		t.Fatalf("etagOf = %s", etag)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/v1/knowledge/metrics/x", nil)
	req.Header.Set("If-Match", etag)
	got, err := parseIfMatch(req)
	if err != nil {
		t.Fatalf("parseIfMatch: %v", err)
	}
	if got == nil || !got.Equal(updated) {
		t.Errorf("parseIfMatch(%s) = %v, want %v", etag, got, updated)
	}

	// Absent and "*" carry no version precondition.
	for _, v := range []string{"", "*"} {
		r := httptest.NewRequest(http.MethodPut, "/x", nil)
		if v != "" {
			r.Header.Set("If-Match", v)
		}
		if p, err := parseIfMatch(r); err != nil || p != nil {
			t.Errorf("parseIfMatch(If-Match:%q) = %v, %v; want nil, nil", v, p, err)
		}
	}

	// A non-ETag value is a client error, not a silent no-precondition.
	r := httptest.NewRequest(http.MethodPut, "/x", nil)
	r.Header.Set("If-Match", `"not-a-timestamp"`)
	if _, err := parseIfMatch(r); err == nil {
		t.Error("malformed If-Match must be an error")
	}
}
