-- A concept's other names are searchable (design doc 0105).
--
-- `Metric` is described, in the type vocabulary the CLI help, the MCP
-- schema and docs/architecture.md all print, as holding "what a number
-- means and what it is called — its definition and synonyms". The
-- examples bundle writes them the obvious way:
--
--     synonyms: [net sales, top line, 売上]
--
-- and until now nothing could find the metric by any of the three. The
-- haystack is id, title, description, tags, body and the names of the
-- files in the concept's namespace (0016, 0029, 0033); a producer key
-- lives in attrs, which nothing read. So the product invited a writer to
-- record what their metric is called and then answered "no concept
-- matched" when somebody searched for it — sharpest in Japanese, where
-- 売上 is exactly the word a reader has and `Revenue` is the word the
-- warehouse has.
--
-- Only `synonyms`, and only as text. Every other producer key is a value
-- *about* the concept — a model, a receipt, an executor's resource, an
-- id another concept owns — and putting those in the haystack would make
-- a search for one concept return the concepts that merely mention it,
-- which is what --links-to and --source answer properly. A name is the
-- one attr whose entire purpose is to be searched for.
--
-- The key stays the producer's: no validation, no wire field, no filter
-- of its own, and it round-trips as written. This reads it; it does not
-- own it.

CREATE OR REPLACE FUNCTION ochakai_synonym_text(p_attrs jsonb) RETURNS text
LANGUAGE sql IMMUTABLE AS $$
    -- A list is the form the examples use and the form a reader expects;
    -- a bare string is what somebody with one alias writes, and refusing
    -- it would be a rule nobody was told. Anything else — a number, an
    -- object, a null — has no name in it to index.
    SELECT COALESCE((SELECT string_agg(v, ' ')
                       FROM jsonb_array_elements_text(
                           CASE jsonb_typeof(p_attrs -> 'synonyms')
                               WHEN 'array' THEN p_attrs -> 'synonyms'
                               WHEN 'string' THEN jsonb_build_array(p_attrs -> 'synonyms')
                               ELSE '[]'::jsonb
                           END) v), '')
$$;

-- Appended where the trigger already appends the file names, rather than
-- inside ochakai_search_text: that function's arguments are the entry's
-- own columns, and attrs is not one a file has (a file's haystack is
-- empty, above).
CREATE OR REPLACE FUNCTION ochakai_knowledge_search_text() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id IS NULL THEN
        NEW.search_text := '';
        RETURN NEW;
    END IF;
    NEW.search_text := ochakai_search_text(NEW.id, NEW.title, NEW.description, NEW.tags, NEW.body)
        || ' ' || COALESCE((SELECT string_agg(regexp_replace(x, '^.*/', ''), ' ')
                              FROM jsonb_array_elements_text(NEW.files) x), '')
        || ' ' || ochakai_synonym_text(NEW.attrs);
    RETURN NEW;
END $$;

-- The haystacks already stored carry no synonyms until something touches
-- the row, so they are recomputed once. Assigning search_text to itself
-- is how every migration before this one has asked the trigger to run;
-- the tsvector column follows it (0036).
UPDATE object SET search_text = search_text WHERE id IS NOT NULL;
