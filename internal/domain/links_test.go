package domain

import (
	"reflect"
	"testing"
)

// The five forms design doc 0024 §3.3 recognizes, and the things that
// look like links but are not: external URLs, attachments, anchors.
func TestLinksFromBody(t *testing.T) {
	for _, tc := range []struct {
		name, id, body string
		want           []Link
	}{
		{
			name: "bundle-absolute markdown link",
			id:   "insights/seasonality",
			body: "See [revenue](/metrics/revenue.md) for the definition.",
			want: []Link{{Target: "metrics/revenue", Text: "revenue"}},
		}, {
			name: "relative link resolves against the entry's directory",
			id:   "metrics/revenue",
			body: "Compare [gross](./gross.md) and [orders](../tables/orders.md).",
			want: []Link{{Target: "metrics/gross", Text: "gross"}, {Target: "tables/orders", Text: "orders"}},
		}, {
			name: "canonical URI in a markdown link",
			id:   "insights/a",
			body: "See [revenue](ochakai://metrics/revenue).",
			want: []Link{{Target: "metrics/revenue", Text: "revenue"}},
		}, {
			name: "autolink and bare URI carry no anchor text",
			id:   "insights/a",
			body: "Autolink <ochakai://metrics/revenue> and bare ochakai://tables/orders here.",
			want: []Link{{Target: "metrics/revenue"}, {Target: "tables/orders"}},
		}, {
			name: "external URLs are not entry links",
			id:   "insights/a",
			body: "See [the dashboard](https://example.com/metrics/revenue) and https://example.com/x.",
		}, {
			name: "attachments are not entry links",
			id:   "tables/orders",
			body: "![chart](orders/chart.png) and [the csv](orders/rows.csv).",
		}, {
			name: "self-links are dropped",
			id:   "metrics/revenue",
			body: "This is [itself](/metrics/revenue.md).",
		}, {
			name: "anchors and queries are trimmed off the target",
			id:   "insights/a",
			body: "See [schema](/tables/orders.md#schema).",
			want: []Link{{Target: "tables/orders", Text: "schema"}},
		}, {
			name: "exact duplicates collapse, distinct anchor texts do not",
			id:   "insights/a",
			body: "[revenue](/metrics/revenue.md), [revenue](/metrics/revenue.md), [売上](/metrics/revenue.md)",
			want: []Link{
				{Target: "metrics/revenue", Text: "revenue"},
				{Target: "metrics/revenue", Text: "売上"},
			},
		}, {
			name: "a link inside a markdown link is not counted twice",
			id:   "insights/a",
			body: "See [revenue](ochakai://metrics/revenue) only once.",
			want: []Link{{Target: "metrics/revenue", Text: "revenue"}},
		}, {
			name: "a target climbing above the bundle root is not an entry",
			id:   "a",
			body: "[up](../../elsewhere.md)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := LinksFromBody(tc.id, tc.body); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("LinksFromBody(%q) = %v, want %v", tc.body, got, tc.want)
			}
		})
	}
}

// Documented examples must not become edges — the reason the extractor
// reads prose rather than raw text (design doc 0024 §3.4).
func TestLinksFromBodySkipsCode(t *testing.T) {
	body := "Real: [revenue](/metrics/revenue.md)\n\n" +
		"```\n[fake](/metrics/fake.md)\nochakai://metrics/also-fake\n```\n\n" +
		"Inline `[nope](/metrics/nope.md)` and `ochakai://metrics/nope2` stay put.\n\n" +
		"~~~md\n[tilde-fenced](/metrics/tilde.md)\n~~~\n"
	want := []Link{{Target: "metrics/revenue", Text: "revenue"}}
	if got := LinksFromBody("insights/a", body); !reflect.DeepEqual(got, want) {
		t.Errorf("links = %v, want %v", got, want)
	}
}

// A "# Links" section is ordinary prose now: its links are read like any
// other, and its non-entry links are ignored like any other.
func TestLinksFromBodyReadsLegacyLinksSection(t *testing.T) {
	body := "Intro.\n\n# Links\n\n- [about](/metrics/revenue.md)\n- [the dashboard](https://example.com)\n"
	want := []Link{{Target: "metrics/revenue", Text: "about"}}
	if got := LinksFromBody("insights/a", body); !reflect.DeepEqual(got, want) {
		t.Errorf("links = %v, want %v", got, want)
	}
}

func TestRewriteBodyLinks(t *testing.T) {
	for _, tc := range []struct {
		name, id, body, want string
	}{
		{
			name: "bundle-absolute target",
			id:   "insights/a",
			body: "See [revenue](/metrics/revenue.md).",
			want: "See [revenue](/finance/revenue.md).",
		}, {
			name: "canonical URI keeps its form",
			id:   "insights/a",
			body: "See ochakai://metrics/revenue and <ochakai://metrics/revenue>.",
			want: "See ochakai://finance/revenue and <ochakai://finance/revenue>.",
		}, {
			name: "a relative target normalizes to bundle-absolute",
			id:   "metrics/gross",
			body: "See [revenue](./revenue.md).",
			want: "See [revenue](/finance/revenue.md).",
		}, {
			name: "code is left alone",
			id:   "insights/a",
			body: "Real [r](/metrics/revenue.md), inline `[r](/metrics/revenue.md)`\n```\n[r](/metrics/revenue.md)\n```",
			want: "Real [r](/finance/revenue.md), inline `[r](/metrics/revenue.md)`\n```\n[r](/metrics/revenue.md)\n```",
		}, {
			name: "unrelated links are untouched",
			id:   "insights/a",
			body: "See [orders](/tables/orders.md) and https://example.com/metrics/revenue.",
			want: "See [orders](/tables/orders.md) and https://example.com/metrics/revenue.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := RewriteBodyLinks(tc.id, tc.body, "metrics/revenue", "finance/revenue")
			if got != tc.want {
				t.Errorf("RewriteBodyLinks =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

// Several rewrites on one line must all land, whichever pass found them.
func TestRewriteBodyLinksMultiplePerLine(t *testing.T) {
	body := "ochakai://metrics/revenue then [r](/metrics/revenue.md) then ochakai://metrics/revenue"
	want := "ochakai://m/r then [r](/m/r.md) then ochakai://m/r"
	if got := RewriteBodyLinks("insights/a", body, "metrics/revenue", "m/r"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLinkDisplayText(t *testing.T) {
	if got := (Link{Target: "metrics/revenue", Text: "売上"}).DisplayText(); got != "売上" {
		t.Errorf("DisplayText = %q, want the anchor text", got)
	}
	// No anchor text: the target's last segment is the name (design doc 0022).
	if got := (Link{Target: "metrics/revenue"}).DisplayText(); got != "revenue" {
		t.Errorf("DisplayText = %q, want the target's last segment", got)
	}
}
