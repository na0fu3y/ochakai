-- Japanese lexical search stops reading the table, and English stops
-- missing plurals.
--
-- 0016 measured what pg_trgm could not do: '%売上%' is two characters,
-- no whole trigram can be extracted from it, and the planner reads every
-- row — 16 ms against 0.2 ms for a latin word, on 5,000 entries, and
-- growing with the corpus. 0016 called that scan the price of finding
-- 売上, 原価 and 客数 at all, and ROADMAP has carried "a better lexical
-- index has not been designed" ever since. This is that index.
--
-- The index a two-character term can be looked up in is one whose
-- entries *are* two-character terms. Every run of Han/kana in the
-- haystack is cut into the same sliding two-character windows the query
-- is cut into (store.queryFragments), and those windows are stored as
-- tsvector lexemes: the substring test '%売上%' becomes the lexeme
-- 売上, which GIN finds by equality. The corpus stops being read.
--
-- Latin text goes through to_tsvector('english', ...) in the same
-- column, which buys the other half of the problem for free: 'revenue'
-- and 'revenues' stem to one lexeme, so a plural finds a body that says
-- the singular. ILIKE could not do that at any index cost, and the
-- search that missed was recorded as a knowledge gap (0051) — a recall
-- defect arriving at the curator as "write this".
--
-- Positions are stripped. The scorer asks whether a fragment is present
-- and weights it by how rare it is across the candidates
-- (SearchLexical); it does not read term frequency, deliberately —
-- "frequency in a body is not aboutness". Reading it was tried here and
-- measured: keeping positions and breaking the score's ties with
-- ts_rank moved the golden set's MRR from 0.85 to 0.84, whether the
-- normalization divided by document length or not, so the column does
-- not carry what nothing uses.
--
-- search_text stays. It is the raw haystack the whole-query bonus still
-- reads, and it is what this column is derived from, so the one trigger
-- that has always maintained it now feeds both.

-- The scripts that do not separate words with spaces, so a term in them
-- has to be found by window rather than by word boundary. Generated
-- from Go's unicode.Han, unicode.Hiragana and unicode.Katakana — the
-- three tables store.scriptWithoutSpaces asks — and pinned to them by
-- TestCJKClassAgreesWithGo, because a character the query windows and
-- the document does not (or the reverse) is a term that can never be
-- found, and nothing else would report it.
CREATE OR REPLACE FUNCTION ochakai_cjk_class() RETURNS text
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT '⺀-⺙⺛-⻳⼀-⿕々〇〡-〩'
        || '〸-〻ぁ-ゖゝ-ゟァ-ヺヽ-ヿ'
        || 'ㇰ-ㇿ㋐-㋾㌀-㍗㐀-䶿一-鿿'
        || '豈-舘並-龎ｦ-ｯｱ-ﾝ'
        || '\U00016FE2-\U00016FE3\U00016FF0-\U00016FF1'
        || '\U0001AFF0-\U0001AFF3\U0001AFF5-\U0001AFFB\U0001AFFD-\U0001AFFE'
        || '\U0001B000-\U0001B122\U0001B132\U0001B150-\U0001B152\U0001B155'
        || '\U0001B164-\U0001B167\U0001F200'
        || '\U00020000-\U0002A6DF\U0002A700-\U0002B739\U0002B740-\U0002B81D'
        || '\U0002B820-\U0002CEA1\U0002CEB0-\U0002EBE0\U0002F800-\U0002FA1D'
        || '\U00030000-\U0003134A\U00031350-\U000323AF'
$$;

