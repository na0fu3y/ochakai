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
	"os"
	"testing"
	"time"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/service"
	"github.com/na0fu3y/ochakai/internal/store"
)

// TestRESTIntegration walks one entry through the REST surface against a
// real PostgreSQL (skipped unless OCHAKAI_TEST_DATABASE_URL is set; see
// the store integration test for the docker one-liner): create, read,
// no-op update (Ochakai-Unchanged), browse, revisions, export, delete.
// A run-unique custom type keeps reruns and other tests' rows out of the
// browse assertions.
func TestRESTIntegration(t *testing.T) {
	dbURL := os.Getenv("OCHAKAI_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("OCHAKAI_TEST_DATABASE_URL not set")
	}
	ctx := t.Context()
	s, err := store.New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	svc := &service.Service{Store: s, Log: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	srv := httptest.NewServer(Handler(svc))
	defer srv.Close()

	typ := fmt.Sprintf("restit%d", time.Now().UnixNano())
	entry := map[string]any{"type": typ, "id": "sales/orders", "title": "REST round trip"}
	payload, _ := json.Marshal(entry)

	// Create.
	resp, err := http.Post(srv.URL+"/api/v1/knowledge", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Read it back.
	var got domain.Knowledge
	getJSON(t, srv.URL+"/api/v1/knowledge/"+typ+"/sales/orders", &got)
	if got.Title != "REST round trip" || got.Status != domain.StatusDraft {
		t.Errorf("entry = %+v", got)
	}

	// A content-identical PUT writes nothing and says so in the header.
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/v1/knowledge/"+typ+"/sales/orders", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Ochakai-Unchanged") != "true" {
		t.Errorf("no-op PUT: status = %d, Ochakai-Unchanged = %q",
			resp.StatusCode, resp.Header.Get("Ochakai-Unchanged"))
	}

	// Browse: the type root shows the "sales" directory, the prefix level
	// shows the entry.
	var root service.BrowseResult
	getJSON(t, srv.URL+"/api/v1/browse?type="+typ, &root)
	if len(root.Dirs) != 1 || root.Dirs[0].Name != "sales" || root.Dirs[0].Count != 1 {
		t.Errorf("browse root = %+v", root)
	}
	var level service.BrowseResult
	getJSON(t, srv.URL+"/api/v1/browse?type="+typ+"&prefix=sales", &level)
	if len(level.Entries) != 1 || level.Entries[0].ID != "sales/orders" {
		t.Errorf("browse level = %+v", level)
	}

	// Revisions: exactly the create, with a snapshot.
	var revs struct {
		Revisions []domain.Revision `json:"revisions"`
	}
	getJSON(t, srv.URL+"/api/v1/revisions/"+typ+"/sales/orders", &revs)
	if len(revs.Revisions) != 1 || revs.Revisions[0].Change != "create" ||
		revs.Revisions[0].Snapshot.Title != "REST round trip" {
		t.Errorf("revisions = %+v", revs.Revisions)
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
	dbURL := os.Getenv("OCHAKAI_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("OCHAKAI_TEST_DATABASE_URL not set")
	}
	ctx := t.Context()
	s, err := store.New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	s.UseBlobStore(memBlobStore{})
	svc := &service.Service{Store: s, Log: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	srv := httptest.NewServer(Handler(svc))
	defer srv.Close()

	typ := fmt.Sprintf("restatt%d", time.Now().UnixNano())
	payload, _ := json.Marshal(map[string]any{"type": typ, "id": "reading", "title": "attachment hits"})
	resp, err := http.Post(srv.URL+"/api/v1/knowledge", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", resp.StatusCode)
	}

	png := append([]byte("\x89PNG\r\n\x1a\n"), []byte("hit thumbnail bytes")...)
	attURL := srv.URL + "/api/v1/attachments/" + typ + "/reading/weekly.png"
	req, _ := http.NewRequest(http.MethodPut, attURL, bytes.NewReader(png))
	resp, err = http.DefaultClient.Do(req)
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

func getJSON(t *testing.T, url string, v any) {
	t.Helper()
	resp, err := http.Get(url)
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
