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
	"google.golang.org/api/idtoken"
)

// tokenSource resolves a Google ID token source for the audience (design
// doc 0004 §4): ADC first (service accounts, metadata server,
// impersonation — audience-bound tokens; no gcloud binary needed), then
// the gcloud CLI. User ADC cannot mint audience-bound ID tokens, but
// Cloud Run IAM accepts gcloud's user ID tokens.
func tokenSource(ctx context.Context, audience string) (oauth2.TokenSource, error) {
	if ts, err := idtoken.NewTokenSource(ctx, audience); err == nil {
		return ts, nil
	}
	if _, err := exec.LookPath("gcloud"); err != nil {
		return nil, fmt.Errorf("no Google credentials for %s: need service-account ADC or the gcloud CLI (run `gcloud auth login`)", audience)
	}
	return oauth2.ReuseTokenSource(nil, gcloudSource{}), nil
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

// jwtExpiry reads exp from the (Google-signed, already trusted) token so
// ReuseTokenSource refreshes on time; unparseable tokens get a short TTL.
func jwtExpiry(tok string) time.Time {
	if parts := strings.Split(tok, "."); len(parts) == 3 {
		if data, err := base64.RawURLEncoding.DecodeString(parts[1]); err == nil {
			var claims struct {
				Exp int64 `json:"exp"`
			}
			if json.Unmarshal(data, &claims) == nil && claims.Exp > 0 {
				return time.Unix(claims.Exp, 0)
			}
		}
	}
	return time.Now().Add(5 * time.Minute)
}
