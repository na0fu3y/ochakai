package connector

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/api/idtoken"

	"github.com/na0fu3y/ochakai/internal/config"
	"github.com/na0fu3y/ochakai/internal/httpauth"
	"github.com/na0fu3y/ochakai/internal/store"
)

// fakeStore is an in-memory Store; the real SQL twin is exercised by the
// integration test in internal/store.
type fakeStore struct {
	mu      sync.Mutex
	pending map[string]store.OAuthPending
	codes   map[string]store.OAuthCode
	grants  map[string]store.OAuthGrant // keyed by grant ID
	prev    map[string]string           // grant ID -> rotated-out refresh hash
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		pending: map[string]store.OAuthPending{},
		codes:   map[string]store.OAuthCode{},
		grants:  map[string]store.OAuthGrant{},
		prev:    map[string]string{},
	}
}

func (f *fakeStore) CreateOAuthPending(_ context.Context, p store.OAuthPending) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pending[p.ID] = p
	return nil
}

func (f *fakeStore) GetOAuthPending(_ context.Context, id string) (store.OAuthPending, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.pending[id]
	if !ok || time.Now().After(p.ExpiresAt) {
		return store.OAuthPending{}, store.ErrNotFound
	}
	return p, nil
}

func (f *fakeStore) TakeOAuthPending(ctx context.Context, id string) (store.OAuthPending, error) {
	p, err := f.GetOAuthPending(ctx, id)
	if err == nil {
		f.mu.Lock()
		delete(f.pending, id)
		f.mu.Unlock()
	}
	return p, err
}

func (f *fakeStore) CreateOAuthCode(_ context.Context, c store.OAuthCode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.codes[c.CodeHash] = c
	return nil
}

func (f *fakeStore) TakeOAuthCode(_ context.Context, codeHash string) (store.OAuthCode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.codes[codeHash]
	if !ok || time.Now().After(c.ExpiresAt) {
		return store.OAuthCode{}, store.ErrNotFound
	}
	delete(f.codes, codeHash)
	return c, nil
}

func (f *fakeStore) CreateOAuthGrant(_ context.Context, g store.OAuthGrant) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.grants[g.ID] = g
	return nil
}

func (f *fakeStore) OAuthActorByAccess(_ context.Context, accessHash string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, g := range f.grants {
		if g.AccessHash == accessHash && time.Now().Before(g.AccessExpiresAt) {
			return g.ActorEmail, nil
		}
	}
	return "", store.ErrNotFound
}

func (f *fakeStore) RotateOAuthGrant(_ context.Context, refreshHash, newAccessHash string, accessExpiresAt time.Time, newRefreshHash string) (store.OAuthGrant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, g := range f.grants {
		if g.RefreshHash == refreshHash && time.Now().Before(g.RefreshExpiresAt) {
			f.prev[id] = refreshHash // like the SQL twin, one prev hash per grant
			g.AccessHash, g.AccessExpiresAt, g.RefreshHash = newAccessHash, accessExpiresAt, newRefreshHash
			f.grants[id] = g
			return g, nil
		}
	}
	for id, prev := range f.prev {
		if prev == refreshHash {
			delete(f.grants, id)
			delete(f.prev, id)
			return store.OAuthGrant{}, store.ErrOAuthReuse
		}
	}
	return store.OAuthGrant{}, store.ErrNotFound
}

func (f *fakeStore) PruneOAuth(context.Context) error { return nil }

// testEnv wires a full connector with a CIMD document server and a fake
// Google, and exposes the pieces tests poke at.
type testEnv struct {
	srv     *httptest.Server // the connector itself
	cimdSrv *httptest.Server
	store   *fakeStore
	cfg     *config.ConnectorConfig
	// google controls what the fake Google returns.
	googleEmail    string
	googleVerified bool
	googleHD       string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	env := &testEnv{store: newFakeStore(), googleEmail: "aya@example.co.jp", googleVerified: true, googleHD: "example.co.jp"}

	env.cimdSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"client_id":     env.cimdSrv.URL + "/client-metadata.json",
			"client_name":   "Test MCP Client",
			"redirect_uris": []string{"https://client.example/callback", "http://localhost/callback", "http://127.0.0.1/callback"},
		})
	}))
	t.Cleanup(env.cimdSrv.Close)

	googleTokens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/x-www-form-urlencoded") {
			t.Errorf("google exchange content type = %q", ct)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id_token": "fake-id-token"}`))
	}))
	t.Cleanup(googleTokens.Close)

	env.cfg = &config.ConnectorConfig{
		PublicURL:          "https://connector.example",
		GoogleClientID:     "gcid",
		GoogleClientSecret: "gsecret",
		AllowedDomain:      "example.co.jp",
	}
	s := newServer(env.cfg, env.store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.cimd.insecureLoopback = true
	s.google.tokenEndpoint = googleTokens.URL
	s.google.validate = func(_ context.Context, token, audience string) (*idtoken.Payload, error) {
		if token != "fake-id-token" || audience != "gcid" {
			return nil, fmt.Errorf("unexpected token/audience")
		}
		return &idtoken.Payload{Claims: map[string]any{
			"email": env.googleEmail, "email_verified": env.googleVerified, "hd": env.googleHD,
		}}, nil
	}

	mcp := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor := httpauth.Actor(r.Context())
		_, _ = fmt.Fprintf(w, "%s:%s", actor.Kind, actor.Name)
	})
	env.srv = httptest.NewServer(s.handler(mcp, "test"))
	t.Cleanup(env.srv.Close)
	return env
}

