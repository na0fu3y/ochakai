package main

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/httpauth"
)

type staticServiceTokens struct {
	tok string
	err error
}

func (s staticServiceTokens) token() (string, error) { return s.tok, s.err }

func TestServeUIHandlerServesIndexAndHealth(t *testing.T) {
	h := serveUIHandler(http.NotFoundHandler())

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

// The page always talks to its own origin, so an upstream is not
// optional: without OCHAKAI_URL serve-ui must refuse to start rather
// than serve a UI whose every API call would 404.
func TestServeUIRequiresOchakaiURL(t *testing.T) {
	t.Setenv("OCHAKAI_URL", "")
	err := serveUI(slog.New(slog.DiscardHandler))
	if err == nil || !strings.Contains(err.Error(), "OCHAKAI_URL is required") {
		t.Errorf("serveUI without OCHAKAI_URL = %v, want an OCHAKAI_URL-required error", err)
	}
}

// The proxy authenticates as the service: X-Serverless-Authorization
// carries its ID token (the header Cloud Run validates in preference to
// Authorization), the Host header is rewritten to the target, and the
// browser's own credential headers are dropped -- this proxy speaks as
// the service, never as whoever is holding the page.
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

	h := serveUIHandler(newServiceProxy(u, staticServiceTokens{tok: "sa-id-token"}, nil, slog.New(slog.DiscardHandler)))
	local := httptest.NewServer(h)
	defer local.Close()

	req, _ := http.NewRequest(http.MethodGet, local.URL+"/api/v1/search?q=x", nil)
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
	if auth := got.Get("Authorization"); auth != "" {
		t.Errorf("Authorization = %q, want the browser's header dropped", auth)
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

	h := serveUIHandler(newServiceProxy(u, staticServiceTokens{err: errors.New("no metadata server")}, nil, slog.New(slog.DiscardHandler)))
	local := httptest.NewServer(h)
	defer local.Close()

	req, _ := http.NewRequest(http.MethodGet, local.URL+"/mcp", nil)
	// A browser that sends credentials of its own, at the one moment the
	// proxy has none: httpauth falls back to Authorization when
	// X-Serverless-Authorization is absent, so a forwarded header here
	// would name the actor the page chose.
	req.Header.Set("Authorization", "Bearer browser-supplied")
	req.Header.Set("X-Serverless-Authorization", "Bearer browser-forged")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("proxied GET = %d, want 200", resp.StatusCode)
	}
	for _, h := range []string{"X-Serverless-Authorization", "Authorization"} {
		if auth := got.Get(h); auth != "" {
			t.Errorf("%s = %q; with no service token the request must carry none", h, auth)
		}
	}
}

// fakeIAP stands in for Google's key endpoint.
type fakeIAP struct {
	email string
	err   error
}

func (f fakeIAP) identity(context.Context, string) (string, error) { return f.email, f.err }

// Without IAP configured the proxy still acts as the service — but the
// browser's delegation header never reaches ochakai. Anyone who can open
// the page could otherwise name any author they liked, and the backend
// would believe it as soon as the operator added this service to
// OCHAKAI_DELEGATING_CALLERS (design docs 0027, 0032).
func TestServeUIProxyStripsBrowserDelegation(t *testing.T) {
	var got http.Header
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		_, _ = w.Write([]byte(`{}`))
	}))
	defer backend.Close()
	u, _ := url.Parse(backend.URL)

	h := serveUIHandler(newServiceProxy(u, staticServiceTokens{tok: "t"}, nil, slog.New(slog.DiscardHandler)))
	local := httptest.NewServer(h)
	defer local.Close()

	req, _ := http.NewRequest(http.MethodGet, local.URL+"/api/v1/search?q=x", nil)
	req.Header.Set(httpauth.OnBehalfOfHeader, "human:attacker@example.co.jp")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if v := got.Get(httpauth.OnBehalfOfHeader); v != "" {
		t.Errorf("%s reached ochakai as %q, want it stripped", httpauth.OnBehalfOfHeader, v)
	}
}

