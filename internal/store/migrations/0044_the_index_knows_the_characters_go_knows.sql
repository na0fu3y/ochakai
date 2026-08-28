-- The index knows the same characters Go does, at Unicode 17.
--
-- 0036 generated ochakai_cjk_class() from Go's unicode.Han, Hiragana and
-- Katakana — the three tables store.scriptWithoutSpaces asks — and
-- TestCJKClassAgreesWithGo pins the two together, because a character
-- the query cuts into windows and the document does not is a term that
-- can never be found, and nothing else would report it. 0042 added the
-- marks a word is written through.
--
-- Go 1.27 carries Unicode 17, and Unicode 17 gives Han 645 more
-- characters than Unicode 15 did: CJK extension I arrives at
-- U+2EBF0..U+2EE5D, extensions G and H grow their tails, and three
-- blocks that were adjacent become one range. The pin reported every one
-- of them the first time the suite ran on 1.27 — which is the whole
-- purpose of a pin, and the reason this is a migration rather than an
-- incident.
--
-- What was wrong for those characters: a query holding one was cut into
-- two-character windows (Go said Han), and the document holding it was
-- not (the class said no), so the two halves cut the same run in
-- different places.
--
-- **This does not make the new characters findable, and no class would.**
-- All 645 of them live above the BMP, and what becomes a lexeme up there
-- is decided outside this class: a non-BMP character the text-search
-- parser reads as a word character becomes a token (mangled, but mangled
-- the same way on both sides, so a query reproduces it), and one it does
-- not becomes `blank` — nothing, on both sides.
--
-- **What decides it is the database's ctype provider, not PostgreSQL's
-- version**, and this sentence used to say otherwise. Above the BMP the
-- parser asks the C library whether a character is a letter, so the
-- frontier moves with glibc: 2.36 (bookworm) reads extension I as blank,
-- 2.39 reads it as a word character — same PostgreSQL, opposite answers —
-- and a cluster in the C locale reads every non-ASCII character as a word
-- character, so it has no frontier at all. The same sentence also called
-- extension I a Unicode 17 addition; it is **Unicode 15.1**, and what 17
-- added is extension J at U+323B0.
--
-- TestWhatPostgresCanIndexAboveTheBMP pins the frontier where the
-- provider is a controlled variable — in CI, whose images are bookworm
-- (.github/workflows/ci.yaml says so beside its matrix), so a red row
-- there means the image was repointed. Off CI it reports instead, the
-- provider being whatever the developer has. What this migration settles
-- is the agreement between the class and the binary, which is what the
-- pin asks for and what lets ochakai be built on Go 1.27 at all.
--
-- The class below is generated, not edited: it is what
-- store.scriptWithoutSpaces answers over the whole code space. The test
-- prints the replacement when it fails, so the next Unicode version is a
-- copy rather than a reading of the standard.
--
-- It is spelled literally inside the BMP **except where a normalization
-- would rewrite the character**, and that exception was learned here.
-- The CJK compatibility ideographs decompose under NFC, so writing them
-- literally produces a class that survives this file and not the trip
-- through an editor, a terminal or a message: U+F900 comes back as
-- U+8C48, U+FA6D as U+8218, and the range that read 豈-舘 now runs
-- downhill. PostgreSQL refuses it outright — `invalid character range`,
-- from inside the UPDATE below, which takes the whole migration with it.
-- The escapes above F900 and FA70 are that failure, fixed where it
-- cannot recur: an escape has nothing to normalize.
--
-- **The class and the binary have to come from one Unicode version**, so
-- go.mod's `go` directive is the floor that says which: a build older
-- than the class windows fewer characters than the index holds, and a
-- build newer windows more. Neither is silent — the pin fails — but only
-- the floor keeps a build from being older in the first place. A future
-- Go that carries a new Unicode will land here again, as this one did.
CREATE OR REPLACE FUNCTION ochakai_cjk_class() RETURNS text
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT '⺀-⺙⺛-⻳⼀-⿕々〇〡-〩〸-〻'
        || 'ぁ-ゖゝ-ゟァ-ヺー-ヿㇰ-ㇿ㋐-㋾'
        || '㌀-㍗㐀-䶿一-鿿\U0000F900-\U0000FA6D'
        || '\U0000FA70-\U0000FAD9ｦ-ﾟ'
        || '\U00016FE2\U00016FE3\U00016FF0-\U00016FF6'
        || '\U0001AFF0-\U0001AFF3\U0001AFF5-\U0001AFFB'
        || '\U0001AFFD\U0001AFFE\U0001B000-\U0001B122'
        || '\U0001B132\U0001B150-\U0001B152\U0001B155'
        || '\U0001B164-\U0001B167\U0001F200'
        || '\U00020000-\U0002A6DF\U0002A700-\U0002B81D'
        || '\U0002B820-\U0002CEAD\U0002CEB0-\U0002EBE0'
        || '\U0002EBF0-\U0002EE5D\U0002F800-\U0002FA1D'
        || '\U00030000-\U0003134A\U00031350-\U00033479'
$$;

-- A class the regex engine will not compile is not caught by the update
-- below: on a base with no rows it evaluates nothing, so the migration
-- commits and the failure arrives at whoever writes the next concept —
-- `invalid character range`, from a generated column, on an insert that
-- has nothing to do with it. Ask once here, where the answer is this
-- migration's own.
DO $$
BEGIN
    IF NOT ('売' ~ ('[' || ochakai_cjk_class() || ']')) THEN
        RAISE EXCEPTION 'ochakai_cjk_class() does not hold 売: the class is not the one this migration means';
    END IF;
END
$$;

-- The lexemes already stored were cut by the old class. Assigning
-- search_text to itself is how every migration since 0016 has asked the
-- trigger to run, and the generated tsvector column follows it (0036).
-- Every row is rewritten: on a knowledge base this is thousands of rows,
-- not millions, and it runs inside the same startup the other migrations
-- do.
UPDATE object SET search_text = search_text WHERE id IS NOT NULL;
