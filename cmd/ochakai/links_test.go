package main

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Translating a page rewrites its headings, and a heading is where a
// fragment link lands. docs/configuration.md kept its three anchors by
// putting <a id="…"> above the translated headings and every page since
// has copied that — from the precedent, not from a rule, because there
// was no rule and nothing that would have noticed a miss. A dead
// fragment renders as an ordinary link and takes the reader to the top
// of the right page, which is the kind of wrong that survives review.
//
// So: every relative link between the pages a user reads has to resolve,
// and so does its fragment. Design records are excluded — they are
// immutable, so a link inside one is a record of what was true when it
// was written (0058 §1.1 points at knowledge.md, which 0447 folded away,
// and its Status: header says so rather than the body being edited).
// Their two indexes are not immutable and are checked.
var (
	mdLinkRe  = regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)\)`)
	headingRe = regexp.MustCompile(`(?m)^#{1,6}\s+(.*)$`)
	anchorRe  = regexp.MustCompile(`<a id="([^"]+)"`)
	slugDrop  = regexp.MustCompile(`[^\p{L}\p{N}\p{Han}\p{Hiragana}\p{Katakana} _-]+`)
)

// slug is GitHub's heading-to-fragment rule, near enough for the ASCII
// headings this repository writes: lowercase, punctuation dropped,
// spaces to hyphens. A translated heading is not slugged by hand — it
// carries an explicit <a id="…">, which is the point.
func slug(heading string) string {
	h := strings.ToLower(strings.TrimSpace(heading))
	h = regexp.MustCompile("`[^`]*`").ReplaceAllStringFunc(h, func(s string) string {
		return strings.Trim(s, "`")
	})
	h = slugDrop.ReplaceAllString(h, "")
	return strings.ReplaceAll(h, " ", "-")
}

func fragments(t *testing.T, path string) map[string]bool {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	out := map[string]bool{}
	for _, m := range headingRe.FindAllStringSubmatch(string(body), -1) {
		out[slug(m[1])] = true
	}
	for _, m := range anchorRe.FindAllStringSubmatch(string(body), -1) {
		out[m[1]] = true
	}
	return out
}

func TestManualLinksResolve(t *testing.T) {
	// userDocs names pages as a reader sees them (README.md); on disk
	// they sit above this package.
	const root = "../../"
	named, _ := userDocs(t)
	pages := make([]string, 0, len(named)+2)
	for _, name := range named {
		pages = append(pages, root+name)
	}
	pages = append(pages, designIndex, designEnglishIndex)
	// CLAUDE.md and CONTRIBUTING.md are changer-facing and so excluded
	// from userDocs, but they are living pages whose links are meant to
	// keep resolving — and a broken one survives review the same way a
	// dead fragment does. CHANGELOG.md stays out: each of its entries
	// records the tree as it stood when written.
	pages = append(pages, root+"CLAUDE.md", root+"CONTRIBUTING.md")
	if len(pages) == 0 {
		t.Fatal("no pages to read: this check now guards nothing")
	}
	seen := 0
	for _, page := range pages {
		body, err := os.ReadFile(page)
		if err != nil {
			t.Fatalf("read %s: %v", page, err)
		}
		for _, m := range mdLinkRe.FindAllStringSubmatch(string(body), -1) {
			target := m[1]
			if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			// A leading slash is an address in the knowledge base, not in
			// this repository: [text](/metrics/revenue.md) is the SPEC §6
			// link form a concept's body uses (design doc 0046 §3.6), and
			// the pages that teach it quote it verbatim.
			if strings.HasPrefix(target, "/") {
				continue
			}
			path, frag, _ := strings.Cut(target, "#")
			if path == "" { // same-page fragment
				path = page
			} else {
				path = filepath.Join(filepath.Dir(page), path)
			}
			if decoded, err := url.PathUnescape(path); err == nil {
				path = decoded
			}
			if _, err := os.Stat(path); err != nil {
				t.Errorf("%s links to %s, which does not exist", page, target)
				continue
			}
			seen++
			if frag == "" || !strings.HasSuffix(path, ".md") {
				continue
			}
			if decoded, err := url.PathUnescape(frag); err == nil {
				frag = decoded
			}
			if !fragments(t, path)[frag] {
				t.Errorf(`%s links to %s, and %s has no such heading or <a id="%s">.

Translating a heading moves the fragment it generates. Put an explicit
anchor above the translated heading — docs/configuration.md is the worked
example — or update this link.`, page, target, path, frag)
			}
		}
	}
	if seen == 0 {
		t.Fatal("no relative links found: this check now guards nothing")
	}
}
