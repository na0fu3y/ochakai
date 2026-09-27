package domain

import "testing"

func TestMovePathValue(t *testing.T) {
	moves := ConceptMoves("computations/revenue", "finance/revenue")
	for _, c := range []struct {
		id, value  string
		absolutize bool
		want       string
		changed    bool
	}{
		// The concept itself, in each of the three forms SPEC §6.2 allows.
		{"metrics/revenue", "/computations/revenue.md", false, "/finance/revenue.md", true},
		{"metrics/revenue", "../computations/revenue.md", false, "/finance/revenue.md", true},
		{"computations/other", "revenue.md", false, "/finance/revenue.md", true},
		// A file in its namespace moved with it (design doc 0075 §5).
		{"computations/revenue", "/computations/revenue/check.py", false, "/finance/revenue/check.py", true},
		{"metrics/x", "../computations/revenue/check.py#L3", false, "/finance/revenue/check.py#L3", true},
		// Not carried by the move.
		{"metrics/revenue", "/computations/revenue-ytd.md", false, "/computations/revenue-ytd.md", false},
		{"metrics/revenue", "https://example.com/computations/revenue.md", false, "https://example.com/computations/revenue.md", false},
		{"metrics/revenue", "all queries in BigQuery project acme", false, "all queries in BigQuery project acme", false},
		{"metrics/revenue", "../../outside.md", false, "../../outside.md", false},
		{"metrics/revenue", "", false, "", false},
		// The moved concept's own relative value comes back absolute,
		// read against the directory it was written in.
		{"computations/revenue", "./attesters/eq.py", true, "/computations/attesters/eq.py", true},
		{"computations/revenue", "/references/eq.py", true, "/references/eq.py", false},
		{"computations/revenue", "all queries in BigQuery project acme", true, "all queries in BigQuery project acme", false},
		{"computations/revenue", "bigquery-public-data.thelook.orders", true, "bigquery-public-data.thelook.orders", false},
		{"computations/revenue", "attesters/eq.py", true, "/computations/attesters/eq.py", true},
	} {
		got, changed := MovePathValue(c.id, c.value, moves, c.absolutize)
		if got != c.want || changed != c.changed {
			t.Errorf("MovePathValue(%q, %q, abs=%v) = %q, %v; want %q, %v",
				c.id, c.value, c.absolutize, got, changed, c.want, c.changed)
		}
	}

	dir := PrefixMoves("teams/growth", "teams/marketing")
	if got, ok := MovePathValue("metrics/x", "/teams/growth/a/b.md", dir, false); !ok || got != "/teams/marketing/a/b.md" {
		t.Errorf("prefix move: %q, %v", got, ok)
	}
	if got, ok := MovePathValue("metrics/x", "/teams/growth-legacy/a.md", dir, false); ok {
		t.Errorf("a sibling directory sharing the prefix's spelling moved: %q", got)
	}
}
