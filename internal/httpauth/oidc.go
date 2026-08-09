package httpauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// Off Google Cloud there were two postures and neither is a deployment:
// `dev`, which authenticates nobody, and `read-only`, which authenticates
// nobody and refuses every write. Everything in between rested on Cloud
// Run's IAM check having already happened in front of the process —
// which is why the token is parsed and not verified on that path.
//
// This is the second way to answer "who is calling", for a deployment
// that has an OpenID Connect issuer of its own: Entra, Okta, Auth0,
// Keycloak, GitHub Actions. The issuer's public keys verify the token,
// and **no secret is introduced** — signature verification reads public
// keys over HTTPS, so the property design docs 0065 and 0003 protect
// (nothing to issue, nothing to rotate) is intact. What changes is
// where the check happens: in front of the process on Cloud Run, inside
// it here.
//
// Two things this deliberately does not become. It is not
// authorization — whoever the issuer vouches for can still read and
// write everything, exactly as on Cloud Run (0065 §1). And it is not a
// second embedding provider: semantic search is Vertex AI or nothing
// (0080), so a deployment off Google Cloud is lexical-only, which since
// migration 0036 means Japanese terms are looked up in an index rather
// than scanned for — a smaller search, not a broken one.

// Verifier checks a bearer token and answers with the claims ochakai
// records. Config holds one when a deployment configured an issuer;
// nil means Cloud Run verified the caller before the process saw it.
type Verifier interface {
	// Verify returns the identity in a token it accepts, and an error
	// naming what failed otherwise. The error reaches the caller, so it
	// says what an operator can act on and nothing about key material.
	Verify(ctx context.Context, token string) (Identity, error)
}

// Identity is what a verified token yields: a name, and whether it
// belongs to a person or to a process.
type Identity struct {
	Name    string
	Machine bool
}

// OIDC verifies tokens against one issuer's published keys.
type OIDC struct {
	issuer   string
	audience string
	client   *http.Client

	mu      sync.Mutex
	keys    *jose.JSONWebKeySet
	fetched time.Time
	jwksURI string
}

