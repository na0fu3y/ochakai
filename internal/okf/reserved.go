package okf

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// The two filenames OKF reserves, and what ochakai puts in them.
//
// SPEC §3.1 names index.md (a directory listing, §8) and log.md (an
// update history, §9). ochakai has held both of these all along — the
// tree a browse walks, and the revision ledger — and served them at
// addresses it invented, in shapes it invented: /api/v1/browse and
// /api/v1/revisions/{id}. Design doc 0046 §§3.7-3.8 stops inventing.
// They are derived surfaces now, generated on demand at the paths the
// spec already names.
//
// Both are export-only in the sense that matters: an import skips them
// (they are derived, so there is nothing to read back), and the history
// in log.md is this instance's observation, published the way generated
// and verified are and never read back as a claim (design doc 0009).

// IndexLine is one row of a §8 listing: a link, and the description the
// spec says an entry SHOULD carry.
type IndexLine struct {
	Text        string
	Target      string
	Description string
}

// IndexDocument renders one directory's index.md (SPEC §8). dir is the
// bundle-relative directory, "" for the root — the one index that may
// carry frontmatter, and then only okf_version (SPEC §12).
//
// notes are rendered as a trailing line rather than dropped: a listing
// cut off at a limit that says nothing is a listing that lies about what
// the directory holds (design doc 0014's cut-off, kept and made visible).
func IndexDocument(dir string, sections []IndexSection, notes ...string) []byte {
	var b strings.Builder
	if dir == "" {
		fmt.Fprintf(&b, "---\nokf_version: %q\n---\n\n# ochakai knowledge bundle\n\n", Version)
	} else {
		fmt.Fprintf(&b, "# %s\n\n", dir)
	}
	for _, s := range sections {
		if len(s.Lines) == 0 {
			continue
		}
		if s.Heading != "" {
			fmt.Fprintf(&b, "## %s\n\n", s.Heading)
		}
		for _, l := range s.Lines {
			desc := ""
			if l.Description != "" {
				desc = " - " + l.Description
			}
			fmt.Fprintf(&b, "* [%s](%s)%s\n", l.Text, l.Target, desc)
		}
		b.WriteString("\n")
	}
	for _, n := range notes {
		fmt.Fprintf(&b, "%s\n", n)
	}
	return []byte(strings.TrimRight(b.String(), "\n") + "\n")
}

// IndexSection is a group of lines under an optional heading. SPEC §8
// describes grouped sections; a directory with only concepts in it reads
// better without a heading, so an empty one prints none.
type IndexSection struct {
	Heading string
	Lines   []IndexLine
}

// LogLine is one recorded change, as log.md lists it.
type LogLine struct {
	At     time.Time
	Change string // create | update | move | delete, the revision vocabulary
	Path   string // the object's bundle path — a concept's or a file's
	Title  string
	By     domain.Actor
}

// LogDocument renders a log.md (SPEC §9): date-grouped, newest first,
// ISO 8601 date headings, one line per change.
//
// The leading bold word is what §9 calls a convention, and the change
// vocabulary fills it — the same words the revision ledger uses, so the
// published history and the stored one say the same thing. The actor
// rides at the end because an update history whose entries do not say
// who is a history nobody can act on, and because the spec leaves the
// prose to the producer.
func LogDocument(title string, lines []LogLine) []byte {
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].At.After(lines[j].At) })
	var b strings.Builder
	if title == "" {
		title = "Update Log"
	}
	fmt.Fprintf(&b, "# %s\n", title)
	day := ""
	for _, l := range lines {
		if d := l.At.UTC().Format(domain.DateLayout); d != day {
			day = d
			fmt.Fprintf(&b, "\n## %s\n\n", d)
		}
		text := l.Title
		if text == "" {
			text = l.Path
		}
		fmt.Fprintf(&b, "* **%s**: [%s](/%s) — %s\n",
			strings.ToUpper(l.Change[:1])+l.Change[1:], text, l.Path, l.By.String())
	}
	return []byte(b.String())
}

// ReservedName reports whether a bundle path's last segment is one of the
// two names SPEC §3.1 reserves. Both are derived here, so a write to one
// is refused and a read of one is generated.
func ReservedName(path string) bool {
	base := path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		base = path[i+1:]
	}
	return base == "index.md" || base == "log.md"
}
