package restapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/config"
	"github.com/na0fu3y/ochakai/internal/service"
)

// A read-only deployment refuses writes before anything reaches the
// store, so these need no database: a Service with a nil Store is enough,
// and a handler that got past the guard would panic rather than quietly
// pass (design doc 0040 §2.2).
//
// The server is checkedServer, not a plain one: a 403 nowhere declared in
// api/openapi.yaml is a refusal the one REST contract does not mention
// (design doc 0015 §2), and going through the spec is what keeps the two
// from drifting apart again.
func readOnlyServer(t *testing.T) *httptest.Server {
	t.Helper()
	return checkedServer(t, Handler(&service.Service{Config: &config.Config{ReadOnly: true}}))
}

// Every write verb is refused with 403 — not 404, because the route
// exists and the request was understood; this deployment declines
// (design doc 0040 §4).
func TestReadOnlyRefusesWritesWith403(t *testing.T) {
	srv := readOnlyServer(t)
	const doc = "---\ntype: Metric\ntitle: m\n---\n"
	for _, w := range []struct{ method, path, body, mediaType string }{
		{http.MethodPut, "/api/v1/knowledge/m", doc, "text/markdown"},
		{http.MethodDelete, "/api/v1/knowledge/m", "", ""},
		{http.MethodPost, "/api/v1/verify/m", "", ""},
		{http.MethodPost, "/api/v1/reject/m", `{"note":"no"}`, "application/json"},
		{http.MethodDelete, "/api/v1/reject/m", "", ""},
		{http.MethodPost, "/api/v1/usage/m", `{"outcome":"worked"}`, "application/json"},
		{http.MethodPost, "/api/v1/move", `{"from":"m","to":"n"}`, "application/json"},
		{http.MethodPost, "/api/v1/reembed", "", ""},
		{http.MethodPut, "/api/v1/attachments/m/f.txt", "x", "application/json"},
		{http.MethodDelete, "/api/v1/attachments/m/f.txt", "", ""},
	} {
		req, err := http.NewRequest(w.method, srv.URL+w.path, strings.NewReader(w.body))
		if err != nil {
			t.Fatal(err)
		}
		if w.mediaType != "" {
			req.Header.Set("Content-Type", w.mediaType)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", w.method, w.path, err)
		}
		body := make([]byte, 512)
		n, _ := resp.Body.Read(body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s = %d, want 403 (body: %s)", w.method, w.path, resp.StatusCode, body[:n])
		}
		if !strings.Contains(string(body[:n]), "read-only") {
			t.Errorf("%s %s said %q, which does not explain that the deployment is read-only",
				w.method, w.path, body[:n])
		}
	}
}

// REST has no capability handshake, so a read-only deployment says so on
// every response and clients — the Web UI above all — read it from there
// (design doc 0040 §2.3).
func TestReadOnlyAnnouncesItselfOnEveryResponse(t *testing.T) {
	srv := readOnlyServer(t)
	req, err := http.NewRequest(http.MethodPut, srv.URL+"/api/v1/knowledge/m",
		strings.NewReader("---\ntype: Metric\ntitle: m\n---\n"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "text/markdown")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := resp.Header.Get("Ochakai-Read-Only"); got != "true" {
		t.Errorf("PUT /api/v1/knowledge/m: Ochakai-Read-Only = %q, want \"true\"", got)
	}

	// A route that does not exist carries it too: the header describes the
	// deployment, not the outcome of one call. This one stays on a plain
	// server — a path api/openapi.yaml never declares is nothing the spec
	// can be asked about, and running it through checkedServer would only
	// report the test's own 404 as undocumented traffic.
	plain := httptest.NewServer(Handler(&service.Service{Config: &config.Config{ReadOnly: true}}))
	defer plain.Close()
	resp, err = http.Post(plain.URL+"/api/v1/nope", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := resp.Header.Get("Ochakai-Read-Only"); got != "true" {
		t.Errorf("POST /api/v1/nope: Ochakai-Read-Only = %q, want \"true\"", got)
	}
}

// A writable deployment must not set the header: absent means writable,
// and that is what every server predating this release already says.
func TestWritableServerSetsNoHeader(t *testing.T) {
	srv := httptest.NewServer(Handler(&service.Service{Config: &config.Config{}}))
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/api/v1/nope", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Ochakai-Read-Only"); got != "" {
		t.Errorf("writable server set Ochakai-Read-Only = %q, want it absent", got)
	}
}
