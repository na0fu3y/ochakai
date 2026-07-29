package restapi

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
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

// newIntegrationServer serves a real PostgreSQL through the REST handler,
// skipping the test without OCHAKAI_TEST_DATABASE_URL. The store comes
// back with it for the tests that swap in a blob fake.
//
// Like checkedServer's close, the pool's is registered rather than
// deferred, and registered here so that it is the last cleanup to run:
// the rows a test wrote are removed over HTTP (removeEntries), and a
// deferred Close would take the pool out from under those requests —
// leaving the rows behind in a database the next run reuses (issue #278).
func newIntegrationServer(t *testing.T) (*httptest.Server, *store.Store) {
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
	svc := &service.Service{Store: s, Log: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	return checkedServer(t, Handler(svc)), s
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
				req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/knowledge/"+id+q, nil)
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

	typ := fmt.Sprintf("restit%d", time.Now().UnixNano())
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
	getJSON(t, srv.URL+"/api/v1/knowledge/"+typ+"/sales/orders", &got)
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
	if len(level.Entries) != 1 || level.Entries[0].ID != id || string(level.Entries[0].Type) != typ {
		t.Errorf("index.md level = %+v", level)
	}
	// And the same address without the Accept header is the file itself.
	doc := getText(t, srv.URL+"/api/v1/bundle/"+typ+"/sales/index.md")
	if !strings.Contains(doc, "# "+typ+"/sales") || !strings.Contains(doc, "(orders.md)") {
		t.Errorf("generated index.md:\n%s", doc)
	}

	// log.md: exactly the create, in both representations.
	var revs struct {
		Revisions []domain.Revision `json:"revisions"`
	}
	getJSON(t, srv.URL+"/api/v1/bundle/"+typ+"/sales/orders/log.md", &revs)
	if len(revs.Revisions) != 1 || revs.Revisions[0].Change != "create" ||
		!strings.Contains(revs.Revisions[0].Document, "title: REST round trip") {
		t.Errorf("revisions = %+v", revs.Revisions)
	}
	logDoc := getText(t, srv.URL+"/api/v1/bundle/"+typ+"/log.md")
	if !strings.Contains(logDoc, "**Create**") || !strings.Contains(logDoc, "/"+id+".md") {
		t.Errorf("generated log.md:\n%s", logDoc)
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
	resp, err = http.Get(srv.URL + "/api/v1/export")
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

	// ?attachments=false skips the bytes: a CI backup can take the
	// entries from here and the files straight from GCS, which is both
	// cheaper and the only sane path once attachments outweigh the text.
	resp, err = http.Get(srv.URL + "/api/v1/export?attachments=false")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export without attachments: status = %d", resp.StatusCode)
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
			t.Errorf("attachments=false still carried %s", hdr.Name)
		}
		if hdr.Name == typ+"/sales/orders.md" {
			found = true
		}
	}
	if !found {
		t.Errorf("attachments=false dropped the entries too")
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
	resp, err = http.Get(srv.URL + "/api/v1/export?attachments=false")
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
	req, _ = http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/knowledge/"+typ+"/sales/orders", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("delete status = %d", resp.StatusCode)
	}
	resp, err = http.Get(srv.URL + "/api/v1/knowledge/" + typ + "/sales/orders")
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

	typ := fmt.Sprintf("restatt%d", time.Now().UnixNano())
	payload := docFrom(t, map[string]any{"type": typ, "id": typ + "/reading", "title": "attachment hits"})
	resp := putDoc(t, srv.URL, typ+"/reading", payload, true)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", resp.StatusCode)
	}

	png := append([]byte("\x89PNG\r\n\x1a\n"), []byte("hit thumbnail bytes")...)
	attURL := srv.URL + "/api/v1/attachments/" + typ + "/reading/weekly.png"
	req, _ := http.NewRequest(http.MethodPut, attURL, bytes.NewReader(png))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("attach status = %d", resp.StatusCode)
	}

	// The listing carries the attachment metadata.
	var hits struct {
		Hits []domain.SearchHit `json:"hits"`
	}
	getJSON(t, srv.URL+"/api/v1/knowledge?sort=verified_at&type="+typ, &hits)
	if len(hits.Hits) != 1 || len(hits.Hits[0].Attachments) != 1 ||
		hits.Hits[0].Attachments[0].Name != "weekly.png" ||
		hits.Hits[0].Attachments[0].MediaType != "image/png" {
		t.Fatalf("hits should carry attachment metadata: %+v", hits.Hits)
	}
	sum := hits.Hits[0].Attachments[0].SHA256

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
	req, _ = http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/knowledge/"+typ+"/reading", nil)
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
func putDoc(t *testing.T, base, id string, doc []byte, onlyIfAbsent bool) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, base+"/api/v1/knowledge/"+id, bytes.NewReader(doc))
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

