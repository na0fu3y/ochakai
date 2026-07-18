package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrOAuthReuse is returned by RotateOAuthGrant when a rotated-out
// refresh token is presented again. The grant has been revoked (OAuth 2.1
// refresh token reuse means the token may have been stolen).
var ErrOAuthReuse = errors.New("refresh token reuse detected")

// OAuthPending is an authorization request between /oauth/authorize and
// the Google callback. Its unguessable ID doubles as the state parameter
// sent to Google.
type OAuthPending struct {
	ID            string
	ClientID      string
	ClientName    string
	RedirectURI   string
	State         string
	CodeChallenge string
	ExpiresAt     time.Time
}

// OAuthCode is a single-use authorization code, stored by hash.
type OAuthCode struct {
	CodeHash      string
	ClientID      string
	RedirectURI   string
	CodeChallenge string
	ActorEmail    string
	ExpiresAt     time.Time
}

// OAuthGrant is an issued access + refresh token pair, stored by hash.
type OAuthGrant struct {
	ID               string
	ClientID         string
	ActorEmail       string
	AccessHash       string
	AccessExpiresAt  time.Time
	RefreshHash      string
	RefreshExpiresAt time.Time
}

func (s *Store) CreateOAuthPending(ctx context.Context, p OAuthPending) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO oauth_pending
		(id, client_id, client_name, redirect_uri, state, code_challenge, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		p.ID, p.ClientID, p.ClientName, p.RedirectURI, p.State, p.CodeChallenge, p.ExpiresAt)
	return err
}

// GetOAuthPending returns a live pending request without consuming it
// (the consent POST checks existence; only the callback consumes).
func (s *Store) GetOAuthPending(ctx context.Context, id string) (OAuthPending, error) {
	return scanPending(s.pool.QueryRow(ctx, `SELECT id, client_id, client_name, redirect_uri, state, code_challenge, expires_at
		FROM oauth_pending WHERE id = $1 AND expires_at > now()`, id))
}

// TakeOAuthPending consumes a live pending request (single use).
func (s *Store) TakeOAuthPending(ctx context.Context, id string) (OAuthPending, error) {
	return scanPending(s.pool.QueryRow(ctx, `DELETE FROM oauth_pending
		WHERE id = $1 AND expires_at > now()
		RETURNING id, client_id, client_name, redirect_uri, state, code_challenge, expires_at`, id))
}

func scanPending(row pgx.Row) (OAuthPending, error) {
	var p OAuthPending
	err := row.Scan(&p.ID, &p.ClientID, &p.ClientName, &p.RedirectURI, &p.State, &p.CodeChallenge, &p.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return OAuthPending{}, ErrNotFound
	}
	return p, err
}

func (s *Store) CreateOAuthCode(ctx context.Context, c OAuthCode) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO oauth_code
		(code_hash, client_id, redirect_uri, code_challenge, actor_email, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		c.CodeHash, c.ClientID, c.RedirectURI, c.CodeChallenge, c.ActorEmail, c.ExpiresAt)
	return err
}

// TakeOAuthCode consumes a live authorization code by hash (single use).
func (s *Store) TakeOAuthCode(ctx context.Context, codeHash string) (OAuthCode, error) {
	var c OAuthCode
	err := s.pool.QueryRow(ctx, `DELETE FROM oauth_code
		WHERE code_hash = $1 AND expires_at > now()
		RETURNING code_hash, client_id, redirect_uri, code_challenge, actor_email, expires_at`, codeHash).
		Scan(&c.CodeHash, &c.ClientID, &c.RedirectURI, &c.CodeChallenge, &c.ActorEmail, &c.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return OAuthCode{}, ErrNotFound
	}
	return c, err
}

func (s *Store) CreateOAuthGrant(ctx context.Context, g OAuthGrant) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO oauth_grant
		(id, client_id, actor_email, access_hash, access_expires_at, refresh_hash, refresh_expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		g.ID, g.ClientID, g.ActorEmail, g.AccessHash, g.AccessExpiresAt, g.RefreshHash, g.RefreshExpiresAt)
	return err
}

// OAuthActorByAccess resolves a live access token hash to the actor
// email it was issued for.
func (s *Store) OAuthActorByAccess(ctx context.Context, accessHash string) (string, error) {
	var email string
	err := s.pool.QueryRow(ctx, `SELECT actor_email FROM oauth_grant
		WHERE access_hash = $1 AND access_expires_at > now()`, accessHash).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return email, err
}

// RotateOAuthGrant exchanges a live refresh token for new access +
// refresh hashes, keeping the absolute refresh expiry. Presenting an
// already-rotated refresh hash revokes the grant and returns
// ErrOAuthReuse; an unknown or expired hash returns ErrNotFound.
func (s *Store) RotateOAuthGrant(ctx context.Context, refreshHash, newAccessHash string, accessExpiresAt time.Time, newRefreshHash string) (OAuthGrant, error) {
	var g OAuthGrant
	err := s.pool.QueryRow(ctx, `UPDATE oauth_grant
		SET access_hash = $2, access_expires_at = $3, refresh_hash = $4,
		    prev_refresh_hash = $1, rotated_at = now()
		WHERE refresh_hash = $1 AND refresh_expires_at > now()
		RETURNING id, client_id, actor_email, access_hash, access_expires_at, refresh_hash, refresh_expires_at`,
		refreshHash, newAccessHash, accessExpiresAt, newRefreshHash).
		Scan(&g.ID, &g.ClientID, &g.ActorEmail, &g.AccessHash, &g.AccessExpiresAt, &g.RefreshHash, &g.RefreshExpiresAt)
	if err == nil {
		return g, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return OAuthGrant{}, err
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM oauth_grant WHERE prev_refresh_hash = $1`, refreshHash)
	if err != nil {
		return OAuthGrant{}, err
	}
	if tag.RowsAffected() > 0 {
		return OAuthGrant{}, ErrOAuthReuse
	}
	return OAuthGrant{}, ErrNotFound
}

// PruneOAuth removes expired pending requests, codes, and grants. Called
// opportunistically from the authorize endpoint; the connector's traffic
// is interactive-login scale, so no throttling is needed.
func (s *Store) PruneOAuth(ctx context.Context) error {
	for _, q := range []string{
		`DELETE FROM oauth_pending WHERE expires_at <= now()`,
		`DELETE FROM oauth_code WHERE expires_at <= now()`,
		`DELETE FROM oauth_grant WHERE refresh_expires_at <= now()`,
	} {
		if _, err := s.pool.Exec(ctx, q); err != nil {
			return err
		}
	}
	return nil
}
