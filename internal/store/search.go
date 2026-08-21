// Search: turning a question into a ranking. Lexical fragmentation and
// the tsquery each fragment becomes, the two vector searches, and the ANN
// tuning around them. What narrows a search rather than orders it is in
// filter.go; what lists instead of ranking is in list.go (design doc
// 0050).
package store

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// maxQueryFragments bounds how many pieces a query is split into: every
// fragment is another ILIKE term on the same scan, and a pasted paragraph
// would otherwise put a hundred of them there.
const maxQueryFragments = 24

// queryFragments splits a search query into the pieces a document might
// actually contain.
//
// Matching the query as a whole is useless for the input get_context is
// built for. Its documented argument is a data question, and the recall
// hook feeds it a raw user prompt — "why is revenue down?" appears
// verbatim in no body, and trigram similarity between a short query and a
// multi-kilobyte document is 0.01-0.1 at best (for Japanese, where the
// three-character windows of a question and a body almost never align, it
// is routinely exactly 0). Neither a substring test nor a similarity
// threshold can find anything from that. The signal is not the question;
// it is the terms inside it.
//
// Latin-script tokens stay whole: "revenue" is a word, and its pieces are
// not. Runs of Han/Hiragana/Katakana have no spaces to split on, so they
// become sliding two-character windows — the standard n-gram treatment
// for Japanese, and the smallest unit that still carries meaning (売上,
// 受注, 原価).
//
// Two characters is below what a trigram index can serve — LIKE '%売上%'
// has no word boundary to pad against, so no whole trigram can be
// extracted and the planner reads the table — and for as long as the
// haystack was matched with ILIKE, that scan was the price of finding
// 売上, 原価 and 客数 at all (16ms against 0.2ms for a latin word, on
// 5000 entries, and growing with the corpus). It is not any more.
// Migration 0036 stores the document's own windows as lexemes, so the
// window this function cuts is looked up rather than searched for: the
// index entries are the same two characters the query is. What each
// fragment becomes on the way there is fragmentQuery.
func queryFragments(query string) []string {
	// Fragments are collected per run and then interleaved, so the cap
	// falls evenly across the query instead of truncating its tail. A
	// question often names its subject last ("〜の件で、直近の原価率は?"),
	// and cutting in reading order would drop exactly that.
	//
	// A short run holding no content character is collected apart from
	// the rest and used only when nothing else survives (grammarOnly
	// below).
	var runs, grammarOnly [][]string
	for _, token := range strings.FieldsFunc(query, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		// A token can still mix scripts, because FieldsFunc treats latin
		// and kanji alike as letters: "PL科目", "売上KPI", "EC売上" all
		// arrive whole. Dispatching on the first character alone would
		// send "PL科目" through the latin path (matching only the exact
		// string "PL科目") and cut "売上KPI" into windows that break KPI
		// apart. Domain vocabulary is full of these, so split at the
		// script boundary first and treat each run on its own terms.
		for _, run := range loanwordRuns(scriptRuns(token)) {
			runes := []rune(run)
			switch {
			case !scriptWithoutSpaces(runes[0]):
				runs = append(runs, []string{run})
			case len(runes) <= 2:
				// Short enough to be a word of its own — 売上, 与信 — or
				// short enough to be nothing but grammar. Which it is
				// decides whether it is asked for at all: a run this
				// short is kept whole, and fragmentQuery asks for a
				// one-character run as a prefix, so が arrives as が:*
				// and matches nearly every Japanese document there is.
				//
				// This is the rule contentWindows already applies to the
				// runs long enough to be cut ("they match nearly every
				// entry"), reaching the runs that are not. It was
				// measured by the leak the golden set gained with a
				// second domain: 「AI が verify を打ってよいか」 asked
				// for が and touched 18 concepts of a store it has
				// nothing to do with.
				if holdsContent(runes) {
					runs = append(runs, []string{run})
				} else {
					grammarOnly = append(grammarOnly, []string{run})
				}
			case katakanaRun(runes):
				// A katakana run is a word already. Unlike a run of Han
				// and hiragana, which is a sentence and has to be cut
				// into windows to find the words inside it, this one is
				// delimited by what surrounds it — the kanji, kana or
				// latin on either side — so cutting it says less than
				// keeping it whole.
				//
				// Cutting it also collides. Loanwords share long
				// substrings: ット is in スクリーンショット, データセット
				// and ネットワーク; ショ and ョン are in ショット,
				// セッション, ロケーション and ディメンション. Measured
				// over the two bundles the eval loads, ット reached 14 of
				// 28 concepts and セッ reached 16 — while 撮り, 踏む and
				// 正直 reached one each. A question about a screenshot
				// was asking for most of the base, and the words that
				// said what it was about were outnumbered eight
				// fragments to three in its own score.
				//
				// So the run stays one fragment, and fragmentTerm asks
				// the index for all of its windows at once rather than
				// any of them (the AND plainto_tsquery builds). The
				// windows are already stored; nothing about the document
				// side changes.
				runs = append(runs, []string{run})
			default:
				runs = append(runs, contentWindows(runes))
			}
		}
	}

	// A query that is nothing but grammar — ください, か — asks for it
	// rather than asking for nothing, which is contentWindows' own
	// fallback applied to the whole query instead of to one run. The
	// two have to agree: a question is dropped from the fragments only
	// when the question has somewhere else to look.
	if len(runs) == 0 {
		runs = grammarOnly
	}

	var out []string
	seen := map[string]bool{}
	for round := 0; len(out) < maxQueryFragments; round++ {
		progressed := false
		for _, run := range runs {
			if round >= len(run) {
				continue
			}
			progressed = true
			if frag := run[round]; !seen[frag] {
				seen[frag] = true
				if out = append(out, frag); len(out) >= maxQueryFragments {
					break
				}
			}
		}
		if !progressed {
			break
		}
	}
	if len(out) == 0 {
		// A single character, or punctuation only: match it as written
		// rather than matching nothing.
		if q := strings.TrimSpace(query); q != "" {
			out = append(out, q)
		}
	}
	return out
}

