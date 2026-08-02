package restapi

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/okf"
	"github.com/na0fu3y/ochakai/internal/service"
	"github.com/na0fu3y/ochakai/internal/store"
	"github.com/na0fu3y/ochakai/internal/testdb"
)

// testDatabaseURL is the test database, or a skip (see the store
// integration test for the docker one-liner).
func testDatabaseURL(t *testing.T) string {
	t.Helper()
	dbURL := os.Getenv("OCHAKAI_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("OCHAKAI_TEST_DATABASE_URL not set")
	}
	return dbURL
}

// lockLiveAttachments serializes, across the test packages sharing the
// test database, the tests that hold live attachments or scan them all
// (OKF export, ExportSnapshot.AttachmentMeta): bytes resolve against each
// own in-memory blob fake, so a foreign live attachment breaks a
// whole-KB scan. Key shared with the store and service test packages.
//
// Call it before newIntegrationServer: cleanups are LIFO, so the lock is
// released only after the test's rows are gone.
func lockLiveAttachments(t *testing.T) {
	t.Helper()
	ctx := context.Background() // outlives t.Context()'s pre-Cleanup cancel
	conn, err := pgx.Connect(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(0x0c8a1a77)`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) }) // closing the session releases the lock
}

// newIntegrationService is a Service over a real, migrated PostgreSQL,
// skipping the test without OCHAKAI_TEST_DATABASE_URL.
//
// The pool's cleanup is registered rather than deferred, and registered
// here so that it is the last cleanup to run: the rows a test wrote are
// removed over HTTP (removeEntries), and a deferred Close would take the
// pool out from under those requests — leaving the rows behind in a
// database the next run reuses (issue #278).
func newIntegrationService(t *testing.T) *service.Service {
	t.Helper()
	ctx := context.Background() // outlives t.Context()'s pre-Cleanup cancel
	s, err := store.New(ctx, testDatabaseURL(t), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	return &service.Service{Store: s, Log: slog.New(slog.NewTextHandler(os.Stderr, nil))}
}

// newIntegrationServer serves a real PostgreSQL through the REST handler,
// checked against api/openapi.yaml on every request and response. The
// store comes back with it for the tests that swap in a blob fake.
func newIntegrationServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	svc := newIntegrationService(t)
	return checkedServer(t, Handler(svc)), svc.Store
}

// removeEntries frees the ids a test wrote once it ends, so that a
// developer reusing a test database across runs does not accumulate them:
// twenty leftover entries are enough to push a test's own entry out of a
// bounded hit list, and the failure reads as a search bug (issue #278).
//
// Two requests per id, because that is what freeing an id takes: the soft
// delete, then the purge that requires it — purging a live entry is a
// 409. A 404 from either is the state this wants, so only a request that
// never got an answer is worth reporting.
func removeEntries(t *testing.T, srv *httptest.Server, ids ...string) {
	t.Helper()
	t.Cleanup(func() {
		for _, id := range ids {
			for _, q := range []string{"", "?purge=true"} {
				req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/bundle/"+id+".md"+q, nil)
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Errorf("cleaning up %s%s: %v", id, q, err)
					continue
				}
				resp.Body.Close()
				if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
					t.Errorf("cleaning up %s%s: status %d", id, q, resp.StatusCode)
				}
			}
		}
	})
}

// TestRESTIntegration walks one entry through the REST surface against a
// real PostgreSQL (skipped unless OCHAKAI_TEST_DATABASE_URL is set; see
// the store integration test for the docker one-liner): create, read,
// no-op update (Ochakai-Unchanged), browse, revisions, export, delete.
// A run-unique top-level id segment (doubling as a custom type) keeps
// reruns and other tests' rows out of the browse assertions.
func TestRESTIntegration(t *testing.T) {
	lockLiveAttachments(t) // the export step scans every live attachment
	srv, _ := newIntegrationServer(t)

	typ := testdb.Unique(t, "restit")
	id := typ + "/sales/orders"
	entry := map[string]any{"type": typ, "id": id, "title": "REST round trip"}
	payload := docFrom(t, entry)

	// Create.
	resp := putDoc(t, srv.URL, id, payload, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Read it back. A read is a view: the document it is, the projection
	// to read it by, and what this instance observed (design doc 0043
	// §3.5).
	var got domain.View
	getJSON(t, srv.URL+"/api/v1/bundle/"+typ+"/sales/orders.md", &got)
	// The document named no status, so it reads as OKF's default rather
	// than as a draft — and nobody has confirmed it, which is what the
	// trust tier says (design doc 0046 §§3.9-3.10).
	if got.Summary.Title != "REST round trip" || got.Summary.Status != domain.StatusStable ||
		got.Summary.Trust != domain.TrustUnverified {
		t.Errorf("summary = %+v", got.Summary)
	}
	if !strings.Contains(got.Document, "title: REST round trip") {
		t.Errorf("document = %q", got.Document)
	}

	// A content-identical PUT writes nothing and says so in the header.
	// No If-None-Match this time: the same call replaces what it finds.
	resp = putDoc(t, srv.URL, typ+"/sales/orders", payload, false)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Ochakai-Unchanged") != "true" {
		t.Errorf("no-op PUT: status = %d, Ochakai-Unchanged = %q",
			resp.StatusCode, resp.Header.Get("Ochakai-Unchanged"))
	}

	// index.md: under the run-unique top segment the "sales" directory
	// shows, and the level below shows the entry (with its type as
	// metadata in the projection).
	var root service.BrowseResult
	getJSON(t, srv.URL+"/api/v1/bundle/"+typ+"/index.md", &root)
	if len(root.Dirs) != 1 || root.Dirs[0].Name != "sales" || root.Dirs[0].Count != 1 {
		t.Errorf("index.md root = %+v", root)
	}
	var level service.BrowseResult
	getJSON(t, srv.URL+"/api/v1/bundle/"+typ+"/sales/index.md", &level)
	if len(level.Concepts) != 1 || level.Concepts[0].ID != id || string(level.Concepts[0].Type) != typ {
		t.Errorf("index.md level = %+v", level)
	}
	// And the same address without the Accept header is the file itself.
	doc := getText(t, srv.URL+"/api/v1/bundle/"+typ+"/sales/index.md")
	if !strings.Contains(doc, "# "+typ+"/sales") || !strings.Contains(doc, "(orders.md)") {
		t.Errorf("generated index.md:\n%s", doc)
	}

	// The object's own history is at the object's own address (design
	// doc 0046 §3.5): exactly the create, carrying the document it was.
	var revs struct {
		Revisions []domain.Revision `json:"revisions"`
	}
	getJSON(t, srv.URL+"/api/v1/bundle/"+id+".md?history", &revs)
	if len(revs.Revisions) != 1 || revs.Revisions[0].Change != "create" ||
		!strings.Contains(revs.Revisions[0].Document, "title: REST round trip") {
		t.Errorf("revisions = %+v", revs.Revisions)
	}
	// The directory's log.md is the changes under it, in both
	// representations of the one address.
	logDoc := getText(t, srv.URL+"/api/v1/bundle/"+typ+"/log.md")
	if !strings.Contains(logDoc, "**Create**") ||
		!strings.Contains(logDoc, "("+strings.TrimPrefix(id, typ+"/")+".md)") {
		t.Errorf("generated log.md:\n%s", logDoc)
	}
	var changes struct {
		Changes []struct {
			Path   string `json:"path"`
			Change string `json:"change"`
		} `json:"changes"`
	}
	getJSON(t, srv.URL+"/api/v1/bundle/"+typ+"/log.md", &changes)
	if len(changes.Changes) != 1 || changes.Changes[0].Path != id+".md" ||
		changes.Changes[0].Change != "create" {
		t.Errorf("the JSON of a directory's log.md = %+v", changes.Changes)
	}

	// Neither is writable: they are generated, not stored, so a write to
	// one conflicts with what the address is (design doc 0046 §3.5).
	req, err := http.NewRequest(http.MethodPut, srv.URL+"/api/v1/bundle/"+typ+"/index.md", strings.NewReader("x"))
	if err != nil {
		t.Fatal(err)
	}
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("PUT index.md = %d, want 409", resp.StatusCode)
	}

	// Export: the bundle carries the entry at its canonical path.
	resp, err = getArchive(t, srv.URL+"/api/v1/bundle/", "")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/gzip" {
		t.Fatalf("export: status = %d, content-type = %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for tr := tar.NewReader(gz); ; {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Name == typ+"/sales/orders.md" {
			found = true
		}
	}
	if !found {
		t.Errorf("export bundle misses %s/sales/orders.md", typ)
	}

	// ?files=false skips the bytes: a CI backup can take the
	// entries from here and the files straight from GCS, which is both
	// cheaper and the only sane path once attachments outweigh the text.
	resp, err = getArchive(t, srv.URL+"/api/v1/bundle/", "files=false")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export without files: status = %d", resp.StatusCode)
	}
	gz, err = gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for tr := tar.NewReader(gz); ; {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(hdr.Name, ".md") {
			t.Errorf("files=false still carried %s", hdr.Name)
		}
		if hdr.Name == typ+"/sales/orders.md" {
			found = true
		}
	}
	if !found {
		t.Errorf("files=false dropped the entries too")
	}

	// The export must survive crossing a batch boundary: entries are
	// fetched in groups so that peak memory does not scale with the
	// knowledge base, and an off-by-one there would silently drop
	// whichever entries fall past the first group.
	var planted []string
	for i := range exportBatch + 3 {
		id := fmt.Sprintf("%s/batch/%d", typ, i)
		planted = append(planted, id)
		body := docFrom(t, map[string]any{"type": typ, "id": id, "title": fmt.Sprintf("batch %d", i)})
		resp := putDoc(t, srv.URL, id, body, true)
		resp.Body.Close()
	}
	removeEntries(t, srv, planted...)
	resp, err = getArchive(t, srv.URL+"/api/v1/bundle/", "files=false")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	gz, err = gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	inBundle := map[string]bool{}
	for tr := tar.NewReader(gz); ; {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("bundle truncated: %v", err)
		}
		inBundle[hdr.Name] = true
	}
	for _, id := range planted {
		if !inBundle[id+".md"] {
			t.Errorf("export dropped %s across a batch boundary", id)
		}
	}

	// Delete, then the entry is gone.
	req, _ = http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/bundle/"+typ+"/sales/orders.md", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("delete status = %d", resp.StatusCode)
	}
	resp, err = http.Get(srv.URL + "/api/v1/bundle/" + typ + "/sales/orders.md")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("get after delete = %d, want 404", resp.StatusCode)
	}
}

// TestRESTIntegrationAttachments covers the attachment surface the web
// UI leans on: hit lists carry attachment metadata (filled in batch),
// and attachment GETs answer conditional requests from metadata alone
// (ETag = content hash, If-None-Match → 304).
func TestRESTIntegrationAttachments(t *testing.T) {
	lockLiveAttachments(t)
	srv, s := newIntegrationServer(t)
	s.UseBlobStore(memBlobStore{})

	typ := testdb.Unique(t, "restatt")
	payload := docFrom(t, map[string]any{"type": typ, "id": typ + "/reading", "title": "attachment hits"})
	resp := putDoc(t, srv.URL, typ+"/reading", payload, true)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", resp.StatusCode)
	}

	png := append([]byte("\x89PNG\r\n\x1a\n"), []byte("hit thumbnail bytes")...)
	attURL := srv.URL + "/api/v1/bundle/" + typ + "/reading/weekly.png"
	req, _ := http.NewRequest(http.MethodPut, attURL, bytes.NewReader(png))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	// 201: this address held nothing before (design doc 0064, matching
	// what a concept PUT already answers).
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("attach status = %d, want 201", resp.StatusCode)
	}

	// Replacing the same file's bytes is 200, not 201: the address
	// already held an object.
	req, _ = http.NewRequest(http.MethodPut, attURL, bytes.NewReader(png))
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("re-PUT status = %d, want 200", resp.StatusCode)
	}

	// The listing carries the attachment metadata.
	var hits struct {
		Hits []domain.SearchHit `json:"hits"`
	}
	getJSON(t, srv.URL+"/api/v1/search?sort=verified_at&type="+typ, &hits)
	if len(hits.Hits) != 1 || len(hits.Hits[0].Files) != 1 ||
		hits.Hits[0].Files[0].Name != "weekly.png" ||
		hits.Hits[0].Files[0].MediaType != "image/png" {
		t.Fatalf("hits should carry attachment metadata: %+v", hits.Hits)
	}
	sum := hits.Hits[0].Files[0].SHA256

	// Plain GET: bytes, content-hash ETag, revalidation policy.
	resp, err = http.Get(attURL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != string(png) {
		t.Fatalf("attachment GET: status = %d, %d bytes", resp.StatusCode, len(body))
	}
	if resp.Header.Get("ETag") != `"`+sum+`"` || resp.Header.Get("Cache-Control") != "private, no-cache" {
		t.Errorf("caching headers = %q / %q", resp.Header.Get("ETag"), resp.Header.Get("Cache-Control"))
	}
	// A PNG renders in place, and says so with no sandbox on it; the
	// sniffed type is never re-decided by the browser (design doc 0046
	// §3.2).
	if d := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(d, "inline;") {
		t.Errorf("Content-Disposition = %q, want inline for an image", d)
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", resp.Header.Get("X-Content-Type-Options"))
	}
	if csp := resp.Header.Get("Content-Security-Policy"); csp != "" {
		t.Errorf("Content-Security-Policy = %q on an image served inline", csp)
	}

	// Accept: application/json on the same address answers metadata, not
	// bytes — the only way to read a file's sha256 without downloading it
	// (design doc 0064).
	req, _ = http.NewRequest(http.MethodGet, attURL, nil)
	req.Header.Set("Accept", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var meta domain.File
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || meta.SHA256 != sum || meta.Name != "weekly.png" {
		t.Fatalf("Accept: application/json on a file = %d, meta %+v, want 200 with sha256 %q", resp.StatusCode, meta, sum)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	// Conditional GET with the current hash: 304, no body.
	req, _ = http.NewRequest(http.MethodGet, attURL, nil)
	req.Header.Set("If-None-Match", `"`+sum+`"`)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotModified || len(body) != 0 {
		t.Errorf("conditional GET = %d with %d bytes, want 304 empty", resp.StatusCode, len(body))
	}

	// A stale hash still gets the bytes.
	req, _ = http.NewRequest(http.MethodGet, attURL, nil)
	req.Header.Set("If-None-Match", `"deadbeef"`)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != string(png) {
		t.Errorf("stale conditional GET = %d with %d bytes, want 200 with the file", resp.StatusCode, len(body))
	}

	// Detach: the attachment goes away, the entry stays.
	req, _ = http.NewRequest(http.MethodDelete, attURL, nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("detach status = %d, want 204", resp.StatusCode)
	}
	resp, err = http.Get(attURL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET a detached attachment = %d, want 404", resp.StatusCode)
	}

	// Soft-delete the entry so its attachment row does not stay live in
	// the shared test database: other packages' tests scan all live
	// attachments (the export snapshot) and resolve bytes from their own
	// blob stores, while this test's bytes live only in this process.
	req, _ = http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/bundle/"+typ+"/reading.md", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("cleanup delete status = %d", resp.StatusCode)
	}
}

// memBlobStore is a minimal in-memory blob.Store for the attachment
// round-trip without GCS.
type memBlobStore map[string][]byte

func (m memBlobStore) Put(_ context.Context, sum, _ string, data []byte) error {
	if _, ok := m[sum]; !ok {
		m[sum] = append([]byte(nil), data...)
	}
	return nil
}

func (m memBlobStore) Get(_ context.Context, sum string) ([]byte, error) {
	data, ok := m[sum]
	if !ok {
		return nil, fmt.Errorf("mem blob store: %s not found", sum)
	}
	return data, nil
}

// docFrom renders a test's entry map as the OKF document the server takes
// (design doc 0043 §3.5). The maps stay: they are how these tests say
// "an entry with these fields", and only the wire format changed.
func docFrom(t *testing.T, entry map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var k domain.Knowledge
	if err := json.Unmarshal(raw, &k); err != nil {
		t.Fatal(err)
	}
	doc, err := okf.Canonical(&k)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// putDoc writes a document to id. onlyIfAbsent asks for a create, which
// is what the tests that used to POST mean.
// postRuling records one human ruling on an entry — the single face that
// replaced verify, reject and un-reject (design doc 0055 §3.1). The body
// travels as a string so a test can send one the vocabulary refuses.
func postRuling(base, id, body string) (*http.Response, error) {
	return http.Post(base+"/api/v1/review/"+id, "application/json", strings.NewReader(body))
}

func putDoc(t *testing.T, base, id string, doc []byte, onlyIfAbsent bool) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, base+"/api/v1/bundle/"+id+".md", bytes.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "text/markdown")
	if onlyIfAbsent {
		req.Header.Set("If-None-Match", "*")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// getText fetches a path without asking for JSON — what a reader of the
// bundle gets.
func getText(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", url, resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
		t.Errorf("GET %s: content-type = %q, want markdown", url, ct)
	}
	return string(body)
}

func getJSON(t *testing.T, url string, v any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The reserved paths answer with a file unless asked otherwise
	// (design doc 0046 §§3.7-3.8); every other path ignores this.
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET %s = %d: %s", url, resp.StatusCode, body)
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("GET %s: decode: %v", url, err)
	}
}

// getMarkdown fetches a generated file as the file it is — no Accept,
// which is what a browser or a curl sends and what the reserved paths
// answer with (design doc 0046 §§3.7-3.8).
func getMarkdown(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", url, resp.StatusCode, body)
	}
	return string(body)
}

// Guard: the vocabulary domain.Rulings names is exactly the one the
// review face accepts. Every surface quotes that list — the refusal
// text, docs/surface.md's VOCAB count, the OpenAPI enum — so a fourth
// arm added to the handler without a word added to the list would go out
// undocumented and uncounted, and a word removed would leave the list
// promising something that 400s.
func TestRESTIntegrationEveryRulingIsAccepted(t *testing.T) {
	srv, _ := newIntegrationServer(t)

	typ := testdb.Unique(t, "restit")
	id := typ + "/ruled-on"
	removeEntries(t, srv, id)
	putDoc(t, srv.URL, id, docFrom(t, map[string]any{"type": typ, "id": id}), true).Body.Close()

	// In this order the three are legal back to back: a rejection to
	// stand up, then a withdrawal to take it back, then a verification.
	// What a ruling outside the list does is restapi_test.go's
	// TestReviewRefusesRulingsItCannotRecord — a body the spec refuses is
	// one this suite cannot send, because every request here is validated
	// against api/openapi.yaml before it is answered.
	for _, ruling := range []string{"rejected", "withdrawn", "verified"} {
		if !slices.Contains(domain.Rulings, ruling) {
			t.Fatalf("this test rules %q, which domain.Rulings no longer names", ruling)
		}
		resp, err := postRuling(srv.URL, id, `{"ruling":"`+ruling+`"}`)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("ruling %q = %d: %s", ruling, resp.StatusCode, body)
		}
	}
	for _, ruling := range domain.Rulings {
		if !slices.Contains([]string{"rejected", "withdrawn", "verified"}, ruling) {
			t.Errorf("domain.Rulings names %q, which this test never sends", ruling)
		}
	}
}

// TestRESTIntegrationWithdrawWithNothingToWithdrawIs409 pins design doc
// 0064: a live concept carrying no rejection is a 409 on "withdrawn", the
// same shape purge-on-a-live-concept already answers — not a 404, which
// would be indistinguishable from "no such concept".
func TestRESTIntegrationWithdrawWithNothingToWithdrawIs409(t *testing.T) {
	srv, _ := newIntegrationServer(t)

	typ := testdb.Unique(t, "restnowithdraw")
	id := typ + "/never-rejected"
	putDoc(t, srv.URL, id, docFrom(t, map[string]any{"type": typ, "id": id}), true).Body.Close()

	resp, err := postRuling(srv.URL, id, `{"ruling":"withdrawn"}`)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("withdraw with nothing to withdraw = %d, want 409: %s", resp.StatusCode, body)
	}

	// A concept that never existed is still a 404 — the two are told
	// apart, not both folded into one status.
	resp, err = postRuling(srv.URL, typ+"/does-not-exist", `{"ruling":"withdrawn"}`)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("withdraw on a missing concept = %d, want 404", resp.StatusCode)
	}
}

// POST /api/v1/review/{id} {"ruling":"verified"} records a verification
// against the entry as it stands. The second call is the point:
// re-affirming an entry that is already verified is what empties the
// review feeds, and PUT cannot do it (design doc 0025 §6, respelled onto
// the one ruling face by 0055).
func TestRESTIntegrationVerify(t *testing.T) {
	srv, _ := newIntegrationServer(t)

	typ := testdb.Unique(t, "restit")
	id := typ + "/verify-me"
	payload := docFrom(t, map[string]any{"type": typ, "id": id, "title": "verify round trip"})
	resp := putDoc(t, srv.URL, id, payload, true)
	if resp.StatusCode != http.StatusCreated {
		resp.Body.Close()
		t.Fatalf("create status = %d", resp.StatusCode)
	}
	var created domain.View
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	// A create returns the entry, so it returns the version with it: a
	// client that creates and then updates conditionally should not have
	// to GET the entry it just wrote to learn what to put in If-Match.
	hash := created.Summary.ContentHash
	if got, want := resp.Header.Get("ETag"), `"`+hash+`"`; got != want || hash == "" {
		t.Errorf("create ETag = %q, want %q", got, want)
	}

	verify := func() domain.View {
		t.Helper()
		resp, err := postRuling(srv.URL, id, `{"ruling":"verified"}`)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("verify = %d: %s", resp.StatusCode, body)
		}
		if resp.Header.Get("ETag") == "" {
			t.Error("verify response carries no ETag")
		}
		var v domain.View
		if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}

	first := verify()
	// The test server reads no identity, so the confirmation is a
	// process's: machine-confirmed, not human-reviewed (SPEC §5.3).
	if len(first.Observed.Verified) != 1 || first.Summary.Trust != domain.TrustMachine {
		t.Fatalf("verify appended nothing: %+v", first.Observed.Verified)
	}
	// The ledger is an observation, so it travels beside the document
	// rather than inside it, and confirming an entry moves no version
	// (design docs 0009, 0043 §§3.2, 3.4).
	if strings.Contains(first.Document, "verified") {
		t.Errorf("verification leaked into the document: %q", first.Document)
	}
	if first.Summary.ContentHash != hash {
		t.Errorf("verify moved the version: %q -> %q", hash, first.Summary.ContentHash)
	}
	// Re-verifying appends rather than replacing, so the ledger answers
	// "how often, and by whom" (design doc 0043 §3.2).
	second := verify()
	if len(second.Observed.Verified) != 2 {
		t.Fatalf("re-verify replaced the ledger: %+v", second.Observed.Verified)
	}
	if !second.Observed.LastVerified().At.After(first.Observed.LastVerified().At) {
		t.Errorf("re-verify did not move the newest verification: %v -> %v",
			first.Observed.LastVerified().At, second.Observed.LastVerified().At)
	}

	resp, err := postRuling(srv.URL, typ+"/no-such-entry", `{"ruling":"verified"}`)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("verify of a missing entry = %d, want 404", resp.StatusCode)
	}

	// Leave nothing live behind: the test database is shared (CONTRIBUTING).
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/bundle/"+id+".md", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("cleanup delete status = %d", resp.StatusCode)
	}
}

// TestRESTContextBudgetGovernsTheResponse pins the budget on the surface
// an embedding host actually calls. The cap is a promise about what the
// caller receives, and hits used to embed the whole entry: an entry the
// packer refused to deliver arrived in full through hits anyway, so the
// outline row pointed at knowledge the response had already spent the
// budget on twice (design doc 0033).
func TestRESTContextBudgetGovernsTheResponse(t *testing.T) {
	srv, _ := newIntegrationServer(t)

	typ := testdb.Unique(t, "ctxbudget")
	body := strings.Repeat("x", 2000)
	var ids []string
	for i := range 4 {
		id := fmt.Sprintf("%s/entry-%d", typ, i)
		payload := docFrom(t, map[string]any{
			"type": typ, "id": id, "title": typ + " budget",
			"description": "an entry about " + typ, "body": body,
		})
		resp := putDoc(t, srv.URL, id, payload, true)
		resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create %s: status %d", id, resp.StatusCode)
		}
		ids = append(ids, id)
	}
	removeEntries(t, srv, ids...)

	const budget = 2500
	resp, err := http.Get(fmt.Sprintf("%s/api/v1/context?q=%s&limit=5&budget=%d", srv.URL, typ, budget))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("context status = %d", resp.StatusCode)
	}
	wire, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Hits      []domain.ContextRank    `json:"hits"`
		Concepts  []domain.View           `json:"concepts"`
		Outline   []domain.ContextOutline `json:"outline"`
		Truncated int                     `json:"truncated"`
	}
	if err := json.Unmarshal(wire, &got); err != nil {
		t.Fatal(err)
	}
	if got.Truncated == 0 {
		t.Fatalf("budget %d over 4 concepts of %d bytes truncated nothing", budget, len(body))
	}
	if len(got.Hits) == 0 {
		t.Error("the ranking is gone; hits still have to say what matched")
	}
	if bytes.Count(wire, []byte(body)) > len(got.Concepts) {
		t.Errorf("response carries %d copies of the body for %d delivered concepts",
			bytes.Count(wire, []byte(body)), len(got.Concepts))
	}
	ranking, err := json.Marshal(got.Hits)
	if err != nil {
		t.Fatal(err)
	}
	if over := len(wire) - len(ranking); over > budget {
		t.Errorf("response carries %d bytes of knowledge over a %d budget", over, budget)
	}
}

// TestRESTIntegrationUsageBacklinksAndMove covers what the REST
// integration tests had never called — report_outcome, usage, the
// links_to reverse lookup, and move — which also left them outside the
// contract check (design doc 0035 §3.2). The move is the assertion worth having: design
// doc 0021 promises a rename carries everything keyed by the id and
// rewrites inbound references, so the insight that linked to the metric
// must still link to it afterwards, at its new address.
func TestRESTIntegrationUsageBacklinksAndMove(t *testing.T) {
	srv, _ := newIntegrationServer(t)

	root := testdb.Unique(t, "restumb")
	metric, insight := root+"/revenue", root+"/revenue-reading"
	create := func(id, title, body string) {
		t.Helper()
		payload := docFrom(t, map[string]any{
			"type": root, "id": id, "title": title, "body": body,
		})
		resp := putDoc(t, srv.URL, id, payload, true)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("create %s = %d: %s", id, resp.StatusCode, b)
		}
	}
	create(metric, "Revenue", "The headline number.")
	create(insight, "Reading revenue", "Seasonality caveats for [Revenue](/"+metric+".md).")
	removeEntries(t, srv, metric, insight)

	// The reverse edge: the insight links to the metric, not the other
	// way round. It is a filter on the search face (design doc 0046
	// §3.5), and with no query it lists rather than ranks.
	var backlinks struct {
		Hits   []domain.SearchHit `json:"hits"`
		Cursor string             `json:"cursor,omitempty"`
	}
	linksTo := func(id string) string {
		return srv.URL + "/api/v1/search?links_to=" + url.QueryEscape(id)
	}
	getJSON(t, linksTo(metric), &backlinks)
	if len(backlinks.Hits) != 1 || backlinks.Hits[0].ID != insight {
		t.Fatalf("links_to %s = %+v, want just %s", metric, backlinks.Hits, insight)
	}
	if backlinks.Cursor != "" {
		t.Errorf("a one-page listing handed out a cursor: %q", backlinks.Cursor)
	}
	// It is a filter, so it narrows with the rest of them — which is
	// what the endpoint it replaced could not do.
	var narrowed struct {
		Hits []domain.SearchHit `json:"hits"`
	}
	getJSON(t, linksTo(metric)+"&type="+url.QueryEscape(root), &narrowed)
	if len(narrowed.Hits) != 1 {
		t.Errorf("links_to narrowed by type = %+v, want the insight", narrowed.Hits)
	}
	getJSON(t, linksTo(metric)+"&status=deprecated", &narrowed)
	if len(narrowed.Hits) != 0 {
		t.Errorf("links_to narrowed to a status nothing has = %+v, want none", narrowed.Hits)
	}

	// Report an outcome, then read the totals back.
	report, _ := json.Marshal(map[string]string{"outcome": "worked", "note": "ran it against last quarter"})
	resp, err := http.Post(srv.URL+"/api/v1/usage/"+metric, "application/json", bytes.NewReader(report))
	if err != nil {
		t.Fatal(err)
	}
	var reported domain.Usage
	if err := json.NewDecoder(resp.Body).Decode(&reported); err != nil {
		t.Fatalf("report outcome: decode: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || reported.Worked != 1 {
		t.Fatalf("report outcome = %d, worked = %d, want 200 and 1", resp.StatusCode, reported.Worked)
	}
	var usage domain.Usage
	getJSON(t, srv.URL+"/api/v1/usage/"+metric, &usage)
	if usage.Worked != 1 {
		t.Errorf("usage after one worked report = %+v", usage)
	}

	// Move, and check the insight's link came along (design doc 0021).
	// The destination is registered for removal as well: the id the move
	// frees and the id it takes are two rows to clean up, and which of
	// them exists at the end depends on how far this test got.
	moved := root + "/finance/revenue"
	removeEntries(t, srv, moved)
	move, _ := json.Marshal(map[string]string{"from": metric, "to": moved})
	resp, err = http.Post(srv.URL+"/api/v1/move", "application/json", bytes.NewReader(move))
	if err != nil {
		t.Fatal(err)
	}
	var after domain.View
	if err := json.NewDecoder(resp.Body).Decode(&after); err != nil {
		t.Fatalf("move: decode: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || after.ID != moved {
		t.Fatalf("move = %d, id = %q, want 200 and %q", resp.StatusCode, after.ID, moved)
	}
	backlinks.Hits = nil
	getJSON(t, linksTo(moved), &backlinks)
	if len(backlinks.Hits) != 1 || backlinks.Hits[0].ID != insight {
		t.Errorf("links_to after the move = %+v, want the insight to have followed", backlinks.Hits)
	}

	// Usage is keyed by the id, so it moved too.
	usage = domain.Usage{}
	getJSON(t, srv.URL+"/api/v1/usage/"+moved, &usage)
	if usage.Worked != 1 {
		t.Errorf("usage after the move = %+v, want the worked report to have followed", usage)
	}
}

// The path scope reaching the wire (design doc 0041): a search narrowed
// to one subtree, the same search widened to two, and the sibling
// directory whose name merely starts with the scope staying out of both.
// Running through checkedServer also validates the new `prefix` parameter
// against api/openapi.yaml, so the spec cannot describe a filter the
// server does not have (design doc 0035 §3.2).
//
// The link expansion is the part worth pinning: get_context follows links
// out of the scope on purpose, because a team's metric citing the shared
// glossary is exactly the case the one-call read exists for
// (design doc 0041 §2.6).
func TestRESTIntegrationPrefixScopesSearchNotLinks(t *testing.T) {
	srv, _ := newIntegrationServer(t)

	// A run-unique root keeps other tests' rows (and reruns) out of the
	// assertions, the way the other REST integration tests do it.
	root := testdb.Unique(t, "scope")
	term := root + "/company/glossary/activation"
	entries := []struct{ id, body string }{
		// The team metric cites the shared term, so context must reach it.
		{root + "/teams/growth/metrics/activation", "see [activation](/" + term + ".md)"},
		{term, "the company-wide definition"},
		{root + "/teams/growth-archive/metrics/activation", "retired"},
	}
	var ids []string
	for _, e := range entries {
		payload := docFrom(t, map[string]any{
			"type": "Metric", "id": e.id, "title": "activation " + root,
			"description": "an entry about " + root, "body": e.body,
		})
		resp := putDoc(t, srv.URL, e.id, payload, true)
		resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create %s: status %d", e.id, resp.StatusCode)
		}
		ids = append(ids, e.id)
	}
	removeEntries(t, srv, ids...)

	search := func(query string) []string {
		t.Helper()
		resp, err := http.Get(srv.URL + "/api/v1/search?" + query)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET ?%s: status %d", query, resp.StatusCode)
		}
		var got struct {
			Hits []domain.SearchHit `json:"hits"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, h := range got.Hits {
			if strings.HasPrefix(h.ID, root+"/") {
				out = append(out, h.ID)
			}
		}
		slices.Sort(out)
		return out
	}

	// Unscoped, the query reaches all three — otherwise the scoped
	// assertions below would pass for the wrong reason.
	if all := search("q=" + root + "&limit=50"); len(all) != 3 {
		t.Fatalf("unscoped search found %v, want all 3 entries", all)
	}

	scoped := search("q=" + root + "&limit=50&prefix=" + root + "/teams/growth")
	want := []string{root + "/teams/growth/metrics/activation"}
	if !slices.Equal(scoped, want) {
		t.Errorf("scoped search = %v, want %v (growth-archive is a different directory)", scoped, want)
	}

	widened := search(fmt.Sprintf("q=%s&limit=50&prefix=%s/teams/growth&prefix=%s/company", root, root, root))
	want = []string{term, root + "/teams/growth/metrics/activation"}
	if !slices.Equal(widened, want) {
		t.Errorf("two scopes = %v, want %v", widened, want)
	}

	// A scope that cannot lead an id is a 400, not a silent everything.
	resp, err := http.Get(srv.URL + "/api/v1/search?q=" + root + "&prefix=" + url.QueryEscape("a//b"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("malformed prefix status = %d, want 400", resp.StatusCode)
	}

	// Context: scoped to the team, the shared term still arrives, pulled
	// in by the link rather than by the search.
	cresp, err := http.Get(fmt.Sprintf("%s/api/v1/context?q=%s&limit=5&prefix=%s/teams/growth", srv.URL, root, root))
	if err != nil {
		t.Fatal(err)
	}
	defer cresp.Body.Close()
	if cresp.StatusCode != http.StatusOK {
		t.Fatalf("context status = %d", cresp.StatusCode)
	}
	var pack struct {
		Hits     []domain.ContextRank `json:"hits"`
		Concepts []domain.View        `json:"concepts"`
	}
	if err := json.NewDecoder(cresp.Body).Decode(&pack); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, e := range pack.Concepts {
		got[e.ID] = true
	}
	if !got[term] {
		t.Errorf("concepts = %v; the cited glossary term must travel with the scoped entry", got)
	}
	for _, h := range pack.Hits {
		if h.ID == term {
			t.Error("the out-of-scope term ranked as a hit; the scope must bound the search itself")
		}
	}
}

// The write path is a document, and PUT is the whole of it (design doc
// 0043 §3.5): create-or-replace, with the preconditions carrying what
// used to be the difference between POST and PUT.
func TestRESTIntegrationDocumentWrites(t *testing.T) {
	srv, _ := newIntegrationServer(t)

	id := testdb.Unique(t, "restdoc") + "/revenue"
	removeEntries(t, srv, id)
	const doc = "---\ntype: Metric\ntitle: 売上\nowner: finance\n---\n\n本文。\n"

	// A create-only write on a free id is a 201.
	resp := putDoc(t, srv.URL, id, []byte(doc), true)
	var created domain.View
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create = %d", resp.StatusCode)
	}
	// The producer key rode along in the document and is written back
	// where its writer put it — inline, beside the keys the spec defines.
	if !strings.Contains(created.Document, "owner: finance") {
		t.Errorf("producer key lost:\n%s", created.Document)
	}

	// The same write again is a 412: the If-None-Match "*" precondition
	// failed, since the id is no longer free (design doc 0064 — RFC 9110
	// and the spec both say 412 for a failed precondition; 409 is for an
	// ErrAlreadyExists with no precondition behind it, like move onto an
	// occupied id).
	resp = putDoc(t, srv.URL, id, []byte(doc), true)
	resp.Body.Close()
	if resp.StatusCode != http.StatusPreconditionFailed {
		t.Errorf("create-only over a live entry = %d, want 412", resp.StatusCode)
	}

	// Without the precondition it replaces — and an identical document
	// writes nothing.
	resp = putDoc(t, srv.URL, id, []byte(doc), false)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Ochakai-Unchanged") != "true" {
		t.Errorf("identical replace = %d, Ochakai-Unchanged = %q",
			resp.StatusCode, resp.Header.Get("Ochakai-Unchanged"))
	}

	// A document ochakai wrote can be edited and sent back as it came:
	// the keys the server owns are ignored rather than refused, which is
	// what makes get → edit → put a loop a human can run.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/bundle/"+id+".md", nil)
	req.Header.Set("Accept", "text/markdown")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	served, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(served), "generated:") || !strings.Contains(string(served), "created_by:") {
		t.Fatalf("the served document carries no server-owned provenance:\n%s", served)
	}
	edited := strings.Replace(string(served), "title: 売上", "title: 売上(改)", 1)
	resp = putDoc(t, srv.URL, id, []byte(edited), false)
	var back domain.View
	if err := json.NewDecoder(resp.Body).Decode(&back); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || back.Summary.Title != "売上(改)" {
		t.Errorf("round-tripped edit = %d, title %q", resp.StatusCode, back.Summary.Title)
	}
	if back.Observed.CreatedBy.Name != created.Observed.CreatedBy.Name {
		t.Errorf("a document's created_by was read back as provenance: %+v", back.Observed.CreatedBy)
	}
	// The view's document is the stored one: the server-owned keys the
	// text/markdown read carries are not in it, so it needs no stripping
	// before the next edit (design docs 0043 §3.5, 0046 §2.2).
	if strings.Contains(back.Document, "generated:") || strings.Contains(back.Document, "created_by:") {
		t.Errorf("the view's document carries server-owned keys:\n%s", back.Document)
	}

	// A stale version is refused and writes nothing.
	req, _ = http.NewRequest(http.MethodPut, srv.URL+"/api/v1/bundle/"+id+".md", strings.NewReader(doc))
	req.Header.Set("Content-Type", "text/markdown")
	req.Header.Set("If-Match", `"`+created.Summary.ContentHash+`"`)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusPreconditionFailed {
		t.Errorf("stale If-Match = %d, want 412", resp.StatusCode)
	}
}