func (env *testEnv) clientID() string { return env.cimdSrv.URL + "/client-metadata.json" }

// noRedirect returns a client that surfaces redirects instead of following.
func noRedirect() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}

var pendingRe = regexp.MustCompile(`name="pending" value="([^"]+)"`)

// authorize walks authorize → consent → callback and returns the query
// values delivered to the client's redirect URI.
func (env *testEnv) authorize(t *testing.T, challenge, redirectURI, state string) url.Values {
	t.Helper()
	q := url.Values{
		"response_type": {"code"}, "client_id": {env.clientID()},
		"redirect_uri": {redirectURI}, "state": {state},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"},
	}
	resp, err := http.Get(env.srv.URL + "/oauth/authorize?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authorize = %d: %s", resp.StatusCode, body)
	}
	m := pendingRe.FindSubmatch(body)
	if m == nil {
		t.Fatalf("no pending field in consent page: %s", body)
	}
	if !strings.Contains(string(body), "Test MCP Client") {
		t.Errorf("consent page does not show the client name")
	}

	resp, err = noRedirect().PostForm(env.srv.URL+"/oauth/consent", url.Values{"pending": {string(m[1])}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("consent = %d", resp.StatusCode)
	}
	googleURL, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if got := googleURL.Query().Get("state"); got != string(m[1]) {
		t.Fatalf("google state = %q, want pending id", got)
	}

	cb := url.Values{"state": {string(m[1])}, "code": {"google-code"}}
	resp, err = noRedirect().Get(env.srv.URL + "/oauth/callback?" + cb.Encode())
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback = %d", resp.StatusCode)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(loc.String(), redirectURI) {
		t.Fatalf("callback redirected to %q, want prefix %q", loc, redirectURI)
	}
	return loc.Query()
}

func pkce() (verifier, challenge string) {
	verifier = newToken()
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Error        string `json:"error"`
}

func (env *testEnv) exchange(t *testing.T, form url.Values) (tokenResponse, int) {
	t.Helper()
	resp, err := http.PostForm(env.srv.URL+"/oauth/token", form)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		t.Fatal(err)
	}
	return tr, resp.StatusCode
}

func (env *testEnv) callMCP(t *testing.T, token string) (string, int) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, env.srv.URL+"/mcp", strings.NewReader("{}"))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(body), resp.StatusCode
}