// contentWindows cuts a space-less run into two-character windows and
// keeps the ones that begin with a content character.
//
// Japanese grammar is written in hiragana and content words in kanji or
// katakana, so a window's first character says which it is: 売上 and 下が
// (the stem of 下がる) begin a word, while ぜ売 and が下 straddle a
// boundary and mean nothing. Keeping only the former drops both the
// function words, which match nearly every entry, and the accidental
// fragments, which match almost none — and the latter matter more than
// they look, because scoring weights rare fragments highest and a
// meaningless one is rare by construction.
//
// A run with no content character at all — an all-kana query like
// ください — keeps every window, or it would match nothing.
//
// The honorific prefixes are the exception to the first-character rule,
// because お盆 and ご要望 are content words that do begin with hiragana.
// Their window is the only fragment that finds them: the one after it
// (盆の, 望の) carries whichever particle the document happened to use,
// so 「お盆」 found an entry and 「お盆は」 found nothing until this
// branch existed.
func contentWindows(runes []rune) []string {
	all := make([]string, 0, len(runes))
	var content []string
	for i := 0; i+2 <= len(runes); i++ {
		w := string(runes[i : i+2])
		all = append(all, w)
		if contentChar(runes[i]) || honorific(runes[i], runes[i+1]) {
			content = append(content, w)
		}
	}
	if len(content) > 0 {
		return content
	}
	return all
}

// minLoanword is how long a katakana stretch has to be before it is
// taken out of the run around it as a word of its own.
//
// **Five, and it was measured rather than derived.** The floor is two
// on principle — 三ヶ月 and 一ケタ are kanji words with a katakana
// character inside them, and splitting there would leave 三 and 月 as
// one-character runs, asked for as prefixes, which is the defect this
// line of work has been removing. Everything above two was a question
// for the golden set, and it answered:
//
//	threshold   lexical MRR   reach    leak
//	(unsplit)   0.90          12.04    2.87
//	3           0.89          10.24    1.84
//	5           0.90          10.75    2.19
//
// At three, 「注文テーブルのスキーマ」 falls from first to fifth, and
// what it falls behind is worth naming because it is not this rule's
// fault. Asked for as words, that question is 注文, テーブル and
// スキーマ; スキーマ is in exactly one concept of the twenty-eight and
// in the other bundle entirely, so its rarity weight is the largest
// there is, and a concept holding only it outscores the orders table
// holding the other two. **The score is a fraction of the query's
// weight, and a term nobody else has can be most of that fraction.**
// Cutting テーブル into windows had been hiding it by spending the
// query's weight on three correlated fragments instead of one.
//
// So five is where this rule stops before it uncovers a scoring
// question it has no business answering, and the words it does take —
// スクリーンショット, セッション, パーティション, ロケーション,
// データセット — are the compounds whose windows collide in the first
// place. The scoring question is real and still open; it wants its own
// change and its own numbers.
const minLoanword = 5