// TestRESTIntegrationDocumentSurvivesTheRoundTrip pins design doc 0046
// §2.2 at the surface: what a read hands back is what was written.
//
// Storing the bytes is only half of it. The web UI's editor and
// `ochakai get` read the JSON view's document and write it back, so a
// canonical rendering there would reformat every entry those surfaces
// touch — comments gone, key order settled, a status the writer never
// wrote appearing — and the promise would hold only for the callers that
// asked for text/markdown. The property is stated as a round trip
// because that is how it is broken: read, send back, nothing happened.
func TestRESTIntegrationDocumentSurvivesTheRoundTrip(t *testing.T) {
	srv, _ := newIntegrationServer(t)

	id := testdb.Unique(t, "restbytes") + "/revenue"
	removeEntries(t, srv, id)

	// Everything a rendering would quietly normalize: a comment, the
	// title after a producer key, a lowercase recommended type, and no
	// status at all.
	const written = "---\ntype: metric\n# 定義は財務の合意による\nowner: finance\ntitle: 売上\n---\n\n受注合計。\n"

	resp := putDoc(t, srv.URL, id, []byte(written), true)
	var created domain.View
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create = %d", resp.StatusCode)
	}
	if created.Document != written {
		t.Errorf("the write rewrote the document:\n got %q\nwant %q", created.Document, written)
	}

	var read domain.View
	getJSON(t, srv.URL+"/api/v1/bundle/"+id+".md", &read)
	if read.Document != written {
		t.Errorf("the read rewrote the document:\n got %q\nwant %q", read.Document, written)
	}
	// The projection applies OKF's default to a silent document (SPEC
	// §5.4) without the document gaining a key (design doc 0046 §3.9),
	// and the writer's casing is the stored casing.
	if read.Summary.Status != domain.StatusStable {
		t.Errorf("summary status = %q, want stable", read.Summary.Status)
	}
	if strings.Contains(read.Document, "status:") {
		t.Errorf("a status was written into a document that named none:\n%s", read.Document)
	}
	if read.Summary.Type != "metric" {
		t.Errorf("type = %q, want the writer's casing", read.Summary.Type)
	}

	// The markdown representation is the same bytes with this instance's
	// observations appended — the export form, not a second document.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/bundle/"+id+".md", nil)
	req.Header.Set("Accept", "text/markdown")
	mdResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	served, _ := io.ReadAll(mdResp.Body)
	mdResp.Body.Close()
	for _, want := range []string{"# 定義は財務の合意による", "type: metric", "generated:", "created_by:"} {
		if !strings.Contains(string(served), want) {
			t.Errorf("the served document is missing %q:\n%s", want, served)
		}
	}

	// And the loop closes: what the read handed over, sent back, is not
	// a change.
	resp = putDoc(t, srv.URL, id, []byte(read.Document), false)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Ochakai-Unchanged") != "true" {
		t.Errorf("sending back what was read = %d, Ochakai-Unchanged = %q",
			resp.StatusCode, resp.Header.Get("Ochakai-Unchanged"))
	}
}

