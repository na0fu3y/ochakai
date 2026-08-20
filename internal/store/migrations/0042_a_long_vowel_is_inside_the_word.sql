-- The mark inside a loanword stops being a word boundary.
--
-- 0036 cut every run of Han/kana into two-character windows so that a
-- two-character Japanese term could be looked up instead of scanned
-- for, and pinned the class of characters it cuts to Go's unicode.Han,
-- unicode.Hiragana and unicode.Katakana. ー is in none of them: Unicode
-- gives the prolonged sound mark Script=Common, because it is written
-- after hiragana as readily as after katakana. So both sides of the
-- search treated it as a boundary, and データ was two runs of one
-- character each — asked for as the prefixes デ:* and タ:*, and stored
-- as the lone lexeme デ.
--
-- Measured over examples/demo before this migration: a katakana
-- question reached 11.00 of the 12 concepts in the base, and reaches
-- 2.60 after it (the harness reports this as the katakana dimension's
-- reach). The right one ranked first either way — the scorer is not
-- what was broken — and the rest followed it with a non-zero score, on
-- the strength of a shared first character. That is the corpus reading itself, which
-- is the cost 0036 exists to have removed, and it applies to most of
-- the vocabulary a data team writes in.
--
-- The halfwidth voiced marks are the same defect in the other spelling:
-- ﾃﾞｰﾀ is ﾃ + ﾞ + ｰ + ﾀ, and only ﾃ and ﾀ were in the class, so the run
-- split at both marks. ｰ, ﾞ and ﾟ join here for the reason ー does.
--
-- ・ stays out. It separates words rather than joining them, which is
-- what a boundary is for.
CREATE OR REPLACE FUNCTION ochakai_cjk_class() RETURNS text
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT '⺀-⺙⺛-⻳⼀-⿕々〇〡-〩'
        || '〸-〻ぁ-ゖゝ-ゟァ-ヺーヽ-ヿ'
        || 'ㇰ-ㇿ㋐-㋾㌀-㍗㐀-䶿一-鿿'
        || '豈-舘並-龎ｦ-ﾟ'
        || '\U00016FE2-\U00016FE3\U00016FF0-\U00016FF1'
        || '\U0001AFF0-\U0001AFF3\U0001AFF5-\U0001AFFB\U0001AFFD-\U0001AFFE'
        || '\U0001B000-\U0001B122\U0001B132\U0001B150-\U0001B152\U0001B155'
        || '\U0001B164-\U0001B167\U0001F200'
        || '\U00020000-\U0002A6DF\U0002A700-\U0002B739\U0002B740-\U0002B81D'
        || '\U0002B820-\U0002CEA1\U0002CEB0-\U0002EBE0\U0002F800-\U0002FA1D'
        || '\U00030000-\U0003134A\U00031350-\U000323AF'
$$;

-- Two edits to the string above, and both are ranges the old one wrote
-- around: ー (U+30FC) sits between ヺ and ヽ, and ｰ ﾞ ﾟ (U+FF70,
-- U+FF9E, U+FF9F) are what closing ｦ-ｯ ｱ-ﾝ up into ｦ-ﾟ takes in. ・
-- (U+30FB) is still excluded, because ー is written in and the range
-- stops before it. TestCJKClassAgreesWithGo walks every code point of
-- the affected planes and holds this string to the Go predicate that
-- cuts the query side (store.scriptWithoutSpaces), so the two halves
-- cannot drift apart again.

-- The windows already stored were cut by the old class. Assigning
-- search_text to itself is how every migration since 0016 has asked the
-- trigger to run, and the generated tsvector column follows it (0036) —
-- which is what makes the derivation change reach what is already in
-- the base rather than only what is written next.
UPDATE object SET search_text = search_text WHERE id IS NOT NULL;