// loanwordRuns splits each run at the loanwords inside it, so that
// スクリーンショットの撮り直し arrives as スクリーンショット and
// の撮り直し rather than as one stretch of sentence.
//
// **Katakana delimits itself.** A run of Han and hiragana is a sentence
// — where its words begin is a question no rule here can answer, which
// is why they are cut into windows and why the windows that begin with
// hiragana are dropped. A stretch of katakana is not: the script change
// on either side of it is the word boundary, written by whoever wrote
// the sentence. Reading it is free, and everything downstream of this
// gets easier — the loanword is asked for as a word (katakanaRun,
// fragmentTerm), and the windows that used to straddle its edge (トの
// in ショットの, ンで in ションで) are not asked for at all.
func loanwordRuns(runs []string) []string {
	var out []string
	for _, run := range runs {
		runes := []rune(run)
		if !scriptWithoutSpaces(runes[0]) {
			out = append(out, run)
			continue
		}
		start := 0
		for i := 0; i <= len(runes); i++ {
			// The end of a katakana stretch, or the end of the run.
			if i < len(runes) && (unicode.Is(unicode.Katakana, runes[i]) || joiningMark(runes[i])) {
				continue
			}
			j := i
			for j > start && (unicode.Is(unicode.Katakana, runes[j-1]) || joiningMark(runes[j-1])) {
				j--
			}
			// j..i is the katakana stretch ending here; start..j is what
			// came before it.
			if i-j >= minLoanword && katakanaRun(runes[j:i]) {
				if j > start {
					out = append(out, string(runes[start:j]))
				}
				out = append(out, string(runes[j:i]))
				start = i + 1
				if i < len(runes) {
					// The character that ended the stretch belongs to
					// what follows it.
					start = i
				}
			}
		}
		if start < len(runes) {
			out = append(out, string(runes[start:]))
		}
	}
	return out
}

// katakanaRun reports whether a run is a loanword rather than a stretch
// of sentence: every character katakana, or one of the marks written
// inside a word (joiningMark).
//
// Hiragana disqualifies it, which is the point — ですます and the
// particles are the sentence, and a run holding them is not one word.
func katakanaRun(runes []rune) bool {
	for _, r := range runes {
		if !unicode.Is(unicode.Katakana, r) && !joiningMark(r) {
			return false
		}
	}
	return true
}

// fragmentTerm is what the index is asked for on behalf of a fragment.
//
// For everything but a katakana run it is the fragment itself. A
// katakana run is asked for as all of its two-character windows at
// once: the document stores windows (migration 0036), so the word
// itself is in no lexeme, and plainto_tsquery ANDs the terms it is
// given — which turns "any window of this word" into "all of them",
// the difference between スクリーンショット reaching every concept that
// says ット and reaching the one that says the word.
//
// The fragment keeps its own spelling everywhere else: the name test
// matches it as a substring, and the snippet looks for it in the body.
// Both want the word a person typed, not the windows it was cut into.
func fragmentTerm(frag string) string {
	runes := []rune(frag)
	if len(runes) <= 2 || !katakanaRun(runes) {
		return frag
	}
	windows := make([]string, 0, len(runes)-1)
	for i := 0; i+2 <= len(runes); i++ {
		windows = append(windows, string(runes[i:i+2]))
	}
	return strings.Join(windows, " ")
}

// fragmentWindows is how many of the document's two-character windows a
// fragment stands for: one for anything asked for as itself, and the
// count of them for a loanword asked for as all of its windows at once
// (fragmentTerm).
func fragmentWindows(frag string) int {
	runes := []rune(frag)
	if len(runes) <= 2 || !katakanaRun(runes) {
		return 1
	}
	return len(runes) - 1
}

// holdsContent reports whether a short run carries a content word
// rather than spelling grammar. Any content character in it answers —
// the run is the word here, not a window of one, so the
// first-character rule contentWindows uses would drop ヶ月 and お盆
// for where the content sits rather than for whether it is there.
func holdsContent(runes []rune) bool {
	for _, r := range runes {
		if contentChar(r) {
			return true
		}
	}
	return false
}

