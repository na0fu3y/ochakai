package store

import (
	"context"
	"testing"
)

// What migration 0044 buys, and where it stops.
//
// It buys the agreement the pin demands: the class in the database and
// the tables the binary reads answer the same for every code point, so
// ochakai can be built on the toolchain carrying Unicode 17 at all.
//
// It does not, on its own, make the characters Unicode 17 added
// findable, and no class could. They are all outside the BMP, and what
// decides a lexeme there is **PostgreSQL's own Unicode knowledge, not
// ochakai's**: a non-BMP character the parser knows becomes a token
// (mangled, but mangled identically on both sides, so a query
// reproduces it), and one it does not know becomes `blank` — nothing at
// all, on both sides, so nothing can be asked for.
//
// The line between the two cases is the server's Unicode version, and it
// moves when PostgreSQL moves. That is why it is a test: the limit is
// invisible from the Go side, it belongs to a component ochakai does not
// ship, and the day it lifts is a day ochakai can promise something it
// cannot promise now.
func TestWhatPostgresCanIndexAboveTheBMP(t *testing.T) {
	ctx := context.Background()
	s := newSearchStore(t, ctx)

	for _, tc := range []struct {
		name      string
		term      string
		indexable bool
		note      string
	}{
		{
			name: "extension B, old enough for this PostgreSQL to know",
			// In the class since 0036, and it round-trips: the parser
			// makes the same odd token of the document and of the query.
			term: "\U00020000\U00020001", indexable: true,
		},
		{
			name: "extension I, which Unicode 17 added",
			// In the class since 0044, and it does not round-trip: this
			// PostgreSQL reads it as blank, so neither side has a lexeme.
			term: "\U0002EBF0\U0002EBF1", indexable: false,
			note: "a term written in these characters is findable by nothing here, whatever the class says",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// ochakai's half, which is right either way: these are
			// characters of a script that does not space its words.
			var inClass bool
			if err := s.pool.QueryRow(ctx,
				`SELECT $1 ~ ('[' || ochakai_cjk_class() || ']')`,
				string([]rune(tc.term)[0])).Scan(&inClass); err != nil {
				t.Fatal(err)
			}
			if !inClass {
				t.Errorf("%q is outside ochakai_cjk_class(): the class and Go disagree, which is the pin's business", tc.term)
			}
			// PostgreSQL's half, which is where the answer is decided.
			var indexed bool
			if err := s.pool.QueryRow(ctx,
				`SELECT to_tsvector('simple', $1) @@ plainto_tsquery('simple', $1)`,
				tc.term).Scan(&indexed); err != nil {
				t.Fatal(err)
			}
			switch {
			case tc.indexable && !indexed:
				t.Errorf("%q stopped round-tripping through to_tsvector: a term that used to be findable is not", tc.term)
			case !tc.indexable && indexed:
				t.Errorf("PostgreSQL now indexes %q.\n\n"+
					"That is good news and it makes this row wrong: %s no longer holds, "+
					"so replace it with the recall test it was standing in for, and correct "+
					"migration 0044's prose, which says the same thing.", tc.term, tc.note)
			}
		})
	}
}
