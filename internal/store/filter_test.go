package store

import (
	"reflect"
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

// SearchLexical's substring floor feeds the query into an ILIKE pattern.
// '%' and '_' are ILIKE wildcards; unescaped, they would turn a literal
// search into a match-anything and flatten the ranking. TestIntegration
// covers the effect on real rows; this pins the escaping itself.
func TestEscapeLike(t *testing.T) {
	cases := map[string]string{
		"売上":      "売上",
		"a%b":     `a\%b`,
		"a_b":     `a\_b`,
		`a\b`:     `a\\b`,
		`100%_\`:  `100\%\_\\`,
	}
	for in, want := range cases {
		if got := escapeLike(in); got != want {
			t.Errorf("escapeLike(%q) = %q, want %q", in, got, want)
		}
	}
}
