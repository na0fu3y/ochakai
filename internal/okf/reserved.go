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
	return indexDocument(dir, "", sections, notes...)
}

// SubtreeIndexDocument is IndexDocument for the root of an archive that
// carries one subtree rather than a whole base (design doc 0134). The
// scope is written in the heading and in a sentence of the body, for the
// person who unpacked it months later and is deciding whether this is the
// backup they were looking for.
//
// It used to be a frontmatter key beside okf_version as well, and SPEC §8
// does not have room for one: "Index files contain no frontmatter, with
// one exception: a bundle-root index.md MAY carry an okf_version key",
// and §11's third conformance rule holds a reserved filename to §8. A
// bundle_scope key there made every --prefix archive non-conformant to
// say a third time what the heading, the body and the archive's own
// filename already said (design doc 0134 §2).
//
// Only the root says it. Every deeper index.md is the same document a
// whole-base archive would carry at that path, because it *is* that
// document — what the archive is missing is above them, not below.
func SubtreeIndexDocument(scope string, sections []IndexSection, notes ...string) []byte {
	return indexDocument("", scope, sections, notes...)
}

func indexDocument(dir, scope string, sections []IndexSection, notes ...string) []byte {
	var b strings.Builder
	switch {
	case dir == "" && scope != "":
		fmt.Fprintf(&b, "---\nokf_version: %q\n---\n\n# ochakai knowledge bundle: %s\n\n"+
			"This bundle is the **%s** subtree of a larger knowledge base, not a whole-base backup: "+
			"everything outside %s is missing from it by design.\n\n", Version, scope, scope, scope)
	case dir == "":
		fmt.Fprintf(&b, "---\nokf_version: %q\n---\n\n# ochakai knowledge bundle\n\n", Version)
	default:
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

// FileSection is the heading the files of a directory are listed under
// (design doc 0046 §3.7). Subdirectory lines end in "/" and concept
// lines do not, so those two read as one list without help; a file line
// would not, and this is where the reader is told which kind it is.
const FileSection = "Files"

// FileIndexLine is one file's row in a directory's index.md. The name is
// what the directory holds it under and what the link resolves to; the
// description is what the file is, because OKF's §8 line wants one and
// nobody wrote prose for a .csv.
//
// Both callers render it here — the export writes the whole tree and a
// read generates one directory, and an index.md whose shape depended on
// which asked would be two formats (design doc 0046 §3.7).
func FileIndexLine(name, mediaType string, size int64) IndexLine {
	return IndexLine{Text: name, Target: name, Description: mediaType + ", " + humanSize(size)}
}

// humanSize renders a size the way a listing should read it — whole
// units, one decimal past a kilobyte. The listing is for a person
// deciding whether to open something, and "1.4 MB" answers that where
// 1428406 does not.
func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f kB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// LogLine is one recorded change, as log.md lists it.
type LogLine struct {
	At     time.Time
	Change string // create | update | move | delete | reject, the revision vocabulary
	Path   string // the object's bundle path — a concept's or a file's
	Title  string
	By     domain.Actor
	// Note is why the change was made — a rejection's reason, and no
	// other change carries one (design doc 0135). This is where a
	// ruling travels: §9 records events, and an event is the only thing
	// the format can say about a concept that is no longer there.
	Note string
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
//
// A reject line carries its reason after the actor. §9 says entries are
// prose, so there is nothing to declare and nothing for a consumer to
// parse: the sentence that says why a concept was turned down is the
// half of a ruling the next writer can act on, and this is the only
// place OKF has to put it — the link beside it points at a concept that
// is gone, which §6.1 requires consumers to tolerate.
//
// dir is the directory this document is written into, and the links are
// relative to it — the way index.md's are (IndexDocument), and the way
// SPEC §6 writes a link between two objects of one bundle. They used to
// be root-absolute ("/metrics/revenue.md"), which resolves to the
// filesystem root once somebody extracts the archive: the two generated
// files in one directory disagreed about what a bundle path means.
func LogDocument(title, dir string, lines []LogLine) []byte {
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
		fmt.Fprintf(&b, "* **%s**: [%s](%s) — %s",
			strings.ToUpper(l.Change[:1])+l.Change[1:], text, relativeTo(dir, l.Path), l.By.String())
		if l.Note != "" {
			fmt.Fprintf(&b, ": %s", strings.Join(strings.Fields(l.Note), " "))
		}
		b.WriteString("\n")
	}
	return []byte(b.String())
}

// relativeTo renders the bundle path target as it reads from inside dir.
// dir is a directory ("" is the bundle root) and target an object's path,
// both already clean.
//
// The "../" case is not hypothetical: a directory's log.md lists the
// concept the directory is named after (revisionsUnderSQL matches
// "<dir>.md" as well as the subtree), and that concept lives one level up
// — metrics/log.md records what happened to ../metrics.md.
func relativeTo(dir, target string) string {
	if dir == "" {
		return target
	}
	from, to := strings.Split(dir, "/"), strings.Split(target, "/")
	i := 0
	for i < len(from) && i < len(to)-1 && from[i] == to[i] {
		i++
	}
	return strings.Repeat("../", len(from)-i) + strings.Join(to[i:], "/")
}