// A document written somewhere else says who generated and confirmed it.
// That is a claim, not this instance's observation — so it reaches no
// ledger and no trust tier, and it is not destroyed either: it is kept
// under `received` and reported (design doc 0046 §2.2, issue #292).
func TestRESTIntegrationForeignTrustFamilyIsKeptAsAClaim(t *testing.T) {
	srv, _ := newIntegrationServer(t)

	id := testdb.Unique(t, "restclaim") + "/revenue"
	removeEntries(t, srv, id)

	// The export form of another instance: a human wrote it there, and a
	// different human confirmed it there.
	const written = "---\ntype: Metric\ntitle: 売上\n" +
		"generated:\n  by: human:sato@example.co.jp\n  at: 2026-07-01T00:00:00Z\n" +
		"verified:\n  - by: human:ceo@example.co.jp\n    at: 2026-07-02T00:00:00Z\n" +
		"created_by: human:sato@example.co.jp\n---\n\n受注合計。\n"

	resp := putDoc(t, srv.URL, id, []byte(written), true)
	var created domain.View
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create = %d", resp.StatusCode)
	}
	// Nothing was applied in silence: SPEC §11 wants the reinterpretation
	// said out loud, and this is the one that used to be mute.
	if notes := resp.Header.Values("Ochakai-Note"); len(notes) != 1 ||
		!strings.Contains(notes[0], "received") {
		t.Errorf("Ochakai-Note = %q, want one note naming the claim", notes)
	}
	// The claim is in the stored bytes, where a later release can still
	// find it, and out of the keys this instance owns.
	if !strings.Contains(created.Document, "received:") ||
		!strings.Contains(created.Document, "human:ceo@example.co.jp") {
		t.Errorf("the claim was not kept:\n%s", created.Document)
	}
	if strings.Contains(created.Document, "\ncreated_by:") ||
		strings.Contains(created.Document, "\nverified:") {
		t.Errorf("a claim was left where this instance's own keys go:\n%s", created.Document)
	}
	// And nowhere else. The entry is unverified here, by the importer.
	if created.Summary.Trust != domain.TrustUnverified {
		t.Errorf("trust = %q, want unverified: a claim is not a confirmation", created.Summary.Trust)
	}
	if len(created.Observed.Verified) != 0 {
		t.Errorf("a claim reached the ledger: %+v", created.Observed.Verified)
	}
	if created.Observed.CreatedBy.Name == "sato@example.co.jp" {
		t.Errorf("a claim became this instance's created_by: %+v", created.Observed.CreatedBy)
	}

	// Importing the same foreign document again writes nothing: the claim
	// it carries is the claim already recorded, so a recurring sync does
	// not churn revisions.
	resp = putDoc(t, srv.URL, id, []byte(written), false)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Ochakai-Unchanged") != "true" {
		t.Errorf("re-importing = %d, Ochakai-Unchanged = %q",
			resp.StatusCode, resp.Header.Get("Ochakai-Unchanged"))
	}

	// The export form carries both — the claim and this instance's own
	// observation — and sending it back is still not a change.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/bundle/"+id+".md", nil)
	req.Header.Set("Accept", "text/markdown")
	mdResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	served, _ := io.ReadAll(mdResp.Body)
	mdResp.Body.Close()
	for _, want := range []string{"received:", "generated:", "created_by:"} {
		if !strings.Contains(string(served), want) {
			t.Errorf("the export form is missing %q:\n%s", want, served)
		}
	}
	resp = putDoc(t, srv.URL, id, served, false)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Ochakai-Unchanged") != "true" {
		t.Errorf("sending back the export form = %d, Ochakai-Unchanged = %q",
			resp.StatusCode, resp.Header.Get("Ochakai-Unchanged"))
	}
	if notes := resp.Header.Values("Ochakai-Note"); len(notes) != 0 {
		t.Errorf("our own export form was reported as a claim: %q", notes)
	}
}

