// Package httpauth authenticates requests with static bearer tokens and
// resolves the acting client (human or agent) for provenance tracking.
package httpauth

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/na0fu3y/ochakai/internal/config"
	"github.com/na0fu3y/ochakai/internal/domain"
)

type ctxKey struct{}

// Middleware resolves the acting client and stores it in the request
// context. ochakai does no authorization — reachability is the deploy
// layer's job (design doc 0002); the actor is provenance only.
func Middleware(cfg *config.Config, next http.Handler) http.Handler {
	if cfg.AuthMode == config.AuthCloudRunIAM {
		return cloudRunIAMMiddleware(next)
	}
	return clientsMiddleware(cfg, next)
}

// clientsMiddleware checks the Authorization: Bearer token against
// configured clients. With no clients configured, requests act as
// human/anonymous (local development).
func clientsMiddleware(cfg *config.Config, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(cfg.Clients) == 0 {
			next.ServeHTTP(w, r.WithContext(WithActor(r.Context(), domain.Actor{Kind: domain.ActorHuman, Name: "anonymous"})))
			return
		}
		token := bearerToken(r)
		for _, c := range cfg.Clients {
			if subtle.ConstantTimeCompare([]byte(token), []byte(c.Token)) == 1 {
				next.ServeHTTP(w, r.WithContext(WithActor(r.Context(), c.Actor)))
				return
			}
		}
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

// cloudRunIAMMiddleware resolves the actor from the Google-verified ID
// token that Cloud Run forwards after its IAM check. It parses claims
// WITHOUT verifying the signature: on a non-public Cloud Run service the
// token was already verified by Google (and X-Serverless-Authorization
// arrives with its signature replaced by SIGNATURE_REMOVED_BY_GOOGLE).
// This mode must never be enabled on a publicly invokable service.
func cloudRunIAMMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// When both headers are present, Cloud Run validates only
		// X-Serverless-Authorization — so it must take precedence here,
		// or an authorized caller could impersonate via Authorization.
		token := bearerFrom(r.Header.Get("X-Serverless-Authorization"))
		if token == "" {
			token = bearerFrom(r.Header.Get("Authorization"))
		}
		actor, err := actorFromIDToken(token)
		if err != nil {
			http.Error(w, "cloudrun-iam auth: "+err.Error(), http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithActor(r.Context(), actor)))
	})
}

// actorFromIDToken extracts provenance from ID token claims: the email is
// the actor name; service accounts are agents, people are humans.
func actorFromIDToken(token string) (domain.Actor, error) {
	if token == "" {
		return domain.Actor{}, errors.New("no identity token; is the service non-public with Cloud Run IAM enforced?")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return domain.Actor{}, errors.New("malformed identity token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return domain.Actor{}, errors.New("malformed identity token payload")
	}
	var claims struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Email == "" {
		return domain.Actor{}, errors.New("identity token has no email claim")
	}
	kind := domain.ActorHuman
	if strings.HasSuffix(claims.Email, ".gserviceaccount.com") {
		kind = domain.ActorAgent
	}
	return domain.Actor{Kind: kind, Name: claims.Email}, nil
}

func bearerFrom(header string) string {
	const prefix = "Bearer "
	if len(header) > len(prefix) && strings.EqualFold(header[:len(prefix)], prefix) {
		return header[len(prefix):]
	}
	return ""
}

func bearerToken(r *http.Request) string {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if len(h) > len(prefix) && h[:len(prefix)] == prefix {
		return h[len(prefix):]
	}
	return ""
}

func WithActor(ctx context.Context, a domain.Actor) context.Context {
	return context.WithValue(ctx, ctxKey{}, a)
}

// Actor returns the authenticated actor, defaulting to agent/unknown so a
// missing context never grants human privileges.
func Actor(ctx context.Context) domain.Actor {
	if a, ok := ctx.Value(ctxKey{}).(domain.Actor); ok {
		return a
	}
	return domain.Actor{Kind: domain.ActorAgent, Name: "unknown"}
}
