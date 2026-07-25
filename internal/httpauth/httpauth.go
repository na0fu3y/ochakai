// Package httpauth resolves the acting client for provenance. ochakai
// does no authorization — reachability is Cloud Run IAM's job (design
// docs 0002/0003); the actor is only recorded on writes.
package httpauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
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
// human:anonymous instead — but X-Ochakai-On-Behalf-Of is still honored,
// from any caller, so a delegating integration can be developed and its
// mistakes seen locally.
func Middleware(cfg *config.Config, next http.Handler) http.Handler {
	if cfg.InsecureDev {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Delegation is processed here too, and every caller may
			// delegate: someone building an embedding host develops
			// against this mode, and short-circuiting past delegate()
			// would let them ship a header that silently did nothing —
			// exactly the failure design doc 0027 §5.2 refuses to allow in
			// production, hidden until the first deployment.
			actor, status, err := delegate(
				domain.Actor{Kind: domain.ActorHuman, Name: "anonymous"},
				r.Header.Get(OnBehalfOfHeader), []string{"*"})
			if err != nil {
				http.Error(w, "auth: "+err.Error(), status)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithActor(r.Context(), actor)))
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
		actor, status, err := delegate(actor, r.Header.Get(OnBehalfOfHeader), cfg.Delegators)
		if err != nil {
			http.Error(w, "auth: "+err.Error(), status)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithActor(r.Context(), actor)))
	})
}

// OnBehalfOfHeader carries the end-user identity a trusted caller is
// acting for: "human:tanaka@example.co.jp" (design doc 0027).
const OnBehalfOfHeader = "X-Ochakai-On-Behalf-Of"

// maxOnBehalfOf bounds the header so a provenance column cannot be filled
// with arbitrary bulk.
const maxOnBehalfOf = 320 // the maximum length of an email address

// delegate resolves the actor to record when caller forwards an end-user
// identity. The forwarded identity becomes the actor and the caller is
// kept as Via — never dropped. Recording only the human would make a
// delegated write indistinguishable from one the human made themselves,
// which is the definition of a forgery; recording only the caller is the
// collapse this exists to fix (design doc 0002 §3).
//
// A header from a caller that is not permitted to delegate is an error,
// not a silent downgrade: an application that believes it is writing as
// tanaka must not discover months later that every entry says otherwise.
func delegate(caller domain.Actor, header string, delegators []string) (domain.Actor, int, error) {
	header = strings.TrimSpace(header)
	if header == "" {
		return caller, 0, nil
	}
	if !permitted(caller.Name, delegators) {
		return caller, http.StatusForbidden, fmt.Errorf(
			"%s: %s is not permitted to act on behalf of others (add it to OCHAKAI_DELEGATING_CALLERS)",
			OnBehalfOfHeader, caller.Name)
	}
	onBehalf, err := parseOnBehalfOf(header)
	if err != nil {
		return caller, http.StatusBadRequest, err
	}
	onBehalf.Via = caller.String()
	return onBehalf, 0, nil
}

func permitted(caller string, delegators []string) bool {
	for _, d := range delegators {
		if d == "*" || d == caller {
			return true
		}
	}
	return false
}

// parseOnBehalfOf reads "kind:name". The kind is explicit rather than
// guessed from the name: the caller knows whether it is forwarding a
// person or another agent, and a guess would silently mislabel every
// identity that does not look like the domain expects.
func parseOnBehalfOf(v string) (domain.Actor, error) {
	if len(v) > maxOnBehalfOf {
		return domain.Actor{}, fmt.Errorf("%s: value exceeds %d bytes", OnBehalfOfHeader, maxOnBehalfOf)
	}
	kind, name, ok := strings.Cut(v, ":")
	name = strings.TrimSpace(name)
	if !ok || name == "" || (kind != domain.ActorHuman && kind != domain.ActorAgent) {
		return domain.Actor{}, fmt.Errorf(
			`%s: want "human:<identity>" or "agent:<identity>", got %q`, OnBehalfOfHeader, v)
	}
	if strings.ContainsAny(name, " \t") {
		return domain.Actor{}, fmt.Errorf("%s: identity must not contain whitespace", OnBehalfOfHeader)
	}
	return domain.Actor{Kind: kind, Name: name}, nil
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
