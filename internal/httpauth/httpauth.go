// Package httpauth resolves the acting client for provenance. ochakai
// does no authorization — reachability is Cloud Run IAM's job (design
// docs 0002/0003); the actor is only recorded on writes.
package httpauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/na0fu3y/ochakai/internal/config"
	"github.com/na0fu3y/ochakai/internal/domain"
)

type ctxKey struct{}

// Middleware resolves the actor from the Google-verified ID token that
// Cloud Run forwards after its IAM check. It parses claims WITHOUT
// verifying the signature: on a non-public Cloud Run service the token
// was already verified by Google (and X-Serverless-Authorization arrives
// with its signature replaced by SIGNATURE_REMOVED_BY_GOOGLE). ochakai
// must therefore never run publicly invokable.
//
// With cfg.InsecureDev (local development only), every request acts as
// human:anonymous instead.
func Middleware(cfg *config.Config, next http.Handler) http.Handler {
	if cfg.InsecureDev {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(WithActor(r.Context(), domain.Actor{Kind: domain.ActorHuman, Name: "anonymous"})))
		})
	}
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
			http.Error(w, "auth: "+err.Error(), http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithActor(r.Context(), actor)))
	})
}

// actorFromIDToken extracts provenance from ID token claims: the email is
// the actor name; service accounts are agents, people are humans.
func actorFromIDToken(token string) (domain.Actor, error) {
	if token == "" {
		return domain.Actor{}, errors.New("no identity token; is the service non-public with Cloud Run IAM enforced? (for local development set OCHAKAI_INSECURE_DEV=true)")
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

func WithActor(ctx context.Context, a domain.Actor) context.Context {
	return context.WithValue(ctx, ctxKey{}, a)
}

// Actor returns the authenticated actor, defaulting to agent/unknown so a
// missing context never grants human provenance.
func Actor(ctx context.Context) domain.Actor {
	if a, ok := ctx.Value(ctxKey{}).(domain.Actor); ok {
		return a
	}
	return domain.Actor{Kind: domain.ActorAgent, Name: "unknown"}
}
