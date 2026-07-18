package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"google.golang.org/api/idtoken"
)

// googleOIDC delegates interactive login to Google using the
// organization's single pre-registered OAuth client. Unlike httpauth
// (private service, IAM-verified upstream), the connector runs publicly,
// so the returned id_token is verified against Google's JWKS.
type googleOIDC struct {
	clientID     string
	clientSecret string
	redirectURI  string

	// Test seams; production uses Google's endpoints and validator.
	authEndpoint  string
	tokenEndpoint string
	validate      func(ctx context.Context, token, audience string) (*idtoken.Payload, error)
}

const (
	googleAuthEndpoint  = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenEndpoint = "https://oauth2.googleapis.com/token"
)

// authURL builds the Google authorization redirect. state is the pending
// request ID; hd is a UX hint only — the authoritative domain check is
// on the verified id_token claim in verifiedEmail.
func (g *googleOIDC) authURL(state, hd string) string {
	endpoint := g.authEndpoint
	if endpoint == "" {
		endpoint = googleAuthEndpoint
	}
	q := url.Values{
		"client_id":     {g.clientID},
		"redirect_uri":  {g.redirectURI},
		"response_type": {"code"},
		"scope":         {"openid email"},
		"state":         {state},
		"hd":            {hd},
	}
	return endpoint + "?" + q.Encode()
}

// verifiedEmail exchanges Google's authorization code and returns the
// signature-verified, domain-checked account email.
func (g *googleOIDC) verifiedEmail(ctx context.Context, code, allowedDomain string) (string, error) {
	endpoint := g.tokenEndpoint
	if endpoint == "" {
		endpoint = googleTokenEndpoint
	}
	form := url.Values{
		"code":          {code},
		"client_id":     {g.clientID},
		"client_secret": {g.clientSecret},
		"redirect_uri":  {g.redirectURI},
		"grant_type":    {"authorization_code"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("google token exchange: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("google token exchange returned %d", resp.StatusCode)
	}
	var tok struct {
		IDToken string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil || tok.IDToken == "" {
		return "", fmt.Errorf("google token exchange returned no id_token")
	}

	validate := g.validate
	if validate == nil {
		validate = idtoken.Validate // JWKS signature check + iss/aud/exp
	}
	payload, err := validate(ctx, tok.IDToken, g.clientID)
	if err != nil {
		return "", fmt.Errorf("id_token verification: %w", err)
	}
	email, _ := payload.Claims["email"].(string)
	verified, _ := payload.Claims["email_verified"].(bool)
	hd, _ := payload.Claims["hd"].(string)
	if email == "" || !verified {
		return "", fmt.Errorf("id_token has no verified email")
	}
	// hd is the authoritative Workspace-domain claim: consumer accounts
	// don't carry it, so this also rejects any @gmail.com login. The
	// email-suffix check is defense in depth.
	if hd != allowedDomain || !strings.HasSuffix(email, "@"+allowedDomain) {
		return "", fmt.Errorf("account %q is not in the allowed domain %q", email, allowedDomain)
	}
	return email, nil
}