func TestFullFlow(t *testing.T) {
	env := newTestEnv(t)
	verifier, challenge := pkce()

	q := env.authorize(t, challenge, "https://client.example/callback", "client-state")
	if q.Get("state") != "client-state" {
		t.Errorf("state = %q, want client-state", q.Get("state"))
	}
	code := q.Get("code")
	if code == "" {
		t.Fatalf("no code in redirect: %v", q)
	}

	tr, status := env.exchange(t, url.Values{
		"grant_type": {"authorization_code"}, "code": {code},
		"client_id": {env.clientID()}, "redirect_uri": {"https://client.example/callback"},
		"code_verifier": {verifier},
	})
	if status != http.StatusOK || tr.AccessToken == "" || tr.RefreshToken == "" {
		t.Fatalf("token exchange = %d %+v", status, tr)
	}
	if tr.TokenType != "Bearer" || tr.ExpiresIn != advertisedTTL {
		t.Errorf("token_type/expires_in = %q/%d", tr.TokenType, tr.ExpiresIn)
	}

	body, status := env.callMCP(t, tr.AccessToken)
	if status != http.StatusOK || body != "human:aya@example.co.jp" {
		t.Fatalf("mcp with token = %d %q, want the Google-verified human actor", status, body)
	}

	// Refresh rotates: new pair works, old refresh becomes reuse.
	tr2, status := env.exchange(t, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tr.RefreshToken}})
	if status != http.StatusOK || tr2.AccessToken == "" || tr2.AccessToken == tr.AccessToken {
		t.Fatalf("refresh = %d %+v", status, tr2)
	}
	if _, status := env.callMCP(t, tr2.AccessToken); status != http.StatusOK {
		t.Fatalf("rotated access token rejected: %d", status)
	}
	if _, status := env.exchange(t, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tr.RefreshToken}}); status != http.StatusBadRequest {
		t.Fatalf("reused refresh token = %d, want 400", status)
	}
	// Reuse revoked the grant: even the rotated tokens are dead now.
	if _, status := env.callMCP(t, tr2.AccessToken); status != http.StatusUnauthorized {
		t.Fatalf("access token after revocation = %d, want 401", status)
	}
	if _, status := env.exchange(t, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tr2.RefreshToken}}); status != http.StatusBadRequest {
		t.Fatalf("refresh after revocation = %d, want 400", status)
	}
}

func TestCodeIsSingleUseAndBound(t *testing.T) {
	env := newTestEnv(t)
	verifier, challenge := pkce()

	code := env.authorize(t, challenge, "https://client.example/callback", "s").Get("code")
	base := url.Values{
		"grant_type": {"authorization_code"}, "code": {code},
		"client_id": {env.clientID()}, "redirect_uri": {"https://client.example/callback"},
		"code_verifier": {verifier},
	}

	wrong := url.Values{}
	for k, v := range base {
		wrong[k] = v
	}
	wrong.Set("code_verifier", "not-the-verifier")
	if tr, status := env.exchange(t, wrong); status != http.StatusBadRequest || tr.Error != "invalid_grant" {
		t.Fatalf("wrong verifier = %d %+v, want invalid_grant", status, tr)
	}
	// The failed attempt consumed the code (single use), so even the
	// correct verifier is rejected now.
	if tr, status := env.exchange(t, base); status != http.StatusBadRequest || tr.Error != "invalid_grant" {
		t.Fatalf("replayed code = %d %+v, want invalid_grant", status, tr)
	}

	// A fresh code with mismatched redirect_uri is also rejected.
	code2 := env.authorize(t, challenge, "https://client.example/callback", "s").Get("code")
	base.Set("code", code2)
	base.Set("redirect_uri", "https://client.example/other")
	if tr, status := env.exchange(t, base); status != http.StatusBadRequest || tr.Error != "invalid_grant" {
		t.Fatalf("mismatched redirect_uri = %d %+v, want invalid_grant", status, tr)
	}
}

func TestCallbackRejectsWrongDomain(t *testing.T) {
	env := newTestEnv(t)
	env.googleEmail, env.googleHD = "eve@evil.example", "evil.example"
	_, challenge := pkce()
	q := env.authorize(t, challenge, "https://client.example/callback", "s")
	if q.Get("error") != "access_denied" || q.Get("code") != "" {
		t.Fatalf("wrong-domain login = %v, want access_denied and no code", q)
	}
}

func TestCallbackRejectsMissingHD(t *testing.T) {
	env := newTestEnv(t)
	// A consumer account with a spoofed-looking email but no hd claim.
	env.googleEmail, env.googleHD = "someone@example.co.jp", ""
	_, challenge := pkce()
	if q := env.authorize(t, challenge, "https://client.example/callback", "s"); q.Get("error") != "access_denied" {
		t.Fatalf("no-hd login = %v, want access_denied", q)
	}
}

