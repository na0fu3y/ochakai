-- Image attachments (design doc 0008): entries can carry images — the
-- dashboard screenshot behind an insight, the ER diagram behind a table
-- entry. Bytes live in content-addressed immutable blobs (dedup, and
-- revisions can reference history by hash); the attachment table maps
-- entry + filename to a blob. Postgres keeps the single-store,
-- secret-zero deployment: no bucket, no new credentials.

CREATE TABLE IF NOT EXISTS blob (
    sha256     text        NOT NULL PRIMARY KEY,
    media_type text        NOT NULL,
    bytes      bytea       NOT NULL,
    size       bigint      NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS attachment (
    knowledge_type  text        NOT NULL,
    knowledge_id    text        NOT NULL,
    name            text        NOT NULL,
    sha256          text        NOT NULL REFERENCES blob (sha256),
    -- Bundle path this attachment originally arrived at (foreign OKF
    -- imports); export writes it back there so body links keep working.
    -- '' for attachments born in ochakai (exported to <type>/<id>/<name>).
    okf_path        text        NOT NULL DEFAULT '',
    created_by_kind text        NOT NULL,
    created_by_name text        NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (knowledge_type, knowledge_id, name)
);