// With IAP configured the header is rebuilt from the verified assertion,
// so a write is recorded as the person in the browser (via the service).
// The browser's own value is still discarded, not merged.
func TestServeUIProxyNamesTheIAPUser(t *testing.T) {
	var got http.Header
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		_, _ = w.Write([]byte(`{}`))
	}))
	defer backend.Close()
	u, _ := url.Parse(backend.URL)

	h := serveUIHandler(newServiceProxy(u, staticServiceTokens{tok: "t"},
		fakeIAP{email: "tanaka@example.co.jp"}, slog.New(slog.DiscardHandler)))
	local := httptest.NewServer(h)
	defer local.Close()

	req, _ := http.NewRequest(http.MethodGet, local.URL+"/api/v1/search?q=x", nil)
	req.Header.Set(httpauth.OnBehalfOfHeader, "human:attacker@example.co.jp")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if v := got.Get(httpauth.OnBehalfOfHeader); v != "human:tanaka@example.co.jp" {
		t.Errorf("%s = %q, want the IAP user", httpauth.OnBehalfOfHeader, v)
	}
}

// An operator who configured an audience asked for per-user provenance.
// A request IAP did not sign must be refused, not quietly recorded as
// the service account — the silent downgrade 0027 §5.2 rejects.
func TestServeUIProxyRefusesUnsignedRequestsWhenIAPConfigured(t *testing.T) {
	reached := false
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		_, _ = w.Write([]byte(`{}`))
	}))
	defer backend.Close()
	u, _ := url.Parse(backend.URL)

	h := serveUIHandler(newServiceProxy(u, staticServiceTokens{tok: "t"},
		fakeIAP{err: errors.New("no assertion")}, slog.New(slog.DiscardHandler)))
	local := httptest.NewServer(h)
	defer local.Close()

	resp, err := http.Get(local.URL + "/api/v1/search?q=x")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
	if reached {
		t.Error("an unverified request reached ochakai")
	}
}

// The real verifier refuses an absent assertion before touching the
// network, and its error tells the operator which audience mismatched —
// the one thing they need to fix a misconfigured OCHAKAI_IAP_AUDIENCE.
func TestGoogleIAPRefusesMissingAssertion(t *testing.T) {
	g := &googleIAP{audience: "/projects/1/x"}
	if _, err := g.identity(context.Background(), ""); err == nil ||
		!strings.Contains(err.Error(), iapAssertionHeader) {
		t.Errorf("missing assertion: %v", err)
	}
}

func TestUnverifiedAudience(t *testing.T) {
	// header.payload.signature, payload = {"aud":"/projects/42/svc"}
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"aud":"/projects/42/svc"}`))
	if got := unverifiedAudience("h." + payload + ".s"); got != "/projects/42/svc" {
		t.Errorf("unverifiedAudience = %q", got)
	}
	for _, bad := range []string{"", "a.b", "h.!!!.s"} {
		if got := unverifiedAudience(bad); got != "" {
			t.Errorf("unverifiedAudience(%q) = %q, want empty", bad, got)
		}
	}
}

// IAP enforcement covers the proxied API, not the container's own
// endpoints: Cloud Run's health checks reach the instance directly, not
// through IAP, so putting them behind the assertion would fail the
// deployment rather than protect anything.
func TestServeUIHealthStaysOutsideIAP(t *testing.T) {
	u, _ := url.Parse("http://ochakai.internal")
	h := serveUIHandler(newServiceProxy(u, staticServiceTokens{tok: "t"},
		fakeIAP{err: errors.New("no assertion")}, slog.New(slog.DiscardHandler)))
	srv := httptest.NewServer(h)
	defer srv.Close()

	for _, path := range []string{"/health", "/"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, resp.StatusCode)
		}
	}
}
