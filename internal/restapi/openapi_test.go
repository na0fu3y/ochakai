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
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/pb33f/libopenapi"
	validator "github.com/pb33f/libopenapi-validator"
	valerrors "github.com/pb33f/libopenapi-validator/errors"

	"github.com/na0fu3y/ochakai/internal/domain"
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
	"/api/v1/bundle/",
	"/api/v1/review/",
	"/api/v1/usage/",
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
//
// The bundle root is the same gap seen from the other end. "" is a
// legal bundle path — it is the whole bundle, and GET /api/v1/bundle/
// with Accept: application/gzip is how the archive of everything is
// asked for (design doc 0046 §3.5) — but a path *template* parameter
// cannot match the empty string in OpenAPI, and there is no syntax that
// would let it. Standing one placeholder segment in keeps the request
// under the same checks as any other, which is the point; the spec says
// in prose that the root is spelled with an empty path, because that is
// as precise as the format allows.
func forSpecRouting(p string) string {
	if p == bundlePath {
		return bundlePath + "-"
	}
	for _, prefix := range idAddressed {
		if rest, ok := strings.CutPrefix(p, prefix); ok && strings.Contains(rest, "/") {
			return prefix + strings.ReplaceAll(rest, "/", "-")
		}
	}
	return p
}

// bundlePath is the address every object of the bundle lives under, and
// with nothing after it, the bundle itself.
const bundlePath = "/api/v1/bundle/"

// carriesOpaqueBytes reports whether this request stores raw bytes: a
// PUT of a file, whose body is the file and whose Content-Type is
// whatever the client sent, or nothing at all.
//
// The validator resolves media types by exact match — it reads "*/*" as
// a literal type name — so a body under a wildcard is reported as
// undeclared. Skipping costs nothing that could have drifted:
// `{type: string, format: binary}` asserts nothing about the bytes,
// which is the whole point of a file.
func carriesOpaqueBytes(r *http.Request) bool {
	return r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, bundlePath)
}

// sniffedResponse reports whether a response's media type was decided by
// sniffing the bytes rather than chosen by the handler — a file coming
// back from the attachment or bundle address. Same exact-match problem,
// same reasoning: the declared schema is a wildcard over bytes.
//
// Everything the handler does choose stays checked, which is what
// matters here: a concept read through the bundle address answers
// application/json or text/markdown and is validated like any other.
func sniffedResponse(res *http.Response) bool {
	switch mt, _, _ := strings.Cut(res.Header.Get("Content-Type"), ";"); strings.TrimSpace(mt) {
	case "", "application/json", "text/markdown", "application/gzip":
		return false
	}
	return true
}

// checkedServer is httptest.NewServer(Handler(svc)) with the spec in the
// path: it fails the test on any request or response the spec does not
// describe. Integration tests use it instead of httptest.NewServer so
// their existing traffic doubles as the contract's coverage.
//
// The close is registered here rather than left to the caller's `defer
// srv.Close()`: cleanups run after a function's defers, so a test that
// closed the server with defer and deleted its rows from t.Cleanup sent
// those deletes to a server that was already gone — the error discarded,
// the rows left in a reused test database (issue #278). Registered at
// creation it is the first cleanup on the list, and cleanups are LIFO, so
// it runs after everything the test body registers afterwards.
func checkedServer(t *testing.T, h http.Handler) *httptest.Server {
	v := openAPIValidator(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		if !sniffedResponse(res) {
			if ok, errs := v.ValidateHttpResponse(specReq(), res); !ok {
				reportSpecErrors(t, "response", r, errs)
			}
		}

		for k, vs := range rec.Header() {
			w.Header()[k] = vs
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestCheckedServerOutlivesTheTestBody pins that ordering. It needs no
// database — readOnlyServer is checkedServer over a service that reaches
// none — because whether a cleanup can still reach the server is the
// whole of what the integration tests' row deletion rests on: a closed
// server answers a delete with a transport error, entry or no entry.
func TestCheckedServerOutlivesTheTestBody(t *testing.T) {
	srv := readOnlyServer(t)
	t.Cleanup(func() {
		req, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/bundle/gone.md", nil)
		if err != nil {
			t.Error(err)
			return
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Errorf("a cleanup registered after checkedServer cannot reach it: %v", err)
			return
		}
		resp.Body.Close()
	})
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

// TestOpenAPITypeVocabularyMatchesDomain pins the Type schema against
// domain.Types. The spec is a static document, so its copy of the
// vocabulary can only drift, and a free-type model means drift is silent:
// a retired spelling left in `examples` goes on recommending itself to
// every reader of the public wire surface without failing anything
// (design doc 0038 §4.4).
func TestOpenAPITypeVocabularyMatchesDomain(t *testing.T) {
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	spec := string(raw)
	i := strings.Index(spec, "    Type:\n")
	if i < 0 {
		t.Fatal("the Type schema is gone from api/openapi.yaml")
	}
	// Up to the next sibling schema (four-space key), which is Status.
	block := spec[i:]
	if end := strings.Index(block, "\n    Status:"); end >= 0 {
		block = block[:end]
	}
	for _, ty := range domain.Types {
		if !strings.Contains(block, string(ty)) {
			t.Errorf("the Type schema never mentions the recommended type %q", ty)
		}
	}
	for _, retired := range []string{"Semantic Model", "Golden Query", "Playbook", "API Endpoint"} {
		if strings.Contains(block, retired) {
			t.Errorf("the Type schema still lists %q, retired by design doc 0038 or 0063", retired)
		}
	}

	// The comment above named `examples` as the place a retired spelling
	// keeps recommending itself, and the check did not look there: the
	// create example taught `type: Golden Query` with the SQL in attrs for
	// two releases after 0038 (issue #222). Examples are what a reader
	// copies, so they are held to the same vocabulary as the schema.
	//
	// The value form only. Prose that has to name a retired spelling — a
	// migration note, say — is still free to.
	for _, m := range regexp.MustCompile(`(?m)^\s*(?:- )?type: (Golden Query|Semantic Model)\s*$`).
		FindAllStringSubmatch(spec, -1) {
		t.Errorf("an example still writes %q as a type. It is a free type and still works, "+
			"but the spec is where people copy from (design doc 0038)", m[1])
	}
}
