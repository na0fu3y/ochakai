package apiclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/idtoken"
)

// tokenSource resolves a Google ID token source for the audience (design
// doc 0004 §4): ADC first (service accounts, metadata server,
// impersonation — audience-bound tokens; no gcloud binary needed), then
// the gcloud CLI. User ADC cannot mint audience-bound ID tokens, but
// Cloud Run IAM accepts gcloud's user ID tokens. The second return is
// the human-readable auth path, shown by `ochakai whoami`.
func tokenSource(ctx context.Context, audience string) (oauth2.TokenSource, string, error) {
	if ts, err := idtoken.NewTokenSource(ctx, audience); err == nil {
		return ts, "service-account ADC", nil
	}
	if _, err := exec.LookPath("gcloud"); err != nil {
		return nil, "", fmt.Errorf("no Google credentials for %s: need service-account ADC or the gcloud CLI (run `gcloud auth login`)", audience)
	}
	return oauth2.ReuseTokenSource(nil, gcloudSource{}), "gcloud", nil
}

// BigQueryReadOnly is the scope a query token is asked for where the
// credential honours scopes. A gcloud user token does not — it carries
// cloud-platform — so a caller that must stay read-only checks what it
// runs rather than trusting the scope (cmd/ochakai, `ochakai ui`).
const BigQueryReadOnly = "https://www.googleapis.com/auth/bigquery.readonly"

// QueryTokenSource resolves an OAuth access token for calling BigQuery as
// the same principal tokenSource names to ochakai, in the same order:
// service-account ADC first, then the gcloud CLI. Resolving them in the
// same order is what keeps a query from running as somebody other than
// the name `ochakai ui` printed at start.
func QueryTokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	if _, err := idtoken.NewTokenSource(ctx, "https://bigquery.googleapis.com"); err == nil {
		return google.DefaultTokenSource(ctx, BigQueryReadOnly)
	}
	if _, err := exec.LookPath("gcloud"); err != nil {
		return nil, errors.New("no Google credentials to run a query with: need service-account ADC or the gcloud CLI (run `gcloud auth login`)")
	}
	return oauth2.ReuseTokenSource(nil, gcloudAccessSource{}), nil
}

// gcloudAccessSource is gcloudSource's access-token twin. gcloud does not
// say when the token expires, so it is reused for well under the hour
// Google issues it for.
type gcloudAccessSource struct{}

func (gcloudAccessSource) Token() (*oauth2.Token, error) {
	out, err := exec.Command("gcloud", "auth", "print-access-token").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("gcloud auth print-access-token: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("gcloud auth print-access-token: %w", err)
	}
	return &oauth2.Token{AccessToken: strings.TrimSpace(string(out)), Expiry: time.Now().Add(30 * time.Minute)}, nil
}

type gcloudSource struct{}

func (gcloudSource) Token() (*oauth2.Token, error) {
	out, err := exec.Command("gcloud", "auth", "print-identity-token").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("gcloud auth print-identity-token: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("gcloud auth print-identity-token: %w", err)
	}
	tok := strings.TrimSpace(string(out))
	return &oauth2.Token{AccessToken: tok, Expiry: jwtExpiry(tok)}, nil
}

// jwtClaims holds the payload claims the client reads from a
// (Google-signed, already trusted) token — decoded, never verified.
type jwtClaims struct {
	Exp   int64  `json:"exp"`
	Email string `json:"email"`
}

// parseJWTClaims decodes a token's payload; ok is false when it does not
// parse as a JWT.
func parseJWTClaims(tok string) (jwtClaims, bool) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return jwtClaims{}, false
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return jwtClaims{}, false
	}
	var claims jwtClaims
	if err := json.Unmarshal(data, &claims); err != nil {
		return jwtClaims{}, false
	}
	return claims, true
}

// jwtExpiry reads exp from the token so ReuseTokenSource refreshes on
// time; unparseable tokens get a short TTL.
func jwtExpiry(tok string) time.Time {
	if claims, ok := parseJWTClaims(tok); ok && claims.Exp > 0 {
		return time.Unix(claims.Exp, 0)
	}
	return time.Now().Add(5 * time.Minute)
}

// jwtEmail reads the email claim from the token; empty when absent or
// unparseable.
func jwtEmail(tok string) string {
	claims, _ := parseJWTClaims(tok)
	return claims.Email
}
