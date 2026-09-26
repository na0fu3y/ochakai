package httpauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GoogleIssuer is Google's OpenID Connect issuer. A deployment that names
// it (OCHAKAI_OIDC_ISSUER) with an OAuth client ID as its audience answers
// a client that signed its people in with Google Workspace — the Claude
// connector's case (design doc 0151).
const GoogleIssuer = "https://accounts.google.com"

// googleTokeninfo is where Google says whose an access token is. A
// variable so tests can stand in.
var googleTokeninfo = "https://oauth2.googleapis.com/tokeninfo"

// googleCacheFor bounds how long a verified access token is believed
// without asking again. Google's tokens live an hour; a person whose
// access is revoked should stop reaching the deployment within minutes,
// not at the end of it.
const googleCacheFor = 5 * time.Minute

// googleCacheMax bounds the cache. Past it the cache is emptied rather
// than managed: a deployment with that many distinct live tokens in five
// minutes pays one more round trip each, which is the cost of not
// writing an eviction policy.
const googleCacheMax = 10000

// googleAccess verifies Google's access tokens, which are not JWTs: an
// OAuth client that signs a person in with Google gets an opaque token,
// and only Google can say what it means. Asking needs no secret — the
// tokeninfo endpoint answers anyone holding the token — so a deployment
// that verifies this way still holds nothing to issue or rotate (0065,
// 0086 §2).
type googleAccess struct {
	audience string
	client   *http.Client

	mu    sync.Mutex
	cache map[string]googleVerdict
}

type googleVerdict struct {
	id      Identity
	byEmail bool
	until   time.Time
}

func newGoogleAccess(audience string, client *http.Client) *googleAccess {
	return &googleAccess{audience: audience, client: client, cache: map[string]googleVerdict{}}
}

// opaque reports whether a token is not a JWT and so is one only its
// issuer can read.
func opaque(token string) bool {
	return strings.Count(token, ".") != 2
}

// verify asks Google whose token this is, and accepts it when it was
// issued to this deployment's OAuth client, has not expired, and names a
// verified email — or, lacking one, is recorded by subject as the JWT
// path records it (0117).
func (g *googleAccess) verify(ctx context.Context, token string) (Identity, bool, error) {
	sum := sha256.Sum256([]byte(token))
	key := hex.EncodeToString(sum[:])
	now := time.Now()
	g.mu.Lock()
	if v, ok := g.cache[key]; ok && now.Before(v.until) {
		g.mu.Unlock()
		return v.id, v.byEmail, nil
	}
	g.mu.Unlock()

	// POST, not a query string: the token must not land in a URL, where
	// proxies and logs keep it.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, googleTokeninfo,
		strings.NewReader(url.Values{"access_token": {token}}.Encode()))
	if err != nil {
		return Identity{}, false, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := g.client.Do(req)
	if err != nil {
		return Identity{}, false, fmt.Errorf("could not ask Google whose token this is: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Identity{}, false, fmt.Errorf("access token not recognized by Google (tokeninfo %d): it is expired, revoked or not Google's", resp.StatusCode)
	}
	var info struct {
		Aud           string `json:"aud"`
		Azp           string `json:"azp"`
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified string `json:"email_verified"`
		ExpiresIn     string `json:"expires_in"`
	}
	if err := json.NewDecoder(&limitedReader{r: resp.Body, n: 1 << 16}).Decode(&info); err != nil {
		return Identity{}, false, fmt.Errorf("could not read Google's answer about the token: %w", err)
	}
	// The client the token was issued to. Without this check a token
	// Google issued to any other application — anybody's — would be
	// accepted here: the confused deputy 0086 §2 refuses.
	if info.Aud != g.audience && info.Azp != g.audience {
		return Identity{}, false, fmt.Errorf("token rejected: it was issued to OAuth client %q, and this deployment answers to %q", info.Aud, g.audience)
	}
	secs, _ := strconv.Atoi(info.ExpiresIn)
	if secs <= 0 {
		return Identity{}, false, fmt.Errorf("token rejected: expired")
	}
	var verified *bool
	if info.EmailVerified != "" {
		v := info.EmailVerified == "true"
		verified = &v
	}
	id, byEmail := identityFrom(info.Email, verified, info.Sub)
	until := now.Add(min(googleCacheFor, time.Duration(secs)*time.Second))
	g.mu.Lock()
	if len(g.cache) >= googleCacheMax {
		g.cache = map[string]googleVerdict{}
	}
	g.cache[key] = googleVerdict{id: id, byEmail: byEmail, until: until}
	g.mu.Unlock()
	return id, byEmail, nil
}
