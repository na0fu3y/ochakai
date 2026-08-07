// Search: turning a question into a ranking. Lexical fragmentation for
// pg_trgm, the two vector searches, and the ANN tuning around them. What
// narrows a search rather than orders it is in filter.go; what lists
// instead of ranking is in list.go (design doc 0050).
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
// Two characters is below what a trigram index can serve, and pg_trgm
// does not narrow the scan for such a pattern: LIKE '%売上%' has no word
// boundary to pad against, so no whole trigram can be extracted and the
// planner reads the table. Measured on 5000 entries, '%revenue%' and
// '%売上の%' are index scans at 0.2ms while '%売上%' is a sequential scan
// at 16ms. Three-character windows would restore the index and lose 売上,
// 原価, 客数 — the terms the search is for — so the scan is the price of
// finding them. SearchLexical keeps that scan cheap by scoring over ids
// alone.
func queryFragments(query string) []string {
	// Fragments are collected per run and then interleaved, so the cap
	// falls evenly across the query instead of truncating its tail. A
	// question often names its subject last ("〜の件で、直近の原価率は?"),
	// and cutting in reading order would drop exactly that.
	var runs [][]string
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
		for _, run := range scriptRuns(token) {
			runes := []rune(run)
			if !scriptWithoutSpaces(runes[0]) || len(runes) <= 2 {
				runs = append(runs, []string{run})
				continue
			}
			runs = append(runs, contentWindows(runes))
		}
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
func contentWindows(runes []rune) []string {
	all := make([]string, 0, len(runes))
	var content []string
	for i := 0; i+2 <= len(runes); i++ {
		w := string(runes[i : i+2])
		all = append(all, w)
		if contentChar(runes[i]) {
			content = append(content, w)
		}
	}
	if len(content) > 0 {
		return content
	}
	return all
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
func scriptWithoutSpaces(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r)
}

// contentChar reports whether r is the kind of character a Japanese
// content word is written in, as opposed to the hiragana that spells the
// grammar around it.
func contentChar(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Katakana, r)
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
// maintained by trigger.
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
//   - A name that *is* the query outranks every entry that merely
//     contains it. The bonus is 1.0 against a ceiling of 1.85 for
//     everything else, so this is a rule rather than a nudge: asking for
//     売上 puts 売上 first. Either name serves — the deep-linked filename
//     is a name a person chose as much as the title is.
//
// Both terms read expressions on the row the scan already has, and a name
// match implies a search_text match (title and id are both inside it), so
// the candidate set is unchanged and no index is asked for anything new.
//
// Every term of that is a substring test — against search_text, or
// against the name read off the same row — so the candidate predicate and
// the score read the same expressions, and the GIN trigram index serves
// the ones it can: patterns of three characters or more (migration 0016;
// two-character Japanese windows are answered by a scan, see
// queryFragments). Fragment matching
// replaced a similarity()/`%` pair that could not work: `%` is true only
// above pg_trgm.similarity_threshold (0.3) and a real query never scores
// that against a real document, so the candidate set was whatever the
// whole-query substring test found — nothing, for a question.
func (s *Store) SearchLexical(ctx context.Context, query string, f Filter, limit int) ([]domain.SearchHit, error) {
	where, args := f.buildWhere("k.")
	// Each pattern is escaped: unescaped, a query containing '%' matches
	// every entry and flattens the ranking ('_' matches any one character).
	args = append(args, "%"+escapeLike(query)+"%")
	wholeParam := len(args)
	// The same escaped query with no wildcards around it: ILIKE without a
	// pattern is case-insensitive equality, which is the exact-name test.
	args = append(args, escapeLike(query))
	nameParam := len(args)
	frags := queryFragments(query)
	var hit, weight, weighted, named, weights, tests []string
	for i, frag := range frags {
		args = append(args, "%"+escapeLike(frag)+"%")
		tests = append(tests, fmt.Sprintf("k.search_text ILIKE $%d", len(args)))
		hit = append(hit, fmt.Sprintf("k.search_text ILIKE $%d AS h%d", len(args), i))
		hit = append(hit, fmt.Sprintf("nm.name ILIKE $%d AS n%d", len(args), i))
		// Document frequency over the candidates, not the whole table: one
		// window pass, no extra scans.
		weight = append(weight, fmt.Sprintf(
			"ln(1 + total::float / GREATEST(count(*) FILTER (WHERE h%d) OVER (), 1)) AS w%d", i, i))
		weighted = append(weighted, fmt.Sprintf("CASE WHEN h%d THEN w%d ELSE 0 END", i, i))
		// A fragment in the name is scored with the same weight it earns
		// in the body, so a rare term stays rare wherever it lands.
		named = append(named, fmt.Sprintf("CASE WHEN n%d THEN w%d ELSE 0 END", i, i))
		weights = append(weights, fmt.Sprintf("w%d", i))
	}
	// Scoring carries ids and booleans, never rows. The window functions
	// force every candidate to be materialized before LIMIT can apply, and
	// the candidate set is an OR over fragments — one common two-character
	// window and it is most of the table. Selecting k.* through those
	// layers would push every body, and search_text (a near-copy of the
	// body) alongside it, through a sort node: tens of megabytes on a
	// corpus of a few thousand, spilling work_mem on the instance size
	// this is meant to run on. The entries are fetched once the top N is
	// known.
	q := fmt.Sprintf(`
		WITH scored AS (
			SELECT w.id,
				((%[1]s) + 0.5 * (%[2]s)) / NULLIF(%[3]s, 0)
					+ w.whole + w.named + w.verified AS score
			FROM (
				SELECT c.*, %[4]s FROM (
					SELECT k.id, %[5]s, count(*) OVER () AS total,
						CASE WHEN k.search_text ILIKE $%[6]d THEN 0.3 ELSE 0 END AS whole,
						CASE WHEN k.title ILIKE $%[7]d
							OR `+filenameText+` ILIKE $%[7]d
							THEN 1 ELSE 0 END AS named,
						CASE WHEN EXISTS (SELECT 1 FROM knowledge_verification v
							WHERE v.id = k.id) THEN 0.05 ELSE 0 END AS verified
					FROM object k, LATERAL (SELECT `+nameText+` AS name) nm
					WHERE (%[8]s) AND %[9]s
				) c
			) w
			ORDER BY score DESC LIMIT %[10]d
		)
		SELECT `+knowledgeSelectK+`, scored.score
		FROM object k JOIN scored ON scored.id = k.id
		ORDER BY scored.score DESC`,
		strings.Join(weighted, " + "), strings.Join(named, " + "),
		strings.Join(weights, " + "),
		strings.Join(weight, ", "), strings.Join(hit, ", "), wholeParam, nameParam,
		strings.Join(tests, " OR "), where, limit)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanHit)
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
func (s *Store) SearchVectorAttachments(ctx context.Context, vec []float32, model string, f Filter, limit int) ([]domain.SearchHit, error) {
	where, args := f.buildWhere("k.")
	args = append(args, encodeVector(vec))
	vecParam := len(args)
	args = append(args, model) // this model's vectors only, as in SearchVector
	q := fmt.Sprintf(`
		SELECT `+withoutDoc(knowledgeCols)+", "+ledgerCols("best")+`, score FROM (
			SELECT DISTINCT ON (k.id) k.*, 1 - (e.embedding <=> $%d::vector) AS score
			FROM object k JOIN attachment_embedding e ON k.id = e.knowledge_id AND e.model = $%d
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
