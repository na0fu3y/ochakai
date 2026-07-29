-- A path prefix is a string, not a LIKE pattern.
--
-- An id may contain "_", and LIKE reads it as "any single character".
-- The predicate that decides which files sit inside a concept's own
-- namespace was written with LIKE, so "sales_2024" matched every path
-- under "salesX2024/" as well as its own: the lexical haystack of one
-- entry took the filenames of another's files.
--
-- Browsing has avoided LIKE for this reason since 0014, and the search
-- filters since 0041. This is the same rule, one level down, in the one
-- place it is written in SQL rather than in Go.

CREATE OR REPLACE FUNCTION ochakai_search_text(
    p_id text, p_title text, p_description text, p_tags text[], p_body text
) RETURNS text LANGUAGE sql STABLE AS $$
    SELECT p_id || ' ' || p_title || ' ' || p_description || ' '
        || array_to_string(p_tags, ' ') || ' ' || p_body || ' '
        || COALESCE((SELECT string_agg(regexp_replace(f.path, '^.*/', ''), ' ')
                       FROM object f
                      WHERE f.id IS NULL AND f.deleted_at IS NULL
                        AND starts_with(f.path, p_id || '/')
                        AND strpos(substr(f.path, length(p_id) + 2), '/') = 0), '')
$$;

-- The haystacks computed under the old predicate carry the wrong
-- filenames until something touches the row, so they are recomputed
-- once here. The row trigger does the work; assigning search_text to
-- itself is how every other migration has asked for that.
UPDATE object SET search_text = search_text WHERE id IS NOT NULL;
