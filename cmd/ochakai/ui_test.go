package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

func TestUIHandlerServesIndex(t *testing.T) {
	h, err := uiHandler("http://ochakai.internal", nil)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8098/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "<title>ochakai</title>") {
		t.Error("index.html not served at /")
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
}

// The proxy substitutes the CLI user's ID token for whatever the browser
// sent — never forward browser credentials upstream.
func TestUIHandlerProxiesWithToken(t *testing.T) {
	var got http.Header
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		_, _ = w.Write([]byte(`{"hits":[]}`))
	}))
	defer backend.Close()

	tokens := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "id-token"})
	h, err := uiHandler(backend.URL, tokens)
	if err != nil {
		t.Fatal(err)
	}
	local := httptest.NewServer(h)
	defer local.Close()

	req, _ := http.NewRequest(http.MethodGet, local.URL+"/api/v1/knowledge?q=x", nil)
	req.Header.Set("Authorization", "Bearer browser-supplied")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("proxied GET = %d, want 200", resp.StatusCode)
	}
	if auth := got.Get("Authorization"); auth != "Bearer id-token" {
		t.Errorf("upstream Authorization = %q, want the CLI user's token", auth)
	}
}

func TestUIHandlerNilTokensSendsNoCredentials(t *testing.T) {
	var got http.Header
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
	}))
	defer backend.Close()

	h, err := uiHandler(backend.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	local := httptest.NewServer(h)
	defer local.Close()

	req, _ := http.NewRequest(http.MethodGet, local.URL+"/api/v1/knowledge", nil)
	req.Header.Set("Authorization", "Bearer browser-supplied")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if auth := got.Get("Authorization"); auth != "" {
		t.Errorf("upstream Authorization = %q, want none for plain-http servers", auth)
	}
}

// DNS rebinding guard: a page at attacker.example resolving to 127.0.0.1
// reaches the listener, but its Host header gives it away.
func TestUIHandlerRejectsForeignHost(t *testing.T) {
	h, err := uiHandler("http://ochakai.internal", nil)
	if err != nil {
		t.Fatal(err)
	}
	for host, want := range map[string]int{
		"localhost:8098":        http.StatusOK,
		"127.0.0.1:8098":        http.StatusOK,
		"[::1]:8098":            http.StatusOK,
		"attacker.example:8098": http.StatusForbidden,
		"attacker.example":      http.StatusForbidden,
	} {
		req := httptest.NewRequest(http.MethodGet, "http://placeholder/", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("Host %q: GET / = %d, want %d", host, rec.Code, want)
		}
	}
}

// CSRF guard: a cross-site page reaching 127.0.0.1 directly carries the
// loopback Host (passing the rebinding guard) but a foreign Origin. The
// UI's own requests send no Origin or a loopback one and must reach the
// backend. A foreign Origin must be refused before the proxy signs and
// forwards the write.
func TestUIHandlerRejectsForeignOrigin(t *testing.T) {
	var reached bool
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
	}))
	defer backend.Close()
	h, err := uiHandler(backend.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	for origin, want := range map[string]int{
		"":                          http.StatusOK, // same-origin GET, or a non-browser client
		"http://127.0.0.1:8098":     http.StatusOK,
		"http://localhost:8098":     http.StatusOK,
		"https://evil.example":      http.StatusForbidden,
		"http://127.0.0.1.evil.com": http.StatusForbidden,
		"null":                      http.StatusForbidden, // opaque origin (sandboxed iframe, data: page)
	} {
		reached = false
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8098/api/v1/knowledge", nil)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("Origin %q: POST = %d, want %d", origin, rec.Code, want)
		}
		if reachedBackend := reached; reachedBackend != (want == http.StatusOK) {
			t.Errorf("Origin %q: reached backend = %v, want %v", origin, reachedBackend, want == http.StatusOK)
		}
	}
}
