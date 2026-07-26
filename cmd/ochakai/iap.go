package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/api/idtoken"
)

// IAP puts a signed assertion of the end user on every request it lets
// through. It is the only statement about who is driving the browser
// that serve-ui can trust: the browser itself can set any header it
// likes, and the proxy is the thing that decides whose name a write
// carries (design doc 0032).
const iapAssertionHeader = "X-Goog-IAP-JWT-Assertion"

// iapIssuer is the only issuer whose signature means "IAP admitted this
// user". idtoken.Validate checks the signature, the audience and the
// expiry, but not the issuer, and a Google-signed token with the right
// audience could otherwise come from somewhere else.
const iapIssuer = "https://cloud.google.com/iap"

// iapVerifier turns an IAP assertion into the end user's identity.
// Mirrors serviceTokenSource: an interface so serve-ui can be tested
// without reaching Google's key endpoint.
type iapVerifier interface {
	// identity returns the email IAP vouched for, or an error the proxy
	// must refuse the request with.
	identity(ctx context.Context, assertion string) (string, error)
}

// googleIAP verifies against Google's published IAP keys (ES256; the
// fetch and its cache live in idtoken).
type googleIAP struct{ audience string }

func (g *googleIAP) identity(ctx context.Context, assertion string) (string, error) {
	if assertion == "" {
		// Configured for IAP but nothing signed the request: either IAP
		// is not actually in front of this service, or the caller
		// reached it another way. Both mean the proxy cannot say who
		// this is, and guessing is how provenance silently collapses
		// (design doc 0027 §5.2).
		return "", fmt.Errorf("no %s header: is IAP actually in front of this service?", iapAssertionHeader)
	}
	payload, err := idtoken.Validate(ctx, assertion, g.audience)
	if err != nil {
		return "", fmt.Errorf("%w (configured audience %q, token audience %q)",
			err, g.audience, unverifiedAudience(assertion))
	}
	if payload.Issuer != iapIssuer {
		return "", fmt.Errorf("issuer is %q, want %q", payload.Issuer, iapIssuer)
	}
	email, _ := payload.Claims["email"].(string)
	if email == "" {
		return "", fmt.Errorf("assertion has no email claim")
	}
	return email, nil
}

// unverifiedAudience reads the aud claim without checking anything, for
// one purpose: telling an operator whose audience does not match what
// to set OCHAKAI_IAP_AUDIENCE to. Never use it to make a decision.
func unverifiedAudience(assertion string) string {
	parts := strings.Split(assertion, ".")
	if len(parts) != 3 {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Audience string `json:"aud"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		return ""
	}
	return claims.Audience
}
