-- A directory's description (decision 0154). OKF has one place to say
-- what a directory is for: the line its parent's index.md gives it
-- (SPEC §8). ochakai generates index.md from the bundle, so the
-- listing is always what the directory holds; what it could not keep
-- was the sentence a producer wrote about a subdirectory, which an
-- import dropped with the rest of the file. That sentence lives here,
-- keyed by the directory's bundle path.
CREATE TABLE IF NOT EXISTS directory_description (
    prefix          text        NOT NULL PRIMARY KEY,
    description     text        NOT NULL,
    updated_by_kind text        NOT NULL,
    updated_by_name text        NOT NULL,
    updated_at      timestamptz NOT NULL DEFAULT now()
);
