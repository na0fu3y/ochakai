package projstore

import (
	"math"
	"testing"
)

// TestTrigramSet locks the pg_trgm.show_trgm output the 0026 §3 match
// depends on, including the CJK padding that lets 2-char queries hit.
func TestTrigramSet(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"word", []string{"  w", " wo", "wor", "ord", "rd "}},
		{"添付", []string{"  添", " 添付", "添付 "}},
		{"a", []string{"  a", " a "}},
		{"a b", []string{"  a", " a ", "  b", " b "}}, // two words, separate padding
	}
	for _, c := range cases {
		got := trigramSet(c.in)
		if len(got) != len(c.want) {
			t.Errorf("%q: got %d trigrams %v, want %d %v", c.in, len(got), keys(got), len(c.want), c.want)
			continue
		}
		for _, w := range c.want {
			if _, ok := got[w]; !ok {
				t.Errorf("%q: missing trigram %q (got %v)", c.in, w, keys(got))
			}
		}
	}
}

func TestSimilarity(t *testing.T) {
	// Identical strings are perfectly similar.
	if s := similarity("revenue", "revenue"); math.Abs(s-1) > 1e-9 {
		t.Errorf("identical similarity = %v, want 1", s)
	}
	// Disjoint strings share nothing.
	if s := similarity("revenue", "xyz"); s != 0 {
		t.Errorf("disjoint similarity = %v, want 0", s)
	}
	// Case-insensitive (pg_trgm folds case).
	if s := similarity("Revenue", "revenue"); math.Abs(s-1) > 1e-9 {
		t.Errorf("case-folded similarity = %v, want 1", s)
	}
	// Partial overlap sits strictly between 0 and 1.
	if s := similarity("revenue", "revenues"); s <= 0 || s >= 1 {
		t.Errorf("partial similarity = %v, want in (0,1)", s)
	}
	// Hand-computed value: T("添付")={"  添"," 添付","添付 "},
	// T("添付検索")={"  添"," 添付","添付検","付検索","検索 "}. Intersection
	// {"  添"," 添付"}=2, union 3+5-2=6 -> 1/3.
	if s := similarity("添付", "添付検索"); math.Abs(s-1.0/3.0) > 1e-9 {
		t.Errorf("CJK substring similarity = %v, want 1/3", s)
	}
}

func keys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
