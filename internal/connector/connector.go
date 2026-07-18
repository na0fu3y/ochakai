// Package connector implements the MCP OAuth connector service (design
// doc 0010): a minimal OAuth 2.1 authorization server that delegates
// login to Google, plus the token-guarded /mcp endpoint, so claude.ai /
// ChatGPT remote connectors (and Claude Code without a proxy) can reach
// ochakai.
//
// This is the only ochakai surface designed to run publicly reachable.
// Unlike httpauth (private service; claims trusted after Cloud Run IAM),
// every identity here is verified cryptographically: Google id_tokens by
// signature, ochakai tokens by hash lookup. OAuth remains reachability +
// provenance, not authorization — no roles, no scopes with meaning.
package connector

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/na0fu3y/ochakai/internal/config"
	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/httpauth"
	"github.com/na0fu3y/ochakai/internal/store"
)

const (
	pendingTTL    = 10 * time.Minute
	codeTTL       = 10 * time.Minute
	accessTTL     = time.Hour
	refreshTTL    = 30 * 24 * time.Hour
	advertisedTTL = int(accessTTL / time.Second)
)

// scopes exist only so clients can complete the flow and request refresh
// tokens (offline_access, SEP-2207); they carry no authorization meaning.
const scopes = "openid email offline_access"

// Store is the persistence the connector needs; *store.Store implements
// it (tests use an in-memory fake).
type Store interface {
	CreateOAuthPending(ctx context.Context, p store.OAuthPending) error
	GetOAuthPending(ctx context.Context, id string) (store.OAuthPending, error)
	TakeOAuthPending(ctx context.Context, id string) (store.OAuthPending, error)
	CreateOAuthCode(ctx context.Context, c store.OAuthCode) error
	TakeOAuthCode(ctx context.Context, codeHash string) (store.OAuthCode, error)
	CreateOAuthGrant(ctx context.Context, g store.OAuthGrant) error
	OAuthActorByAccess(ctx context.Context, accessHash string) (string, error)
	RotateOAuthGrant(ctx context.Context, refreshHash, newAccessHash string, accessExpiresAt time.Time, newRefreshHash string) (store.OAuthGrant, error)
	PruneOAuth(ctx context.Context) error
}

type server struct {
	cfg    *config.ConnectorConfig
	store  Store
	log    *slog.Logger
	cimd   *cimdFetcher
	google *googleOIDC
	limit  *rateLimiter
}

// Handler builds the connector service's full HTTP surface. Only OAuth
// endpoints, the well-known documents, /mcp, and health are served —
// REST and the web UI deliberately do not exist here (attack surface).
func Handler(cfg *config.ConnectorConfig, st Store, mcp http.Handler, version string, log *slog.Logger) http.Handler {
	return newServer(cfg, st, log).handler(mcp, version)
}

func newServer(cfg *config.ConnectorConfig, st Store, log *slog.Logger) *server {
	return &server{
		cfg:   cfg,
		store: st,
		log:   log,
		cimd:  newCIMDFetcher(),
		google: &googleOIDC{
			clientID:     cfg.GoogleClientID,
			clientSecret: cfg.GoogleClientSecret,
			redirectURI:  cfg.PublicURL + "/oauth/callback",
		},
		limit: newRateLimiter(60, time.Minute),
	}
}

func (s *server) handler(mcp http.Handler, version string) http.Handler {
	mux := http.NewServeMux()
	health := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}
	mux.HandleFunc("GET /health", health)
	mux.HandleFunc("GET /healthz", health)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintf(w, `ochakai connector %s — OAuth-guarded MCP endpoint

Add %s/mcp to claude.ai or ChatGPT as a custom connector, or to
Claude Code:

  claude mcp add --transport http ochakai %s/mcp

Sign-in is delegated to Google; only %s accounts are accepted.
`, version, s.cfg.PublicURL, s.cfg.PublicURL, s.cfg.AllowedDomain)
	})

	mux.HandleFunc("GET /.well-known/oauth-authorization-server", s.serveASMetadata)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", s.servePRM)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", s.servePRM)
	mux.HandleFunc("GET /oauth/authorize", s.rateLimited(s.handleAuthorize))
	mux.HandleFunc("POST /oauth/consent", s.rateLimited(s.handleConsent))
	mux.HandleFunc("GET /oauth/callback", s.rateLimited(s.handleCallback))
	mux.HandleFunc("POST /oauth/token", s.rateLimited(s.handleToken))
	mux.Handle("/mcp", s.requireToken(mcp))
	return mux
}

// serveASMetadata serves RFC 8414 authorization server metadata.
// client_id_metadata_document_supported + token_endpoint_auth "none" is
// what makes claude.ai and ChatGPT pick CIMD over DCR; offline_access in
// scopes_supported is what makes them request refresh tokens (SEP-2207).
func (s *server) serveASMetadata(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                s.cfg.PublicURL,
		"authorization_endpoint":                s.cfg.PublicURL + "/oauth/authorize",
		"token_endpoint":                        s.cfg.PublicURL + "/oauth/token",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"scopes_supported":                      strings.Fields(scopes),
		"client_id_metadata_document_supported": true,
	})
}

// servePRM serves RFC 9728 protected resource metadata. The resource
// value must match the URL users enter in their client exactly.
func (s *server) servePRM(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 s.cfg.PublicURL + "/mcp",
		"authorization_servers":    []string{s.cfg.PublicURL},
		"scopes_supported":         strings.Fields(scopes),
		"bearer_methods_supported": []string{"header"},
	})
}

// requireToken resolves the actor from an ochakai-issued access token.
// Anything else gets the RFC 9728 handshake: a 401 whose
// WWW-Authenticate header points at the protected resource metadata
// (claude.ai requires the 401 status; it ignores the header on a 200).
func (s *server) requireToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		challenge := fmt.Sprintf("Bearer resource_metadata=%q", s.cfg.PublicURL+"/.well-known/oauth-protected-resource")
		token := bearerToken(r.Header.Get("Authorization"))
		if token == "" {
			w.Header().Set("WWW-Authenticate", challenge)
			http.Error(w, "authorization required", http.StatusUnauthorized)
			return
		}
		email, err := s.store.OAuthActorByAccess(r.Context(), hashToken(token))
		if err != nil {
			w.Header().Set("WWW-Authenticate", challenge+`, error="invalid_token"`)
			http.Error(w, "invalid or expired token", http.StatusUnauthorized)
			return
		}
		actor := domain.Actor{Kind: domain.ActorHuman, Name: email}
		next.ServeHTTP(w, r.WithContext(httpauth.WithActor(r.Context(), actor)))
	})
}

// bearerToken extracts the RFC 6750 bearer credential; the scheme is
// case-insensitive.
func bearerToken(header string) string {
	const prefix = "Bearer "
	if len(header) > len(prefix) && strings.EqualFold(header[:len(prefix)], prefix) {
		return header[len(prefix):]
	}
	return ""
}

// newToken returns a fresh 256-bit opaque token; only its hash is stored.
func newToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
