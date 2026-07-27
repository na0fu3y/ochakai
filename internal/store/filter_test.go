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
		{"short japanese term", "売上", []string{"売上"}},
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
