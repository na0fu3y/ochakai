package connector

import (
	"errors"
	"net/http"
	"time"

	"github.com/na0fu3y/ochakai/internal/store"
)

// handleToken is the RFC 6749 token endpoint. claude.ai sends
// application/x-www-form-urlencoded and expects RFC-compliant error
// codes (a refresh failure must be invalid_grant, or the client won't
// fall back to a fresh authorization).
func (s *server) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		tokenError(w, http.StatusBadRequest, "invalid_request", "body must be application/x-www-form-urlencoded")
		return
	}
	if resource := r.PostForm.Get("resource"); resource != "" && resource != s.cfg.PublicURL+"/mcp" {
		tokenError(w, http.StatusBadRequest, "invalid_target", "unknown resource")
		return
	}
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		s.tokenFromCode(w, r)
	case "refresh_token":
		s.tokenFromRefresh(w, r)
	default:
		tokenError(w, http.StatusBadRequest, "unsupported_grant_type", "use authorization_code or refresh_token")
	}
}

func (s *server) tokenFromCode(w http.ResponseWriter, r *http.Request) {
	code, err := s.store.TakeOAuthCode(r.Context(), hashToken(r.PostForm.Get("code")))
	if err != nil {
		tokenError(w, http.StatusBadRequest, "invalid_grant", "unknown, expired, or already used code")
		return
	}
	// The code was consumed above, so a failed check can't be retried
	// with corrected parameters — exactly the single-use semantics OAuth
	// wants for leaked codes.
	if clientID := r.PostForm.Get("client_id"); clientID != code.ClientID {
		tokenError(w, http.StatusBadRequest, "invalid_grant", "code was issued to a different client")
		return
	}
	if redirect := r.PostForm.Get("redirect_uri"); redirect != code.RedirectURI {
		tokenError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri does not match the authorization request")
		return
	}
	if !verifierMatches(code.CodeChallenge, r.PostForm.Get("code_verifier")) {
		tokenError(w, http.StatusBadRequest, "invalid_grant", "PKCE verification failed")
		return
	}

	access, refresh := newToken(), newToken()
	now := time.Now()
	err = s.store.CreateOAuthGrant(r.Context(), store.OAuthGrant{
		ID:               newToken(),
		ClientID:         code.ClientID,
		ActorEmail:       code.ActorEmail,
		AccessHash:       hashToken(access),
		AccessExpiresAt:  now.Add(accessTTL),
		RefreshHash:      hashToken(refresh),
		RefreshExpiresAt: now.Add(refreshTTL),
	})
	if err != nil {
		s.log.Error("connector: store grant", "error", err)
		tokenError(w, http.StatusInternalServerError, "server_error", "could not persist the grant")
		return
	}
	s.log.Info("connector: token issued", "actor", code.ActorEmail, "client_id", code.ClientID)
	writeTokens(w, access, refresh)
}

func (s *server) tokenFromRefresh(w http.ResponseWriter, r *http.Request) {
	presented := r.PostForm.Get("refresh_token")
	if presented == "" {
		tokenError(w, http.StatusBadRequest, "invalid_request", "refresh_token is required")
		return
	}
	access, refresh := newToken(), newToken()
	g, err := s.store.RotateOAuthGrant(r.Context(), hashToken(presented),
		hashToken(access), time.Now().Add(accessTTL), hashToken(refresh))
	switch {
	case errors.Is(err, store.ErrOAuthReuse):
		// OAuth 2.1: a rotated-out refresh token coming back means the
		// token may be in two hands; the whole grant is revoked and the
		// user signs in again.
		s.log.Warn("connector: refresh token reuse; grant revoked")
		tokenError(w, http.StatusBadRequest, "invalid_grant", "refresh token reuse detected; the grant has been revoked")
		return
	case errors.Is(err, store.ErrNotFound):
		tokenError(w, http.StatusBadRequest, "invalid_grant", "unknown or expired refresh token")
		return
	case err != nil:
		s.log.Error("connector: rotate grant", "error", err)
		tokenError(w, http.StatusInternalServerError, "server_error", "could not rotate the grant")
		return
	}
	s.log.Info("connector: token refreshed", "actor", g.ActorEmail, "client_id", g.ClientID)
	writeTokens(w, access, refresh)
}

// writeTokens returns the pair; the new refresh token rides the same
// response that invalidated the old one (claude.ai requirement).
func writeTokens(w http.ResponseWriter, access, refresh string) {
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  access,
		"token_type":    "Bearer",
		"expires_in":    advertisedTTL,
		"refresh_token": refresh,
		"scope":         scopes,
	})
}

func tokenError(w http.ResponseWriter, status int, code, description string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": description})
}