func TestAuthorizeValidation(t *testing.T) {
	env := newTestEnv(t)
	_, challenge := pkce()

	get := func(q url.Values) *http.Response {
		t.Helper()
		resp, err := noRedirect().Get(env.srv.URL + "/oauth/authorize?" + q.Encode())
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}
	valid := func() url.Values {
		return url.Values{
			"response_type": {"code"}, "client_id": {env.clientID()},
			"redirect_uri":   {"https://client.example/callback"},
			"code_challenge": {challenge}, "code_challenge_method": {"S256"},
		}
	}

	// Unregistered redirect_uri: error page, never a redirect.
	q := valid()
	q.Set("redirect_uri", "https://attacker.example/steal")
	if resp := get(q); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("unregistered redirect_uri = %d, want 400", resp.StatusCode)
	}

	// Missing PKCE: redirected error to the validated redirect_uri.
	q = valid()
	q.Del("code_challenge")
	resp := get(q)
	if resp.StatusCode != http.StatusFound || !strings.Contains(resp.Header.Get("Location"), "error=invalid_request") {
		t.Errorf("missing PKCE = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}

	// plain method is a PKCE downgrade: rejected.
	q = valid()
	q.Set("code_challenge_method", "plain")
	if resp := get(q); !strings.Contains(resp.Header.Get("Location"), "error=invalid_request") {
		t.Errorf("plain PKCE accepted: %q", resp.Header.Get("Location"))
	}

	// Wrong resource indicator.
	q = valid()
	q.Set("resource", "https://other.example/mcp")
	if resp := get(q); !strings.Contains(resp.Header.Get("Location"), "error=invalid_target") {
		t.Errorf("wrong resource accepted: %q", resp.Header.Get("Location"))
	}

	// Loopback redirect with an ephemeral port matches the portless
	// registration (RFC 8252 §7.3).
	q = valid()
	q.Set("redirect_uri", "http://127.0.0.1:39181/callback")
	if resp := get(q); resp.StatusCode != http.StatusOK {
		t.Errorf("loopback redirect with port = %d, want consent page", resp.StatusCode)
	}
}

func TestMCPUnauthorizedHandshake(t *testing.T) {
	env := newTestEnv(t)

	body, status := env.callMCP(t, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("no token = %d %q", status, body)
	}
	resp, err := http.Post(env.srv.URL+"/mcp", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	want := `Bearer resource_metadata="https://connector.example/.well-known/oauth-protected-resource"`
	if got := resp.Header.Get("WWW-Authenticate"); got != want {
		t.Errorf("WWW-Authenticate = %q, want %q", got, want)
	}

	if _, status := env.callMCP(t, "made-up-token"); status != http.StatusUnauthorized {
		t.Errorf("bogus token = %d, want 401", status)
	}
}

func TestMetadataDocuments(t *testing.T) {
	env := newTestEnv(t)

	var as map[string]any
	fetchJSON(t, env.srv.URL+"/.well-known/oauth-authorization-server", &as)
	if as["issuer"] != "https://connector.example" {
		t.Errorf("issuer = %v", as["issuer"])
	}
	if as["client_id_metadata_document_supported"] != true {
		t.Errorf("CIMD support not advertised")
	}
	if fmt.Sprint(as["token_endpoint_auth_methods_supported"]) != "[none]" {
		t.Errorf("token auth methods = %v, want [none] (claude.ai CIMD requires it)", as["token_endpoint_auth_methods_supported"])
	}
	if fmt.Sprint(as["code_challenge_methods_supported"]) != "[S256]" {
		t.Errorf("pkce methods = %v", as["code_challenge_methods_supported"])
	}
	if !strings.Contains(fmt.Sprint(as["scopes_supported"]), "offline_access") {
		t.Errorf("offline_access missing from scopes_supported (needed for refresh tokens): %v", as["scopes_supported"])
	}

	for _, path := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp"} {
		var prm map[string]any
		fetchJSON(t, env.srv.URL+path, &prm)
		if prm["resource"] != "https://connector.example/mcp" {
			t.Errorf("%s resource = %v", path, prm["resource"])
		}
		if fmt.Sprint(prm["authorization_servers"]) != "[https://connector.example]" {
			t.Errorf("%s authorization_servers = %v", path, prm["authorization_servers"])
		}
	}
}

func fetchJSON(t *testing.T, url string, v any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d", url, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatal(err)
	}
}

func TestTokenEndpointErrors(t *testing.T) {
	env := newTestEnv(t)

	if tr, status := env.exchange(t, url.Values{"grant_type": {"password"}}); status != http.StatusBadRequest || tr.Error != "unsupported_grant_type" {
		t.Errorf("password grant = %d %+v", status, tr)
	}
	if tr, status := env.exchange(t, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"nope"}}); status != http.StatusBadRequest || tr.Error != "invalid_grant" {
		t.Errorf("unknown refresh = %d %+v, want invalid_grant", status, tr)
	}
	if tr, status := env.exchange(t, url.Values{"grant_type": {"authorization_code"}, "code": {"nope"}}); status != http.StatusBadRequest || tr.Error != "invalid_grant" {
		t.Errorf("unknown code = %d %+v, want invalid_grant", status, tr)
	}
}
