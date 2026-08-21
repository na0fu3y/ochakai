-- The index and the query agree on one spelling of a character.
--
-- Halfwidth katakana and fullwidth latin are what a system upstream of
-- the reader produces: a warehouse export written by something
-- fixed-width, an IME that yields ＢｉｇＱｕｅｒｙ mid-sentence, a CSV
-- that came through a mainframe. Nothing in the lexical search folded
-- them, so each spelling was its own island — measured, on a base
-- holding one document of each: 「データセット」 found only the
-- normally-written one, 「ﾃﾞｰﾀｾｯﾄ」 only the halfwidth one, and neither
-- ever found the other. That is recall, not rank: the document could
-- not be found at all.
--
-- NFKC is the fold, applied once to the haystack before either pass
-- cuts it. It is the compatibility normalization the standard defines
-- for exactly this — ｦ-ﾟ to their fullwidth kana, Ａ-Ｚ to ASCII, ㍿ to
-- 株式会社 — and Postgres and Go implement the same one: 7,377 code
-- points across ASCII, kana, CJK compatibility and the halfwidth and
-- fullwidth block agree between normalize(…, NFKC) and Go's
-- golang.org/x/text/unicode/norm, which TestNFKCAgreesWithGo pins so
-- the query side and the document side cannot drift apart.
--
-- The query side folds in store.queryFragments and store.QueryTerms.
-- What does not fold is the whole-query bonus (SearchLexical): it asks
-- whether the question appears in the haystack verbatim, which is a
-- question about the raw text, and folding it would put a normalize()
-- over a multi-kilobyte body on every candidate row for a bonus that is
-- already a tiebreaker.

CREATE OR REPLACE FUNCTION ochakai_search_tsv(p_text text) RETURNS tsvector
LANGUAGE sql IMMUTABLE PARALLEL SAFE STRICT AS $$
    SELECT strip(
        to_tsvector('english', regexp_replace(
            regexp_replace(
                regexp_replace(f.folded, '[' || ochakai_cjk_class() || ']+', ' ', 'g'),
                '[^[:alnum:]]+', ' ', 'g'),
            '[^[:space:]]{255}[^[:space:]]{255}[^[:space:]]*', ' ', 'g'))
        || to_tsvector('simple', ochakai_cjk_bigrams(f.folded)))
    FROM (SELECT normalize(p_text, NFKC)) AS f(folded)
$$;

-- The lexemes already stored were cut before the fold. Assigning
-- search_text to itself is how every migration since 0016 has asked the
-- trigger to run, and the generated tsvector column follows it (0036).
UPDATE object SET search_text = search_text WHERE id IS NOT NULL;
