package httpauth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/na0fu3y/ochakai/internal/config"
)

// fakeTokeninfo answers like Google's tokeninfo for the tokens it knows,
// and counts how often it was asked.
func fakeTokeninfo(t *testing.T, known map[string]map[string]string) *atomic.Int32 {
	t.Helper()
	var asked atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		if r.Method != http.MethodPost || r.URL.RawQuery != "" {
			t.Errorf("tokeninfo called as %s %s — the token must travel in the body", r.Method, r.URL)
		}
		b, _ := io.ReadAll(r.Body)
		v, _ := url.ParseQuery(string(b))
		info, ok := known[v.Get("access_token")]
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"invalid_token"}`)
			return
		}
		_ = json.NewEncoder(w).Encode(info)
	}))
	t.Cleanup(srv.Close)
	old := googleTokeninfo
	googleTokeninfo = srv.URL
	t.Cleanup(func() { googleTokeninfo = old })
	return &asked
}

const clientID = "123-abc.apps.googleusercontent.com"

func TestGoogleAccessTokensAreAskedAbout(t *testing.T) {
	asked := fakeTokeninfo(t, map[string]map[string]string{
		"ya29.person":  {"aud": clientID, "azp": clientID, "sub": "1", "email": "a@example.com", "email_verified": "true", "expires_in": "3000"},
		"ya29.other":   {"aud": "999-other.apps.googleusercontent.com", "sub": "2", "email": "b@example.com", "email_verified": "true", "expires_in": "3000"},
		"ya29.expired": {"aud": clientID, "sub": "3", "email": "c@example.com", "email_verified": "true", "expires_in": "0"},
		"ya29.unverif": {"aud": clientID, "sub": "4", "email": "d@example.com", "email_verified": "false", "expires_in": "3000"},
	})
	o, err := NewOIDC(GoogleIssuer, clientID)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id, err := o.Verify(ctx, "ya29.person")
	if err != nil || id.Name != "a@example.com" || id.Machine {
		t.Fatalf("person: %+v, %v", id, err)
	}
	if _, err := o.Verify(ctx, "ya29.person"); err != nil || asked.Load() != 1 {
		t.Errorf("a verified token should be believed from cache for a while; asked %d times", asked.Load())
	}
	for name, tok := range map[string]string{
		"another application's token":  "ya29.other",
		"an expired token":             "ya29.expired",
		"a token Google does not know": "ya29.forged",
	} {
		if _, err := o.Verify(ctx, tok); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	// An email Google will not vouch for is not a name (0116).
	if id, err := o.Verify(ctx, "ya29.unverif"); err != nil || id.Name != "4" || !id.Machine {
		t.Errorf("unverified email: %+v, %v", id, err)
	}
}

func TestOnlyGoogleIsAskedAboutAnOpaqueToken(t *testing.T) {
	asked := fakeTokeninfo(t, map[string]map[string]string{})
	o, err := NewOIDC("https://issuer.example.com", "https://ochakai.example.com/mcp")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.Verify(context.Background(), "ya29.whatever"); err == nil {
		t.Error("an opaque token was accepted by a JWT issuer's deployment")
	}
	if asked.Load() != 0 {
		t.Error("a non-Google deployment asked Google about a token")
	}
}

func TestGoogleDiscoveryNamesTheHostItWasReachedAt(t *testing.T) {
	d := NewDiscovery(&config.Config{OIDCIssuer: GoogleIssuer, OIDCAudience: clientID})
	if d == nil || d.Path() != "/.well-known/oauth-protected-resource/mcp" {
		t.Fatalf("discovery = %+v", d)
	}
	mux := http.NewServeMux()
	d.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "https://ochakai-1.run.app/.well-known/oauth-protected-resource/mcp", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var doc map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &doc)
	if doc["resource"] != "https://ochakai-1.run.app/mcp" || !strings.Contains(rec.Body.String(), `"authorization_servers":["https://accounts.google.com"]`) ||
		!strings.Contains(rec.Body.String(), `"scopes_supported":["openid","email"]`) {
		t.Errorf("document = %s", rec.Body)
	}

	h := d.Challenge(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "https://ochakai-1.run.app/mcp", nil))
	want := `Bearer resource_metadata="https://ochakai-1.run.app/.well-known/oauth-protected-resource/mcp", scope="openid email"`
	if got := rec.Header().Get("WWW-Authenticate"); got != want {
		t.Errorf("challenge = %q, want %q", got, want)
	}
}