// A bundle carries what it carries (design doc 0046 §3.2): the write
// path refuses no media type, and the delivery rule is what keeps the
// dangerous ones from running. This is the pair #280 could only test one
// half of, because nothing that takes the defended branch could be
// stored yet.
func TestRESTIntegrationAnyFileIsAcceptedAndServedInert(t *testing.T) {
	lockLiveAttachments(t)
	srv, s := newIntegrationServer(t)
	s.UseBlobStore(memBlobStore{})

	typ := testdb.Unique(t, "restany")
	id := typ + "/diagram"
	// Purge at the end, like every other test that holds live files: the
	// shared test database is scanned whole by the export and by the
	// re-embed backlog, and those resolve bytes against their own blob
	// fake. Purge rather than a soft delete, because a file is an object
	// with a life of its own (design doc 0046 §3.3) — deleting the entry
	// leaves the files in its namespace exactly where they are, and the
	// archive carries them. A defer rather than t.Cleanup — cleanups run
	// after this function's defers, by which time the server is closed.
	defer func() {
		for _, q := range []string{"", "?purge=true"} {
			req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/bundle/"+id+".md"+q, nil)
			if resp, err := http.DefaultClient.Do(req); err == nil {
				resp.Body.Close()
			}
		}
	}()
	resp := putDoc(t, srv.URL, id, docFrom(t, map[string]any{
		"type": typ, "id": id, "title": "anything the bundle carried"}), true)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", resp.StatusCode)
	}

	for _, tc := range []struct {
		name, mediaType string
		data            []byte
		inline          bool
	}{
		// The two that carry script, and the two that were simply not on
		// the old list. None of them is refused; the first two are handed
		// over rather than rendered.
		{"er.svg", "text/xml", []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"/>`), false},
		{"report.html", "text/html", []byte(`<!DOCTYPE html><script>alert(1)</script>`), false},
		{"seeds.zip", "application/zip", append([]byte("PK\x03\x04"), make([]byte, 16)...), false},
		{"shot.gif", "image/gif", append([]byte("GIF89a"), make([]byte, 16)...), true},
	} {
		attURL := srv.URL + "/api/v1/bundle/" + id + "/" + tc.name
		req, _ := http.NewRequest(http.MethodPut, attURL, bytes.NewReader(tc.data))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Errorf("attaching %s = %d, want 201: %s", tc.name, resp.StatusCode, body)
			continue
		}

		resp, err = http.Get(attURL)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(got) != string(tc.data) {
			t.Errorf("%s came back as %d bytes, want %d", tc.name, len(got), len(tc.data))
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, tc.mediaType) {
			t.Errorf("%s is served as %q, want %q", tc.name, ct, tc.mediaType)
		}
		if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: no nosniff", tc.name)
		}
		want, csp := "attachment;", "sandbox"
		if tc.inline {
			want, csp = "inline;", ""
		}
		if d := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(d, want) {
			t.Errorf("%s: Content-Disposition = %q, want %s…", tc.name, d, want)
		}
		if got := resp.Header.Get("Content-Security-Policy"); got != csp {
			t.Errorf("%s: Content-Security-Policy = %q, want %q", tc.name, got, csp)
		}
	}

	// And the entry carries all four: no cap on how many objects a
	// directory of the bundle may hold.
	var view domain.View
	getJSON(t, srv.URL+"/api/v1/bundle/"+id+".md", &view)
	if len(view.Files) != 4 {
		t.Errorf("the entry carries %d files, want 4 — nothing caps how many", len(view.Files))
	}
}

