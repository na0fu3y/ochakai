package httpauth

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/na0fu3y/ochakai/internal/config"
	"github.com/na0fu3y/ochakai/internal/domain"
)

// unsignedToken builds a token whose payload names whoever you like and
// whose signature is the placeholder Cloud Run substitutes. Nothing
// verifies it — which is the whole reason the public posture exists
// (design doc 0041 §1).
func unsignedToken(email string) string {
	part := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	return part(map[string]any{"alg": "RS256"}) + "." +
		part(map[string]any{"email": email, "email_verified": true}) + "." +
		"SIGNATURE_REMOVED_BY_GOOGLE"
}

// The claim design doc 0041 §3 makes is not "the actor is anonymous" but
// "the actor does not depend on the request". A forged token, a real
// one, a delegation header and nothing at all must all resolve the same,
// because on a public deployment none of them is evidence of anything.
func TestPublicReadOnlyIgnoresEveryIdentityHeader(t *testing.T) {
	cfg := &config.Config{PublicReadOnly: true, ReadOnly: true}
	want := domain.Actor{Kind: domain.ActorHuman, Name: "anonymous"}

	for _, tc := range []struct {
		name string
		h    http.Header
	}{
		{"no headers at all", http.Header{}},
		{"a forged bearer token", http.Header{"Authorization": {"Bearer " + unsignedToken("ceo@example.com")}}},
		{"the header Cloud Run would set", http.Header{"X-Serverless-Authorization": {"Bearer " + unsignedToken("real@example.com")}}},
		{"both, disagreeing", http.Header{
			"Authorization":              {"Bearer " + unsignedToken("a@example.com")},
			"X-Serverless-Authorization": {"Bearer " + unsignedToken("b@example.com")},
		}},
		{"a delegation attempt", http.Header{OnBehalfOfHeader: {"human:tanaka@example.co.jp"}}},
		{"a token and a delegation together", http.Header{
			"Authorization":  {"Bearer " + unsignedToken("app@example.iam.gserviceaccount.com")},
			OnBehalfOfHeader: {"human:tanaka@example.co.jp"},
		}},
		{"garbage where a token goes", http.Header{"Authorization": {"Bearer not-a-token"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, status, err := ActorFromHeader(cfg, tc.h)
			if err != nil {
				t.Fatalf("refused a caller: %v (status %d)", err, status)
			}
			if got != want {
				t.Errorf("actor = %+v, want %+v — the request changed the answer", got, want)
			}
		})
	}
}

// A demo visitor arrives with no credentials. Without this posture they
// get a 401 and never see the product (design doc 0041 §1).
func TestPublicReadOnlyRefusesNobody(t *testing.T) {
	var seen domain.Actor
	h := Middleware(&config.Config{PublicReadOnly: true, ReadOnly: true},
		http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			seen = Actor(r.Context())
		}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/knowledge?q=revenue", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("anonymous read = %d, want 200", rec.Code)
	}
	if seen.Name != "anonymous" || seen.Via != "" {
		t.Errorf("actor = %+v, want a bare anonymous human", seen)
	}
}

// The private posture must be untouched: a caller with no token is still
// refused there, or this change would have quietly opened every existing
// deployment.
func TestPrivatePostureStillRefusesAnonymous(t *testing.T) {
	_, status, err := ActorFromHeader(&config.Config{}, http.Header{})
	if err == nil {
		t.Fatal("a private deployment accepted a request with no token")
	}
	if status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", status)
	}
}
