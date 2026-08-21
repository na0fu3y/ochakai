package store

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// Type filters match case-insensitively (design doc 0023 §3.3). This is the
// DB-free half of that guarantee: the column is folded in SQL and the
// arguments are folded in Go, so neither side can drift back to a byte
// comparison. TestIntegration covers the same rule against real rows.
func TestBuildWhereFoldsTypes(t *testing.T) {
	where, args := Filter{Types: []domain.Type{"BigQuery Table", "  METRIC  "}}.buildWhere("k.")

	if !strings.Contains(where, "lower(k.type) = ANY($1)") {
		t.Errorf("type condition must fold the column, got %q", where)
	}
	if strings.Contains(where, "k.type = ANY") {
		t.Errorf("type condition must not compare raw bytes, got %q", where)
	}
	want := []string{"bigquery table", "metric"}
	if len(args) == 0 || !reflect.DeepEqual(args[0], want) {
		t.Errorf("type args = %#v, want %#v", args, want)
	}
}

// A free type folds too: §3.3 widens the tolerance to every comparison,
// not just the recommended eight, so the fix cannot be a CanonicalType
// lookup on the read path.
func TestBuildWhereFoldsFreeTypes(t *testing.T) {
	_, args := Filter{Types: []domain.Type{"DATA CONTRACT"}}.buildWhere("")

	want := []string{"data contract"}
	if len(args) == 0 || !reflect.DeepEqual(args[0], want) {
		t.Errorf("free type args = %#v, want %#v", args, want)
	}
}

// A path scope must be matched on segment boundaries, not as a raw string
// prefix (design doc 0041 §2.2), and it must not reach for LIKE — ids
// contain "_", which LIKE reads as a wildcard. This pins the shape of the
// predicate; TestIntegrationPrefixFilterMatchesSegments proves the
// behaviour against real rows.
func TestBuildWherePrefixMatchesSegmentBoundaries(t *testing.T) {
	where, args := Filter{Prefixes: []string{"metrics", "teams/growth"}}.buildWhere("k.")

	if !strings.Contains(where, "k.id = p") || !strings.Contains(where, "left(k.id, length(p) + 1) = p || '/'") {
		t.Errorf("prefix condition must match the root and its subtree, got %q", where)
	}
	for _, bad := range []string{"LIKE", "ILIKE"} {
		if strings.Contains(where, bad) {
			t.Errorf("prefix condition must not use %s (ids may contain _), got %q", bad, where)
		}
	}
	// One array argument, not one placeholder per scope: the count of
	// scopes must not change the query text, or every extra scope is a
	// new plan for the database to compile.
	want := []string{"metrics", "teams/growth"}
	if len(args) != 1 || !reflect.DeepEqual(args[0], want) {
		t.Errorf("prefix args = %#v, want one array %#v", args, want)
	}
}

// No scopes means no condition. A filter that narrows nothing must render
// nothing, or the listing feeds would carry a predicate matching every row.
func TestBuildWhereOmitsAbsentPrefixes(t *testing.T) {
	where, args := Filter{}.buildWhere("")

	if strings.Contains(where, "unnest") {
		t.Errorf("empty filter rendered a prefix condition: %q", where)
	}
	if len(args) != 0 {
		t.Errorf("empty filter produced args %#v", args)
	}
}

