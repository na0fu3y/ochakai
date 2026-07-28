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
// Two forms are recognized, and they are the two SPEC §6 defines:
//
//	[text](/metrics/revenue.md)     bundle-absolute, the spec's recommended form
//	[text](./revenue.md)            relative to the entry's own directory
//
// ochakai:// used to be a third, taught as first-class. It went with
// design doc 0046 §3.6: a body travels in the bundle, and no other
// consumer can resolve a scheme ochakai invented — an exported bundle
// full of them is a bundle whose links only work here. (The scheme
// survives where it is not a portability question: MCP requires a URI
// for a resource, and that URI never leaves this server.)
//
// http(s) URLs are not links between entries (design doc 0024 §4), and
// non-.md file references are attachments (design doc 0008), so both are
// left alone.

var (
	// mdLinkRe matches an inline markdown link, capturing text and target.
	// A leading "!" (image) is excluded by the caller — images are
	// attachment references, never entry links.
	mdLinkRe = regexp.MustCompile(`(!?)\[([^\]]*)\]\(([^)\s]+)\)`)
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
		for _, m := range mdLinkRe.FindAllStringSubmatch(line, -1) {
			if m[1] == "!" { // image: an attachment reference
				continue
			}
			add(m[3], m[2])
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
	// A scheme (http, https, mailto, gs) addresses something outside the
	// bundle — including ochakai://, which is no longer a link form a
	// body may use (design doc 0046 §3.6).
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
//
// Fences and code spans only: an indented code block (four spaces) reads
// as prose, and a link inside one becomes an edge. Detecting indentation
// as code means deciding whether a four-space line is a code block or the
// continuation of a list item, and getting that wrong in the other
// direction drops an edge the author meant — a link that is missing is
// worse than an example that is counted, because nothing in the entry
// shows it is gone.
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
// A rewritten target becomes bundle-absolute ("/newID.md"). A relative
// target cannot simply have its id swapped — where it resolves to depends on the referring
// entry's own location, which a move may have just changed — so
// normalizing to absolute is the one predictable outcome (design doc
// 0024 §3.5).
func RewriteBodyLinks(id, body, oldID, newID string) string {
	return rewriteBody(body, func(target string) string {
		target, frag := splitFragment(target)
		if resolveTarget(id, target) != oldID {
			return target + frag
		}
		return "/" + newID + ".md" + frag
	})
}

// AbsolutizeBodyLinks rewrites the body's relative links into
// bundle-absolute form, resolved as if the body still lived at id.
//
// This is what a move owes the entry that moved. Links are derived from
// the body against the entry's own id (design doc 0024), so a relative
// target reads differently the moment the entry changes directory:
// "./gross.md" in metrics/revenue means metrics/gross, and the same three
// words in insights/revenue mean insights/gross. Nothing in the body
// changed, so nothing looks wrong — the edge just points somewhere else,
// or nowhere, the next time the entry is written.
//
// Absolute is the one form a move cannot reinterpret, which is the same
// reasoning 0024 §3.5 applies to the links that point at a moved entry.
// Targets that already carry a fragment keep it.
func AbsolutizeBodyLinks(id, body string) string {
	return rewriteBody(body, func(target string) string {
		path, frag := splitFragment(target)
		if path == "" || strings.HasPrefix(path, "/") || schemeRe.MatchString(path) {
			return target // already absolute, or not an entry link at all
		}
		resolved := resolveTarget(id, path)
		if resolved == "" {
			return target
		}
		return "/" + resolved + ".md" + frag
	})
}

// splitFragment separates a link target from its "#anchor" or "?query"
// tail, which a rewrite must carry over rather than drop.
func splitFragment(target string) (path, frag string) {
	if i := strings.IndexAny(target, "#?"); i >= 0 {
		return target[:i], target[i:]
	}
	return target, ""
}

// rewriteBody applies rewriteTarget to every link target in the body's
// prose, leaving fenced code blocks and inline code spans alone — the
// same reading LinksFromBody gives them, so a rewrite never invents an
// edge an example was not.
func rewriteBody(body string, rewriteTarget func(string) string) string {
	if body == "" {
		return body
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
	slices.SortFunc(edits, func(a, b edit) int { return a.lo - b.lo })
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		line = line[:e.lo] + e.text + line[e.hi:]
	}
	return line
}

// FilesFromBody extracts the bundle paths of the non-concept objects an
// entry's body points at: the images it embeds and the links it makes to
// files rather than to other concepts. id is the entry's own id, which
// relative targets resolve against.
//
// This is one half of how a file is attributed to an entry (design doc
// 0046 §3.3) — the other half is living under the entry's own `<id>/`
// namespace, which is a question about the path and needs no parsing.
// Attribution is derived rather than recorded because a bundle carries
// no place to record it: what says a diagram belongs to a metric is that
// the metric's document shows it.
//
// LinksFromBody is the same walk over the same prose, keeping what this
// one drops. The two are separate functions rather than one returning
// both because their results mean different things — an edge between
// concepts is knowledge, and a file reference is an ingredient — and
// they are indexed and asked about separately.
func FilesFromBody(id, body string) []string {
	var files []string
	seen := map[string]bool{}
	for _, line := range proseLines(body) {
		for _, m := range mdLinkRe.FindAllStringSubmatch(line, -1) {
			p := resolveFileTarget(id, m[3])
			if p == "" || seen[p] {
				continue
			}
			seen[p] = true
			files = append(files, p)
		}
	}
	return files
}

// resolveFileTarget turns one link target into the bundle path of a file,
// or "" when the target is not one: an external URL, an anchor, or a
// markdown document (which addresses a concept, and is LinksFromBody's).
func resolveFileTarget(id, target string) string {
	if i := strings.IndexAny(target, "#?"); i >= 0 {
		target = target[:i]
	}
	if target == "" || schemeRe.MatchString(target) || strings.HasSuffix(target, ".md") {
		return ""
	}
	if strings.HasPrefix(target, "/") {
		return Normalize(strings.Trim(target, "/"))
	}
	dir := path.Dir(id)
	if dir == "." {
		dir = ""
	}
	joined := path.Join(dir, target)
	if joined == "." || strings.HasPrefix(joined, "..") {
		return ""
	}
	return Normalize(joined)
}