// QueryTerms extracts the terms of a query — the names a compound
// question might call concepts by. A term is a maximal run of letters
// and digits that are not hiragana, kept only when it contains a content
// character (contentChar — Han or Katakana): hiragana spells the grammar
// between content words (の, を, して), so 「EC事業のアクティブ会員の
// 継続率を計算して」 carries the terms EC事業, アクティブ会員, 継続率
// and 計算, with no morphological analyzer. Exported because the name
// rule reads it in two places — this store's scorer and the service's
// fusion sort key — and they must agree on what a query names.
//
// The content-character condition is where the extraction stops being
// trustworthy, and it was measured, not reasoned: scripts written with
// spaces make every word of a question a run — why, is, down — and a
// question saying revenue is not a lookup of the concept called revenue.
// Granting those the name rule cost the golden set six points of lexical
// MRR (0.92 to 0.86), the questions losing their answers to the
// definitions of their own words. In the scripts without spaces the
// grammar is excluded by construction, so a term is a word somebody
// chose to write in the name-bearing scripts. The cost is that a purely
// latin name (SaaS) or an all-digit one (2026) is lifted only when the
// query is the name whole — the reach the rule had before terms existed
// — and a name with hiragana inside it (売り上げ) likewise.
func QueryTerms(query string) []string {
	runes := []rune(query)
	var terms []string
	seen := map[string]bool{}
	start, hasContent := -1, false
	flush := func(end int) {
		if start < 0 {
			return
		}
		t := string(runes[start:end])
		start = -1
		key := strings.ToLower(t)
		if !hasContent || seen[key] || len(terms) >= maxQueryFragments {
			return
		}
		seen[key] = true
		terms = append(terms, t)
	}
	for i, r := range runes {
		if (unicode.IsLetter(r) || unicode.IsDigit(r)) && !unicode.Is(unicode.Hiragana, r) {
			if start < 0 {
				start, hasContent = i, false
			}
			if contentChar(r) {
				hasContent = true
			}
			continue
		}
		flush(i)
	}
	flush(len(runes))
	return terms
}

// scriptRuns splits a token wherever it crosses between a space-less
// script (Han, kana) and anything else, so "PL科目" becomes "PL" and
// "科目".
func scriptRuns(token string) []string {
	var runs []string
	runes := []rune(token)
	start := 0
	for i := 1; i < len(runes); i++ {
		if scriptWithoutSpaces(runes[i]) != scriptWithoutSpaces(runes[start]) {
			runs = append(runs, string(runes[start:i]))
			start = i
		}
	}
	return append(runs, string(runes[start:]))
}

// scriptWithoutSpaces reports whether r belongs to a script that does not
// separate words with spaces, so a token of it needs windowing rather than
// taking whole.
//
// The three scripts, and the marks that are written inside a word
// without belonging to a script (joiningMark).
func scriptWithoutSpaces(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) || joiningMark(r)
}

// joiningMark reports whether r is one of the marks a Japanese word is
// written *through* — the prolonged sound mark that spells the long
// vowel of a loanword, and the voiced marks the halfwidth forms leave
// standing alone.
//
// None of them belongs to a script. Unicode gives them Script=Common,
// because ー is written after hiragana as readily as after katakana, and
// that answer is right for a property table and wrong for a word
// boundary: it made ー a boundary on both sides of the search at once,
// so データ was cut into デ and タ — two runs of one character, which
// fragmentQuery can only ask for as the prefixes デ:* and タ:*.
//
// **That is most of a data team's vocabulary.** テーブル, ロード,
// パーティション, カレンダー, レポート, ユーザー, サービスアカウント:
// each was a prefix scan rather than a lookup, and the corpus answered
// every one of them with most of itself — over examples/demo a katakana
// question reached 11.00 of the base's 12 concepts, the right one first
// and the rest trailing it on a shared first character. Migration 0036
// exists so a two-character Japanese term stops reading the table, and
// for every word with a long vowel in it the mark had quietly given
// that back.
//
// ・ is deliberately not here. It separates words rather than joining
// them (サービス・アカウント is two names), which is what a boundary
// is for.
func joiningMark(r rune) bool {
	switch r {
	case 'ー', // U+30FC prolonged sound mark
		'ｰ', // U+FF70 halfwidth prolonged sound mark
		'ﾞ', // U+FF9E halfwidth voiced mark
		'ﾟ': // U+FF9F halfwidth semi-voiced mark
		return true
	}
	return false
}

// contentChar reports whether r is the kind of character a Japanese
// content word is written in, as opposed to the hiragana that spells the
// grammar around it.
func contentChar(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Katakana, r)
}

// honorific reports whether first and second spell a content word behind
// an honorific prefix — お盆, ご要望 — which is the one shape in which a
// word begins with hiragana.
//
// Both conditions carry weight. Restricting it to お and ご keeps ぜ売
// (なぜ売上) a straddling fragment, and requiring a content character
// after keeps おく (〜しておく) grammar: without the second test every
// hiragana-to-kanji boundary in a question would become a fragment, which
// is what the first-character rule exists to exclude.
func honorific(first, second rune) bool {
	return (first == 'お' || first == 'ご') && contentChar(second)
}