-- Every sliding two-character window of every space-less run, space
-- separated so to_tsvector reads each as its own lexeme.
--
-- All windows, unlike the query side. contentWindows keeps only the
-- windows that begin a content word, because a query has to choose what
-- to ask for; a document has to be able to answer whatever is asked, so
-- it stores every window. A run of one character is stored as itself —
-- otherwise a lone 円 between latin words would be in no lexeme at all.
--
-- Set-based rather than a loop: this runs on every write, and appending
-- to an array in plpgsql would make a 5 kB Japanese body quadratic.
CREATE OR REPLACE FUNCTION ochakai_cjk_bigrams(p_text text) RETURNS text
LANGUAGE sql IMMUTABLE PARALLEL SAFE STRICT AS $$
    SELECT COALESCE(string_agg(
        CASE WHEN char_length(run) = 1 THEN run ELSE substr(run, g, 2) END, ' '), '')
    FROM regexp_split_to_table(p_text, '[^' || ochakai_cjk_class() || ']+') AS run,
         LATERAL generate_series(1, GREATEST(char_length(run) - 1, 1)) AS g
    WHERE run <> ''
$$;

-- The haystack as an index can hold it: stemmed latin words, and the
-- windows of everything written without spaces.
--
-- The two passes read disjoint halves of the text. Han and kana are cut
-- out before the english pass, so a paragraph of Japanese never reaches
-- a stemmer that has nothing to say about it — and, more to the point,
-- never arrives as one token: the default parser reads an unbroken run
-- as a single word, and a lexeme over 2 kB raises "word is too long" —
-- which would be a document somebody could no longer save, not merely
-- one that searches badly. The second replace drops any run of 510 or
-- more non-space characters for that reason. What that long and
-- unbroken actually is, is a base64 blob pasted into a body; a URL is
-- shorter than the threshold and, below it, the default parser splits
-- it into host and path itself. (510 rather than one bound of 2047:
-- Postgres caps a regex repetition count at 255, so the floor is
-- written as two of them.)
-- Everything that is not a letter or a digit becomes a space before the
-- english pass, which is what keeps a filename findable by its parts.
-- The default parser is cleverer than that and it is the wrong kind of
-- clever here: it reads sales-seeds.txt as one host-shaped token and
-- metrics/sales/revenue as one file-shaped one, so "seeds" would not
-- find the concept carrying sales-seeds.txt — a promise design doc 0020
-- makes — and a path segment would not be a term at all, though the id
-- is in the haystack precisely so that it is one (design doc 0022).
-- Splitting on punctuation costs the parser's URL and email handling
-- and buys every word inside a name; for a haystack whose whole purpose
-- is that names are searchable, that is the right side of the trade.
CREATE OR REPLACE FUNCTION ochakai_search_tsv(p_text text) RETURNS tsvector
LANGUAGE sql IMMUTABLE PARALLEL SAFE STRICT AS $$
    SELECT strip(
        to_tsvector('english', regexp_replace(
            regexp_replace(
                regexp_replace(p_text, '[' || ochakai_cjk_class() || ']+', ' ', 'g'),
                '[^[:alnum:]]+', ' ', 'g'),
            '[^[:space:]]{255}[^[:space:]]{255}[^[:space:]]*', ' ', 'g'))
        || to_tsvector('simple', ochakai_cjk_bigrams(p_text)))
$$;

-- Generated rather than triggered: search_text already arrives correct
-- from the trigger 0016 installed (Postgres computes stored generated
-- columns after BEFORE row triggers), so there is nothing for a second
-- trigger to remember. The cost of the choice is that replacing the
-- function above does not recompute what is stored — a migration that
-- changes the derivation has to rewrite the column, the way this one
-- does by adding it.
ALTER TABLE object ADD COLUMN IF NOT EXISTS search_tsv tsvector
    GENERATED ALWAYS AS (ochakai_search_tsv(search_text)) STORED;

CREATE INDEX IF NOT EXISTS object_search_tsv ON object USING gin (search_tsv);

-- The trigram index has no reader left. SearchLexical's candidate
-- predicate was the only thing that could use it; the whole-query bonus
-- and the name test are expressions over rows already chosen, and an
-- index nobody reads is paid for on every write. pg_trgm itself stays
-- installed (0001) — retiring the extension is a change to what a
-- deployment requires, and that is not this migration's business.
DROP INDEX IF EXISTS knowledge_search_trgm;
