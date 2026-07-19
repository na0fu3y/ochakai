package importer

import "testing"

// A fully-qualified BigQuery source normalizes to the canonical REST
// resource URL the OKF knowledge-catalog bundles use; anything else is
// kept verbatim (design doc 0016).
func TestBQResource(t *testing.T) {
	cases := map[string]string{
		"myproject.shop.orders":   "https://bigquery.googleapis.com/v2/projects/myproject/datasets/shop/tables/orders",
		"`myproject.shop.orders`": "https://bigquery.googleapis.com/v2/projects/myproject/datasets/shop/tables/orders",
		"p.d.orders_*":            "https://bigquery.googleapis.com/v2/projects/p/datasets/d/tables/orders_*",
		"shop.orders":             "shop.orders", // no project part: no canonical URL
		"orders":                  "orders",
		"a..b":                    "a..b",
	}
	for in, want := range cases {
		if got := bqResource(in); got != want {
			t.Errorf("bqResource(%q) = %q, want %q", in, got, want)
		}
	}
}

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
