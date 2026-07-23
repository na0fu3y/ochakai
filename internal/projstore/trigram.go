package projstore

import (
	"strings"
	"unicode"
)

// trigramSet reproduces PostgreSQL pg_trgm.show_trgm for s: lowercase,
// split into words on non-alphanumeric runes, pad each word with two
// leading and one trailing space, and take every length-3 rune window.
//
//	"word"  -> {"  w", " wo", "wor", "ord", "rd "}
//	"添付"  -> {"  添", " 添付", "添付 "}   (padding makes 2-char CJK searchable)
//
// This is the basis of the 99.3% top-10 match to pg_trgm measured in
// 0026 §3. Unlike PostgreSQL, multibyte trigrams are kept as true runes
// rather than CRC32-compressed into 3 bytes, which is strictly more
// faithful (the sole source of the 0.7% divergence, always in our favor).
func trigramSet(s string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, word := range words(strings.ToLower(s)) {
		rs := make([]rune, 0, len([]rune(word))+3)
		rs = append(rs, ' ', ' ')
		rs = append(rs, []rune(word)...)
		rs = append(rs, ' ')
		for i := 0; i+3 <= len(rs); i++ {
			set[string(rs[i:i+3])] = struct{}{}
		}
	}
	return set
}

// words splits on runes pg_trgm does not treat as word characters
// (anything that is neither a Unicode letter nor number).
func words(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsNumber(r))
	})
}

// similaritySets is pg_trgm.similarity: |A∩B| / |A∪B| over trigram sets.
func similaritySets(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	// Iterate the smaller set for the intersection.
	small, large := a, b
	if len(large) < len(small) {
		small, large = large, small
	}
	inter := 0
	for t := range small {
		if _, ok := large[t]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// similarity is pg_trgm.similarity(a, b) computed from scratch.
func similarity(a, b string) float64 {
	return similaritySets(trigramSet(a), trigramSet(b))
}
