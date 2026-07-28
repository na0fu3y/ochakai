-- Bodies stop carrying ochakai:// links (design doc 0046 §3.6).
--
-- SPEC §6 defines two link forms, bundle-absolute and relative.
-- ochakai:// was a third, invented here and taught as first-class — and
-- a body travels in the bundle, so every export carried links no other
-- consumer could resolve. Retiring the form from the parser is not
-- enough: a body that still says ochakai://metrics/revenue would simply
-- stop having that edge.
--
-- The rewrite itself is Go (backfillLinkForms): it has to re-derive the
-- links column and re-hash the document, and the document is text this
-- migration must not reformat. All this file does is mark the entries
-- that still need it, so the backfill has a work list it can resume.
--
-- Revision snapshots are left alone. They are what the entry said at the
-- time, and what it said at the time included that spelling.
CREATE TABLE IF NOT EXISTS link_form_backfill (
    id text PRIMARY KEY
);

INSERT INTO link_form_backfill (id)
SELECT id FROM knowledge WHERE body LIKE '%ochakai://%'
ON CONFLICT (id) DO NOTHING;
