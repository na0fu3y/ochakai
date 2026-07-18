package main

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type staticServiceTokens struct {
	tok string
	err error
}

func (s staticServiceTokens) token() (string, error) { return s.tok, s.err }

func TestServeUIHandlerServesIndexAndHealth(t *testing.T) {
	h := serveUIHandler(nil)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "<title>ochakai</title>") {
		t.Error("index.html not served at /")
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Errorf("GET /health = %d %q, want 200 ok", rec.Code, rec.Body.String())
	}
}

// Without OCHAKAI_URL there is no upstream: the API paths must 404
// instead of dialing anywhere.
func TestServeUIHandlerNoProxyIs404(t *testing.T) {
	h := serveUIHandler(nil)
	for _, path := range []string{"/api/v1/knowledge", "/mcp"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, rec.Code)
		}
	}
}

// The proxy authenticates as the service: X-Serverless-Authorization
// carries its ID token (the header Cloud Run validates in preference to
// Authorization), the Host header is rewritten to the target, and any
// browser-sent Authorization passes through untouched.
func TestServeUIProxySignsAsService(t *testing.T) {
	var got http.Header
	var gotHost string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		gotHost = r.Host
		_, _ = w.Write([]byte(`{"hits":[]}`))
	}))
	defer backend.Close()
	u, _ := url.Parse(backend.URL)

	h := serveUIHandler(newServiceProxy(u, staticServiceTokens{tok: "sa-id-token"}, slog.New(slog.DiscardHandler)))
	local := httptest.NewServer(h)
	defer local.Close()

	req, _ := http.NewRequest(http.MethodGet, local.URL+"/api/v1/knowledge?q=x", nil)
	req.Header.Set("Authorization", "Bearer browser-supplied")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("proxied GET = %d, want 200", resp.StatusCode)
	}
	if auth := got.Get("X-Serverless-Authorization"); auth != "Bearer sa-id-token" {
		t.Errorf("X-Serverless-Authorization = %q, want the service's token", auth)
	}
	if auth := got.Get("Authorization"); auth != "Bearer browser-supplied" {
		t.Errorf("Authorization = %q, want pass-through", auth)
	}
	if gotHost != u.Host {
		t.Errorf("upstream Host = %q, want %q", gotHost, u.Host)
	}
}

// No metadata server (local runs against an unrestricted ochakai): the
// request still goes through, just without the identity header.
func TestServeUIProxyForwardsWithoutToken(t *testing.T) {
	var got http.Header
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
	}))
	defer backend.Close()
	u, _ := url.Parse(backend.URL)

	h := serveUIHandler(newServiceProxy(u, staticServiceTokens{err: errors.New("no metadata server")}, slog.New(slog.DiscardHandler)))
	local := httptest.NewServer(h)
	defer local.Close()

	resp, err := http.Get(local.URL + "/mcp")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("proxied GET = %d, want 200", resp.StatusCode)
	}
	if auth := got.Get("X-Serverless-Authorization"); auth != "" {
		t.Errorf("X-Serverless-Authorization = %q, want none", auth)
	}
}
