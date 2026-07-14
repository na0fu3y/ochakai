// Package httpauth authenticates requests with static bearer tokens and
// resolves the acting client (human or agent) for provenance tracking.
package httpauth

import (
	"context"
	"crypto/subtle"
	"net/http"

	"github.com/na0fu3y/ochakai/internal/config"
	"github.com/na0fu3y/ochakai/internal/domain"
)

type ctxKey struct{}

// Middleware checks the Authorization: Bearer token against configured
// clients and stores the resolved actor in the request context. With no
// clients configured, auth is disabled and requests act as human/anonymous
// (local development).
func Middleware(cfg *config.Config, next http.Handler) http.Handler {
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
