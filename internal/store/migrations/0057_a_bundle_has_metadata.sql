-- What a bundle says about itself (OKF SPEC §8.1). A bundle-root
-- index.md may carry frontmatter — title, description, license, any
-- extension key — and a producer that regenerates index.md must keep it.
-- ochakai generates every index.md, so the frontmatter a producer wrote
-- is kept here, as the YAML text it was written in, less the okf_version
-- line ochakai writes itself. One bundle per deployment, so one row.
CREATE TABLE IF NOT EXISTS bundle_metadata (
    only_row        boolean     PRIMARY KEY DEFAULT true CHECK (only_row),
    frontmatter     text        NOT NULL,
    updated_by_kind text        NOT NULL,
    updated_by_name text        NOT NULL,
    updated_at      timestamptz NOT NULL DEFAULT now()
);