// The bundle address serves and takes every object in it (design doc
// 0046 §3.5): a concept at its own path, a file at its own path, and a
// markdown document that carries no type — which is not a broken concept
// but a file that happens to be markdown (SPEC §11 makes the key what a
// concept has), and which a bundle that carried one gets back.
func TestRESTIntegrationBundleAddressWritesEveryObject(t *testing.T) {
	lockLiveAttachments(t)
	srv, s := newIntegrationServer(t)
	s.UseBlobStore(memBlobStore{})

	typ := testdb.Unique(t, "restbundle")
	id := typ + "/revenue"
	bundle := srv.URL + "/api/v1/bundle/"
	defer func() {
		req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/bundle/"+id+".md", nil)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close()
		}
	}()

	put := func(t *testing.T, path string, body []byte) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodPut, bundle+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	// A concept, written at the path it lives at — which is now the only
	// place it can be written (design doc 0046 §3.5).
	doc := fmt.Sprintf("---\ntype: %s\ntitle: 売上\n---\n\n![chart](revenue/chart.png)\n", typ)
	resp := put(t, id+".md", []byte(doc))
	var view domain.View
	if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	etag := resp.Header.Get("ETag")
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated || view.Document != doc {
		t.Fatalf("writing a concept at its path = %d, document %q", resp.StatusCode, view.Document)
	}
	if etag == "" {
		t.Error("no ETag on a concept written through the bundle address")
	}
	// The same bytes again write nothing.
	resp = put(t, id+".md", []byte(doc))
	resp.Body.Close()
	if resp.Header.Get("Ochakai-Unchanged") != "true" {
		t.Errorf("an identical write through the bundle address changed something")
	}
	// And it reads back at the same address.
	var read domain.View
	getJSON(t, bundle+id+".md", &read)
	if read.Document != doc {
		t.Errorf("reading the concept back gave %q", read.Document)
	}

	// A file, at a path of its own.
	png := append([]byte("\x89PNG\r\n\x1a\n"), []byte("bundle chart")...)
	resp = put(t, id+"/chart.png", png)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("writing a file = %d, want 201", resp.StatusCode)
	}
	resp, err := http.Get(bundle + id + "/chart.png")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(got) != string(png) {
		t.Errorf("the file came back as %d bytes, want %d", len(got), len(png))
	}
	// It is attributed to the entry that shows it, without anybody
	// having said so (design doc 0046 §3.3).
	getJSON(t, srv.URL+"/api/v1/bundle/"+id+".md", &read)
	if len(read.Files) != 1 || read.Files[0].Name != "chart.png" {
		t.Errorf("the entry does not carry the file its body shows: %+v", read.Files)
	}

	// A markdown document with no type is a file, not a bad concept.
	notes := []byte("# 会議メモ\n\ntype なし。\n")
	resp = put(t, typ+"/notes.md", notes)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("writing a typeless markdown file = %d, want 201", resp.StatusCode)
	}
	resp, err = http.Get(bundle + typ + "/notes.md")
	if err != nil {
		t.Fatal(err)
	}
	got, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(got) != string(notes) {
		t.Errorf("the typeless markdown file came back as %q", got)
	}
	// It is not a concept, and the way to see that is what lists it —
	// nothing does. There is no second surface left to ask: the concept
	// address /api/v1/knowledge/{id} is gone (design doc 0046 §3.5), so
	// "the concept surface 404s for it" is no longer a question anybody
	// can put. index.md lists the concepts at a level, and this file is
	// not among them.
	var listing service.BrowseResult
	getJSON(t, srv.URL+"/api/v1/bundle/"+typ+"/index.md", &listing)
	for _, e := range listing.Concepts {
		if e.ID == typ+"/notes" || e.ID == typ+"/notes.md" {
			t.Errorf("a typeless markdown file is listed as a concept: %+v", e)
		}
	}

	// Delete reaches both kinds.
	for _, path := range []string{typ + "/notes.md", id + "/chart.png"} {
		req, _ := http.NewRequest(http.MethodDelete, bundle+path, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Errorf("deleting %s = %d", path, resp.StatusCode)
		}
	}
}

// The instance stats on the wire (design doc 0051), which also brings
// them under the contract check (design doc 0035 §3.2): the tally counts
// the entry this test just wrote, the search that found nothing is
// counted as a question, and a window past the retention is refused
// rather than quietly shortened.
func TestRESTIntegrationStats(t *testing.T) {
	srv, s := newIntegrationServer(t)

	root := testdb.Unique(t, "stats")
	id := root + "/revenue"
	resp := putDoc(t, srv.URL, id, docFrom(t, map[string]any{
		"type": root, "id": id, "title": "Revenue", "status": "draft",
	}), true)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create = %d", resp.StatusCode)
	}
	removeEntries(t, srv, id)

	var before domain.Stats
	getJSON(t, srv.URL+"/api/v1/stats?days=1", &before)
	if before.WindowDays != 1 || before.Concepts.Total == 0 {
		t.Fatalf("stats = %+v", before)
	}
	if _, ok := before.Concepts.Status[string(domain.StatusDraft)]; !ok {
		t.Errorf("status tally has no draft: %+v", before.Concepts.Status)
	}

	// A search nothing answers. Recording is off the read path, so the
	// count only moves after a flush (design docs 0029, 0051 §3.2).
	nonsense := "qqzzxx" + root
	var hits struct {
		Hits []domain.SearchHit `json:"hits"`
	}
	getJSON(t, srv.URL+"/api/v1/search?q="+nonsense, &hits)
	if len(hits.Hits) != 0 {
		t.Fatalf("the nonsense query matched %d entries", len(hits.Hits))
	}
	if err := s.FlushUsage(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { deleteMisses(t, nonsense) })

	var after domain.Stats
	getJSON(t, srv.URL+"/api/v1/stats?days=1", &after)
	if !after.Misses.Recording {
		t.Error("misses.recording is false on a deployment that records them")
	}
	// `misses` is the one number here that no prefix can narrow — a
	// search that found nothing found it nowhere, so there is no id to
	// scope it by (design doc 0051 §3.7). That makes it instance-wide,
	// and the test database is one instance shared by every package's
	// tests, which run as concurrent processes and delete their own miss
	// rows as they finish (CONTRIBUTING). A before/after delta on a
	// global count is therefore not this test's to assert: it went
	// negative in CI when a sibling package's cleanup landed between the
	// two reads.
	//
	// What is this test's is its own miss, so that is what it checks —
	// that the read path recorded it, and that the count the face
	// reports includes it.
	if n := missCount(t, nonsense); n != 1 {
		t.Errorf("the read path recorded %d misses for its own query, want 1", n)
	}
	if after.Misses.Count < 1 {
		t.Errorf("misses.count = %d after a miss was flushed, want at least 1", after.Misses.Count)
	}

	// The window is bounded by what the raw events can answer for.
	resp, err := http.Get(srv.URL + "/api/v1/stats?days=365")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("stats?days=365 = %d, want 400", resp.StatusCode)
	}
}

