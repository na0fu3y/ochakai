package importer

import "testing"

func TestSlugify(t *testing.T) {
	for in, want := range map[string]string{
		"Revenue":          "revenue",
		"Avg Order Value":  "avg-order-value",
		"GA_sessions_2017": "ga_sessions_2017",
		"  padded  ":       "padded",
		"日本語 metric":       "metric", // non-ASCII collapses into "-", trimmed at the edges
	} {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}
