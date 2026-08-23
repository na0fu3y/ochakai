package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/oauth2"

	"github.com/na0fu3y/ochakai/internal/httpauth"
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

// The page renders a concept's body, which is text somebody else wrote,
// so the second wall behind the escaping is a policy the browser
// enforces. Both serving paths get it, because both serve the same page
// to the same kind of reader (design doc 0006).
//
// What is pinned is the half that matters: no inline script, and nothing
// reachable that is not named. `style-src` keeps 'unsafe-inline' for the
// page's literal style attributes and says so out loud — the test would
// otherwise pass a policy that had quietly given up on scripts too.
func TestTheUIPageCarriesASecurityPolicy(t *testing.T) {
	for _, tc := range []struct {
		name string
		mux  func(t *testing.T) http.Handler
	}{
		{"ui", func(t *testing.T) http.Handler {
			h, err := uiHandler("http://ochakai.internal", nil)
			if err != nil {
				t.Fatal(err)
			}
			return h
		}},
		{"serve-ui", func(*testing.T) http.Handler {
			return serveUIHandler(http.NotFoundHandler())
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8098/", nil)
			rec := httptest.NewRecorder()
			tc.mux(t).ServeHTTP(rec, req)
			csp := rec.Header().Get("Content-Security-Policy")
			if csp == "" {
				t.Fatal("the page is served without a Content-Security-Policy")
			}
			for _, want := range []string{
				"default-src 'none'",     // nothing that is not named below
				"script-src 'self'",      // the modules, and nothing inline
				"frame-ancestors 'none'", // not somebody else's frame
				"base-uri 'none'",
			} {
				if !strings.Contains(csp, want) {
					t.Errorf("policy is missing %q:\n%s", want, csp)
				}
			}
			// The one that would undo the rest. A page with no inline
			// script has no reason to allow one (design doc 0094).
			if strings.Contains(csp, "script-src 'self' 'unsafe-inline'") ||
				strings.Contains(csp, "script-src 'unsafe-inline'") {
				t.Errorf("script-src allows inline again, which is what the policy is for:\n%s", csp)
			}
			if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
			}
		})
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

	req, _ := http.NewRequest(http.MethodGet, local.URL+"/api/v1/search?q=x", nil)
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

	req, _ := http.NewRequest(http.MethodGet, local.URL+"/api/v1/search", nil)
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
// loopback Host (passing the rebinding guard) but says where it came
// from. The UI's own requests must reach the backend; a foreign one must
// be refused before the proxy signs and forwards the write.
//
// Since design doc 0123 the rule is net/http's rather than this
// package's, so the cases below are the ones that rule decides:
// Sec-Fetch-Site when the browser sent it, the Origin/Host comparison
// when it did not, and neither header meaning a client that is not a
// browser. serve-ui is held to the same rule by
// TestServeUIRefusesACrossSiteWrite; this is the loopback half.
//
// "http://localhost:8098" against a Host of "127.0.0.1:8098" is refused
// now and was allowed before: the old check asked only whether the
// Origin was some loopback name, and the standard one asks whether it is
// *this* one. No browser produces that pair — it fetches the host the
// page was served from — so what the tightening costs is a leniency
// nobody could reach.
func TestUIHandlerRefusesACrossSiteWrite(t *testing.T) {
	var reached bool
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
	}))
	defer backend.Close()
	h, err := uiHandler(backend.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, secFetchSite, origin string
		want                       int
	}{
		{"no headers at all (curl, the CLI, an agent)", "", "", http.StatusOK},
		{"the page's own fetch", "same-origin", "http://127.0.0.1:8098", http.StatusOK},
		{"a form the page submitted to itself", "same-origin", "", http.StatusOK},
		{"another site's page", "cross-site", "https://evil.example", http.StatusForbidden},
		{"another site's form, no Origin", "cross-site", "", http.StatusForbidden},
		{"a subdomain of the same site", "same-site", "https://ui.evil.example", http.StatusForbidden},
		{"an older browser: Origin only", "", "https://evil.example", http.StatusForbidden},
		{"a hostname that merely starts the same", "", "http://127.0.0.1.evil.com", http.StatusForbidden},
		{"another loopback spelling than this Host", "", "http://localhost:8098", http.StatusForbidden},
		{"an opaque origin (sandboxed iframe, data: page)", "", "null", http.StatusForbidden},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reached = false
			req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8098/api/v1/search", nil)
			req.Host = "127.0.0.1:8098"
			if c.secFetchSite != "" {
				req.Header.Set("Sec-Fetch-Site", c.secFetchSite)
			}
			if c.origin != "" {
				req.Header.Set("Origin", c.origin)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Errorf("POST = %d, want %d (body: %s)", rec.Code, c.want, rec.Body)
			}
			if reached != (c.want == http.StatusOK) {
				t.Errorf("reached backend = %v, want %v", reached, c.want == http.StatusOK)
			}
		})
	}
}

