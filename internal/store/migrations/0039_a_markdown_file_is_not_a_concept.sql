-- A `.md` address holds a concept, so the files that took one move off
-- it (design doc 0100).
--
-- SPEC §3.1 says every non-reserved `.md` in a bundle is a concept
-- document and §11 makes frontmatter with a non-empty `type` the
-- condition of conformance. Until now a markdown file carrying no type
-- was stored at its own `.md` path, and every export that carried one
-- handed out a bundle that fails both. The write face refuses it from
-- here on; these are the rows already on disk.
--
-- The bytes are not touched. "What enters the bundle leaves it" (0075
-- §2) survives this rename: what changes is the spelling of the address,
-- to one that no longer claims to be a concept. Links in concept bodies
-- that pointed at the old spelling break, and that is the one break OKF
-- requires a consumer to tolerate (SPEC §6.1).
--
-- knowledge_revision moves with the object because the ledger is keyed
-- by path and numbers revisions per path (0028): leaving the rows behind
-- would orphan the history `?history` reads and restart `rev` at 1 for
-- the next write. No revision is added — a migration is not an actor,
-- and there is no one to record as having moved it.
--
-- `.markdown` rather than a bare name: the extension is information for
-- whoever opens the exported bundle, and it is not `.md`, which is all
-- §3.1 asks. A collision takes the suffix twice (`x.md` → `x.markdown`
-- → `x.markdown.markdown`), the way the import path settles the same
-- question — moving the loser beats dropping it (0046 §3.13).

CREATE OR REPLACE FUNCTION ochakai_unclaim_markdown(p text) RETURNS text
LANGUAGE plpgsql AS $$
DECLARE
    target text := left(p, length(p) - 3) || '.markdown';
BEGIN
    WHILE EXISTS (SELECT 1 FROM object WHERE path = target) LOOP
        target := target || '.markdown';
    END LOOP;
    RETURN target;
END $$;

DO $$
DECLARE
    row record;
    target text;
BEGIN
    FOR row IN
        SELECT path FROM object
         WHERE id IS NULL AND path LIKE '%.md'
         ORDER BY path
    LOOP
        target := ochakai_unclaim_markdown(row.path);
        UPDATE object SET path = target WHERE path = row.path;
        UPDATE knowledge_revision SET path = target WHERE path = row.path;
        -- The vector table is created by the embedding path rather than
        -- by a numbered migration, so it is absent on an instance with
        -- embeddings off. Left behind, its row would answer searches for
        -- an address nothing is at.
        IF to_regclass('attachment_embedding') IS NOT NULL THEN
            EXECUTE 'UPDATE attachment_embedding SET path = $1 WHERE path = $2'
              USING target, row.path;
        END IF;
    END LOOP;
END $$;

DROP FUNCTION ochakai_unclaim_markdown(text);
