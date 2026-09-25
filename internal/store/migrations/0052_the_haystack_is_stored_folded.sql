-- The haystack is stored in the one spelling a search compares it in:
-- NFKC, lower case.
--
-- search_text has one reader left, the whole-query bonus in
-- SearchLexical, which asks whether the question appears verbatim —
-- search_tsv is derived from it too, but folds case and NFKC itself.
-- That bonus was an ILIKE over every candidate's search_text, and ILIKE
-- under a non-C collation lowers the whole text before it compares.
-- Issue #883 measured what that costs when a fragment is common: 360 ms
-- of a 456 ms query on 7,543 table concepts. Reproduced on a synthetic
-- corpus of 7,500 (candidates 4,712 to 7,500), it was 115 to 230 ms of
-- 170 to 430, the rest of the query being the index scan, the per-row
-- tsvector tests and the sort.
--
-- Stored lowered, the test is a LIKE against a pattern lowered once per
-- query, and the per-row work is reading the text rather than
-- rewriting it — about five times cheaper measured on the same rows.
-- Stored folded, the bonus also finally sees ＢｉｇＱｕｅｒｙ as
-- BigQuery, which 0043 left out only because folding at read time
-- would have put normalize() on every candidate.
--
-- Nothing else reads the column, so nothing else changes: the
-- english pass lowers anyway, the windows of Han and kana have no case,
-- and ochakai_identifiers lowers what it keeps.
CREATE OR REPLACE FUNCTION ochakai_knowledge_search_text() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id IS NULL THEN
        NEW.search_text := '';
        RETURN NEW;
    END IF;
    NEW.search_text := lower(normalize(
        ochakai_search_text(NEW.id, NEW.title, NEW.description, NEW.tags, NEW.body)
        || ' ' || COALESCE((SELECT string_agg(regexp_replace(x, '^.*/', ''), ' ')
                              FROM jsonb_array_elements_text(NEW.files) x), '')
        || ' ' || ochakai_synonym_text(NEW.attrs), NFKC));
    RETURN NEW;
END $$;

-- Assigning search_text to itself runs the trigger above, and the
-- generated search_tsv follows, as every migration since 0016 has done.
UPDATE object SET search_text = search_text WHERE id IS NOT NULL;
