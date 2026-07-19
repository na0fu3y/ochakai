package restapi

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
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
// A run-unique top-level id segment (doubling as a custom type) keeps
// reruns and other tests' rows out of the browse assertions.
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
	id := typ + "/sales/orders"
	entry := map[string]any{"type": typ, "id": id, "title": "REST round trip"}
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

	// Browse: under the run-unique top segment the "sales" directory
	// shows, and the prefix level shows the entry (with its type as
	// metadata in the projection).
	var root service.BrowseResult
	getJSON(t, srv.URL+"/api/v1/browse?prefix="+typ, &root)
	if len(root.Dirs) != 1 || root.Dirs[0].Name != "sales" || root.Dirs[0].Count != 1 {
		t.Errorf("browse root = %+v", root)
	}
	var level service.BrowseResult
	getJSON(t, srv.URL+"/api/v1/browse?prefix="+typ+"/sales", &level)
	if len(level.Entries) != 1 || level.Entries[0].ID != id || string(level.Entries[0].Type) != typ {
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
