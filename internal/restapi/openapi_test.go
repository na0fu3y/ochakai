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
	"fmt"
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
	yaml "go.yaml.in/yaml/v3"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/service"
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

// specHTTPMethods are the map keys a path item in api/openapi.yaml can
// hold besides "parameters" (the path-level parameters shared by every
// method under it).
var specHTTPMethods = map[string]bool{"get": true, "put": true, "post": true, "delete": true, "patch": true}

type specParam struct {
	Name string `yaml:"name"`
	In   string `yaml:"in"`
}

// specQueryParams reads api/openapi.yaml and returns, for every
// "METHOD /path" operation, the query parameter names it declares — real
// names, plus the sentinel "fm." standing for the `fm.{key}` family that
// is this contract's one prefix exemption (see the spec's own prose
// above TestOpenAPISpecIsValid's paths).
//
// This is the minimal reader TestUnknownQueryParamsMatchSpec needs, not
// a second OpenAPI implementation: a $ref among components/parameters
// resolves to a header on every one of them today (If-Match,
// If-None-Match, Ochakai-On-Behalf-Of, Ochakai-Producer), so only
// operations' own inline `parameters` are read — a query parameter added
// later as a component would need this taught to resolve it, the same
// way cmd/ochakai/surface_test.go's wireSpec does.
func specQueryParams(t *testing.T) map[string]map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read %s: %v", specPath, err)
	}
	var doc struct {
		Paths map[string]map[string]yaml.Node `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s: %v", specPath, err)
	}
	if len(doc.Paths) == 0 {
		t.Fatal("no paths found in openapi.yaml: this check now guards nothing")
	}
	out := map[string]map[string]bool{}
	for path, item := range doc.Paths {
		var shared []specParam
		if node, ok := item["parameters"]; ok {
			if err := node.Decode(&shared); err != nil {
				t.Fatalf("%s: parameters: %v", path, err)
			}
		}
		for method, node := range item {
			if !specHTTPMethods[method] {
				continue
			}
			var op struct {
				Parameters []specParam `yaml:"parameters"`
			}
			if err := node.Decode(&op); err != nil {
				t.Fatalf("%s %s: %v", method, path, err)
			}
			names := map[string]bool{}
			for _, p := range append(append([]specParam{}, shared...), op.Parameters...) {
				switch {
				case p.In != "query":
					continue
				case p.Name == "fm.{key}":
					names["fm."] = true
				default:
					names[p.Name] = true
				}
			}
			out[strings.ToUpper(method)+" "+path] = names
		}
	}
	return out
}

// TestUnknownQueryParamsMatchSpec is issue #409's item 3: a hand-picked
// list of examples cannot tell a per-operation allowlist from a shared or
// globally-exempt one, because it never tries the one combination that
// would fail — a parameter real on operation B, sent to operation A,
// where A does not declare it. This builds that combination from
// api/openapi.yaml itself, the way cmd/ochakai/surface_test.go's
// readWireSpec builds the surface count, instead of by hand: every
// parameter declared anywhere in the contract is tried against every
// operation that does not declare it, and every one of those must be a
// 400 naming it. Replacing any of restapi.go's per-operation allowlists
// with a shared or wider one — the change #409 showed leaves a
// hand-written five-case test green — fails this test on the first
// parameter the merge widened.
func TestUnknownQueryParamsMatchSpec(t *testing.T) {
	h := Handler(&service.Service{})
	declared := specQueryParams(t)

	universe := map[string]bool{}
	for _, names := range declared {
		for name := range names {
			universe[name] = true
		}
	}
	if len(universe) == 0 {
		t.Fatal("no query parameters found in openapi.yaml: this check now guards nothing")
	}

	for _, op := range restOperations {
		allowed, ok := declared[op.spec]
		if !ok {
			t.Fatalf("%s: openapi.yaml has no operation %q", op.name, op.spec)
		}
		for name := range universe {
			if allowed[name] {
				continue
			}
			// dry_run is DELETE's one deliberate exception: PUT and
			// DELETE share an allowlist (restapi.go) so DELETE can turn
			// it into its own refusal ("a delete has no plan beside
			// it") rather than "unknown query parameter" — pinned by
			// TestRESTIntegrationDryRunAnswersWithoutWriting's DELETE
			// ?dry_run=true case in restapi_integration_test.go.
			// openapi.yaml correctly does not declare it there, since
			// every value of it is refused; that is not the silent
			// no-op this test guards against.
			if op.name == "bundle delete" && name == "dry_run" {
				continue
			}
			key := name
			if name == "fm." {
				key = "fm.owner"
			}
			t.Run(op.name+"/"+key, func(t *testing.T) {
				url := op.url + "?" + key + "=x"
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequest(op.method, url, nil))
				want := fmt.Sprintf("unknown query parameter %q", key)
				if rec.Code != http.StatusBadRequest || !strings.Contains(errMessage(t, rec), want) {
					t.Errorf("%s %s = %d %q, want 400 %q — api/openapi.yaml declares %q on another "+
						"operation but not on %s", op.method, url, rec.Code, rec.Body, want, key, op.spec)
				}
			})
		}
	}
}

// TestSpecParamsAreAccepted is TestUnknownQueryParamsMatchSpec's other
// half, closing the gap issue #427 found: #409 asked that each operation
// "accepts every parameter it declares and rejects every one it does
// not", and the check above only ever tries the `allowed[name]` branch's
// opposite. This tries that branch instead — every parameter an
// operation's own allowlist declares, sent to that same operation, must
// not come back a 400 naming it unknown. The mutation issue #427
// describes — dropping trust and cursor off search's allowlist, and
// trust and budget off context's — leaves TestUnknownQueryParamsMatchSpec
// fully green, because removing a declared parameter only ever narrows
// what that check tries; this is the half that would have caught it.
//
// It needs a real store: unlike the negative half, a parameter this test
// sends is — by construction — one the handler is meant to act on, and
// most of them read all the way through to Handler(&service.Service{})'s
// nil Store, which panics (see readOnlyServer's comment on the same
// hazard for writes). Skipped without OCHAKAI_TEST_DATABASE_URL, like
// every other test that needs one.
//
// Not checkedServer, deliberately: this test sends "x" to parameters
// api/openapi.yaml constrains with an enum (trust) or a stricter type
// (limit), which the spec's own request validation would refuse before
// the handler ever saw it. What this test is asking is whether the
// handler's allowlist recognizes the parameter, not whether "x" is a
// value the spec would accept — TestBadRequestValidation and its
// neighbors already hold value validation to account. Matching on the
// "unknown query parameter" text, the same way the negative half does,
// is what lets an unrelated 400 — search or context with no q — pass
// rather than forcing every operation's own required parameters into
// this loop's URLs.
func TestSpecParamsAreAccepted(t *testing.T) {
	h := Handler(newIntegrationService(t))
	declared := specQueryParams(t)

	for _, op := range restOperations {
		allowed, ok := declared[op.spec]
		if !ok {
			t.Fatalf("%s: openapi.yaml has no operation %q", op.name, op.spec)
		}
		for name := range allowed {
			key := name
			if name == "fm." {
				key = "fm.owner"
			}
			t.Run(op.name+"/"+key, func(t *testing.T) {
				url := op.url + "?" + key + "=x"
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequest(op.method, url, nil))
				unknown := fmt.Sprintf("unknown query parameter %q", key)
				if rec.Code == http.StatusBadRequest && strings.Contains(errMessage(t, rec), unknown) {
					t.Errorf("%s %s = 400 %q — api/openapi.yaml declares %q on %s, so this should not "+
						"be rejected as unknown", op.method, url, rec.Body, key, op.spec)
				}
			})
		}
	}
}
