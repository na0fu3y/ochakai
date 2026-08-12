package apiclient

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/oauth2"
)

func fakeJWT(payload string) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"none"}`)) + "." + enc([]byte(payload)) + ".sig"
}

func TestJWTEmail(t *testing.T) {
	if got := jwtEmail(fakeJWT(`{"email":"na0@example.com","exp":1}`)); got != "na0@example.com" {
		t.Errorf("email = %q", got)
	}
	if got := jwtEmail("not-a-jwt"); got != "" {
		t.Errorf("garbage token yielded %q", got)
	}
}

func TestIdentityPlainHTTPIsAnonymous(t *testing.T) {
	c, err := New(context.Background(), "http://localhost:1")
	if err != nil {
		t.Fatal(err)
	}
	actor, auth, err := c.Identity()
	if err != nil || actor != "human:anonymous" || auth != "plain http, no credentials" {
		t.Errorf("actor = %q, auth = %q, err = %v", actor, auth, err)
	}
}

// withoutCredentials makes credential resolution fail for one test, the
// way it fails on a machine with no service-account ADC and no gcloud.
func withoutCredentials(t *testing.T) error {
	t.Helper()
	boom := errors.New("no Google credentials for the audience: need service-account ADC or the gcloud CLI (run `gcloud auth login`)")
	saved := resolveTokenSource
	resolveTokenSource = func(context.Context, string) (oauth2.TokenSource, string, error) {
		return nil, "", boom
	}
	t.Cleanup(func() { resolveTokenSource = saved })
	return boom
}

// A caller with no Google credentials must still get a client for an
// https server: whether the deployment wants an identity is the server's
// answer, and a public demo (design doc 0066 §3) wants none.
func TestHTTPSWithoutCredentialsStillBuildsAnAnonymousClient(t *testing.T) {
	withoutCredentials(t)
	c, err := New(context.Background(), "https://demo.example/")
	if err != nil {
		t.Fatalf("New refused to build a client: %v", err)
	}
	if c.tokens != nil || c.TokenSource() != nil {
		t.Error("client carries a token source it could not resolve")
	}
	actor, auth, err := c.Identity()
	if err != nil || actor != "human:anonymous" || auth != "no Google credentials found" {
		t.Errorf("actor = %q, auth = %q, err = %v", actor, auth, err)
	}
}

// The public posture never answers 401 and reads no identity, so the
// request goes out bare and succeeds — the whole point of the deferral.
func TestPublicServerAnswersAnUncredentialedClient(t *testing.T) {
	withoutCredentials(t)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("sent Authorization %q; want none", got)
		}
		_, _ = w.Write([]byte(`{"hits":[]}`))
	}))
	defer srv.Close()

	c, err := New(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	c.http = srv.Client()
	if _, err := c.Search(context.Background(), SearchParams{Query: "revenue"}); err != nil {
		t.Errorf("public server refused an anonymous search: %v", err)
	}
}

// withUnmintableCredentials makes credential resolution *succeed* and
// minting fail, the way it fails on a machine that has gcloud but whose
// session wants reauthentication, has no account selected, or is being
// asked for one in a shell that cannot answer. The source is resolvable,
// so New keeps it; the failure arrives one call later, at send time.
func withUnmintableCredentials(t *testing.T) error {
	t.Helper()
	boom := errors.New("gcloud auth print-identity-token: ERROR: Reauthentication failed. cannot prompt during non-interactive execution")
	saved := resolveTokenSource
	resolveTokenSource = func(context.Context, string) (oauth2.TokenSource, string, error) {
		return failingTokenSource{boom}, "gcloud", nil
	}
	t.Cleanup(func() { resolveTokenSource = saved })
	return boom
}

type failingTokenSource struct{ err error }

func (s failingTokenSource) Token() (*oauth2.Token, error) { return nil, s.err }

// Credentials that cannot be minted are credentials this client will not
// send, so it presents what it will actually be: anonymous, reported the
// same way as a machine that never had any. Naming gcloud here would name
// a path the request does not take.
func TestUnmintableCredentialsPresentAsAnonymous(t *testing.T) {
	withUnmintableCredentials(t)
	c, err := New(context.Background(), "https://demo.example/")
	if err != nil {
		t.Fatal(err)
	}
	actor, auth, err := c.Identity()
	if err != nil || actor != "human:anonymous" || auth != "no Google credentials found" {
		t.Errorf("actor = %q, auth = %q, err = %v", actor, auth, err)
	}
}

// The public demo the README opens with: no identity is read and none is
// wanted, so a broken gcloud session must not be the thing that decides
// the caller cannot have it. The deferral covers both shapes of having
// no credentials, not only the machine that never had any.
func TestPublicServerAnswersAClientWhoseTokenWillNotMint(t *testing.T) {
	withUnmintableCredentials(t)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("sent Authorization %q; want none", got)
		}
		_, _ = w.Write([]byte(`{"hits":[]}`))
	}))
	defer srv.Close()

	c, err := New(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	c.http = srv.Client()
	if _, err := c.Search(context.Background(), SearchParams{Query: "revenue"}); err != nil {
		t.Errorf("public server refused a caller whose token would not mint: %v", err)
	}
}

// And where an identity *was* wanted, the minting failure is the answer
// to "why did that go out bare?" — it reaches the caller at the 401, with
// the remedy (`gcloud auth login`) in the text gcloud itself wrote.
func TestUnauthorizedExplainsAnUnmintableToken(t *testing.T) {
	boom := withUnmintableCredentials(t)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c, err := New(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	c.http = srv.Client()
	if err := c.Health(context.Background()); !errors.Is(err, boom) {
		t.Errorf("401 did not carry the minting failure: %v", err)
	}
}

// A token that mints is still attached — the fallback is for the failure,
// not a quiet retreat from authenticating.
func TestMintableCredentialsAreStillSent(t *testing.T) {
	saved := resolveTokenSource
	resolveTokenSource = func(context.Context, string) (oauth2.TokenSource, string, error) {
		return oauth2.StaticTokenSource(&oauth2.Token{
			AccessToken: fakeJWT(`{"email":"na0@example.com"}`),
			TokenType:   "Bearer",
		}), "gcloud", nil
	}
	t.Cleanup(func() { resolveTokenSource = saved })

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got == "" {
			t.Error("sent no Authorization header")
		}
		_, _ = w.Write([]byte(`{"hits":[]}`))
	}))
	defer srv.Close()

	c, err := New(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	c.http = srv.Client()
	if _, err := c.Search(context.Background(), SearchParams{Query: "revenue"}); err != nil {
		t.Fatal(err)
	}
	if actor, auth, err := c.Identity(); err != nil || actor != "human:na0@example.com" || auth != "gcloud" {
		t.Errorf("actor = %q, auth = %q, err = %v", actor, auth, err)
	}
}

// A server that does want an identity says so with 401, and only then
// does the caller hear why the request went out without one.
func TestUnauthorizedExplainsTheMissingCredentials(t *testing.T) {
	boom := withoutCredentials(t)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Cloud Run rejects ahead of the container: HTML, not JSON.
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("<html><title>401</title></html>"))
	}))
	defer srv.Close()

	c, err := New(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	c.http = srv.Client()
	err = c.Health(context.Background())
	if !errors.Is(err, boom) {
		t.Errorf("401 did not carry the credential reason: %v", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("401 did not stay inspectable as an APIError: %v", err)
	}
}

// A 403 is a caller IAM refuses, not a caller with nothing to present;
// nothing about credentials belongs in that message.
func TestForbiddenIsNotBlamedOnCredentials(t *testing.T) {
	boom := withoutCredentials(t)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c, err := New(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	c.http = srv.Client()
	if err := c.Health(context.Background()); errors.Is(err, boom) {
		t.Errorf("403 blamed on missing credentials: %v", err)
	}
}

func TestIdentityPrefixesActors(t *testing.T) {
	for email, want := range map[string]string{
		"someone@example.com":                 "human:someone@example.com",
		"robot@proj.iam.gserviceaccount.com":  "process:robot@proj.iam.gserviceaccount.com",
		"ochakai@appspot.gserviceaccount.com": "process:ochakai@appspot.gserviceaccount.com",
	} {
		c := &Client{
			tokens: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: fakeJWT(`{"email":"` + email + `"}`)}),
			auth:   "service-account ADC",
		}
		actor, _, err := c.Identity()
		if err != nil || actor != want {
			t.Errorf("Identity(%s) = %q, %v; want %q", email, actor, err, want)
		}
	}
}

func TestHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	c, err := New(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Health(context.Background()); err != nil {
		t.Errorf("healthy server: %v", err)
	}

	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer broken.Close()
	c, err = New(context.Background(), broken.URL)
	if err != nil {
		t.Fatal(err)
	}
	var apiErr *APIError
	if err := c.Health(context.Background()); !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusForbidden {
		t.Errorf("want 403 APIError, got %v", err)
	}
}
