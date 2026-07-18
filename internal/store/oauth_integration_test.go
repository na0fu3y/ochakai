package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// TestOAuthIntegration exercises the connector's persistence against a
// real PostgreSQL. Skipped unless OCHAKAI_TEST_DATABASE_URL is set (see
// TestIntegration).
func TestOAuthIntegration(t *testing.T) {
	dbURL := os.Getenv("OCHAKAI_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("OCHAKAI_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"oauth_pending", "oauth_code", "oauth_grant"} {
		if _, err := s.pool.Exec(ctx, `DELETE FROM `+table); err != nil {
			t.Fatal(err)
		}
	}
	live := time.Now().Add(10 * time.Minute)

	// Pending: get leaves it, take consumes, second take misses.
	p := OAuthPending{ID: "pend-1", ClientID: "https://c.example/meta", ClientName: "c",
		RedirectURI: "https://c.example/cb", State: "st", CodeChallenge: "ch", ExpiresAt: live}
	if err := s.CreateOAuthPending(ctx, p); err != nil {
		t.Fatal(err)
	}
	if got, err := s.GetOAuthPending(ctx, "pend-1"); err != nil || got.State != "st" {
		t.Fatalf("get pending = %+v, %v", got, err)
	}
	if got, err := s.TakeOAuthPending(ctx, "pend-1"); err != nil || got.CodeChallenge != "ch" {
		t.Fatalf("take pending = %+v, %v", got, err)
	}
	if _, err := s.TakeOAuthPending(ctx, "pend-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second take = %v, want ErrNotFound", err)
	}

	// Expired pending is invisible.
	p.ID, p.ExpiresAt = "pend-old", time.Now().Add(-time.Minute)
	if err := s.CreateOAuthPending(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetOAuthPending(ctx, "pend-old"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired pending = %v, want ErrNotFound", err)
	}

	// Codes are single use.
	c := OAuthCode{CodeHash: "code-h", ClientID: p.ClientID, RedirectURI: p.RedirectURI,
		CodeChallenge: "ch", ActorEmail: "a@example.co.jp", ExpiresAt: live}
	if err := s.CreateOAuthCode(ctx, c); err != nil {
		t.Fatal(err)
	}
	if got, err := s.TakeOAuthCode(ctx, "code-h"); err != nil || got.ActorEmail != "a@example.co.jp" {
		t.Fatalf("take code = %+v, %v", got, err)
	}
	if _, err := s.TakeOAuthCode(ctx, "code-h"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second take code = %v, want ErrNotFound", err)
	}

	// Grants: lookup by access hash, rotation, reuse revocation.
	g := OAuthGrant{ID: "grant-1", ClientID: p.ClientID, ActorEmail: "a@example.co.jp",
		AccessHash: "acc-1", AccessExpiresAt: live, RefreshHash: "ref-1", RefreshExpiresAt: live}
	if err := s.CreateOAuthGrant(ctx, g); err != nil {
		t.Fatal(err)
	}
	if email, err := s.OAuthActorByAccess(ctx, "acc-1"); err != nil || email != "a@example.co.jp" {
		t.Fatalf("actor by access = %q, %v", email, err)
	}
	rotated, err := s.RotateOAuthGrant(ctx, "ref-1", "acc-2", live, "ref-2")
	if err != nil || rotated.ActorEmail != "a@example.co.jp" {
		t.Fatalf("rotate = %+v, %v", rotated, err)
	}
	if _, err := s.OAuthActorByAccess(ctx, "acc-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old access after rotation = %v, want ErrNotFound", err)
	}
	if email, err := s.OAuthActorByAccess(ctx, "acc-2"); err != nil || email != "a@example.co.jp" {
		t.Fatalf("new access = %q, %v", email, err)
	}
	// Presenting the rotated-out refresh hash revokes everything.
	if _, err := s.RotateOAuthGrant(ctx, "ref-1", "acc-3", live, "ref-3"); !errors.Is(err, ErrOAuthReuse) {
		t.Fatalf("reuse = %v, want ErrOAuthReuse", err)
	}
	if _, err := s.OAuthActorByAccess(ctx, "acc-2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("access after revocation = %v, want ErrNotFound", err)
	}
	if _, err := s.RotateOAuthGrant(ctx, "ref-2", "acc-4", live, "ref-4"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("refresh after revocation = %v, want ErrNotFound", err)
	}

	// Prune clears the expired leftovers.
	if err := s.PruneOAuth(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM oauth_pending`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("pending after prune = %d, %v", n, err)
	}
}