// POST /api/v1/verify/{id} records a verification against the entry as it
// stands. The second call is the point: re-affirming an entry that is
// already verified is what empties the review feeds, and PUT cannot do it
// (design doc 0025 §6).
func TestRESTIntegrationVerify(t *testing.T) {
	srv, _ := newIntegrationServer(t)

	typ := fmt.Sprintf("restit%d", time.Now().UnixNano())
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
		resp, err := http.Post(srv.URL+"/api/v1/verify/"+id, "", nil)
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

	resp, err := http.Post(srv.URL+"/api/v1/verify/"+typ+"/no-such-entry", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("verify of a missing entry = %d, want 404", resp.StatusCode)
	}

	// Leave nothing live behind: the test database is shared (CONTRIBUTING).
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/knowledge/"+id, nil)
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

	typ := fmt.Sprintf("ctxbudget%d", time.Now().UnixNano())
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
		Entries   []domain.View           `json:"entries"`
		Outline   []domain.ContextOutline `json:"outline"`
		Truncated int                     `json:"truncated"`
	}
	if err := json.Unmarshal(wire, &got); err != nil {
		t.Fatal(err)
	}
	if got.Truncated == 0 {
		t.Fatalf("budget %d over 4 entries of %d bytes truncated nothing", budget, len(body))
	}
	if len(got.Hits) == 0 {
		t.Error("the ranking is gone; hits still have to say what matched")
	}
	if bytes.Count(wire, []byte(body)) > len(got.Entries) {
		t.Errorf("response carries %d copies of the body for %d delivered entries",
			bytes.Count(wire, []byte(body)), len(got.Entries))
	}
	ranking, err := json.Marshal(got.Hits)
	if err != nil {
		t.Fatal(err)
	}
	if over := len(wire) - len(ranking); over > budget {
		t.Errorf("response carries %d bytes of knowledge over a %d budget", over, budget)
	}
}

