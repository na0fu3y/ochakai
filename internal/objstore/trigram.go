package objstore

import "strings"

// trigram similarity is the in-memory analog of PostgreSQL pg_trgm, whose
// index the SQL store leans on for lexical ranking (store.SearchLexical).
// Japanese text is not word-tokenized, so trigrams over the raw runes plus
// the substring floor in SearchLexical are the same baseline the SQL path
// uses — the comment in migration 0001 spells this out.

// trigrams returns the set of 3-rune shingles of s. Each whitespace-
// separated word is padded with two leading blanks and one trailing blank,
// the pg_trgm convention, so short words still yield boundary trigrams.
func trigrams(s string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, word := range strings.Fields(s) {
		padded := "  " + word + " "
		r := []rune(padded)
		for i := 0; i+3 <= len(r); i++ {
			out[string(r[i:i+3])] = struct{}{}
		}
	}
	return out
}

// trigramSimilarity is the Jaccard index of two trigram sets — |∩| / |∪| —
// which is exactly pg_trgm's similarity() definition.
func trigramSimilarity(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for g := range a {
		if _, ok := b[g]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}
