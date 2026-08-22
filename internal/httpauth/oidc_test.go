package httpauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/na0fu3y/ochakai/internal/config"
	"github.com/na0fu3y/ochakai/internal/domain"
)

// fakeIssuer is an OpenID Connect issuer: a discovery document, a key
// set, and a signer for minting the tokens a test wants verified.
type fakeIssuer struct {
	url    string
	key    *rsa.PrivateKey
	kid    string
	signer jose.Signer
	client *http.Client
}

func newFakeIssuer(t *testing.T) *fakeIssuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	iss := &fakeIssuer{key: key, kid: "test-key-1"}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer": iss.url, "jwks_uri": iss.url + "/jwks",
		})
	})
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
			Key: key.Public(), KeyID: iss.kid, Algorithm: string(jose.RS256), Use: "sig",
		}}})
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	iss.url = srv.URL
	iss.signer, err = jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", iss.kid))
	if err != nil {
		t.Fatal(err)
	}
	// The verifier must reach a server with a test certificate.
	iss.client = srv.Client()
	return iss
}

func (f *fakeIssuer) token(t *testing.T, claims any) string {
	t.Helper()
	s, err := jwt.Signed(f.signer).Claims(claims).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// testAudience is what these deployments answer to; a token minted for
// anything else is one of the refusals below.
const testAudience = "ochakai"

// verifierFor points an OIDC verifier at the fake issuer, over the test
// server's own client so its certificate is trusted.
func verifierFor(t *testing.T, iss *fakeIssuer) *OIDC {
	t.Helper()
	v, err := NewOIDC(strings.Replace(iss.url, "http://", "https://", 1), testAudience)
	if err != nil {
		t.Fatal(err)
	}
	v.issuer = iss.url // the fake speaks https through a test certificate
	v.client = iss.client
	return v
}

func standardClaims(iss *fakeIssuer, audience string) jwt.Claims {
	now := time.Now()
	return jwt.Claims{
		Issuer:   iss.url,
		Subject:  "user-1",
		Audience: jwt.Audience{audience},
		Expiry:   jwt.NewNumericDate(now.Add(time.Hour)),
		IssuedAt: jwt.NewNumericDate(now),
	}
}

func TestOIDCVerifiesAToken(t *testing.T) {
	iss := newFakeIssuer(t)
	v := verifierFor(t, iss)
	token := iss.token(t, struct {
		jwt.Claims
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}{standardClaims(iss, "ochakai"), "tanaka@example.co.jp", true})

	id, err := v.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("a token this issuer signed for this audience should verify: %v", err)
	}
	if id.Name != "tanaka@example.co.jp" || id.Machine {
		t.Errorf("identity = %+v, want the verified email as a person", id)
	}
}

// Each of these is a token somebody could present, and each has to be
// refused for its own reason. A verifier that accepts any of them is not
// authenticating anything.
func TestOIDCRefusals(t *testing.T) {
	iss := newFakeIssuer(t)
	other := newFakeIssuer(t)

	for name, tc := range map[string]struct {
		token func() string
		want  string
	}{
		"another audience": {
			token: func() string { return iss.token(t, standardClaims(iss, "some-other-service")) },
			want:  "audience",
		},
		"another issuer": {
			// Signed by a key this deployment never trusted.
			token: func() string {
				c := standardClaims(other, "ochakai")
				c.Issuer = iss.url // claims to be ours; the signature is not
				return other.token(t, c)
			},
			want: "signature",
		},
		"expired": {
			token: func() string {
				c := standardClaims(iss, "ochakai")
				c.Expiry = jwt.NewNumericDate(time.Now().Add(-2 * time.Hour))
				return iss.token(t, c)
			},
			want: "expired",
		},
		"unsigned": {
			// alg=none, the classic forgery: header and payload with an
			// empty signature.
			token: func() string {
				return "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0." +
					"eyJpc3MiOiJodHRwczovL2V4YW1wbGUiLCJhdWQiOiJvY2hha2FpIn0."
			},
			want: "not a signed JWT",
		},
		"empty": {
			token: func() string { return "" },
			want:  "no bearer token",
		},
	} {
		t.Run(name, func(t *testing.T) {
			v := verifierFor(t, iss)
			_, err := v.Verify(context.Background(), tc.token())
			if err == nil {
				t.Fatalf("%s was accepted", name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not say %q, which is what an operator acts on", err, tc.want)
			}
		})
	}
}

// An issuer that will not vouch for an address has said the claim is the
// user's own text. Provenance built on that is a name anybody can take,
// so the subject is recorded instead.
func TestOIDCDoesNotBelieveAnUnverifiedEmail(t *testing.T) {
	iss := newFakeIssuer(t)
	v := verifierFor(t, iss)
	token := iss.token(t, struct {
		jwt.Claims
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}{standardClaims(iss, "ochakai"), "someone@example.com", false})

	id, err := v.Verify(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	if id.Name != "user-1" || !id.Machine {
		t.Errorf("identity = %+v, want the subject rather than an email nobody vouched for", id)
	}
}

// A token with no email at all — a machine's — is recorded by subject and
// as a process, so provenance never reads as a person who does not exist.
func TestOIDCRecordsAMachineBySubject(t *testing.T) {
	iss := newFakeIssuer(t)
	v := verifierFor(t, iss)
	id, err := v.Verify(context.Background(), iss.token(t, standardClaims(iss, "ochakai")))
	if err != nil {
		t.Fatal(err)
	}
	if id.Name != "user-1" || !id.Machine {
		t.Errorf("identity = %+v, want the subject as a process", id)
	}
}

// The pair is refused where it is written, so a deployment cannot come up
// half-configured — an issuer with no audience accepts tokens minted for
// other services.
func TestNewOIDCRefusesAHalfConfiguredPair(t *testing.T) {
	for name, args := range map[string][2]string{
		"no audience": {"https://issuer.example", ""},
		"no issuer":   {"", "ochakai"},
		"plain http":  {"http://issuer.example", "ochakai"},
	} {
		if _, err := NewOIDC(args[0], args[1]); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// The verified identity is what the request is recorded as, and an
// unverifiable one never reaches a handler: the check that used to
// happen in front of the process now happens in the middleware.
func TestMiddlewareAuthenticatesWithTheVerifier(t *testing.T) {
	iss := newFakeIssuer(t)
	cfg := &config.Config{Verifier: verifierFor(t, iss)}

	var seen domain.Actor
	h := Middleware(cfg, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = Actor(r.Context())
	}))

	token := iss.token(t, struct {
		jwt.Claims
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}{standardClaims(iss, "ochakai"), "tanaka@example.co.jp", true})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=x", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("a verified caller got %d: %s", rec.Code, rec.Body)
	}
	if seen.Name != "tanaka@example.co.jp" || seen.Kind != domain.ActorHuman {
		t.Errorf("recorded actor = %+v", seen)
	}

	// And the same request without a token gets no further.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/search?q=x", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("an unauthenticated caller got %d, want 401", rec.Code)
	}
}

