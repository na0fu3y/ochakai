package apiclient

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/oauth2"
)

func fakeJWT(payload string) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"none"}`)) + "." + enc([]byte(payload)) + ".sig"
}

func TestJWTEmail(t *testing.T) {
	if got := jwtEmail(fakeJWT(`{"email":"na0@example.com","exp":1}`)); got != "na0@example.com" {
		t.Errorf("email = %q", got)
	}
	if got := jwtEmail("not-a-jwt"); got != "" {
		t.Errorf("garbage token yielded %q", got)
	}
}

func TestIdentityPlainHTTPIsAnonymous(t *testing.T) {
	c, err := New(context.Background(), "http://localhost:1")
	if err != nil {
		t.Fatal(err)
	}
	actor, auth, err := c.Identity()
	if err != nil || actor != "human:anonymous" || auth != "plain http, no credentials" {
		t.Errorf("actor = %q, auth = %q, err = %v", actor, auth, err)
	}
}

func TestIdentityPrefixesActors(t *testing.T) {
	for email, want := range map[string]string{
		"na0@datanest.jp":                     "human:na0@datanest.jp",
		"robot@proj.iam.gserviceaccount.com":  "agent:robot@proj.iam.gserviceaccount.com",
		"ochakai@appspot.gserviceaccount.com": "agent:ochakai@appspot.gserviceaccount.com",
	} {
		c := &Client{
			tokens: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: fakeJWT(`{"email":"` + email + `"}`)}),
			auth:   "service-account ADC",
		}
		actor, _, err := c.Identity()
		if err != nil || actor != want {
			t.Errorf("Identity(%s) = %q, %v; want %q", email, actor, err, want)
		}
	}
}

func TestHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	c, err := New(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Health(context.Background()); err != nil {
		t.Errorf("healthy server: %v", err)
	}

	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer broken.Close()
	c, err = New(context.Background(), broken.URL)
	if err != nil {
		t.Fatal(err)
	}
	var apiErr *APIError
	if err := c.Health(context.Background()); !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusForbidden {
		t.Errorf("want 403 APIError, got %v", err)
	}
}