// fragmentQuery renders the tsquery that finds one fragment in
// search_tsv, as SQL reading the fragment from parameter n.
//
// Which of the three it is follows from what the fragment is made of,
// because migration 0036 built the column that way: a space-less script
// is in there as windows and is asked for with the 'simple' dictionary,
// which normalizes case and stems nothing; everything else is in there
// stemmed by the 'english' dictionary and has to be asked for the same
// way, or "revenues" would not find the body that says "revenue" — the
// recall this replaced ILIKE to get.
//
// A one-character fragment is the exception, and it is asked for as a
// prefix. queryFragments keeps a run of one or two characters whole, so
// 額 arrives alone; the document holds two-character windows, and no
// lexeme equals 額 unless the document had it alone as well. 額:* finds
// the windows it begins. It cannot find the ones it ends — a cost worth
// naming, and cheaper than storing every unigram beside every window.
func fragmentQuery(frag string, n int) string {
	runes := []rune(frag)
	switch {
	case len(runes) == 0:
		return "''::tsquery"
	case !scriptWithoutSpaces(runes[0]):
		return fmt.Sprintf("plainto_tsquery('english', $%d::text)", n)
	case len(runes) == 1:
		// to_tsquery parses its argument, so a lexeme that happened to
		// spell an operator would be syntax rather than a term. None of
		// the scripts this branch sees has one.
		return fmt.Sprintf("to_tsquery('simple', $%d::text || ':*')", n)
	default:
		return fmt.Sprintf("plainto_tsquery('simple', $%d::text)", n)
	}
}

// nameText is the entry's name as a search matches it: the title, and the
// id's last segment beside it.
//
// Both, not the resolved one. domain.DisplayTitle picks the title when
// there is one and falls back to the filename, because a reader needs a
// single string to print; a search is not printing anything, and an entry
// titled "月次の数字" filed at metrics/売上 is named 売上 by whoever filed
// it there (design doc 0022). The parent segments stay out: under
// 売上/2026-q1 every child would otherwise be named 売上, which is what
// the prefix filter is for.
const (
	filenameText = `regexp_replace(k.id, '^.*/', '')`
	nameText     = `k.title || ' ' || ` + filenameText
)