// NewOIDC builds a verifier for an issuer and the audience this
// deployment answers to. Both are required: without the audience, a
// token the same issuer minted for a different service would be
// accepted here, which is the confused deputy the aud claim exists to
// prevent.
//
// Nothing is fetched yet. A deployment must start without depending on
// the issuer answering — the keys arrive with the first request that
// needs them, and an issuer that is down is a 401 with a reason rather
// than a container that will not boot.
func NewOIDC(issuer, audience string) (*OIDC, error) {
	if issuer == "" || audience == "" {
		return nil, fmt.Errorf("OIDC needs both an issuer and an audience")
	}
	if !strings.HasPrefix(issuer, "https://") {
		// http:// would make the keys spoofable by anyone on the path,
		// which is the whole of what this verifies with.
		return nil, fmt.Errorf("OIDC issuer must be https://, got %q", issuer)
	}
	return &OIDC{
		issuer:   strings.TrimSuffix(issuer, "/"),
		audience: audience,
		client:   &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// signatureAlgorithms is the allowlist. Asymmetric only: a symmetric
// algorithm verifies with the same secret it signs with, so accepting one
// would mean holding a secret — the thing this design refuses — and
// "none" is the classic forgery. go-jose requires the list, which is why
// the refusal is a declaration rather than a check somebody has to
// remember to write.
var signatureAlgorithms = []jose.SignatureAlgorithm{
	jose.RS256, jose.RS384, jose.RS512,
	jose.ES256, jose.ES384, jose.ES512,
	jose.PS256, jose.PS384, jose.PS512,
}

func (o *OIDC) Verify(ctx context.Context, token string) (Identity, error) {
	if token == "" {
		return Identity{}, fmt.Errorf("no bearer token (this deployment authenticates with OIDC issuer %s)", o.issuer)
	}
	parsed, err := jwt.ParseSigned(token, signatureAlgorithms)
	if err != nil {
		return Identity{}, fmt.Errorf("token is not a signed JWT this deployment accepts: %w", err)
	}
	keys, err := o.keySet(ctx, false)
	if err != nil {
		return Identity{}, err
	}
	var claims struct {
		jwt.Claims
		Email         string `json:"email"`
		EmailVerified *bool  `json:"email_verified"`
	}
	err = parsed.Claims(keys, &claims)
	if err != nil {
		// An unknown key is the ordinary case, not an attack: issuers
		// rotate. Refetch once, then believe the answer.
		fresh, ferr := o.keySet(ctx, true)
		if ferr != nil {
			return Identity{}, ferr
		}
		if err = parsed.Claims(fresh, &claims); err != nil {
			return Identity{}, fmt.Errorf("token signature does not verify against %s's published keys", o.issuer)
		}
	}
	// Issuer, audience and expiry, with a minute of leeway for clock
	// skew — the same tolerance every OIDC library takes, and small
	// enough that an expired token is not usefully replayable.
	if err := claims.Validate(jwt.Expected{
		Issuer:      o.issuer,
		AnyAudience: jwt.Audience{o.audience},
		Time:        time.Now(),
	}); err != nil {
		return Identity{}, fmt.Errorf("token rejected: %w (this deployment expects issuer %q and audience %q)",
			err, o.issuer, o.audience)
	}
	return identityFrom(claims.Email, claims.EmailVerified, claims.Subject), nil
}

// identityFrom decides what a verified token is recorded as.
//
// An email the issuer says it verified is the name a person is known by,
// and it reads the same way as the Cloud Run path's. An unverified email
// is not used at all: an issuer that will not vouch for the address has
// told us the claim is the user's own text, and provenance built on that
// is a name anybody can take. Everything else is recorded by subject,
// which is what a machine's token carries.
func identityFrom(email string, verified *bool, subject string) Identity {
	if email != "" && (verified == nil || *verified) {
		// Absent email_verified is treated as verified: several issuers
		// omit it for accounts they own outright, and refusing those
		// would leave the common enterprise case unauthenticatable.
		// A false one is never believed.
		return Identity{Name: email, Machine: strings.HasSuffix(email, ".gserviceaccount.com")}
	}
	return Identity{Name: subject, Machine: true}
}

// keySet returns the issuer's keys, fetching them when they are missing,
// stale, or when force says a rotation may have happened.
func (o *OIDC) keySet(ctx context.Context, force bool) (*jose.JSONWebKeySet, error) {
	const ttl = time.Hour
	o.mu.Lock()
	defer o.mu.Unlock()
	if !force && o.keys != nil && time.Since(o.fetched) < ttl {
		return o.keys, nil
	}
	// A forced refetch is rate-limited to once a minute: an unknown kid
	// is otherwise a way for anyone who can reach this deployment to
	// make it hammer the issuer, one request per forged token.
	if force && o.keys != nil && time.Since(o.fetched) < time.Minute {
		return o.keys, nil
	}
	if o.jwksURI == "" {
		uri, err := o.discover(ctx)
		if err != nil {
			return nil, err
		}
		o.jwksURI = uri
	}
	keys, err := o.fetchKeys(ctx, o.jwksURI)
	if err != nil {
		if o.keys != nil {
			// Keep serving with what we have: an issuer that is briefly
			// unreachable should not take authentication down with it.
			return o.keys, nil
		}
		return nil, err
	}
	o.keys, o.fetched = keys, time.Now()
	return o.keys, nil
}

func (o *OIDC) discover(ctx context.Context) (string, error) {
	var doc struct {
		Issuer  string `json:"issuer"`
		JWKSURI string `json:"jwks_uri"`
	}
	if err := o.getJSON(ctx, o.issuer+"/.well-known/openid-configuration", &doc); err != nil {
		return "", fmt.Errorf("reading %s's OpenID configuration: %w", o.issuer, err)
	}
	// The document has to agree about who it belongs to, or a redirect
	// has quietly moved which issuer this deployment trusts.
	if strings.TrimSuffix(doc.Issuer, "/") != o.issuer {
		return "", fmt.Errorf("%s's OpenID configuration names issuer %q", o.issuer, doc.Issuer)
	}
	if !strings.HasPrefix(doc.JWKSURI, "https://") {
		return "", fmt.Errorf("%s publishes a non-https jwks_uri", o.issuer)
	}
	return doc.JWKSURI, nil
}

func (o *OIDC) fetchKeys(ctx context.Context, uri string) (*jose.JSONWebKeySet, error) {
	keys := &jose.JSONWebKeySet{}
	if err := o.getJSON(ctx, uri, keys); err != nil {
		return nil, fmt.Errorf("reading %s's public keys: %w", o.issuer, err)
	}
	if len(keys.Keys) == 0 {
		return nil, fmt.Errorf("%s published no keys", o.issuer)
	}
	return keys, nil
}

func (o *OIDC) getJSON(ctx context.Context, url string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", url, resp.Status)
	}
	// Bounded: this is a document from a host the deployment named, but
	// a stuck or hostile response must not be read into memory forever.
	return json.NewDecoder(&limitedReader{r: resp.Body, n: 1 << 20}).Decode(into)
}

type limitedReader struct {
	r interface{ Read([]byte) (int, error) }
	n int64
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.n <= 0 {
		return 0, fmt.Errorf("document is larger than this deployment will read")
	}
	if int64(len(p)) > l.n {
		p = p[:l.n]
	}
	n, err := l.r.Read(p)
	l.n -= int64(n)
	return n, err
}
