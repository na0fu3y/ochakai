-- MCP OAuth connector service (design doc 0010): pending authorization
-- requests, single-use authorization codes, and token grants. Only a
-- connector-mode deployment touches these tables; tokens are stored as
-- SHA-256 hashes so the database never holds a usable credential.

CREATE TABLE oauth_pending (
    id             text PRIMARY KEY,
    client_id      text NOT NULL,
    client_name    text NOT NULL,
    redirect_uri   text NOT NULL,
    state          text NOT NULL DEFAULT '',
    code_challenge text NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    expires_at     timestamptz NOT NULL
);

CREATE TABLE oauth_code (
    code_hash      text PRIMARY KEY,
    client_id      text NOT NULL,
    redirect_uri   text NOT NULL,
    code_challenge text NOT NULL,
    actor_email    text NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    expires_at     timestamptz NOT NULL
);

CREATE TABLE oauth_grant (
    id                 text PRIMARY KEY,
    client_id          text NOT NULL,
    actor_email        text NOT NULL,
    access_hash        text NOT NULL UNIQUE,
    access_expires_at  timestamptz NOT NULL,
    refresh_hash       text NOT NULL UNIQUE,
    -- Previous refresh hash after a rotation: presenting it again is
    -- reuse (theft detection) and revokes the whole grant.
    prev_refresh_hash  text,
    -- Absolute lifetime; rotation never extends it.
    refresh_expires_at timestamptz NOT NULL,
    created_at         timestamptz NOT NULL DEFAULT now(),
    rotated_at         timestamptz
);

CREATE INDEX oauth_grant_prev_refresh ON oauth_grant (prev_refresh_hash);