// A frontmatter value arrives as text, but YAML types what it parses: a
// document saying `required: true` is indexed as a boolean and one saying
// `usage_count: 5` as a number. Matching text alone left every such key
// unaskable, and unaskable silently — zero rows rather than an error. The
// filter therefore asks for the typed spelling too, both bare and inside
// a list. TestIntegrationFrontmatterFilter proves the effect on real rows;
// this pins the arguments.
func TestBuildWhereFrontmatterTriesTypedValues(t *testing.T) {
	for _, tc := range []struct {
		name  string
		key   string
		value string
		want  []string
	}{
		{"a boolean", "required", "true", []string{
			`{"required":"true"}`, `{"required":["true"]}`,
			`{"required":true}`, `{"required":[true]}`,
		}},
		{"a number", "usage_count", "5", []string{
			`{"usage_count":"5"}`, `{"usage_count":["5"]}`,
			`{"usage_count":5}`, `{"usage_count":[5]}`,
		}},
		{"a negative number", "delta", "-1.5", []string{
			`{"delta":"-1.5"}`, `{"delta":["-1.5"]}`,
			`{"delta":-1.5}`, `{"delta":[-1.5]}`,
		}},
		// Text keeps the two forms it always had. A value that is not a
		// number or a boolean has no second reading to try, and a code
		// with a leading zero is text however much it looks like a number.
		{"a string", "owner", "finance", []string{
			`{"owner":"finance"}`, `{"owner":["finance"]}`,
		}},
		{"a leading zero is not a number", "code", "007", []string{
			`{"code":"007"}`, `{"code":["007"]}`,
		}},
		{"a date stays text", "stale_after", "2026-12-31", []string{
			`{"stale_after":"2026-12-31"}`, `{"stale_after":["2026-12-31"]}`,
		}},
		// A value may not smuggle a structure past the "no operators"
		// rule (design doc 0046 §5): what a caller types is a value.
		{"an array is not read as JSON", "systems", `["dbt"]`, []string{
			`{"systems":"[\"dbt\"]"}`, `{"systems":["[\"dbt\"]"]}`,
		}},
		{"an object is not read as JSON", "owner", `{"team":"finance"}`, []string{
			`{"owner":"{\"team\":\"finance\"}"}`, `{"owner":["{\"team\":\"finance\"}"]}`,
		}},
		{"null is not read as JSON", "owner", "null", []string{
			`{"owner":"null"}`, `{"owner":["null"]}`,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			where, args := Filter{Frontmatter: map[string]string{tc.key: tc.value}}.buildWhere("k.")

			got := make([]string, 0, len(args))
			for _, a := range args {
				b, ok := a.([]byte)
				if !ok {
					t.Fatalf("frontmatter arg %#v is not jsonb bytes", a)
				}
				got = append(got, string(b))
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("fm.%s=%s asked for %q, want %q", tc.key, tc.value, got, tc.want)
			}
			// One condition, OR-ing the forms: every form is a containment
			// test the same GIN index answers, and an AND would mean the
			// document had to have written the value every way at once.
			if strings.Count(where, "k.frontmatter @> $") != len(tc.want) {
				t.Errorf("condition %q does not test all %d forms", where, len(tc.want))
			}
			if strings.Contains(where, "@> $1 AND") {
				t.Errorf("the forms of one value must be OR-ed, got %q", where)
			}
		})
	}
}

