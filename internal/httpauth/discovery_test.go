package httpauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/na0fu3y/ochakai/internal/config"
)

// The handshake is off unless a deployment asked for it, and asking is
// two facts it already had to name: an issuer (design doc 0086) and an
// audience spelled as the https URL clients reach it at. Everything
// else is a deployment that is not asking.
func TestOnlyADeploymentThatNamedBothPublishes(t *testing.T) {
	for name, cfg := range map[string]*config.Config{
		"no issuer":          {OIDCAudience: "https://ochakai.example/mcp"},
		"no audience":        {OIDCIssuer: "https://issuer.example"},
		"audience is a word": {OIDCIssuer: "https://issuer.example", OIDCAudience: "ochakai"},
		"audience is http":   {OIDCIssuer: "https://issuer.example", OIDCAudience: "http://ochakai.example/mcp"},
		"audience has a query": {
			OIDCIssuer: "https://issuer.example", OIDCAudience: "https://ochakai.example/mcp?v=1"},
		"nothing at all": {},
	} {
		t.Run(name, func(t *testing.T) {
			if d := NewDiscovery(cfg); d != nil {
				t.Errorf("NewDiscovery = %+v, want nil for a deployment that did not ask", d)
			}
		})
	}
}

// A nil publishes nothing and breaks nothing: the well-known address
// stays unregistered, and /mcp keeps whatever 401 it already wrote.
func TestNothingPublishedChangesNothing(t *testing.T) {
	var d *Discovery
	mux := http.NewServeMux()
	d.Mount(mux)
	mux.Handle("/mcp", d.Challenge(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/mcp", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("well-known = %d, want 404 from a deployment that publishes none", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))
	if got := rec.Header().Get("WWW-Authenticate"); got != "" {
		t.Errorf("WWW-Authenticate = %q, want none", got)
	}
	if d.Path() != "" {
		t.Errorf("Path = %q, want empty", d.Path())
	}
}

// The document carries the two facts and no third one — in particular
// no scopes, because there are none to advertise (design doc 0065 §1).
func TestTheDocumentNamesTheIssuerAndTheResource(t *testing.T) {
	d := NewDiscovery(&config.Config{
		OIDCIssuer:   "https://issuer.example",
		OIDCAudience: "https://ochakai.example/mcp",
	})
	if d == nil {
		t.Fatal("a deployment that named both should publish")
	}
	if d.Path() != "/.well-known/oauth-protected-resource/mcp" {
		t.Errorf("path = %q, want the resource's own path after the well-known segment", d.Path())
	}

	mux := http.NewServeMux()
	d.Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, d.Path(), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var doc struct {
		Resource      string   `json:"resource"`
		Servers       []string `json:"authorization_servers"`
		BearerMethods []string `json:"bearer_methods_supported"`
		Scopes        []string `json:"scopes_supported"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if doc.Resource != "https://ochakai.example/mcp" {
		t.Errorf("resource = %q, want the audience verbatim — a client compares it to what it typed", doc.Resource)
	}
	if len(doc.Servers) != 1 || doc.Servers[0] != "https://issuer.example" {
		t.Errorf("authorization_servers = %v, want the deployment's own issuer", doc.Servers)
	}
	if len(doc.BearerMethods) != 1 || doc.BearerMethods[0] != "header" {
		t.Errorf("bearer_methods_supported = %v, want the header alone", doc.BearerMethods)
	}
	if doc.Scopes != nil {
		t.Errorf("scopes_supported = %v, want none: whoever the issuer vouches for may do everything", doc.Scopes)
	}
}

// A resource at the root of a host is described at the bare well-known
// address, which is the other half of RFC 9728 §3.1.
func TestAResourceAtTheRootIsDescribedAtTheBareAddress(t *testing.T) {
	for _, audience := range []string{"https://ochakai.example", "https://ochakai.example/"} {
		d := NewDiscovery(&config.Config{OIDCIssuer: "https://issuer.example", OIDCAudience: audience})
		if d == nil {
			t.Fatalf("%s: should publish", audience)
		}
		if d.Path() != "/.well-known/oauth-protected-resource" {
			t.Errorf("%s: path = %q, want the bare address", audience, d.Path())
		}
	}
}

// The 401 is the first step of the handshake: without the header naming
// the document, a client that has no configuration knows only that it
// is unauthenticated.
func TestAnUnauthenticatedCallIsToldWhereToAuthenticate(t *testing.T) {
	d := NewDiscovery(&config.Config{
		OIDCIssuer:   "https://issuer.example",
		OIDCAudience: "https://ochakai.example/mcp",
	})
	h := d.Challenge(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer good" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))
	want := `Bearer resource_metadata="https://ochakai.example/.well-known/oauth-protected-resource/mcp"`
	if got := rec.Header().Get("WWW-Authenticate"); got != want {
		t.Errorf("WWW-Authenticate = %q, want %q", got, want)
	}

	// A request that carried a token was refused because of the token,
	// and RFC 6750 §3.1 is where a client hears so.
	rec = httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	r.Header.Set("Authorization", "Bearer stale")
	h.ServeHTTP(rec, r)
	if got := rec.Header().Get("WWW-Authenticate"); !strings.HasPrefix(got, `Bearer error="invalid_token", `) {
		t.Errorf("WWW-Authenticate = %q, want it to name the token as the reason", got)
	}

	// Nothing is added to a response that is not a 401.
	rec = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/mcp", nil)
	r.Header.Set("Authorization", "Bearer good")
	h.ServeHTTP(rec, r)
	if got := rec.Header().Get("WWW-Authenticate"); got != "" {
		t.Errorf("WWW-Authenticate on a 200 = %q, want none", got)
	}
}

// The whole exchange, with nothing configured on the client side but
// the address: call it, read the document the 401 names, find the
// issuer there, present what that issuer signed, and be recorded as the
// person it vouched for. This is the experiment the design question
// turns on — whether a deployment can hand a client an authorization
// server it did not write.
func TestAClientWithOnlyTheAddressCanFindItsWayIn(t *testing.T) {
	iss := newFakeIssuer(t)

	// The server, wired the way cmd/ochakai wires it.
	var seen string
	mux := http.NewServeMux()
	cfg := &config.Config{OIDCIssuer: iss.url, OIDCAudience: ""}
	protected := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = Actor(r.Context()).Name
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	// The audience is the address clients reach, which is only known
	// once the test server has one.
	cfg.OIDCAudience = srv.URL + "/mcp"
	v := verifierFor(t, iss)
	v.audience = cfg.OIDCAudience
	cfg.Verifier = v
	d := NewDiscovery(cfg)
	if d == nil {
		t.Fatal("this deployment named both and should publish")
	}
	d.Mount(mux)
	mux.Handle("/mcp", d.Challenge(Middleware(cfg, protected)))

	client := srv.Client()

	// 1. The client calls the address it was given, with nothing.
	resp, err := client.Post(srv.URL+"/mcp", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}

	// 2. It reads where to look out of the challenge.
	challenge := resp.Header.Get("WWW-Authenticate")
	metadataURL := between(challenge, `resource_metadata="`, `"`)
	if metadataURL == "" {
		t.Fatalf("no metadata address in %q", challenge)
	}

	// 3. It fetches the document — unauthenticated, which it must be:
	// the client has no token yet.
	doc, err := client.Get(metadataURL)
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Body.Close()
	if doc.StatusCode != http.StatusOK {
		t.Fatalf("metadata status = %d, want 200", doc.StatusCode)
	}
	var meta struct {
		Resource string   `json:"resource"`
		Servers  []string `json:"authorization_servers"`
	}
	if err := json.NewDecoder(doc.Body).Decode(&meta); err != nil {
		t.Fatal(err)
	}
	if meta.Resource != srv.URL+"/mcp" {
		t.Errorf("resource = %q, want the address the client called", meta.Resource)
	}
	if len(meta.Servers) != 1 || meta.Servers[0] != iss.url {
		t.Fatalf("authorization_servers = %v, want the issuer this deployment trusts", meta.Servers)
	}

	// 4. It authenticates there. In the real exchange this is the
	// browser round trip the issuer owns; here it is the token that
	// round trip ends with — the point being that ochakai issued
	// nothing, registered nothing, and holds no secret of that
	// issuer's. The audience of that token is the resource rather than
	// the issuer, which is what binds it here (design doc 0086 §2).
	token := iss.token(t, struct {
		jwt.Claims
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}{standardClaims(iss, meta.Resource), "tanaka@example.co.jp", true})

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	final, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer final.Body.Close()
	if final.StatusCode != http.StatusOK {
		t.Fatalf("status with a token from the named issuer = %d, want 200", final.StatusCode)
	}
	if seen != "tanaka@example.co.jp" {
		t.Errorf("recorded actor = %q, want the person the issuer vouched for", seen)
	}
}

// between pulls one quoted parameter out of a challenge, which is all
// the parsing a client needs to take its next step.
func between(s, prefix, suffix string) string {
	i := strings.Index(s, prefix)
	if i < 0 {
		return ""
	}
	rest := s[i+len(prefix):]
	j := strings.Index(rest, suffix)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// An audience whose path is not a plain one would reach the mux as a
// pattern, and an unparseable pattern is a panic at startup. Refusing
// to publish is the same answer this gives every other audience it
// cannot describe.
func TestAnAudienceThatWouldNotBecomeARouteDoesNotPublish(t *testing.T) {
	for _, audience := range []string{
		"https://ochakai.example/{id}",
		"https://ochakai.example/mcp server",
		"https://ochakai.example//mcp",
	} {
		if d := NewDiscovery(&config.Config{
			OIDCIssuer: "https://issuer.example", OIDCAudience: audience,
		}); d != nil {
			t.Errorf("%s: published %q, want nil", audience, d.Path())
		}
	}
}
