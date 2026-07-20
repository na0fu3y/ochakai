package domain

import (
	"path"
	"regexp"
	"slices"
	"strings"
)

// Links are derived from the body, never authored as a field (design doc
// 0024). OKF SPEC §5 defines cross-linking as plain markdown links and
// says the kind of relationship is carried by the surrounding prose, not
// by the link — so ochakai stores what the body says and nothing more.
//
// Five forms are recognized:
//
//	[text](/metrics/revenue.md)     bundle-absolute, the SPEC's primary form
//	[text](./revenue.md)            relative to the entry's own directory
//	[text](ochakai://metrics/revenue)
//	<ochakai://metrics/revenue>     autolink, no anchor text
//	ochakai://metrics/revenue       bare, no anchor text
//
// http(s) URLs are not links between entries (design doc 0024 §4), and
// non-.md file references are attachments (design doc 0008), so both are
// left alone.

var (
	// mdLinkRe matches an inline markdown link, capturing text and target.
	// A leading "!" (image) is excluded by the caller — images are
	// attachment references, never entry links.
	mdLinkRe = regexp.MustCompile(`(!?)\[([^\]]*)\]\(([^)\s]+)\)`)
	// bareURIRe matches ochakai:// references written without markdown
	// link syntax, with or without autolink angle brackets.
	bareURIRe = regexp.MustCompile(`<?(ochakai://[^\s<>()\[\]"']+)>?`)
	// fenceRe matches the opening or closing line of a fenced code block.
	fenceRe = regexp.MustCompile("^\\s{0,3}(```|~~~)")
	// inlineCodeRe matches a backtick code span, which is stripped before
	// links are read so a documented example never becomes an edge.
	inlineCodeRe = regexp.MustCompile("`+[^`]*`+")
)

// LinksFromBody extracts the entry's outbound links from its markdown
// body. id is the entry's own id, used to resolve relative targets and to
// drop self-links. Exact (target, text) duplicates collapse; the same
// target under two different words is kept, because naming the same
// entry twice in prose is legitimate.
func LinksFromBody(id, body string) []Link {
	var links []Link
	seen := map[Link]bool{}
	add := func(target, text string) {
		target = Normalize(resolveTarget(id, target))
		if target == "" || target == id {
			return
		}
		l := Link{Target: target, Text: strings.TrimSpace(text)}
		if seen[l] {
			return
		}
		seen[l] = true
		links = append(links, l)
	}
	for _, line := range proseLines(body) {
		consumed := map[string]bool{}
		for _, m := range mdLinkRe.FindAllStringSubmatch(line, -1) {
			if m[1] == "!" { // image: an attachment reference
				continue
			}
			consumed[m[3]] = true
			add(m[3], m[2])
		}
		for _, m := range bareURIRe.FindAllStringSubmatch(line, -1) {
			if consumed[m[1]] { // already taken as a markdown link target
				continue
			}
			add(m[1], "")
		}
	}
	return links
}

// resolveTarget turns one link target into an entry id, or "" when the
// target does not address an entry (an external URL, an attachment, an
// anchor). id is the referring entry, whose directory relative targets
// resolve against.
func resolveTarget(id, target string) string {
	if i := strings.IndexAny(target, "#?"); i >= 0 {
		target = target[:i]
	}
	if target == "" {
		return ""
	}
	if rest, ok := strings.CutPrefix(target, "ochakai://"); ok {
		return strings.Trim(rest, "/")
	}
	// Any other scheme (http, https, mailto, gs) addresses something
	// outside the bundle.
	if schemeRe.MatchString(target) {
		return ""
	}
	rest, ok := strings.CutSuffix(target, ".md")
	if !ok {
		return "" // a non-markdown file is an attachment (design doc 0008)
	}
	if strings.HasPrefix(rest, "/") {
		return strings.Trim(rest, "/")
	}
	// Relative to the referring entry's directory. path.Join cleans "..",
	// and a target that climbs above the bundle root is not an entry.
	dir := path.Dir(id)
	if dir == "." {
		dir = ""
	}
	joined := path.Join(dir, rest)
	if joined == "." || strings.HasPrefix(joined, "..") {
		return ""
	}
	return joined
}

var schemeRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)

// proseLines returns the body's lines with fenced code blocks dropped and
// inline code spans blanked, so links that appear only as documentation
// examples are not read as edges (design doc 0024 §3.4).
func proseLines(body string) []string {
	var out []string
	var fence string
	for _, line := range strings.Split(body, "\n") {
		if fence != "" {
			if strings.HasPrefix(strings.TrimSpace(line), fence) {
				fence = ""
			}
			continue
		}
		if m := fenceRe.FindStringSubmatch(line); m != nil {
			fence = m[1]
			continue
		}
		out = append(out, inlineCodeRe.ReplaceAllString(line, " "))
	}
	return out
}

// RewriteBodyLinks rewrites links that resolve to oldID so they point at
// newID, and returns the body unchanged when nothing referred to oldID.
// id is the entry whose body this is, needed to resolve relative targets.
//
// An ochakai:// target keeps that form; every other rewritten target
// becomes bundle-absolute ("/newID.md"). A relative target cannot simply
// have its id swapped — where it resolves to depends on the referring
// entry's own location, which a move may have just changed — so
// normalizing to absolute is the one predictable outcome (design doc
// 0024 §3.5).
func RewriteBodyLinks(id, body, oldID, newID string) string {
	if body == "" {
		return body
	}
	rewriteTarget := func(target string) string {
		if resolveTarget(id, target) != oldID {
			return target
		}
		if strings.HasPrefix(target, "ochakai://") {
			return "ochakai://" + newID
		}
		return "/" + newID + ".md"
	}
	var b strings.Builder
	var fence string
	for i, line := range strings.Split(body, "\n") {
		if i > 0 {
			b.WriteString("\n")
		}
		switch {
		case fence != "":
			if strings.HasPrefix(strings.TrimSpace(line), fence) {
				fence = ""
			}
		case fenceRe.MatchString(line):
			fence = fenceRe.FindStringSubmatch(line)[1]
		default:
			line = rewriteLine(line, rewriteTarget)
		}
		b.WriteString(line)
	}
	return b.String()
}

// rewriteLine applies rewriteTarget to every link target on one prose
// line, leaving inline code spans untouched.
func rewriteLine(line string, rewriteTarget func(string) string) string {
	code := inlineCodeRe.FindAllStringIndex(line, -1)
	inCode := func(lo, hi int) bool {
		for _, c := range code {
			if lo >= c[0] && hi <= c[1] {
				return true
			}
		}
		return false
	}
	// Collected in two passes, then applied right-to-left so earlier match
	// offsets stay valid.
	type edit struct {
		lo, hi int
		text   string
	}
	var edits []edit
	for _, m := range mdLinkRe.FindAllStringSubmatchIndex(line, -1) {
		if line[m[2]:m[3]] == "!" || inCode(m[0], m[1]) {
			continue
		}
		lo, hi := m[6], m[7]
		if t := rewriteTarget(line[lo:hi]); t != line[lo:hi] {
			edits = append(edits, edit{lo, hi, t})
		}
	}
	for _, m := range bareURIRe.FindAllStringSubmatchIndex(line, -1) {
		lo, hi := m[2], m[3]
		if inCode(m[0], m[1]) || overlapsLink(line, lo) {
			continue
		}
		if t := rewriteTarget(line[lo:hi]); t != line[lo:hi] {
			edits = append(edits, edit{lo, hi, t})
		}
	}
	slices.SortFunc(edits, func(a, b edit) int { return a.lo - b.lo })
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		line = line[:e.lo] + e.text + line[e.hi:]
	}
	return line
}

// overlapsLink reports whether offset lo sits inside a markdown link's
// target, where the markdown pass has already handled it.
func overlapsLink(line string, lo int) bool {
	for _, m := range mdLinkRe.FindAllStringIndex(line, -1) {
		if lo >= m[0] && lo < m[1] {
			return true
		}
	}
	return false
}
