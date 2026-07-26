package restapi

// Contract tests: api/openapi.yaml is declared the public wire surface,
// but nothing checked the implementation against it — the wire tests in
// internal/apiclient tie the server's types to the client's, leaving the
// spec as the one corner of that triangle held in place by prose alone
// (design doc 0035 §3.2).
//
// checkedServer closes it. It is the same httptest server the integration
// tests already used, with every request and every response run past the
// spec on the way through, so the coverage comes from the tests that were
// already there rather than from a second set written to mirror them.

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/pb33f/libopenapi"
	validator "github.com/pb33f/libopenapi-validator"
	valerrors "github.com/pb33f/libopenapi-validator/errors"
)

const specPath = "../../api/openapi.yaml"

// The spec is parsed once per test binary: building the validator walks
// every schema, which is far too slow to repeat per request.
var (
	specOnce sync.Once
	specVal  validator.Validator
	specErr  error
)

func openAPIValidator(t *testing.T) validator.Validator {
	t.Helper()
	specOnce.Do(func() {
		var raw []byte
		if raw, specErr = os.ReadFile(specPath); specErr != nil {
			return
		}
		var doc libopenapi.Document
		if doc, specErr = libopenapi.NewDocument(raw); specErr != nil {
			return
		}
		var errs []error
		if specVal, errs = validator.NewValidator(doc); len(errs) > 0 {
			specErr = errs[0]
		}
	})
	if specErr != nil {
		t.Fatalf("loading %s: %v", specPath, specErr)
	}
	return specVal
}

// TestOpenAPISpecIsValid checks the document against the OpenAPI schema
// itself. Everything below is only as good as this: a spec that does not
// parse would validate every response vacuously.
func TestOpenAPISpecIsValid(t *testing.T) {
	ok, errs := openAPIValidator(t).ValidateDocument()
	if !ok {
		for _, e := range errs {
			t.Errorf("%s: %s", e.Message, e.Reason)
		}
	}
}

// idAddressed are the endpoints whose trailing path parameter is an entry
// id — or, for attachments, an id followed by a filename.
var idAddressed = []string{
	"/api/v1/knowledge/",
	"/api/v1/verify/",
	"/api/v1/usage/",
	"/api/v1/revisions/",
	"/api/v1/backlinks/",
	"/api/v1/attachments/",
}

// forSpecRouting collapses a multi-segment id down to one segment so the
// request reaches the operation the spec declares.
//
// OpenAPI path templating is segment-based: {id} matches "revenue" but
// not "queries/revenue", and 3.1 has no syntax for a parameter that spans
// separators. ochakai's ids are bundle paths (design doc 0017), so every
// id-addressed operation runs into this — the spec says so in prose ("the
// entry's id — its bundle path. Slashes are real path separators"),
// which is as precise as the format allows.
//
// Collapsing costs nothing the spec was checking: it constrains the id
// only as `type: string`, and any single segment satisfies that. The
// method, query, headers, both bodies and the status code stay under the
// spec's eye, which is the whole of what drifts.
func forSpecRouting(p string) string {
	for _, prefix := range idAddressed {
		if rest, ok := strings.CutPrefix(p, prefix); ok && strings.Contains(rest, "/") {
			return prefix + strings.ReplaceAll(rest, "/", "-")
		}
	}
	return p
}

// carriesOpaqueBytes reports whether this is one of the two operations
// whose body is raw attachment bytes: PUT (the file being stored) and GET
// (the file coming back with its sniffed media type).
//
// Both are declared "*/*" in the spec, and the validator resolves media
// types by exact match — it reads "*/*" as a literal type name, so an
// image/png response, or a PUT that sends no Content-Type at all, is
// reported as undeclared. Skipping them costs nothing that could have
// drifted: `{type: string, format: binary}` under a wildcard asserts
// nothing about the bytes, which is the whole point of an attachment.
// DELETE on the same path keeps its 204 checked, and export — a tarball
// under a named media type — validates normally.
func carriesOpaqueBytes(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, "/api/v1/attachments/") &&
		(r.Method == http.MethodPut || r.Method == http.MethodGet)
}

// checkedServer is httptest.NewServer(Handler(svc)) with the spec in the
// path: it fails the test on any request or response the spec does not
// describe. Integration tests use it instead of httptest.NewServer so
// their existing traffic doubles as the contract's coverage.
func checkedServer(t *testing.T, h http.Handler) *httptest.Server {
	v := openAPIValidator(t)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if carriesOpaqueBytes(r) {
			h.ServeHTTP(w, r)
			return
		}
		// The handler consumes the body, and so does the validator; read
		// it once here and hand each of them its own reader.
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("%s %s: reading request body: %v", r.Method, r.URL.Path, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = r.Body.Close()

		specReq := func() *http.Request {
			c := r.Clone(r.Context())
			c.URL.Path = forSpecRouting(r.URL.Path)
			c.URL.RawPath = ""
			c.Body = io.NopCloser(bytes.NewReader(body))
			return c
		}

		if ok, errs := v.ValidateHttpRequest(specReq()); !ok {
			reportSpecErrors(t, "request", r, errs)
		}

		r.Body = io.NopCloser(bytes.NewReader(body))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		res := rec.Result()
		res.Body = io.NopCloser(bytes.NewReader(rec.Body.Bytes()))
		if ok, errs := v.ValidateHttpResponse(specReq(), res); !ok {
			reportSpecErrors(t, "response", r, errs)
		}

		for k, vs := range rec.Header() {
			w.Header()[k] = vs
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
	}))
}

// reportSpecErrors prints what the spec objected to, including the
// per-field schema reasons — without them a failure only says "the body
// is wrong somewhere".
func reportSpecErrors(t *testing.T, what string, r *http.Request, errs []*valerrors.ValidationError) {
	t.Helper()
	var b strings.Builder
	for _, e := range errs {
		b.WriteString("\n  " + e.Message + ": " + e.Reason)
		for _, sv := range e.SchemaValidationErrors {
			b.WriteString("\n    " + sv.FieldPath + ": " + sv.Reason)
		}
	}
	t.Errorf("%s %s: %s does not match api/openapi.yaml:%s", r.Method, r.URL.RequestURI(), what, b.String())
}
