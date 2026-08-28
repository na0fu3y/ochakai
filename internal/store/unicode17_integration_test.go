package store

import (
	"context"
	"os"
	"testing"
)

// What migration 0044 buys, and where it stops.
//
// It buys the agreement the pin demands: the class in the database and
// the tables the binary reads answer the same for every code point, so
// ochakai can be built on the toolchain carrying Unicode 17 at all.
// That half is ochakai's own, it holds under every server, and it is
// asserted unconditionally below.
//
// It does not, on its own, make the characters above the BMP findable,
// and no class would. What becomes a lexeme up there is decided outside
// ochakai: a character the text-search parser reads as a word character
// becomes a token (mangled, but mangled identically in the document and
// in the query, so a query reproduces it), and one it does not becomes
// `blank` — nothing at all, on both sides, so nothing can be asked for.
//
// **The decider is the database's ctype provider, not PostgreSQL.** Above
// the BMP the default parser asks the C library whether a character is a
// letter, so the frontier moves with glibc: 2.36 (bookworm) reads
// extension I as blank, 2.39 reads it as a word character. The CI
// workflow says this too, beside the matrix it explains, because both
// images resolve to bookworm today and that is what makes the pin below
// meaningful there.
//
// **So the pin is CI's, and only CI's.** In CI the ctype provider is a
// controlled variable, and a red row means the image was repointed —
// which is the signal `.github/workflows/ci.yaml` says this test exists
// to give. Anywhere else the provider is whatever the developer has, and
// the row would be reporting the machine rather than the code: glibc 2.39
// reads extension I as a word character, and a cluster in the **C locale**
// — which `scripts/check` builds when Docker cannot serve the CI image —
// has no frontier at all, since the parser reads every non-ASCII
// character as one. Both of those made this test fail locally while CI
// was green. Outside CI the frontier is reported instead.
//
// One thing is asserted wherever the probe runs, because it is nobody's
// Unicode version: **the document and the query agree.** A character that
// produces no lexeme is the limit honestly reached; one that produces a
// lexeme the query cannot reproduce would be a term stored and
// unreachable.
func TestWhatPostgresCanIndexAboveTheBMP(t *testing.T) {
	ctx := context.Background()
	s := newSearchStore(t, ctx)

	// GitHub Actions sets CI, and the workflow is where the image — and
	// so the ctype provider — is pinned. Outside it, nothing here is
	// controlled.
	pinned := os.Getenv("CI") != ""

	var ctype string
	if err := s.pool.QueryRow(ctx,
		`SELECT datctype FROM pg_database WHERE datname = current_database()`).Scan(&ctype); err != nil {
		t.Fatal(err)
	}
	t.Logf("ctype provider locale: %s (frontier %s)", ctype,
		map[bool]string{true: "pinned, this is CI", false: "reported, this is not CI"}[pinned])

	for _, tc := range []struct {
		name      string
		term      string
		unicode   string
		indexable bool // what bookworm's glibc 2.36 answers, which is CI's
		note      string
	}{
		{
			name: "extension B, old enough for every provider to know",
			// It round-trips: the parser makes the same odd token of the
			// document and of the query.
			term: "\U00020000\U00020001", unicode: "3.1", indexable: true,
		},
		{
			name: "extension I, which bookworm's glibc does not know",
			// In the class since 0044, and blank under Unicode 15.0.
			// **Extension I is Unicode 15.1, not Unicode 17** — it was
			// already assigned when Go 1.27's tables gained it, and this
			// row used to say otherwise.
			term: "\U0002EBF0\U0002EBF1", unicode: "15.1", indexable: false,
			note: "a term written in these characters is findable by nothing here, whatever the class says",
		},
		{
			name: "extension J, which Unicode 17 added",
			// The newest range Go 1.27's Han holds, and blank under every
			// glibc shipping today.
			term: "\U000323B0\U000323B1", unicode: "17.0", indexable: false,
			note: "a term written in these characters is findable by nothing here, whatever the class says",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// ochakai's half, which is right either way: these are
			// characters of a script that does not space its words, so
			// the class has to hold them however the server reads them.
			var inClass bool
			if err := s.pool.QueryRow(ctx,
				`SELECT $1 ~ ('[' || ochakai_cjk_class() || ']')`,
				string([]rune(tc.term)[0])).Scan(&inClass); err != nil {
				t.Fatal(err)
			}
			if !inClass {
				t.Errorf("%q is outside ochakai_cjk_class(): the class and Go disagree, which is the pin's business", tc.term)
			}

			// The provider's half. lexed says the document produced a
			// lexeme at all; found says the query reproduced it.
			var lexed, found bool
			if err := s.pool.QueryRow(ctx,
				`SELECT to_tsvector('simple', $1) != '',
				        to_tsvector('simple', $1) @@ plainto_tsquery('simple', $1)`,
				tc.term).Scan(&lexed, &found); err != nil {
				t.Fatal(err)
			}
			if lexed && !found {
				t.Errorf("%q is indexed but not findable: the document produced a lexeme and "+
					"the query did not reproduce it, so a term written in these characters is "+
					"stored and unreachable. This one is not a Unicode version — it is the two "+
					"sides of one parser disagreeing", tc.term)
			}
			if !pinned {
				t.Logf("Unicode %s: %s", tc.unicode, map[bool]string{
					true:  "a word character on this server",
					false: "blank on this server",
				}[lexed])
				return
			}
			switch {
			case tc.indexable && !lexed:
				t.Errorf("%q stopped round-tripping through to_tsvector: a term that used to be findable is not", tc.term)
			case !tc.indexable && lexed:
				t.Errorf("PostgreSQL now indexes %q (Unicode %s).\n\n"+
					"That is good news and it makes this row wrong: %s no longer holds. "+
					"The likeliest cause is the pgvector image being repointed off bookworm, "+
					"which is what .github/workflows/ci.yaml says this row is here to catch — "+
					"check the image's glibc, then replace this row with the recall test it "+
					"was standing in for and correct migration 0044's prose, which says the "+
					"same thing.", tc.term, tc.unicode, tc.note)
			}
		})
	}
}