// TestRESTIntegrationUsageBacklinksAndMove covers the four endpoints the
// REST integration tests had never called — report_outcome, usage,
// backlinks, and move — which also left them outside the contract check
// (design doc 0035 §3.2). The move is the assertion worth having: design
// doc 0021 promises a rename carries everything keyed by the id and
// rewrites inbound references, so the insight that linked to the metric
// must still link to it afterwards, at its new address.
func TestRESTIntegrationUsageBacklinksAndMove(t *testing.T) {
	srv, _ := newIntegrationServer(t)

	root := fmt.Sprintf("restumb%d", time.Now().UnixNano())
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

	// Backlinks: the insight links to the metric, not the other way round.
	var backlinks struct {
		Entries []domain.Summary `json:"entries"`
	}
	getJSON(t, srv.URL+"/api/v1/backlinks/"+metric, &backlinks)
	if len(backlinks.Entries) != 1 || backlinks.Entries[0].ID != insight {
		t.Fatalf("backlinks of %s = %+v, want just %s", metric, backlinks.Entries, insight)
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
	backlinks.Entries = nil
	getJSON(t, srv.URL+"/api/v1/backlinks/"+moved, &backlinks)
	if len(backlinks.Entries) != 1 || backlinks.Entries[0].ID != insight {
		t.Errorf("backlinks after the move = %+v, want the insight to have followed", backlinks.Entries)
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
	root := fmt.Sprintf("scope%d", time.Now().UnixNano())
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
		resp, err := http.Get(srv.URL + "/api/v1/knowledge?" + query)
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
	resp, err := http.Get(srv.URL + "/api/v1/knowledge?q=" + root + "&prefix=" + url.QueryEscape("a//b"))
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
		Hits    []domain.ContextRank `json:"hits"`
		Entries []domain.View        `json:"entries"`
	}
	if err := json.NewDecoder(cresp.Body).Decode(&pack); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, e := range pack.Entries {
		got[e.ID] = true
	}
	if !got[term] {
		t.Errorf("entries = %v; the cited glossary term must travel with the scoped entry", got)
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

	id := fmt.Sprintf("restdoc%d/revenue", time.Now().UnixNano())
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

	// The same write again is a 409: create-only means create.
	resp = putDoc(t, srv.URL, id, []byte(doc), true)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("create-only over a live entry = %d, want 409", resp.StatusCode)
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
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/knowledge/"+id, nil)
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
	req, _ = http.NewRequest(http.MethodPut, srv.URL+"/api/v1/knowledge/"+id, strings.NewReader(doc))
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

	id := fmt.Sprintf("restbytes%d/revenue", time.Now().UnixNano())
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
	getJSON(t, srv.URL+"/api/v1/knowledge/"+id, &read)
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
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/knowledge/"+id, nil)
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

	id := fmt.Sprintf("restclaim%d/revenue", time.Now().UnixNano())
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
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/knowledge/"+id, nil)
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

	typ := fmt.Sprintf("restany%d", time.Now().UnixNano())
	id := typ + "/diagram"
	// Soft-delete at the end, like every other test that holds live
	// files: the shared test database is scanned whole by the export and
	// by the re-embed backlog, and those resolve bytes against their own
	// blob fake. A defer rather than t.Cleanup — cleanups run after this
	// function's defers, by which time the server is closed.
	defer func() {
		req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/knowledge/"+id, nil)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close()
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
		attURL := srv.URL + "/api/v1/attachments/" + id + "/" + tc.name
		req, _ := http.NewRequest(http.MethodPut, attURL, bytes.NewReader(tc.data))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("attaching %s = %d: %s", tc.name, resp.StatusCode, body)
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
	getJSON(t, srv.URL+"/api/v1/knowledge/"+id, &view)
	if len(view.Attachments) != 4 {
		t.Errorf("the entry carries %d files, want 4 — nothing caps how many", len(view.Attachments))
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

	typ := fmt.Sprintf("restbundle%d", time.Now().UnixNano())
	id := typ + "/revenue"
	bundle := srv.URL + "/api/v1/bundle/"
	defer func() {
		req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/knowledge/"+id, nil)
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

	// A concept, written at the path it lives at. It is the entry
	// surface's write: same status, same ETag, same View.
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
	// The same bytes again write nothing, exactly as on the entry surface.
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
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("writing a file = %d", resp.StatusCode)
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
	getJSON(t, srv.URL+"/api/v1/knowledge/"+id, &read)
	if len(read.Attachments) != 1 || read.Attachments[0].Name != "chart.png" {
		t.Errorf("the entry does not carry the file its body shows: %+v", read.Attachments)
	}

	// A markdown document with no type is a file, not a bad concept.
	notes := []byte("# 会議メモ\n\ntype なし。\n")
	resp = put(t, typ+"/notes.md", notes)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("writing a typeless markdown file = %d", resp.StatusCode)
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
	// It is not an entry: nothing lists it, and the concept surface does
	// not know it.
	resp, err = http.Get(srv.URL + "/api/v1/knowledge/" + typ + "/notes")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("a typeless markdown file answers the entry surface: %d", resp.StatusCode)
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
	root := fmt.Sprintf("page%d", time.Now().UnixNano())
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
		resp, err := http.Get(srv.URL + "/api/v1/knowledge?" + query)
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
	resp, err := http.Get(srv.URL + "/api/v1/knowledge?q=" + root + "&cursor=" + url.QueryEscape(cursorOf(t, page, feed)))
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

// GET /api/v1/queues answers "is there anything to do" with the sizes of
// the three feeds a curator empties (design doc 0049), and the answer
// moves as the work is done: a failure report puts an entry in, the
// verification that answers it takes the entry out. The prefix scope is
// what keeps this test's numbers its own, and it is also the only filter
// the endpoint takes.
func TestRESTQueuesCountsTheReviewQueues(t *testing.T) {
	srv, _ := newIntegrationServer(t)

	root := fmt.Sprintf("queuesit%d", time.Now().UnixNano())
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
		getJSON(t, srv.URL+"/api/v1/queues?prefix="+root, &out)
		return out.Queues
	}
	if got, want := counts(), (domain.QueueCounts{Drafts: 1, PastExpiry: 1}); got != want {
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
	if got, want := counts(), (domain.QueueCounts{Drafts: 1, ReportedWrong: 1, PastExpiry: 1}); got != want {
		t.Fatalf("queues after a failure report = %+v, want %+v", got, want)
	}

	// Verifying is the answer to that report, and the queue must shorten
	// — a queue nobody can empty stops being read (design doc 0025 §6.1).
	resp, err = http.Post(srv.URL+"/api/v1/verify/"+broken, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got, want := counts(), (domain.QueueCounts{Drafts: 1, PastExpiry: 1}); got != want {
		t.Errorf("queues after the re-verification = %+v, want %+v", got, want)
	}

	// Out of scope is out of the count: another prefix sees none of this.
	var elsewhere struct {
		Queues domain.QueueCounts `json:"queues"`
	}
	getJSON(t, srv.URL+"/api/v1/queues?prefix="+root+"x", &elsewhere)
	if elsewhere.Queues != (domain.QueueCounts{}) {
		t.Errorf("a neighbouring prefix counted %+v, want all zero", elsewhere.Queues)
	}
}