// SearchLexical ranks entries by how much of the query they contain and
// by where it lands, verified entries boosted. The haystack is the
// search_text column (migration 0016): the id (design doc 0022 — with
// title optional, the filename may be the entry's only name), the
// envelope fields, the body, and the attachment filenames (design doc
// 0020 — "seeds" finds the entry carrying seeds.txt, embedder or not),
// maintained by trigger. Terms are matched against search_tsv, which is
// that same text as an index can hold it — stemmed latin words, and the
// two-character windows of the scripts written without spaces
// (migration 0036).
//
// The score is the fraction of the query's fragments the entry contains,
// plus a bonus when the whole query appears verbatim (a keyword search
// should outrank an entry that merely shares terms with a question), plus
// the two name terms below, plus the verified boost. An entry containing
// none of the fragments is not a result — the floor is "matched
// something", not a magic number.
//
// The name terms are what makes a keyword search find a definition. That
// haystack is one column, so a concept whose entire name is 売上 and a
// monthly report that says the word eight times in five kilobytes contain
// the same fragments and the same whole query: identical scores, and the
// order between them was whatever the scan produced. Frequency in a body
// is not aboutness, and the column cannot express the difference —
// nameText can.
//
//   - Fragments landing in the name count half again, per fragment and
//     weighted the same way, so it works for the question get_context is
//     built for and not only for a keyword: "なぜ売上が下がっているのか"
//     lifts the concept named 売上 without the question matching it whole.
//   - A name that *is* the query — or is a term of it (QueryTerms) —
//     outranks every entry that merely contains it. The bonus is 1.0
//     against a ceiling of 1.85 for everything else, so this is a rule
//     rather than a nudge: asking for 売上 puts 売上 first, and asking
//     「EC事業の継続率を計算して」 puts the definitions of EC事業 and
//     継続率 above the reports that say all its words — a compound
//     question is the name of nothing, so coverage alone handed its
//     answer to whichever prose co-mentioned the most terms. Either name
//     serves — the deep-linked filename is a name a person chose as much
//     as the title is.
//
// Both terms read expressions on the row the candidate scan already has,
// and a name match implies a search_tsv match (title and id are both
// inside the haystack it is built from), so the candidate set is
// unchanged and no index is asked for anything new.
//
// The candidate predicate is one tsquery — the fragments OR-ed together
// — so the GIN index on search_tsv answers the whole disjunction in a
// single scan, and it answers it for Japanese too: the two-character
// windows this splits a question into are exactly the lexemes migration
// 0036 stores. The per-fragment tests below repeat the same expressions,
// so what makes a row a candidate and what scores it stay one thing.
//
// The whole-query bonus is still a substring test against search_text,
// and stays one on purpose. It asks whether the question appears
// verbatim, which is a question about the raw text rather than about
// terms; it reads rows the tsquery already chose, so it costs no index.
func (s *Store) SearchLexical(ctx context.Context, query string, f Filter, limit int) ([]domain.SearchHit, error) {
	where, args := f.buildWhere("k.")
	// Each pattern is escaped: unescaped, a query containing '%' matches
	// every entry and flattens the ranking ('_' matches any one character).
	args = append(args, "%"+escapeLike(query)+"%")
	wholeParam := len(args)
	// The same escaped query with no wildcards around it: ILIKE without a
	// pattern is case-insensitive equality, which is the exact-name test.
	// Beside the whole query, each of its terms: a compound question
	// (「EC事業の継続率を計算して」) is the name of nothing, but it names
	// EC事業 and 継続率, and the definitions it asks for must not lose to
	// the reports that merely say all its words (QueryTerms).
	args = append(args, escapeLike(query))
	nameTests := []string{fmt.Sprintf(
		"k.title ILIKE $%[1]d OR "+filenameText+" ILIKE $%[1]d", len(args))}
	for _, term := range QueryTerms(query) {
		if strings.EqualFold(term, query) {
			continue // the whole-query test above already asks this
		}
		args = append(args, escapeLike(term))
		nameTests = append(nameTests, fmt.Sprintf(
			"k.title ILIKE $%[1]d OR "+filenameText+" ILIKE $%[1]d", len(args)))
	}
	frags := queryFragments(query)
	var hit, weight, weighted, named, weights, tests []string
	for i, frag := range frags {
		// Two spellings of the same fragment: the term itself, which the
		// tsquery reads, and the escaped pattern the name test reads. The
		// name is one short string on the row, so it is matched as a
		// substring rather than through the index the body needs.
		args = append(args, fragmentTerm(frag))
		match := fragmentQuery(frag, len(args))
		args = append(args, "%"+escapeLike(frag)+"%")
		tests = append(tests, match)
		hit = append(hit, fmt.Sprintf("k.search_tsv @@ %s AS h%d", match, i))
		hit = append(hit, fmt.Sprintf("nm.name ILIKE $%d AS n%d", len(args), i))
		// Document frequency over the candidates, not the whole table: one
		// window pass, no extra scans.
		//
		// A fragment no candidate has weighs nothing. Candidates are the
		// union of the fragments, so a count of zero here means the term
		// is nowhere in the corpus — and the score is a fraction whose
		// denominator is the weight of everything asked for, so leaving
		// such a term in it would dilute every row by the same amount
		// while ranking none of them. Worse, it would dilute by the
		// *largest* amount: rarity is the weight, and a term nobody has
		// is as rare as a term can be. English stopwords land here now
		// that the query is stemmed ("why is revenue down" asks for four
		// terms and two of them are dropped by the dictionary), which is
		// how this came to be measured rather than reasoned about.
		// A loanword asked for as one fragment stands for the windows it
		// was cut from (fragmentTerm), and is weighted as those windows
		// rather than as one term. Without this the unit of the score
		// changes with the script — 「注文テーブルのスキーマ」 asks for
		// three terms where the same question in kanji asks for six — and
		// a concept holding only the rarest of the three outranks the one
		// holding the other two: measured on that query as tables/orders
		// falling from first to fifth behind a concept whose only match
		// was スキーマ.
		weight = append(weight, fmt.Sprintf(
			"CASE WHEN count(*) FILTER (WHERE h%[1]d) OVER () = 0 THEN 0 "+
				"ELSE %[2]d * ln(1 + total::float / count(*) FILTER (WHERE h%[1]d) OVER ()) END AS w%[1]d",
			i, fragmentWindows(frag)))
		weighted = append(weighted, fmt.Sprintf("CASE WHEN h%d THEN w%d ELSE 0 END", i, i))
		// A fragment in the name is scored with the same weight it earns
		// in the body, so a rare term stays rare wherever it lands.
		named = append(named, fmt.Sprintf("CASE WHEN n%d THEN w%d ELSE 0 END", i, i))
		weights = append(weights, fmt.Sprintf("w%d", i))
	}
	// Scoring carries ids and booleans, never rows. The window functions
	// force every candidate to be materialized before LIMIT can apply, and
	// the candidate set is an OR over fragments — one common two-character
	// window and it is a large part of the table, found by index now
	// rather than by reading it, but still materialized. Selecting k.*
	// through those
	// layers would push every body, and search_text (a near-copy of the
	// body) alongside it, through a sort node: tens of megabytes on a
	// corpus of a few thousand, spilling work_mem on the instance size
	// this is meant to run on. The entries are fetched once the top N is
	// known.
	// Ties are broken by verification recency, then id. The score is a
	// fraction over a handful of addends, so a short query leaves several
	// concepts holding exactly the same number — and "whatever order the
	// scan produced" decided what an agent reading top-N under a byte
	// budget saw. When the text cannot tell two concepts apart, the
	// loop's own signal can: the one somebody confirmed most recently
	// goes first (the same recency sort=verified_at feeds on), and the id
	// closes the order so equal-in-every-way concepts still arrive the
	// same way twice. This breaks ties only — no weight, no addend — so
	// it cannot outrank anything the text distinguishes (compare the
	// named sort key in the service's fuse, which follows the same rule).
	q := fmt.Sprintf(`
		WITH scored AS (
			SELECT w.id, w.last_verified,
				((%[1]s) + 0.5 * (%[2]s)) / NULLIF(%[3]s, 0)
					+ w.whole + w.named
					+ CASE WHEN w.last_verified IS NOT NULL THEN 0.05 ELSE 0 END AS score
			FROM (
				SELECT c.*, %[4]s FROM (
					SELECT k.id, %[5]s, count(*) OVER () AS total,
						CASE WHEN k.search_text ILIKE $%[6]d THEN 0.3 ELSE 0 END AS whole,
						CASE WHEN %[7]s
							THEN 1 ELSE 0 END AS named,
						(SELECT max(v.at) FROM knowledge_verification v
							WHERE v.id = k.id) AS last_verified
					FROM object k, LATERAL (SELECT `+nameText+` AS name) nm
					WHERE k.search_tsv @@ (%[8]s) AND %[9]s
				) c
			) w
			ORDER BY score DESC, last_verified DESC NULLS LAST, id LIMIT %[10]d
		)
		SELECT `+knowledgeSelectK+`, scored.score
		FROM object k JOIN scored ON scored.id = k.id
		ORDER BY scored.score DESC, scored.last_verified DESC NULLS LAST, k.id`,
		strings.Join(weighted, " + "), strings.Join(named, " + "),
		strings.Join(weights, " + "),
		strings.Join(weight, ", "), strings.Join(hit, ", "), wholeParam,
		strings.Join(nameTests, " OR "),
		strings.Join(tests, " || "), where, limit)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.SearchHit, error) {
		return scanHitWithSnippet(row, frags)
	})
}

