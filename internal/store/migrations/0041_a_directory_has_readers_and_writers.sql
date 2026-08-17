-- A directory has readers and writers (design doc 0109).
--
-- Until now ochakai held no authorization at all: whoever reached a
-- deployment could read and write everything, and a boundary meant a
-- second deployment (0065 §1, which named the condition for revising
-- itself and this is it). The condition arrived as two needs at once —
-- knowledge some readers must not see, and curated knowledge some
-- writers must not change — inside one bundle that has to stay one
-- bundle, because the teams share a glossary and a split would land
-- each copy as unverified (0075 §3.1).
--
-- One row is one grant: this principal may read under this prefix, and
-- may write there when may_write. There is no deny row and no negative
-- rule. A deny list has to be evaluated against an order nobody can see
-- in the table, and the first thing anybody would ask of it — "why can
-- Tanaka not read this" — becomes a question about rule precedence
-- rather than about a row that is present or absent.
--
-- The prefix is a bundle path with no trailing slash, matched on
-- segment boundaries the way the search filter already matches (0075
-- §6, migration 0033): 'metrics' covers 'metrics/revenue' and does not
-- cover 'metrics-legacy/churn'. The empty prefix is the root, which is
-- how a grant says "the whole bundle" without naming a directory that
-- may not exist yet.
--
-- The principal is the actor spelling the ledger already uses —
-- 'human:tanaka@example.co.jp', 'process:app@proj.iam.gserviceaccount.com'
-- (0065 §2). Reusing it means the name in a grant and the name in a
-- revision are the same string, so "who wrote this and could they" is
-- one comparison rather than a mapping. Delegated writes resolve to the
-- end user (the `via` is not part of the match): an application that
-- forwards Tanaka gets Tanaka's scope, which is the whole point of
-- having forwarded the identity.
--
-- Directories are not rows here. A grant names a path whether or not
-- anything is stored under it, and stays when the last concept beneath
-- it is deleted — a scope outlives its contents, and a policy that
-- disappeared when a directory emptied would silently widen on the day
-- somebody cleaned up.
CREATE TABLE IF NOT EXISTS access_rule (
    prefix     text NOT NULL,
    principal  text NOT NULL,
    may_write  boolean NOT NULL DEFAULT false,
    granted_at timestamptz NOT NULL DEFAULT now(),
    -- Who granted it, in the same actor spelling. The policy is not the
    -- revision ledger and does not get one: what a reader of this table
    -- needs is the current answer and where it came from, and a history
    -- of grants is a second ledger with its own retention question.
    granted_by text NOT NULL DEFAULT '',
    PRIMARY KEY (prefix, principal)
);

-- Every request that arrives under a policy resolves its own principal
-- first, so that lookup is the hot one.
CREATE INDEX IF NOT EXISTS access_rule_principal_idx ON access_rule (principal);