// A read is not a write, and the guard says so: GET is a safe method, so
// a cross-site page can still be told no by the browser's own rules
// without ochakai refusing to serve its page. This is also what keeps
// serve-ui's /health answering (design doc 0006) — it is a GET.
func TestUIHandlerAllowsCrossSiteReads(t *testing.T) {
	h, err := uiHandler("http://ochakai.internal", nil)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8098/", nil)
	req.Host = "127.0.0.1:8098"
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("cross-site GET / = %d, want 200", rec.Code)
	}
}

// The rebinding guard still has work the cross-origin check cannot do:
// a page at attacker.example resolving to 127.0.0.1 sends a Host and an
// Origin that agree with each other, and Sec-Fetch-Site says
// same-origin, so the cross-origin check passes it. The Host is what
// gives it away, and loopbackHostGuard runs first.
func TestUIHandlerRejectsRebindingThatLooksSameOrigin(t *testing.T) {
	var reached bool
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
	}))
	defer backend.Close()
	h, err := uiHandler(backend.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://placeholder/api/v1/search", nil)
	req.Host = "attacker.example:8098"
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Origin", "http://attacker.example:8098")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("rebound POST = %d, want 403", rec.Code)
	}
	if reached {
		t.Error("a rebound request reached the backend")
	}
}

// `ochakai ui` substitutes the CLI user's identity, so a page it serves
// must not be able to claim it is acting for someone else. There is no
// verified source for that claim on loopback — serve-ui gets one from
// IAP, this proxy has none — and the local server may well be one where
// the user is permitted to delegate (design doc 0032).
func TestUIHandlerStripsDelegationHeader(t *testing.T) {
	var got http.Header
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		_, _ = w.Write([]byte(`{}`))
	}))
	defer backend.Close()

	h, err := uiHandler(backend.URL, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "t"}))
	if err != nil {
		t.Fatal(err)
	}
	local := httptest.NewServer(h)
	defer local.Close()

	req, _ := http.NewRequest(http.MethodGet, local.URL+"/api/v1/search?q=x", nil)
	req.Header.Set(httpauth.OnBehalfOfHeader, "human:someone-else@example.co.jp")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)
	if v := got.Get(httpauth.OnBehalfOfHeader); v != "" {
		t.Errorf("%s reached the server as %q, want it stripped", httpauth.OnBehalfOfHeader, v)
	}
}

// The retired "X-" spelling (design doc 0064) must be stripped too, not
// just the current name: a staggered upgrade could otherwise leave this
// header reaching an API new enough to still honor it (issue #410).
func TestUIHandlerStripsRetiredDelegationHeaderSpelling(t *testing.T) {
	var got http.Header
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		_, _ = w.Write([]byte(`{}`))
	}))
	defer backend.Close()

	h, err := uiHandler(backend.URL, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "t"}))
	if err != nil {
		t.Fatal(err)
	}
	local := httptest.NewServer(h)
	defer local.Close()

	req, _ := http.NewRequest(http.MethodGet, local.URL+"/api/v1/search?q=x", nil)
	req.Header.Set("X-"+httpauth.OnBehalfOfHeader, "human:someone-else@example.co.jp")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)
	if v := got.Get("X-" + httpauth.OnBehalfOfHeader); v != "" {
		t.Errorf("X-%s reached the server as %q, want it stripped", httpauth.OnBehalfOfHeader, v)
	}
}

// X-Serverless-Authorization is the other credential header httpauth
// reads, and it reads it *in preference to* Authorization. Forwarding
// what the browser sent would therefore let a page override the very
// identity this proxy exists to attach, so it is dropped alongside
// Authorization rather than only instead of it.
func TestUIHandlerStripsServerlessAuthorization(t *testing.T) {
	var got http.Header
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		_, _ = w.Write([]byte(`{}`))
	}))
	defer backend.Close()

	h, err := uiHandler(backend.URL, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "id-token"}))
	if err != nil {
		t.Fatal(err)
	}
	local := httptest.NewServer(h)
	defer local.Close()

	req, _ := http.NewRequest(http.MethodGet, local.URL+"/api/v1/search?q=x", nil)
	req.Header.Set("X-Serverless-Authorization", "Bearer browser-supplied")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)
	if v := got.Get("X-Serverless-Authorization"); v != "" {
		t.Errorf("X-Serverless-Authorization reached the server as %q, want it stripped", v)
	}
	if auth := got.Get("Authorization"); auth != "Bearer id-token" {
		t.Errorf("upstream Authorization = %q, want the CLI user's token", auth)
	}
}
