-- Purge is the one operation that leaves no trace: it erases the entry,
-- its revisions, its usage, and its events. In a product whose value is
-- provenance, an untraceable destruction path is the wrong kind of hole —
-- and ochakai has no authorization, so "only humans call it" (design doc
-- 0015) is a statement about which surface exposes it, not about who can
-- reach it.
--
-- So the fact of the purge outlives the entry. Not its content: this
-- records that an id was destroyed, by whom, and how much history went
-- with it. The body is gone, which is what the caller asked for.
CREATE TABLE IF NOT EXISTS knowledge_purge (
    id              text        NOT NULL,
    type            text        NOT NULL,
    title           text        NOT NULL,
    revisions       integer     NOT NULL,
    purged_by_kind  text        NOT NULL,
    purged_by_name  text        NOT NULL,
    purged_by_via   text        NOT NULL DEFAULT '',
    purged_at       timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id, purged_at)
);