// The two paths are the whole of design doc 0086, so each is pinned
// against the other: with no verifier the claims are read as Cloud Run
// vouched for them, and with one the same unsigned token is refused.
// A regression that dropped the verifier would otherwise look like a
// working deployment.
func TestVerifierDecidesWhetherATokenIsCheckedAtAll(t *testing.T) {
	// A token nobody signed, naming somebody.
	unsigned := "x." + base64.RawURLEncoding.EncodeToString(
		[]byte(`{"email":"impostor@example.com"}`)) + ".y"

	cloudRun := &config.Config{} // no verifier: the check happened in front
	actor, err := callerFrom(cloudRun, unsigned)
	if err != nil || actor.Name != "impostor@example.com" {
		t.Fatalf("the Cloud Run path reads the claims it was handed: %+v, %v", actor, err)
	}

	iss := newFakeIssuer(t)
	own := &config.Config{Verifier: verifierFor(t, iss)}
	if _, err := callerFrom(own, unsigned); err == nil {
		t.Error("a deployment that verifies its own tokens accepted an unsigned one")
	}
}

// Design doc 0116: a token with no verified email records a person as a
// process, and nothing about the response says so — so the process says
// it, once. Once is the whole design: a machine's token has the same
// shape, so the correct configuration raises this too, and an alarm on
// every request is one nobody reads.
func TestATokenWithNoEmailSaysWhoItIsRecordingAsOnce(t *testing.T) {
	iss := newFakeIssuer(t)
	v := verifierFor(t, iss)

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	for range 2 {
		id, err := v.Verify(context.Background(), iss.token(t, standardClaims(iss, testAudience)))
		if err != nil {
			t.Fatalf("a token this issuer signed should verify: %v", err)
		}
		if id.Name != "user-1" || !id.Machine {
			t.Fatalf("identity = %+v, want the subject as a process (0086 §4)", id)
		}
	}

	if got := strings.Count(buf.String(), "carried no email"); got != 1 {
		t.Errorf("warned %d times, want exactly 1:\n%s", got, buf.String())
	}
	if !strings.Contains(buf.String(), "subject=user-1") {
		t.Errorf("the warning names no subject, so nobody can act on it:\n%s", buf.String())
	}
}

// The same verifier says nothing when the issuer vouched for an email:
// that is the ordinary case, and a warning there would be the alarm that
// always rings.
func TestAVerifiedEmailWarnsAboutNothing(t *testing.T) {
	iss := newFakeIssuer(t)
	v := verifierFor(t, iss)

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	token := iss.token(t, struct {
		jwt.Claims
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}{standardClaims(iss, testAudience), "tanaka@example.co.jp", true})
	if _, err := v.Verify(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Errorf("warned about a verified email:\n%s", buf.String())
	}
}
