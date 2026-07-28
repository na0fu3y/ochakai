package store

import "testing"

// Migration 0026 rewrites the ochakai:// links out of the bodies that
// still carry them (design doc 0046 §3.6). It has to read where a bare
// URI ends the same way the parser read it while the form existed: the
// stop that ends the sentence was never part of the id, in either
// English or Japanese, and an id that legitimately carries a dot keeps
// it.
func TestRewriteOchakaiURIs(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{
			name: "a markdown link keeps its anchor text",
			body: "See [revenue](ochakai://metrics/revenue) for the definition.",
			want: "See [revenue](/metrics/revenue.md) for the definition.",
		}, {
			// A bare path in prose is not a link, so leaving one behind
			// would cost the entry an edge its writer wrote.
			name: "a bare URI becomes a whole link, named by its id",
			body: "See ochakai://metrics/revenue for the definition.",
			want: "See [metrics/revenue](/metrics/revenue.md) for the definition.",
		}, {
			name: "an autolink loses its brackets",
			body: "See <ochakai://metrics/revenue>.",
			want: "See [metrics/revenue](/metrics/revenue.md).",
		}, {
			name: "a sentence-final URI keeps its full stop",
			body: "See ochakai://metrics/revenue. Nothing else.",
			want: "See [metrics/revenue](/metrics/revenue.md). Nothing else.",
		}, {
			name: "Japanese prose keeps the particle that follows",
			body: "ochakai://metrics/revenue、を参照。",
			want: "[metrics/revenue](/metrics/revenue.md)、を参照。",
		}, {
			name: "a dot inside a latin id is part of the id",
			body: "See ochakai://ga4/events/purchase.v2 for the shape.",
			want: "See [ga4/events/purchase.v2](/ga4/events/purchase.v2.md) for the shape.",
		}, {
			name: "several on one line all land",
			body: "[a](ochakai://m/r) then ochakai://m/r then <ochakai://t/o>",
			want: "[a](/m/r.md) then [m/r](/m/r.md) then [t/o](/t/o.md)",
		}, {
			name: "a body with none is untouched",
			body: "See [revenue](/metrics/revenue.md) and https://example.com/x.",
			want: "See [revenue](/metrics/revenue.md) and https://example.com/x.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := rewriteOchakaiURIs(tc.body); got != tc.want {
				t.Errorf("rewriteOchakaiURIs(%q) =\n%q\nwant\n%q", tc.body, got, tc.want)
			}
		})
	}
}
