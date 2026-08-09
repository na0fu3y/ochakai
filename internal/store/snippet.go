package store

import (
	"strings"
	"unicode"
)

// A hit says which concept matched. It did not say *why*, and the
// difference is a round trip per row: a caller reading ten hits to decide
// which one to open had title and description, which are what somebody
// wrote about the concept as a whole, not what answered this question.
// For an agent spending a fixed context budget that is the wrong trade —
// it fetches whole documents to find out which one it wanted.
//
// So a hit carries a snippet: the passage where the query's own words
// land in the body, cut to a readable window.
//
// **Only when the body is where they landed.** A concept whose title or
// description already contains the query needs no snippet — the row
// already shows the match, and a duplicate of it costs the caller bytes
// and tells them nothing. A concept found by meaning rather than by text
// (the vector half) has no passage to point at, and inventing one — the
// opening of the body, say — would answer "why did this match?" with
// something that is not the reason.
const (
	// snippetWindow is the budget in runes, near enough two lines of a
	// terminal. Short deliberately: fifty hits carrying a paragraph each
	// would cost more context than the fetches the snippet saves.
	snippetWindow = 140
	// snippetLead is how much of the window sits before the match, so the
	// passage reads as a sentence rather than starting mid-clause.
	snippetLead = 40
)

// snippetFor returns the passage of body where one of frags occurs, or ""
// when none of them is in the body. Matching is case-insensitive, as the
// scorer's own name test is.
func snippetFor(body string, frags []string) string {
	if body == "" || len(frags) == 0 {
		return ""
	}
	lower := strings.ToLower(body)
	at, width := -1, 0
	for _, frag := range frags {
		if frag == "" {
			continue
		}
		// The earliest match across all fragments, so a question whose
		// terms are scattered gets the passage where reading would start
		// rather than wherever the fragment list happens to be ordered.
		if i := strings.Index(lower, strings.ToLower(frag)); i >= 0 && (at < 0 || i < at) {
			at, width = i, len(frag)
		}
	}
	if at < 0 {
		return ""
	}
	runes := []rune(body)
	// Byte offset to rune offset: the window is counted in runes because
	// it is a reading length, and a body is mostly Japanese here.
	start := len([]rune(body[:at]))
	end := start + len([]rune(body[at:at+width]))

	from := max(start-snippetLead, 0)
	to := min(from+snippetWindow, len(runes))
	if to < end { // a long match: keep its start rather than its middle
		to = min(end, len(runes))
	}
	// Whitespace is where a passage may begin and end without looking cut
	// mid-word. Latin has it; Japanese mostly does not, which is why this
	// only nudges the edges rather than requiring them.
	from = nudgeToBoundary(runes, from, start, -1)
	to = nudgeToBoundary(runes, to, end, 1)

	out := strings.TrimSpace(collapseSpace(string(runes[from:to])))
	if out == "" {
		return ""
	}
	if from > 0 {
		out = "…" + out
	}
	if to < len(runes) {
		out += "…"
	}
	return out
}

// nudgeToBoundary walks at most a few runes in dir looking for a space,
// without crossing limit — the match itself must stay inside the window.
func nudgeToBoundary(runes []rune, pos, limit, dir int) int {
	const reach = 12
	for i := 0; i < reach; i++ {
		p := pos + dir*i
		if p <= 0 || p >= len(runes) {
			return min(max(p, 0), len(runes))
		}
		if dir < 0 && p > limit || dir > 0 && p < limit {
			return pos
		}
		if unicode.IsSpace(runes[p]) {
			return p
		}
	}
	return pos
}

// collapseSpace puts the passage on one line: a snippet crosses markdown
// line breaks, and a caller printing it in a list wants one row.
func collapseSpace(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool {
		return r == '\n' || r == '\r' || r == '\t'
	}), " ")
}