// SearchLexical's substring floor feeds the query into an ILIKE pattern.
// '%' and '_' are ILIKE wildcards; unescaped, they would turn a literal
// search into a match-anything and flatten the ranking. TestIntegration
// covers the effect on real rows; this pins the escaping itself.
func TestEscapeLike(t *testing.T) {
	cases := map[string]string{
		"売上":     "売上",
		"a%b":    `a\%b`,
		"a_b":    `a\_b`,
		`a\b`:    `a\\b`,
		`100%_\`: `100\%\_\\`,
	}
	for in, want := range cases {
		if got := escapeLike(in); got != want {
			t.Errorf("escapeLike(%q) = %q, want %q", in, got, want)
		}
	}
}

// A query is matched by its fragments, not as a whole: the flagship input
// is a question, and a question appears verbatim in no document. Latin
// words stay whole; Japanese, which has no spaces to split on, becomes
// sliding two-character windows.
func TestQueryFragments(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query string
		want  []string
	}{
		{"english question", "why is revenue down?", []string{"why", "is", "revenue", "down"}},
		{"single word", "revenue", []string{"revenue"}},
		// Only windows that begin with a content character survive. The
		// all-hiragana ones (って, てい, のか) are grammar and match nearly
		// every entry; the ones that straddle a word boundary (ぜ売, が下)
		// mean nothing and, being rare, would be weighted highest.
		{"japanese question", "なぜ売上が下がっているのか",
			[]string{"売上", "上が", "下が"}},
		// Latin abbreviations mixed into Japanese are everyday domain
		// vocabulary. Dispatching on the token's first character would
		// send PL科目 down the latin path (matching only the exact string)
		// and cut 売上KPI into windows that break KPI apart.
		{"latin prefix", "PL科目", []string{"PL", "科目"}},
		{"latin suffix", "売上KPI", []string{"売上", "KPI"}},
		{"latin infix", "EC売上高", []string{"EC", "売上", "上高"}},
		{"all-kana query keeps its windows", "ください",
			[]string{"くだ", "ださ", "さい"}},
		// An honorific prefix is the exception to "a content word does
		// not begin with hiragana": お盆 and ご要望 are words, and the
		// window that spells them is the only one that finds them —
		// 盆の and 望の depend on the particle the document happened to
		// use. Without this, 「お盆」 finds the entry and 「お盆は」 does
		// not, which is the difference between a term and a question.
		{"honorific prefix keeps its window", "お盆の影響",
			[]string{"お盆", "盆の", "影響"}},
		{"honorific prefix in a question", "ご要望の件",
			[]string{"ご要", "要望", "望の"}},
		// Only お and ご, and only before a content character: ぜ売
		// (なぜ売上) is still a straddling fragment, and おく (〜しておく)
		// is still grammar.
		{"other hiragana still straddles", "なぜ売上が下がる",
			[]string{"売上", "上が", "下が"}},
		{"honorific before hiragana is grammar", "対応しておく",
			[]string{"対応", "応し"}},
		{"short japanese term", "売上", []string{"売上"}},
		// A loanword delimits itself: the script change on either side of
		// it is a word boundary somebody wrote, unlike the inside of a
		// run of Han and hiragana, where no rule here knows where a word
		// begins. So it leaves the run whole instead of being cut into
		// windows that collide with every other loanword sharing a
		// syllable — ット is in スクリーンショット, データセット and
		// ネットワーク alike — and the windows that used to straddle its
		// edge (トの) are not asked for at all.
		{"a loanword is one fragment", "スクリーンショットの撮り直し",
			[]string{"スクリーンショット", "撮り", "直し"}},
		{"and so is a compound one", "データセットの権限",
			[]string{"データセット", "権限"}},
		// Under the threshold, and deliberately: splitting at a katakana
		// character inside a kanji word would leave 三 and 月 as
		// one-character runs, which fragmentQuery can only ask for as
		// prefixes.
		{"a counter inside a kanji word is not a loanword", "三ヶ月の売上",
			[]string{"三ヶ", "ヶ月", "月の", "売上"}},
		{"a short loanword stays in its run", "ネコの数",
			[]string{"ネコ", "コの"}},
		// A run of one or two characters is kept whole, and that is the
		// right answer only when the run is a word. Spaces and latin
		// words cut the particles out of a question as runs of their
		// own — が, は, を — and fragmentQuery asks for a
		// one-character run as a prefix, so が arrived as が:* and
		// matched nearly every Japanese document in the base. It is the
		// rule contentWindows applies to the runs long enough to be
		// cut, reaching the ones that are not.
		{"a particle between latin words is not a fragment",
			"AI が verify を打ってよいか", []string{"AI", "verify", "打っ"}},
		{"a particle against a digit is not a fragment",
			"department は2値か", []string{"department", "2", "値か"}},
		// Kept, because the run is the word rather than a window of one:
		// asking whether a content character is *in* it, not whether it
		// begins it.
		{"a two-character word is still a fragment", "与信の判断",
			[]string{"与信", "信の", "判断"}},
		{"an honorific two-character word survives", "お茶 を 出す",
			[]string{"お茶", "出す"}},
		// The whole-query fallback: a question with nowhere else to look
		// asks for its grammar rather than asking for nothing. This is
		// contentWindows' own fallback, applied across the query.
		{"a grammar-only query keeps its runs", "か", []string{"か"}},
		{"grammar-only runs survive when nothing else does", "の と は",
			[]string{"の", "と", "は"}},
		{"mixed scripts", "revenue 売上高", []string{"revenue", "売上", "上高"}},
		{"punctuation only falls back to the query", "???", []string{"???"}},
		{"duplicate windows collapse", "ををを", []string{"をを"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := queryFragments(tc.query)
			if !slices.Equal(got, tc.want) {
				t.Errorf("queryFragments(%q) = %q, want %q", tc.query, got, tc.want)
			}
		})
	}

	// A pasted paragraph must not turn one search into hundreds of probes.
	var long strings.Builder
	for i := range 100 {
		fmt.Fprintf(&long, "term%d ", i)
	}
	if got := queryFragments(long.String()); len(got) != maxQueryFragments {
		t.Errorf("long query produced %d fragments, want the cap of %d", len(got), maxQueryFragments)
	}

	// The cap falls evenly across the query rather than truncating its
	// tail: a question often names its subject last, and cutting in
	// reading order would drop exactly that.
	got := queryFragments(
		"先日の定例で共有された年度計画の資料の件で相談があります、" +
			"店舗別の在庫回転率と発注リードタイムの数字を見ていて気づいたのですが、" +
			"直近の原価率はどうなっていますか")
	if len(got) != maxQueryFragments {
		t.Fatalf("capped query produced %d fragments, want %d", len(got), maxQueryFragments)
	}
	if !slices.Contains(got, "原価") {
		t.Errorf("the cap dropped the subject named at the end: %q", got)
	}
}

// A compound question is the name of nothing, but it names things: the
// terms between the hiragana are what a concept could be called, and the
// name rule reads them so the definitions a question asks for outrank the
// prose that merely says all its words.
func TestQueryTerms(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query string
		want  []string
	}{
		{"compound japanese question", "EC事業のアクティブ会員の継続率を計算して",
			[]string{"EC事業", "アクティブ会員", "継続率", "計算"}},
		{"single term is itself", "売上", []string{"売上"}},
		// Scripts with spaces make every word of a question a run — and a
		// question saying revenue is not a lookup of the concept called
		// revenue, which the golden set measured (six points of MRR). No
		// content character, no term; the whole-query rule still serves
		// the exact lookup.
		{"english questions carry no terms", "why is revenue down?", nil},
		{"a latin name needs the whole query", "SaaS", nil},
		{"spaces split like particles", "継続率 アクティブ会員",
			[]string{"継続率", "アクティブ会員"}},
		// A year in a question is not a name; a digit inside a word is
		// part of one.
		{"all-digit runs are not terms", "2026の売上", []string{"売上"}},
		{"digits inside a term stay", "3Q売上の内訳", []string{"3Q売上", "内訳"}},
		// The reach of the extraction, stated as a limit: a name spelled
		// with hiragana in it is cut at each hiragana and cannot be
		// extracted whole. Only the whole query names such a concept.
		{"hiragana-bearing names fall apart", "売り上げの推移",
			[]string{"売", "上", "推移"}},
		{"repeated terms collapse", "売上と売上", []string{"売上"}},
		{"hiragana only", "ください", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := QueryTerms(tc.query)
			if !slices.Equal(got, tc.want) {
				t.Errorf("QueryTerms(%q) = %q, want %q", tc.query, got, tc.want)
			}
		})
	}

	// The cap that keeps a pasted paragraph from becoming hundreds of
	// ILIKE probes, as in queryFragments.
	var long strings.Builder
	for i := range 100 {
		fmt.Fprintf(&long, "用語%d ", i)
	}
	if got := QueryTerms(long.String()); len(got) != maxQueryFragments {
		t.Errorf("long query produced %d terms, want the cap of %d", len(got), maxQueryFragments)
	}
}

// The HNSW index scan runs before the WHERE clause, so ef_search is a
// ceiling on what a vector search can return: at pgvector's default of 40
// a search for 100 rows could not fill its limit even unfiltered.
func TestEfSearchCoversTheLimit(t *testing.T) {
	cases := []struct{ limit, want int }{
		{0, 40},   // never below pgvector's default
		{5, 40},   // a small search is no worse than before
		{10, 40},  // the default still covers 4x
		{40, 160}, // headroom for the filters applied after the scan
		{100, 400},
		{500, 1000}, // pgvector's ceiling
	}
	for _, c := range cases {
		if got := efSearch(c.limit); got != c.want {
			t.Errorf("efSearch(%d) = %d, want %d", c.limit, got, c.want)
		}
		if got := efSearch(c.limit); got < c.limit {
			t.Errorf("efSearch(%d) = %d, below the limit it must cover", c.limit, got)
		}
	}
}