// missCount is how many misses this exact query left behind. Scoping to
// the query is what makes it this test's own number: the table is shared
// with every other package's tests against the same database, so a
// count over all of it answers about them too.
func missCount(t *testing.T, query string) int64 {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatalf("counting misses: %v", err)
	}
	defer conn.Close(ctx)
	var n int64
	if err := conn.QueryRow(ctx,
		`SELECT count(*) FROM search_miss WHERE query = $1`, query).Scan(&n); err != nil {
		t.Fatalf("counting misses: %v", err)
	}
	return n
}

// deleteMisses removes the rows a test's own query left behind: misses
// are pruned on the raw events' 180-day schedule, not by any test, and
// the test database is shared and long-lived (issue #278).
func deleteMisses(t *testing.T, query string) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, testDatabaseURL(t))
	if err != nil {
		t.Errorf("cleaning up misses: %v", err)
		return
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `DELETE FROM search_miss WHERE query = $1`, query); err != nil {
		t.Errorf("cleaning up misses: %v", err)
	}
}

// A listing on the wire pages: the response carries a cursor while there
// is more behind it, passing that cursor back continues the same order,
// and the last page carries none — which is the only signal a caller gets
// that the listing ended (design doc 0050 §§2.1, 2.3). A search refuses
// the parameter with a 400, because a ranking has no page two (§2.2).
func TestRESTIntegrationListingsPage(t *testing.T) {
	srv, _ := newIntegrationServer(t)

	// A run-unique root, as the other REST integration tests do it: the
	// feed below is filtered to this test's own entries, so a shared test
	// database cannot change what the walk should see.
	root := testdb.Unique(t, "page")
	var ids []string
	for i := range 5 {
		id := fmt.Sprintf("%s/e%d", root, i)
		doc := docFrom(t, map[string]any{
			"type": "Insight", "id": id, "title": "draft " + id,
			"status": "draft", "tags": []string{root},
			"stale_after": fmt.Sprintf("2020-01-%02d", i+1),
		})
		resp := putDoc(t, srv.URL, id, doc, true)
		resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create %s: status %d", id, resp.StatusCode)
		}
		ids = append(ids, id)
	}
	removeEntries(t, srv, ids...)

	page := func(query string) (hits []string, cursor string) {
		t.Helper()
		resp, err := http.Get(srv.URL + "/api/v1/search?" + query)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET ?%s: status %d", query, resp.StatusCode)
		}
		var got struct {
			Hits   []domain.SearchHit `json:"hits"`
			Cursor string             `json:"cursor"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		for _, h := range got.Hits {
			hits = append(hits, h.ID)
		}
		return hits, got.Cursor
	}

	feed := "sort=stale_after&tag=" + root
	whole, cursor := page(feed + "&limit=50")
	if len(whole) != len(ids) {
		t.Fatalf("the feed read whole = %v, want the %d entries this test wrote", whole, len(ids))
	}
	if cursor != "" {
		t.Errorf("a listing that ended carried a cursor: %q", cursor)
	}

	var walked []string
	cursor = ""
	for range len(ids) + 2 {
		q := feed + "&limit=2"
		if cursor != "" {
			q += "&cursor=" + url.QueryEscape(cursor)
		}
		var got []string
		got, cursor = page(q)
		walked = append(walked, got...)
		if cursor == "" {
			break
		}
	}
	if !slices.Equal(walked, whole) {
		t.Errorf("walking the feed in pages of 2 saw %v, reading it whole saw %v", walked, whole)
	}

	// A ranking has no next page, and saying so is a client error rather
	// than a cursor quietly ignored.
	resp, err := http.Get(srv.URL + "/api/v1/search?q=" + root + "&cursor=" + url.QueryEscape(cursorOf(t, page, feed)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a cursor on a search: status %d, want 400", resp.StatusCode)
	}
}

// cursorOf reads the first page of a listing for a cursor to reuse.
func cursorOf(t *testing.T, page func(string) ([]string, string), feed string) string {
	t.Helper()
	_, cursor := page(feed + "&limit=1")
	if cursor == "" {
		t.Fatal("the feed ended after one entry; the test set never reached the store")
	}
	return cursor
}

// The `queues` key of GET /api/v1/stats answers "is there anything to
// do" with the sizes of the three feeds a curator empties (design doc
// 0049), and the answer moves as the work is done: a failure report puts
// an entry in, the verification that answers it takes the entry out. The
// prefix scope is what keeps this test's numbers its own — and since the
// counts lost their own endpoint (0049 §3.1) it is `stats` that takes it.
func TestRESTQueuesCountsTheReviewQueues(t *testing.T) {
	srv, _ := newIntegrationServer(t)

	root := testdb.Unique(t, "queuesit")
	draft := root + "/proposals/margin"
	broken := root + "/queries/revenue"
	expired := root + "/insights/seasonality"
	removeEntries(t, srv, draft, broken, expired)

	create := func(id string, entry map[string]any) {
		resp := putDoc(t, srv.URL, id, docFrom(t, entry), true)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("create %s = %d: %s", id, resp.StatusCode, b)
		}
	}
	create(draft, map[string]any{"type": "Insight", "id": draft, "title": "Gross margin", "status": "draft"})
	create(broken, map[string]any{"type": "Metric", "id": broken, "title": "Monthly revenue"})
	create(expired, map[string]any{"type": "Insight", "id": expired, "title": "Seasonality",
		"stale_after": time.Now().UTC().AddDate(0, 0, -1).Format(domain.DateLayout)})

	counts := func() domain.QueueCounts {
		t.Helper()
		var out struct {
			Queues domain.QueueCounts `json:"queues"`
		}
		getJSON(t, srv.URL+"/api/v1/stats?prefix="+root, &out)
		return out.Queues
	}
	if got, want := counts(), (domain.QueueCounts{Drafts: 1, StaleAfter: 1}); got != want {
		t.Fatalf("queues = %+v, want %+v", got, want)
	}

	// A failure report against the untouched entry: now something is
	// reported wrong.
	report, _ := json.Marshal(map[string]string{"outcome": "failed", "note": "double counts split shipments"})
	resp, err := http.Post(srv.URL+"/api/v1/usage/"+broken, "application/json", bytes.NewReader(report))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got, want := counts(), (domain.QueueCounts{Drafts: 1, Failed: 1, StaleAfter: 1}); got != want {
		t.Fatalf("queues after a failure report = %+v, want %+v", got, want)
	}

	// Verifying is the answer to that report, and the queue must shorten
	// — a queue nobody can empty stops being read (design doc 0025 §6.1).
	resp, err = postRuling(srv.URL, broken, `{"ruling":"verified"}`)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got, want := counts(), (domain.QueueCounts{Drafts: 1, StaleAfter: 1}); got != want {
		t.Errorf("queues after the re-verification = %+v, want %+v", got, want)
	}

	// Out of scope is out of the count: another prefix sees none of this.
	var elsewhere struct {
		Queues domain.QueueCounts `json:"queues"`
	}
	getJSON(t, srv.URL+"/api/v1/stats?prefix="+root+"x", &elsewhere)
	if elsewhere.Queues != (domain.QueueCounts{}) {
		t.Errorf("a neighbouring prefix counted %+v, want all zero", elsewhere.Queues)
	}
}

// getArchive asks a bundle path for its OKF archive: the representation
// an Accept header selects (design doc 0046 §3.5), which is why this is
// not a plain http.Get.
func getArchive(t *testing.T, url, query string) (*http.Response, error) {
	t.Helper()
	if query != "" {
		url += "?" + query
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "application/gzip")
	return http.DefaultClient.Do(req)
}

// What enters the bundle leaves it (design doc 0046 §3.2), including a
// file that belongs to no entry: a producer's seed data in a shared
// directory, or a file whose concept has since been deleted. The archive
// is the copy this endpoint exists to hand back, and it used to be built
// from the files *attributed* to a live entry — so those came back
// missing, silently, from the one place a knowledge base is a backup.
func TestRESTIntegrationTheArchiveCarriesAFileNothingOwns(t *testing.T) {
	lockLiveAttachments(t)
	srv, s := newIntegrationServer(t)
	s.UseBlobStore(memBlobStore{})

	root := testdb.Unique(t, "restloose")
	loose := root + "/seed-data.csv"
	// A defer rather than t.Cleanup: cleanups run after this function's
	// defers, by which time the server is closed.
	defer func() {
		req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/bundle/"+loose, nil)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close()
		}
	}()

	req, err := http.NewRequest(http.MethodPut, srv.URL+"/api/v1/bundle/"+loose,
		bytes.NewReader([]byte("region,orders\napac,12\n")))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("writing a file nothing owns = %d, want 201", resp.StatusCode)
	}

	resp, err = getArchive(t, srv.URL+"/api/v1/bundle/", "")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("archive status = %d", resp.StatusCode)
	}
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for tr := tar.NewReader(gz); ; {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Name == loose {
			found = true
		}
	}
	if !found {
		t.Errorf("the archive does not carry %s: a file that entered the bundle did not leave it", loose)
	}
}

// ?purge=true names something only a concept has — a tombstone still
// holding an id. A file has none, and its one delete is already the
// irreversible one, so the parameter is nothing to refuse over. What it
// must not do is answer 404 for a file that is right there: a caller
// scripting "remove this permanently" would read that as done.
func TestRESTIntegrationPurgeOnAFileRemovesIt(t *testing.T) {
	lockLiveAttachments(t)
	srv, s := newIntegrationServer(t)
	s.UseBlobStore(memBlobStore{})

	path := testdb.Unique(t, "restpurge") + "/seed.csv"
	req, err := http.NewRequest(http.MethodPut, srv.URL+"/api/v1/bundle/"+path,
		bytes.NewReader([]byte("a,b\n1,2\n")))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("writing the file = %d, want 201", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/bundle/"+path+"?purge=true", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE ?purge=true on a file = %d, want 204", resp.StatusCode)
	}
	got, err := http.Get(srv.URL + "/api/v1/bundle/" + path)
	if err != nil {
		t.Fatal(err)
	}
	got.Body.Close()
	if got.StatusCode != http.StatusNotFound {
		t.Errorf("the file is still there after the purge: %d", got.StatusCode)
	}
}

// A directory's index.md lists the files in it, in both of its
// representations and in the archive (design doc 0046 §3.7). A file is
// an object in the bundle, not a property of an entry (§3.3), so a
// listing that showed only concepts described a directory that was not
// there — and the archive's index.md and the generated one have to say
// the same thing, or a bundle's navigation depends on which asked.
func TestRESTIntegrationIndexListsTheFilesInADirectory(t *testing.T) {
	lockLiveAttachments(t)
	srv, s := newIntegrationServer(t)
	s.UseBlobStore(memBlobStore{})

	typ := testdb.Unique(t, "restfiles")
	id := typ + "/revenue"
	loose := typ + "/seed-data.csv"
	removeEntries(t, srv, id)
	defer func() {
		req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/bundle/"+loose, nil)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close()
		}
	}()

	resp := putDoc(t, srv.URL, id, docFrom(t, map[string]any{
		"type": typ, "id": id, "title": "Revenue"}), true)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", resp.StatusCode)
	}
	req, err := http.NewRequest(http.MethodPut, srv.URL+"/api/v1/bundle/"+loose,
		bytes.NewReader([]byte("region,orders\napac,12\n")))
	if err != nil {
		t.Fatal(err)
	}
	if resp, err = http.DefaultClient.Do(req); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("writing the file = %d, want 201", resp.StatusCode)
	}

	// JSON: the third kind, beside dirs and entries.
	var listing service.BrowseResult
	getJSON(t, srv.URL+"/api/v1/bundle/"+typ+"/index.md", &listing)
	if len(listing.Files) != 1 || listing.Files[0].Name != "seed-data.csv" ||
		listing.Files[0].Path != loose || listing.Files[0].MediaType == "" {
		t.Errorf("files in the listing = %+v", listing.Files)
	}
	if len(listing.Concepts) != 1 {
		t.Errorf("concepts in the listing = %+v", listing.Concepts)
	}

	// Markdown: the same listing, in the shape the format has a place for.
	generated := getMarkdown(t, srv.URL+"/api/v1/bundle/"+typ+"/index.md")
	if !strings.Contains(generated, "## Files") || !strings.Contains(generated, "[seed-data.csv](seed-data.csv)") {
		t.Errorf("the generated index.md does not list the file:\n%s", generated)
	}

	// And the archive's copy of that same file says the same thing.
	resp, err = getArchive(t, srv.URL+"/api/v1/bundle/", "")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var archived string
	for tr := tar.NewReader(gz); ; {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Name == typ+"/index.md" {
			b, err := io.ReadAll(tr)
			if err != nil {
				t.Fatal(err)
			}
			archived = string(b)
		}
	}
	if archived != generated {
		t.Errorf("the archive's index.md and the generated one differ:\n--- archive ---\n%s\n--- generated ---\n%s",
			archived, generated)
	}
}

// The archive carries the other file OKF reserves (design doc 0046
// §3.8). An export that held the bundle but not what happened to it left
// the ledger readable only from the instance that holds it, and a purge
// is the only thing that ever removes a row from it (0031) — so the
// history was portable in the record and not in the artifact.
//
// The copy in the archive is the copy at the address: one renderer, one
// bound, so a reader extracting the bundle and a reader calling the
// endpoint are not reading two different histories.
func TestRESTIntegrationTheArchiveCarriesTheHistory(t *testing.T) {
	srv, _ := newIntegrationServer(t)

	typ := testdb.Unique(t, "restlog")
	id := typ + "/revenue"
	removeEntries(t, srv, id)
	resp := putDoc(t, srv.URL, id, docFrom(t, map[string]any{
		"type": typ, "id": id, "title": "Revenue"}), true)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", resp.StatusCode)
	}

	archived := map[string]string{}
	resp, err := getArchive(t, srv.URL+"/api/v1/bundle/", "")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	for tr := tar.NewReader(gz); ; {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		archived[hdr.Name] = string(b)
	}

	// One beside every index.md, the root's included.
	for path := range archived {
		if !strings.HasSuffix(path, "index.md") {
			continue
		}
		if _, ok := archived[strings.TrimSuffix(path, "index.md")+"log.md"]; !ok {
			t.Errorf("%s has no log.md beside it", path)
		}
	}
	// The link is relative to the directory the file sits in, as
	// index.md's are: an extracted bundle is a directory tree, and a
	// root-absolute path in it resolves to the filesystem root.
	if got := archived[typ+"/log.md"]; !strings.Contains(got, "**Create**") ||
		!strings.Contains(got, "[Revenue](revenue.md)") {
		t.Errorf("the directory's history does not record the write:\n%s", got)
	}
	// Only the title: the root's history is a bounded window over a
	// database every other test writes into, so asserting that this
	// entry's own line is in it is asserting that nobody else was busy
	// (internal/testdb). Where the links point is pinned at every depth
	// by TestLogDocumentLinksAreRelativeToTheDirectoryItSitsIn.
	if root := archived["log.md"]; !strings.Contains(root, "# Update Log") {
		t.Errorf("the root history is not titled as one:\n%s", root)
	}

	// And it is the same document the address serves.
	if served := getMarkdown(t, srv.URL+"/api/v1/bundle/"+typ+"/log.md"); served != archived[typ+"/log.md"] {
		t.Errorf("the archived history and the served one differ:\n--- archive ---\n%s\n--- served ---\n%s",
			archived[typ+"/log.md"], served)
	}
}

// The two representations of a log.md say the same thing, at every path
// (design doc 0046 §3.8), and the history of one object is at that
// object's own address (§3.5).
//
// They used to be two different things. The JSON was the ledger of the
// concept named by the directory, so `Accept: application/json` on the
// root or on any directory that is not also a concept answered 404
// while the markdown beside it listed the subtree — and a concept's own
// history lived at <id>/log.md, inside a directory named after it that
// need not exist.
func TestRESTIntegrationHistoryIsAtTheObjectAndTheDirectory(t *testing.T) {
	lockLiveAttachments(t)
	srv, s := newIntegrationServer(t)
	s.UseBlobStore(memBlobStore{})

	typ := testdb.Unique(t, "resthist")
	id := typ + "/revenue"
	file := id + "/chart.png"
	removeEntries(t, srv, id)
	defer func() {
		req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/bundle/"+file, nil)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close()
		}
	}()
	resp := putDoc(t, srv.URL, id, docFrom(t, map[string]any{
		"type": typ, "id": id, "title": "Revenue"}), true)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", resp.StatusCode)
	}
	req, err := http.NewRequest(http.MethodPut, srv.URL+"/api/v1/bundle/"+file,
		bytes.NewReader(append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 16)...)))
	if err != nil {
		t.Fatal(err)
	}
	if resp, err = http.DefaultClient.Do(req); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	type changeRow struct {
		Path   string `json:"path"`
		Change string `json:"change"`
	}
	var log struct {
		Changes []changeRow `json:"changes"`
	}

	// A directory that is not a concept: JSON, where it used to 404.
	getJSON(t, srv.URL+"/api/v1/bundle/"+typ+"/log.md", &log)
	seen := map[string]string{}
	for _, c := range log.Changes {
		seen[c.Path] = c.Change
	}
	if seen[id+".md"] != "create" || seen[file] != "create" {
		t.Errorf("the directory's history = %+v, want the concept and its file", log.Changes)
	}

	// The root, likewise — and it is the same set the markdown lists.
	log.Changes = nil
	getJSON(t, srv.URL+"/api/v1/bundle/log.md", &log)
	if len(log.Changes) == 0 {
		t.Error("the root history has no JSON representation")
	}
	rootDoc := getMarkdown(t, srv.URL+"/api/v1/bundle/log.md")
	for _, c := range log.Changes {
		if !strings.Contains(rootDoc, "(/"+c.Path+")") && !strings.Contains(rootDoc, c.Path) {
			t.Errorf("the JSON names %s and the document does not:\n%s", c.Path, rootDoc)
			break
		}
	}

	// ?history on the concept: its own revisions, carrying the document.
	var revs struct {
		Revisions []domain.Revision `json:"revisions"`
	}
	getJSON(t, srv.URL+"/api/v1/bundle/"+id+".md?history", &revs)
	if len(revs.Revisions) != 1 || revs.Revisions[0].Change != "create" ||
		!strings.Contains(revs.Revisions[0].Document, "title: Revenue") {
		t.Errorf("the concept's own history = %+v", revs.Revisions)
	}

	// ?history on the file: it has one too, and no document to carry.
	log.Changes = nil
	getJSON(t, srv.URL+"/api/v1/bundle/"+file+"?history", &log)
	if len(log.Changes) != 1 || log.Changes[0].Path != file || log.Changes[0].Change != "create" {
		t.Errorf("the file's own history = %+v", log.Changes)
	}

	// And as a document, at either kind of address.
	if doc := getMarkdown(t, srv.URL+"/api/v1/bundle/"+id+".md?history"); !strings.Contains(doc, "**Create**") {
		t.Errorf("the concept's history as a document:\n%s", doc)
	}

	// Nothing ever happened here.
	resp, err = http.Get(srv.URL + "/api/v1/bundle/" + typ + "/nothing.md?history")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("history of an object that never existed = %d, want 404", resp.StatusCode)
	}
}

// A markdown document that carries no type is a file (design doc 0046
// §3.2), and it lives at a ".md" path like a concept does. Its history is
// the same history whichever representation asks for it.
//
// It used to be two answers: ?history routed on the suffix alone, so the
// JSON went looking for a concept ledger under an id nothing held and
// answered 404 — at an address whose markdown listed the history fine.
func TestRESTIntegrationTypelessMarkdownHasOneHistory(t *testing.T) {
	lockLiveAttachments(t)
	srv, s := newIntegrationServer(t)
	s.UseBlobStore(memBlobStore{})

	typ := testdb.Unique(t, "resttypeless")
	file := typ + "/notes.md"
	req, err := http.NewRequest(http.MethodPut, srv.URL+"/api/v1/bundle/"+file,
		bytes.NewReader([]byte("# just a note\n\nno frontmatter, so no type, so not a concept\n")))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT a typeless markdown file = %d, want 201 (it is a file)", resp.StatusCode)
	}
	defer func() {
		r, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/bundle/"+file, nil)
		if resp, err := http.DefaultClient.Do(r); err == nil {
			resp.Body.Close()
		}
	}()

	if doc := getMarkdown(t, srv.URL+"/api/v1/bundle/"+file+"?history"); !strings.Contains(doc, "**Create**") {
		t.Errorf("the file's history as a document:\n%s", doc)
	}
	var log struct {
		Changes []struct {
			Path   string `json:"path"`
			Change string `json:"change"`
		} `json:"changes"`
	}
	getJSON(t, srv.URL+"/api/v1/bundle/"+file+"?history", &log)
	if len(log.Changes) != 1 || log.Changes[0].Path != file || log.Changes[0].Change != "create" {
		t.Errorf("the file's history as JSON = %+v, want the one create the document lists", log.Changes)
	}
}

// The two names OKF reserves are generated from the bundle rather than
// stored in it (design doc 0046 §§3.7-3.8), and every face that reads a
// path as something other than "the object at it" says so the same way.
//
// The archive face did not: it normalized "metrics/index.md" as a
// directory nobody lives under and handed back an empty bundle with a 200
// on it, which reads as "there is nothing under metrics" rather than as
// "that is not a directory".
func TestRESTIntegrationReservedNamesAreNotObjects(t *testing.T) {
	srv, _ := newIntegrationServer(t)

	typ := testdb.Unique(t, "restreserved")
	id := typ + "/revenue"
	removeEntries(t, srv, id)
	resp := putDoc(t, srv.URL, id, docFrom(t, map[string]any{
		"type": typ, "id": id, "title": "Revenue"}), true)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", resp.StatusCode)
	}

	for _, name := range []string{"index.md", "log.md"} {
		at := srv.URL + "/api/v1/bundle/" + typ + "/" + name
		// Read as an archive: not a subtree.
		req, err := http.NewRequest(http.MethodGet, at, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Accept", "application/gzip")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusConflict {
			t.Errorf("archive at %s = %d, want 409 (it is generated, not a directory)", name, resp.StatusCode)
		}
		// Read as an object's history: it has none of its own.
		resp, err = http.Get(at + "?history")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusConflict {
			t.Errorf("?history at %s = %d, want 409 (it is generated, not stored)", name, resp.StatusCode)
		}
		// And the file itself still reads, which is the whole point of
		// generating it.
		resp, err = http.Get(at)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", name, resp.StatusCode)
		}
	}
}

// A concept and a file are objects at their own address, not a
// directory — dots are legal inside a segment, so their address
// normalizes as a legal prefix nothing lives under. The archive face
// used to hand back an empty, valid tar.gz with a 200 on it (issue
// #364), which reads as "nothing lives under this path" rather than as
// "this is not a directory" — the same defect
// TestRESTIntegrationReservedNamesAreNotObjects covers for the two
// generated names, still open for every other object.
func TestRESTIntegrationAnObjectIsNotAnArchivableDirectory(t *testing.T) {
	lockLiveAttachments(t)
	srv, s := newIntegrationServer(t)
	s.UseBlobStore(memBlobStore{})

	typ := testdb.Unique(t, "restobjarchive")
	id := typ + "/revenue"
	resp := putDoc(t, srv.URL, id, docFrom(t, map[string]any{
		"type": typ, "id": id, "title": "Revenue"}), true)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", resp.StatusCode)
	}

	loose := typ + "/seed.csv"
	req, err := http.NewRequest(http.MethodPut, srv.URL+"/api/v1/bundle/"+loose,
		bytes.NewReader([]byte("a,b\n1,2\n")))
	if err != nil {
		t.Fatal(err)
	}
	putResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	putResp.Body.Close()
	if putResp.StatusCode != http.StatusCreated {
		t.Fatalf("writing the file = %d, want 201", putResp.StatusCode)
	}

	for _, path := range []string{id + ".md", loose} {
		resp, err := getArchive(t, srv.URL+"/api/v1/bundle/"+path, "")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusConflict {
			t.Errorf("archive at %s = %d, want 409 (it is an object, not a directory)", path, resp.StatusCode)
		}
	}

	// The directory both objects sit in is a real subtree, and archiving
	// it still works, carrying both.
	resp, err = getArchive(t, srv.URL+"/api/v1/bundle/"+typ, "")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("archive status = %d", resp.StatusCode)
	}
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for tr := tar.NewReader(gz); ; {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		found[hdr.Name] = true
	}
	if !found[id+".md"] || !found[loose] {
		t.Errorf("archive of the real directory = %v, want both %s and %s", found, id+".md", loose)
	}
}

// storedDocument reads a concept's stored bytes back, so a test can say
// what a dry run did not write.
func storedDocument(t *testing.T, base, id string) string {
	t.Helper()
	resp, err := http.Get(base + "/api/v1/bundle/" + id + ".md")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var v domain.View
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v.Document
}

// dryRunDoc is putDoc asking what the write would do (design doc 0061).
func dryRunDoc(t *testing.T, base, id string, doc []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut,
		base+"/api/v1/bundle/"+id+".md?dry_run=true", bytes.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "text/markdown")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// A dry run answers what the write would answer, and stores nothing
// (design doc 0061). The note is the case that made this necessary:
// whether a document's trust family stays a claim is decided against the
// stored concept, so a caller reading the bundle alone cannot know — and
// `import --dry-run --strict` reported a clean bundle that the import
// then noted, which is the false green this closes.
func TestRESTIntegrationDryRunAnswersWithoutWriting(t *testing.T) {
	srv, _ := newIntegrationServer(t)

	id := testdb.Unique(t, "restdry") + "/revenue"
	removeEntries(t, srv, id)

	// A foreign export form: it says who generated and confirmed it
	// somewhere else, which is a claim here.
	const written = "---\ntype: Metric\ntitle: 売上\n" +
		"generated:\n  by: human:sato@example.co.jp\n  at: 2026-07-01T00:00:00Z\n" +
		"verified:\n  - by: human:ceo@example.co.jp\n    at: 2026-07-02T00:00:00Z\n" +
		"created_by: human:sato@example.co.jp\n---\n\n受注合計。\n"

	// The plan for a free id is "created", the note is already there, and
	// the concept is not.
	resp := dryRunDoc(t, srv.URL, id, []byte(written))
	var planned domain.View
	if err := json.NewDecoder(resp.Body).Decode(&planned); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dry run = %d, want 200: nothing was created", resp.StatusCode)
	}
	if got := resp.Header.Get("Ochakai-Plan"); got != "created" {
		t.Errorf("Ochakai-Plan = %q, want created", got)
	}
	if got := resp.Header.Get("ETag"); got != "" {
		t.Errorf("ETag = %q on a dry run: nothing was written, so there is no version", got)
	}
	if notes := resp.Header.Values("Ochakai-Note"); len(notes) != 1 ||
		!strings.Contains(notes[0], "received") {
		t.Errorf("Ochakai-Note = %q, want the claim note the write would report", notes)
	}
	if !strings.Contains(planned.Document, "received:") {
		t.Errorf("the planned document does not carry the claim:\n%s", planned.Document)
	}
	getResp, err := http.Get(srv.URL + "/api/v1/bundle/" + id + ".md")
	if err != nil {
		t.Fatal(err)
	}
	getResp.Body.Close()
	if getResp.StatusCode != http.StatusNotFound {
		t.Fatalf("the dry run wrote the concept: GET = %d, want 404", getResp.StatusCode)
	}

	// Now write it for real, and the write says exactly what the plan did.
	resp = putDoc(t, srv.URL, id, []byte(written), false)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create = %d", resp.StatusCode)
	}
	if notes := resp.Header.Values("Ochakai-Note"); len(notes) != 1 {
		t.Errorf("the write reported %q; the dry run promised one note", notes)
	}

	// The same bundle again: the plan is "unchanged", and the note is
	// still there, because the claim is still not this instance's
	// observation.
	resp = dryRunDoc(t, srv.URL, id, []byte(written))
	resp.Body.Close()
	if got := resp.Header.Get("Ochakai-Plan"); got != "unchanged" {
		t.Errorf("Ochakai-Plan = %q, want unchanged: the payload says what is stored", got)
	}
	if got := resp.Header.Get("Ochakai-Unchanged"); got != "" {
		t.Errorf("Ochakai-Unchanged = %q on a dry run: Ochakai-Plan is the one answer", got)
	}
	if notes := resp.Header.Values("Ochakai-Note"); len(notes) != 1 {
		t.Errorf("Ochakai-Note = %q, want the claim note again", notes)
	}

	// An edit plans as an update, and still writes nothing.
	edited := strings.Replace(written, "受注合計。", "受注の合計。", 1)
	resp = dryRunDoc(t, srv.URL, id, []byte(edited))
	resp.Body.Close()
	if got := resp.Header.Get("Ochakai-Plan"); got != "updated" {
		t.Errorf("Ochakai-Plan = %q, want updated", got)
	}
	stored := storedDocument(t, srv.URL, id)
	if strings.Contains(stored, "受注の合計。") {
		t.Errorf("the dry run wrote the edit:\n%s", stored)
	}

	// This instance's own export form, handed back, is not a claim — and
	// the dry run says so without writing, which is what makes
	// `--dry-run --strict` usable on an export → review → import loop.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/bundle/"+id+".md", nil)
	req.Header.Set("Accept", "text/markdown")
	mdResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	served, _ := io.ReadAll(mdResp.Body)
	mdResp.Body.Close()
	resp = dryRunDoc(t, srv.URL, id, served)
	resp.Body.Close()
	if got := resp.Header.Get("Ochakai-Plan"); got != "unchanged" {
		t.Errorf("the export form plans as %q, want unchanged", got)
	}

	// A delete is not something with a plan beside it, and the parameter
	// must not be ignored there: a caller who believed they were asking
	// would lose the object.
	req, _ = http.NewRequest(http.MethodDelete,
		srv.URL+"/api/v1/bundle/"+id+".md?dry_run=true", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("DELETE ?dry_run=true = %d, want 400", resp.StatusCode)
	}
	if storedDocument(t, srv.URL, id) == "" {
		t.Error("the refused dry-run delete removed the concept anyway")
	}
}

// A concept the write path refuses is refused by the plan too, which is
// the other half of what `--strict` fails on and the other half the dry
// run could not see.
func TestRESTIntegrationDryRunRefusesWhatTheWriteRefuses(t *testing.T) {
	srv, _ := newIntegrationServer(t)

	id := testdb.Unique(t, "restdrybad") + "/broken"
	removeEntries(t, srv, id)

	// An Attested Computation without a runtime is what the write path
	// rejects (design doc 0036 §3.4).
	const bad = "---\ntype: Attested Computation\ntitle: Broken\n---\n\nbody\n"
	dry := dryRunDoc(t, srv.URL, id, []byte(bad))
	dry.Body.Close()
	real := putDoc(t, srv.URL, id, []byte(bad), false)
	real.Body.Close()
	if dry.StatusCode != real.StatusCode {
		t.Errorf("dry run = %d, write = %d: the plan must meet the same refusal",
			dry.StatusCode, real.StatusCode)
	}
	if dry.StatusCode != http.StatusBadRequest {
		t.Errorf("dry run = %d, want 400", dry.StatusCode)
	}
}

// TestRESTIntegrationDeleteIfMatch pins design doc 0064: DELETE now takes
// the same If-Match precondition PUT already did (design doc 0030), so a
// delete racing an edit it never saw fails with a conflict instead of
// silently deleting a version the caller did not read.
func TestRESTIntegrationDeleteIfMatch(t *testing.T) {
	srv, _ := newIntegrationServer(t)

	typ := testdb.Unique(t, "restdelifm")
	id := typ + "/entry"
	resp := putDoc(t, srv.URL, id, docFrom(t, map[string]any{
		"type": typ, "id": id, "title": "Original"}), true)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", resp.StatusCode)
	}

	at := srv.URL + "/api/v1/bundle/" + id + ".md"

	// A stale If-Match is a 412, and the concept survives it.
	req, _ := http.NewRequest(http.MethodDelete, at, nil)
	req.Header.Set("If-Match", `"deadbeef"`)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("delete with stale If-Match = %d, want 412", resp.StatusCode)
	}
	getResp, err := http.Get(at)
	if err != nil {
		t.Fatal(err)
	}
	getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Errorf("concept after a refused delete = %d, want still there (200)", getResp.StatusCode)
	}
	etag := getResp.Header.Get("ETag")

	// The current ETag lands the delete.
	req, _ = http.NewRequest(http.MethodDelete, at, nil)
	req.Header.Set("If-Match", etag)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete with the current If-Match = %d, want 204", resp.StatusCode)
	}
	getResp, err = http.Get(at)
	if err != nil {
		t.Fatal(err)
	}
	getResp.Body.Close()
	if getResp.StatusCode != http.StatusNotFound {
		t.Errorf("concept after the delete = %d, want gone (404)", getResp.StatusCode)
	}
}
