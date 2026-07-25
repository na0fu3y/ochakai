-- The lexical search haystack becomes a stored column, so the trigram
-- index and the query can finally be about the same text.
--
-- 0001 indexed (title || description || body). SearchLexical has since
-- grown to score over id (0022), tags, and attachment names (0020), and
-- those three cannot join an index expression at all: array_to_string is
-- not IMMUTABLE, and attachment names live in another table. So the index
-- and the query drifted apart and could never be reconciled in place.
--
-- The deeper problem was that the index was unusable regardless of the
-- expression: similarity() was computed in the SELECT list and the score
-- floor applied outside the subquery, so every row was scanned and scored
-- no matter what. An index needs a predicate in WHERE, which needs one
-- column holding the whole haystack.
--
-- search_text is maintained by trigger rather than in Go: every write
-- path (create, update, move, attach, detach) would otherwise have to
-- remember, and the one that forgets makes an entry silently
-- unsearchable.
--
-- How far the index actually reaches, measured on 5000 entries of about
-- 1.2 kB each: '%revenue%' and '%売上の%' are index scans at ~0.2 ms,
-- '%売上%' is a sequential scan at ~16 ms. A two-character pattern has no
-- word boundary to pad against, so pg_trgm extracts no whole trigram and
-- the planner reads the table — and Japanese search runs on exactly such
-- terms (売上, 原価, 客数). The index earns its place on latin words and
-- longer Japanese phrases; the scan is the price of the short ones.
-- Beware EXPLAIN with enable_seqscan=off: GIN answers by scanning the
-- whole index and rechecking, which reads as an index scan and is slower
-- than the scan it replaced.

ALTER TABLE knowledge ADD COLUMN IF NOT EXISTS search_text text NOT NULL DEFAULT '';

-- One definition of the haystack, used by both triggers and the backfill.
CREATE OR REPLACE FUNCTION ochakai_search_text(
    p_id text, p_title text, p_description text, p_tags text[], p_body text
) RETURNS text LANGUAGE sql STABLE AS $$
    SELECT p_id || ' ' || p_title || ' ' || p_description || ' '
        || array_to_string(p_tags, ' ') || ' ' || p_body || ' '
        || COALESCE((SELECT string_agg(a.name, ' ')
                       FROM attachment a WHERE a.knowledge_id = p_id), '')
$$;

CREATE OR REPLACE FUNCTION ochakai_knowledge_search_text() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    NEW.search_text := ochakai_search_text(NEW.id, NEW.title, NEW.description, NEW.tags, NEW.body);
    RETURN NEW;
END $$;

DROP TRIGGER IF EXISTS knowledge_search_text ON knowledge;
CREATE TRIGGER knowledge_search_text BEFORE INSERT OR UPDATE ON knowledge
    FOR EACH ROW EXECUTE FUNCTION ochakai_knowledge_search_text();

-- Attaching or detaching a file changes the owning entry's haystack
-- (design doc 0020: a filename is a name the entry can be found by).
CREATE OR REPLACE FUNCTION ochakai_attachment_search_text() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    target text := COALESCE(NEW.knowledge_id, OLD.knowledge_id);
BEGIN
    -- The UPDATE fires the knowledge trigger above, which recomputes from
    -- the current attachment rows; assigning here would be redundant.
    UPDATE knowledge SET search_text = search_text WHERE id = target;
    RETURN NULL;
END $$;

DROP TRIGGER IF EXISTS attachment_search_text ON attachment;
CREATE TRIGGER attachment_search_text AFTER INSERT OR UPDATE OR DELETE ON attachment
    FOR EACH ROW EXECUTE FUNCTION ochakai_attachment_search_text();

UPDATE knowledge SET search_text = ochakai_search_text(id, title, description, tags, body);

-- Same name as the 0001 index, new expression: there is only ever one
-- lexical search index, and keeping the name makes that obvious in \di.
DROP INDEX IF EXISTS knowledge_search_trgm;
CREATE INDEX knowledge_search_trgm ON knowledge USING gin (search_text gin_trgm_ops);