// SearchVector ranks by cosine distance against stored embeddings.
func (s *Store) SearchVector(ctx context.Context, vec []float32, model string, f Filter, limit int) ([]domain.SearchHit, error) {
	// Columns must be qualified: knowledge_embedding shares id/updated_at.
	where, args := f.buildWhere("k.")
	args = append(args, encodeVector(vec))
	vecParam := len(args)
	// Only this model's vectors: a vector written by another model lives
	// in a different space, so comparing it to this query's vector yields
	// a number that means nothing. Reembed refills the gap (design doc
	// 0020); until it runs the entry is lexical-only, which is a visible
	// absence rather than a plausible wrong ranking.
	args = append(args, model)
	q := fmt.Sprintf(`
		SELECT `+knowledgeSelectK+`, 1 - (e.embedding <=> $%d::vector) AS score
		FROM object k JOIN knowledge_embedding e ON k.id = e.id AND e.model = $%d
		WHERE %s
		ORDER BY e.embedding <=> $%d::vector LIMIT %d`, vecParam, len(args), where, vecParam, limit)
	return s.annSearch(ctx, limit, q, args)
}

// SearchVectorAttachments ranks entries by their closest attachment
// embedding (design doc 0020). Each entry appears once, carrying its
// best attachment's score — attachments never stand alone, so the hit
// is the owning entry.
//
// The vector is reached through the file object, because the file's path
// is what the vector is keyed by (design doc 0091) and attribution is a
// property of the bundle, not of the vector table: a file the body names
// from another directory ranks the concept that names it, and stops
// ranking it the moment the body stops. A file two concepts both link
// carries one vector and ranks both.
func (s *Store) SearchVectorAttachments(ctx context.Context, vec []float32, model string, f Filter, limit int) ([]domain.SearchHit, error) {
	where, args := f.buildWhere("k.")
	args = append(args, encodeVector(vec))
	vecParam := len(args)
	args = append(args, model) // this model's vectors only, as in SearchVector
	q := fmt.Sprintf(`
		SELECT `+withoutDoc(knowledgeCols)+", "+ledgerCols("best")+`, score FROM (
			SELECT DISTINCT ON (k.id) k.*, 1 - (e.embedding <=> $%d::vector) AS score
			FROM object k JOIN object f ON `+attributedTo+`
			JOIN attachment_embedding e ON e.path = f.path AND e.model = $%d
			WHERE %s
			ORDER BY k.id, e.embedding <=> $%d::vector
		) best
		ORDER BY score DESC LIMIT %d`, vecParam, len(args), where, vecParam, limit)
	return s.annSearch(ctx, limit, q, args)
}

