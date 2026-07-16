package httpauth

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/na0fu3y/ochakai/internal/config"
	"github.com/na0fu3y/ochakai/internal/domain"
)

// fakeIDToken builds an unsigned JWT-shaped token with the given payload,
// mimicking what Cloud Run forwards (X-Serverless-Authorization arrives
// with its signature replaced).
func fakeIDToken(payload string) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"RS256"}`)) + "." + enc([]byte(payload)) + ".SIGNATURE_REMOVED_BY_GOOGLE"
}

func TestActorFromIDToken(t *testing.T) {
	cases := []struct {
		name    string
		token   string
		want    domain.Actor
		wantErr bool
	}{
		{
			name:  "human user account",
			token: fakeIDToken(`{"email":"na0@example.com","email_verified":true}`),
			want:  domain.Actor{Kind: domain.ActorHuman, Name: "na0@example.com"},
		},
		{
			name:  "service account is an agent",
			token: fakeIDToken(`{"email":"bot@myproj.iam.gserviceaccount.com"}`),
			want:  domain.Actor{Kind: domain.ActorAgent, Name: "bot@myproj.iam.gserviceaccount.com"},
		},
		{name: "empty", token: "", wantErr: true},
		{name: "not a jwt", token: "abc", wantErr: true},
		{name: "no email claim", token: fakeIDToken(`{"sub":"123"}`), wantErr: true},
	}
	for _, tc := range cases {
		got, err := actorFromIDToken(tc.token)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%s: want error, got %+v", tc.name, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: got %+v want %+v", tc.name, got, tc.want)
		}
	}
}

func TestCloudRunIAMPrefersServerlessHeader(t *testing.T) {
	// Cloud Run validates only X-Serverless-Authorization when both are
	// present; trusting Authorization instead would allow impersonation.
	cfg := &config.Config{}
	var got domain.Actor
	h := Middleware(cfg, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = Actor(r.Context())
	}))

	r := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge", nil)
	r.Header.Set("Authorization", "Bearer "+fakeIDToken(`{"email":"forged@example.com"}`))
	r.Header.Set("X-Serverless-Authorization", "Bearer "+fakeIDToken(`{"email":"real@example.com"}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if got.Name != "real@example.com" {
		t.Errorf("actor = %+v; must come from X-Serverless-Authorization", got)
	}
}

func TestCloudRunIAMRejectsMissingToken(t *testing.T) {
	cfg := &config.Config{}
	h := Middleware(cfg, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/knowledge", nil))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestInsecureDevActsAsAnonymous(t *testing.T) {
	cfg := &config.Config{InsecureDev: true}
	var got domain.Actor
	h := Middleware(cfg, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = Actor(r.Context())
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/knowledge", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if got.Kind != domain.ActorHuman || got.Name != "anonymous" {
		t.Errorf("actor = %+v, want human:anonymous", got)
	}
}
