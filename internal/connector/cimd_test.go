package connector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func cimdServer(t *testing.T, mutate func(m map[string]any)) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m := map[string]any{
			"client_id":     srv.URL + "/meta.json",
			"client_name":   "c",
			"redirect_uris": []string{"https://client.example/cb"},
		}
		if mutate != nil {
			mutate(m)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(m)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCIMDFetch(t *testing.T) {
	f := newCIMDFetcher()
	f.insecureLoopback = true
	ctx := context.Background()

	srv := cimdServer(t, nil)
	meta, err := f.fetch(ctx, srv.URL+"/meta.json")
	if err != nil {
		t.Fatal(err)
	}
	if meta.ClientName != "c" || len(meta.RedirectURIs) != 1 {
		t.Errorf("meta = %+v", meta)
	}

	cases := []struct {
		name    string
		mutate  func(m map[string]any)
		wantErr string
	}{
		{"client_id mismatch", func(m map[string]any) { m["client_id"] = "https://other.example/meta.json" }, "does not match"},
		{"no redirect_uris", func(m map[string]any) { delete(m, "redirect_uris") }, "no redirect_uris"},
	}
	for _, tc := range cases {
		srv := cimdServer(t, tc.mutate)
		if _, err := f.fetch(ctx, srv.URL+"/meta.json"); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.wantErr)
		}
	}

	nonJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>"))
	}))
	t.Cleanup(nonJSON.Close)
	if _, err := f.fetch(ctx, nonJSON.URL); err == nil || !strings.Contains(err.Error(), "application/json") {
		t.Errorf("non-JSON: err = %v", err)
	}

	huge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(make([]byte, cimdMaxBytes+2))
	}))
	t.Cleanup(huge.Close)
	if _, err := f.fetch(ctx, huge.URL); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("oversized: err = %v", err)
	}

	if _, err := f.fetch(ctx, "not a url"); err == nil {
		t.Error("garbage client_id accepted")
	}
}

// TestCIMDBlocksPrivateTargets is the SSRF guard: without the test-only
// loopback escape hatch, plain http and loopback/private IPs are refused.
func TestCIMDBlocksPrivateTargets(t *testing.T) {
	f := newCIMDFetcher()
	ctx := context.Background()

	srv := cimdServer(t, nil)
	if _, err := f.fetch(ctx, srv.URL+"/meta.json"); err == nil || !strings.Contains(err.Error(), "https") {
		t.Errorf("plain http: err = %v, want https requirement", err)
	}

	// https to a loopback address dies at dial time (before any TLS),
	// which is where the guard must sit: DNS is resolved by then.
	tls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	t.Cleanup(tls.Close)
	if _, err := f.fetch(ctx, tls.URL+"/meta.json"); err == nil || !strings.Contains(err.Error(), "no public IP") {
		t.Errorf("loopback https: err = %v, want dial-time rejection", err)
	}

	if _, err := f.fetch(ctx, "https://user:pw@example.com/meta.json"); err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Errorf("userinfo URL: err = %v", err)
	}
}

func TestRedirectURIAllowed(t *testing.T) {
	registered := []string{"https://client.example/cb", "http://localhost/callback", "http://127.0.0.1/callback"}
	cases := []struct {
		uri  string
		want bool
	}{
		{"https://client.example/cb", true},
		{"https://client.example/cb2", false},
		{"https://evil.example/cb", false},
		{"http://localhost:3118/callback", true}, // RFC 8252 loopback: port ignored
		{"http://127.0.0.1:49152/callback", true},
		{"http://localhost:3118/other", false},     // but not the path
		{"https://localhost:3118/callback", false}, // loopback rule is http-only
		{"http://192.168.1.5:3118/callback", false},
		{"http://[::1]:9999/callback", false}, // ::1 not registered here
	}
	for _, tc := range cases {
		if got := redirectURIAllowed(tc.uri, registered); got != tc.want {
			t.Errorf("redirectURIAllowed(%q) = %v, want %v", tc.uri, got, tc.want)
		}
	}
	if !redirectURIAllowed("http://[::1]:9/cb", []string{"http://[::1]/cb"}) {
		t.Error("IPv6 loopback with port should match portless registration")
	}
}
