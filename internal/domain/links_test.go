package domain

import (
	"reflect"
	"testing"
)

// The two forms SPEC §6 defines (design docs 0024 §3.3, 0046 §3.6), and
// the things that look like links but are not: external URLs,
// attachments, anchors.
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
			// The retired scheme is a URI like any other now: it addresses
			// something outside the bundle, which is exactly what it did
			// for every consumer that was not ochakai (design doc 0046
			// §3.6). A migration rewrote the bodies that used it.
			name: "the ochakai:// scheme is not a link between entries",
			id:   "insights/a",
			body: "See [revenue](ochakai://metrics/revenue), <ochakai://metrics/revenue>, and bare ochakai://tables/orders.",
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
// Fenced and inline code are skipped; indented code is not. Pinned so the
// gap is a decision rather than a surprise (see proseLines).
func TestLinksFromBodyIndentedCodeIsRead(t *testing.T) {
	body := "Example:\n\n    [r](/metrics/revenue.md)\n"
	links := LinksFromBody("insights/a", body)
	if len(links) != 1 || links[0].Target != "metrics/revenue" {
		t.Errorf("links = %+v, want the indented link read as prose", links)
	}
}

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
			// Not an edge any more, so a move leaves it exactly where it
			// is — like any other URI in the prose (design doc 0046 §3.6).
			name: "the retired scheme is left alone",
			id:   "insights/a",
			body: "See ochakai://metrics/revenue and <ochakai://metrics/revenue>.",
			want: "See ochakai://metrics/revenue and <ochakai://metrics/revenue>.",
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

// Several rewrites on one line must all land.
func TestRewriteBodyLinksMultiplePerLine(t *testing.T) {
	body := "[a](/metrics/revenue.md) then [r](/metrics/revenue.md) then [c](./revenue.md)"
	want := "[a](/m/r.md) then [r](/m/r.md) then [c](/m/r.md)"
	if got := RewriteBodyLinks("metrics/x", body, "metrics/revenue", "m/r"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A move changes what a relative target means, because links are derived
// from the body against the entry's own id. Absolutizing against the old
// id is what keeps the edge pointing where the author pointed it.
func TestAbsolutizeBodyLinks(t *testing.T) {
	for _, tc := range []struct {
		name, id, body, want string
	}{
		{
			name: "relative target resolves against the old location",
			id:   "metrics/revenue",
			body: "See [gross](./gross.md) and [net](net.md).",
			want: "See [gross](/metrics/gross.md) and [net](/metrics/net.md).",
		}, {
			name: "a parent-relative target too",
			id:   "metrics/sales/revenue",
			body: "See [orders](../orders.md).",
			want: "See [orders](/metrics/orders.md).",
		}, {
			name: "an absolute target is already stable",
			id:   "metrics/revenue",
			body: "See [g](/metrics/gross.md).",
			want: "See [g](/metrics/gross.md).",
		}, {
			name: "non-entry targets are left alone",
			id:   "metrics/revenue",
			body: "See [site](https://example.com/x.md), [chart](./chart.png), [top](#summary).",
			want: "See [site](https://example.com/x.md), [chart](./chart.png), [top](#summary).",
		}, {
			name: "a fragment survives the rewrite",
			id:   "metrics/revenue",
			body: "See [gross](./gross.md#caveats).",
			want: "See [gross](/metrics/gross.md#caveats).",
		}, {
			name: "code is left alone",
			id:   "metrics/revenue",
			body: "Real [g](./gross.md), inline `[g](./gross.md)`\n```\n[g](./gross.md)\n```",
			want: "Real [g](/metrics/gross.md), inline `[g](./gross.md)`\n```\n[g](./gross.md)\n```",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := AbsolutizeBodyLinks(tc.id, tc.body)
			if got != tc.want {
				t.Errorf("AbsolutizeBodyLinks =\n%q\nwant\n%q", got, tc.want)
			}
			// The point of the exercise: the edges read the same from the
			// new location as they did from the old one.
			before := LinksFromBody(tc.id, tc.body)
			after := LinksFromBody("elsewhere/revenue", got)
			if len(before) != len(after) {
				t.Fatalf("edge count changed: %v -> %v", before, after)
			}
			for i := range before {
				if before[i].Target != after[i].Target {
					t.Errorf("edge %d moved: %q -> %q", i, before[i].Target, after[i].Target)
				}
			}
		})
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
