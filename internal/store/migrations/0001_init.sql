CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE IF NOT EXISTS knowledge (
    type            text        NOT NULL,
    id              text        NOT NULL,
    title           text        NOT NULL,
    description     text        NOT NULL DEFAULT '',
    tags            text[]      NOT NULL DEFAULT '{}',
    status          text        NOT NULL DEFAULT 'draft',
    created_by_kind text        NOT NULL,
    created_by_name text        NOT NULL,
    verified_by_kind text,
    verified_by_name text,
    verified_at     timestamptz,
    links           jsonb       NOT NULL DEFAULT '[]',
    attrs           jsonb       NOT NULL DEFAULT '{}',
    body            text        NOT NULL DEFAULT '',
    deleted_at      timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (type, id),
    CONSTRAINT knowledge_type_check   CHECK (type IN ('metric', 'query', 'insight', 'term', 'table')),
    CONSTRAINT knowledge_status_check CHECK (status IN ('draft', 'verified', 'deprecated'))
);

-- Trigram index over the searchable text. Japanese text is not tokenized by
-- PostgreSQL FTS, so trigram similarity + substring match is the baseline.
-- tags are excluded: array_to_string is not IMMUTABLE, so it cannot appear
-- in an index expression (tags still contribute to query-time similarity).
CREATE INDEX IF NOT EXISTS knowledge_search_trgm ON knowledge
    USING gin ((title || ' ' || description || ' ' || body) gin_trgm_ops);

CREATE INDEX IF NOT EXISTS knowledge_tags ON knowledge USING gin (tags);

-- Full history: every create/update/delete/verify stores a snapshot.
-- Knowledge is co-owned by humans and agents, so provenance is essential.
CREATE TABLE IF NOT EXISTS knowledge_revision (
    type            text        NOT NULL,
    id              text        NOT NULL,
    rev             integer     NOT NULL,
    change          text        NOT NULL, -- create | update | delete
    changed_by_kind text        NOT NULL,
    changed_by_name text        NOT NULL,
    snapshot        jsonb       NOT NULL,
    changed_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (type, id, rev)
);

-- Ossie semantic models (datasets + relationships + metrics), stored verbatim
-- as JSON converted from the imported YAML. Source of truth for compile_sql.
CREATE TABLE IF NOT EXISTS semantic_model (
    name       text        NOT NULL PRIMARY KEY,
    spec       jsonb       NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