// efSearch is how many candidates the HNSW index is asked to consider for
// a search wanting limit rows.
//
// The index scan runs first and the WHERE clause filters what it returns,
// so ef_search is a ceiling on the answer, not a starting point: at
// pgvector's default of 40, a search for 100 rows could never return more
// than 40 even before a type or tag filter took its cut. Four times the
// limit is the headroom for that filtering, floored at the default so a
// small search is no worse than before and capped at pgvector's maximum.
//
// Set per query rather than on the connection: pooled connections are
// shared, and a lingering session GUC would silently retune searches that
// never asked.
func efSearch(limit int) int {
	const (
		defaultEF = 40   // pgvector's own default
		maxEF     = 1000 // pgvector's ceiling
	)
	ef := 4 * limit
	if ef < defaultEF {
		ef = defaultEF
	}
	if ef > maxEF {
		ef = maxEF
	}
	return ef
}

// annSearch runs a vector query with ef_search sized for the limit. The
// transaction exists only to scope SET LOCAL to this one statement; it
// reads, so it ends in a rollback.
func (s *Store) annSearch(ctx context.Context, limit int, q string, args []any) ([]domain.SearchHit, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL hnsw.ef_search = %d", efSearch(limit))); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanHit)
}

// scanHit reads a knowledge row and projects it: a search result carries
// the summary, not the entry (design doc 0043 §3.5). The row is still
// selected whole — the projection is derived from the same columns every
// other read uses, and narrowing the SELECT per caller would put a second
// column list in the file to fall behind.
// scanHitWithSnippet is scanHit plus the passage that explains the hit
// (snippet.go). The body is already on the row — the final select fetches
// whole entries for the top N once scoring has decided who they are — so
// this costs no query and no round trip.
func scanHitWithSnippet(row pgx.CollectableRow, frags []string) (domain.SearchHit, error) {
	var h domain.SearchHit
	var k domain.Knowledge
	dests, finish := knowledgeDest(&k)
	if err := row.Scan(append(dests, &h.Score)...); err != nil {
		return h, err
	}
	if err := finish(); err != nil {
		return h, err
	}
	h.Summary = domain.SummaryOf(&k)
	// Only when the body is where the words landed: a name or description
	// carrying the query is already on the row, and repeating it spends
	// the caller's context to say what they can already read.
	if !namesOrDescribes(&k, frags) {
		h.Snippet = snippetFor(k.Body, frags)
	}
	return h, nil
}

// namesOrDescribes reports whether the row itself already shows the
// match — the fragment is in the title or the description.
func namesOrDescribes(k *domain.Knowledge, frags []string) bool {
	shown := strings.ToLower(k.Title + " " + k.Description)
	for _, frag := range frags {
		if frag != "" && strings.Contains(shown, strings.ToLower(frag)) {
			return true
		}
	}
	return false
}

func scanHit(row pgx.CollectableRow) (domain.SearchHit, error) {
	var h domain.SearchHit
	var k domain.Knowledge
	dests, finish := knowledgeDest(&k)
	if err := row.Scan(append(dests, &h.Score)...); err != nil {
		return h, err
	}
	if err := finish(); err != nil {
		return h, err
	}
	h.Summary = domain.SummaryOf(&k)
	return h, nil
}
