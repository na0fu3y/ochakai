package okf

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// FromBundle is the inverse of Bundle: it reads a path→content map (a
// bundle directory or an unpacked archive) into knowledge entries.
// Following OKF's "concept ID = file path" rule, the path minus ".md" is
// the entry's id, verbatim — the layout is the user's (design doc 0017).
// The type comes from frontmatter alone, verbatim — ochakai's types are
// the OKF vocabulary, so a foreign spelling needs no preservation to
// re-export unchanged (design doc 0023).
// index.md files are navigation that Bundle regenerates, and log.md files
// are the other OKF-reserved name (update history, SPEC §3) — both are
// dropped rather than stored, and each says so in a note, because a
// producer who hand-wrote one is losing bytes (design doc 0079). Hidden
// paths (.git trees, macOS tar's AppleDouble ._* siblings, .DS_Store) are
// skipped silently: they were never knowledge.
//
// Files referenced by a concept's body markdown links become that
// entry's files — attribution is by reference first, so any
// producer's layout works (design doc 0008); the original path is
// preserved for re-export. Unreferenced non-markdown files sitting in an
// entry's canonical namespace ("<id>/<name>") attach to that entry
// (design doc 0013).
//
// Everything else the bundle carried comes back in loose, at the path it
// arrived at: a file no entry references, and a markdown document with
// no type — which SPEC §11 makes not-a-concept, and which is therefore a
// markdown file rather than a broken entry. What enters the bundle
// leaves it (design doc 0046 §3.2), so the skip report is down to the
// paths that cannot be stored at all: a segment that starts with a dot,
// a name OKF reserves for a generated file, an empty file, one past the
// size limit. Those are reported as "path: reason" lines rather than
// failing the whole bundle.
//
// There is no archive unwrapping: `tar czf ga4.tgz ga4/` imports under
// "ga4/" — the packed shape is the structure, and a wrapper directory is
// how a bundle keeps its own namespace (design doc 0017 §4.3).
func FromBundle(files map[string][]byte) (entries []Doc, atts []AttributedFile, loose []BundleFile, skipped, notes []string) {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var nonMarkdown []string
	for _, p := range paths {
		clean := cleanPath(p)
		if hiddenPath(clean) {
			continue
		}
		if base := path.Base(clean); domain.ReservedBundleName(base) {
			// SPEC §8 makes index.md regenerable and §11 forbids requiring
			// one, so ochakai regenerates both reserved files from the
			// bundle rather than storing them (design doc 0075 §4.2) — but
			// a producer who hand-wrote one loses those bytes, and this was
			// the last place an import destroyed something without saying
			// so (design doc 0079). Saying it costs a note, which --strict
			// reads as the refusal it is.
			notes = append(notes, fmt.Sprintf(
				"%s: %s is a filename OKF reserves (SPEC §3.1) and ochakai generates from the bundle, "+
					"not stored in it; not imported", p, base))
			continue
		}
		if !strings.HasSuffix(clean, ".md") {
			nonMarkdown = append(nonMarkdown, p)
			continue
		}
		d, docNotes, err := fromBundleFile(clean, files[p])
		if err != nil {
			// A markdown file that is not a concept is still a file the
			// bundle carried (design doc 0046 §3.2). SPEC §11 makes the
			// type key what a concept has, so a document without one is
			// not a broken concept — and one that does not parse as
			// frontmatter at all is a markdown file, which is a thing a
			// producer's bundle is full of.
			nonMarkdown = append(nonMarkdown, p)
			continue
		}
		for _, n := range docNotes {
			notes = append(notes, p+": "+n)
		}
		entries = append(entries, *d)
	}

	concepts := make([]*domain.Knowledge, len(entries))
	for i := range entries {
		concepts[i] = &entries[i].Knowledge
	}
	atts, used := resolveFiles(files, concepts)
	for _, p := range nonMarkdown {
		clean := cleanPath(p)
		if used[clean] {
			continue
		}
		// Nobody's file is still the bundle's file. What enters leaves
		// (design doc 0046 §3.2): the only thing that cannot be kept is
		// a path ochakai cannot address — a segment starting with a dot,
		// a name OKF reserves for a generated file, an empty file, or
		// one past the size limit.
		if err := loadable(clean, files[p]); err != nil {
			skipped = append(skipped, p+": "+err.Error())
			continue
		}
		// A markdown file that is not a concept cannot keep its `.md`
		// spelling: that address is a concept's, and a bundle carrying a
		// typeless `.md` is the non-conformant one SPEC §11 describes
		// (design doc 0100). Renaming rather than refusing, because what
		// this path restores is somebody's bundle and losing bytes on the
		// way back in is the worse answer — so the file arrives, at a
		// spelling that no longer claims to be a concept, and the note says
		// so (the same shape design doc 0079 gave a hand-written index.md).
		if stored := unclaimMarkdown(clean, files); stored != clean {
			notes = append(notes, fmt.Sprintf(
				"%s: a `.md` file carries no `type`, so it is not a concept (SPEC §3.1, §11) "+
					"and cannot hold a concept's address; imported as %s", p, stored))
			clean = stored
		}
		loose = append(loose, BundleFile{Path: clean, Data: files[p]})
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	sort.Slice(loose, func(i, j int) bool { return loose[i].Path < loose[j].Path })
	return entries, atts, loose, skipped, notes
}

// unclaimMarkdown is the path a non-concept markdown file is stored at:
// its own, with `.md` traded for `.markdown` so it stops sitting at an
// address SPEC §3.1 reserves for concept documents (design doc 0100 §3).
// The suffix is appended again on the rare collision rather than
// overwriting the file already there — the migration that renamed the
// stored ones settles the same question the same way.
func unclaimMarkdown(clean string, files map[string][]byte) string {
	taken := func(p string) bool {
		for q := range files {
			if cleanPath(q) == p {
				return true
			}
		}
		return false
	}
	if !strings.HasSuffix(clean, ".md") {
		return clean
	}
	stored := strings.TrimSuffix(clean, ".md") + ".markdown"
	for taken(stored) {
		stored += ".markdown"
	}
	return stored
}

// BundleFile is one object of the bundle that belongs to no entry: a
// producer's seed data in a shared directory, a diagram nothing links
// yet, a markdown file that carries no type. It is written at the path
// it arrived at (design doc 0046 §§3.2, 3.5), and whether an entry ever
// claims it is a question that entry's body answers (§3.3).
type BundleFile struct {
	Path string
	Data []byte
}

// loadable reports why a bundle path cannot be stored, or nil.
func loadable(clean string, data []byte) error {
	if !domain.ValidBundlePath(clean) {
		return fmt.Errorf("not an addressable bundle path")
	}
	if len(data) == 0 {
		return fmt.Errorf("empty file")
	}
	if len(data) > domain.MaxFileSize {
		return fmt.Errorf("exceeds %d MiB", domain.MaxFileSize>>20)
	}
	return nil
}

// cleanPath canonicalizes one bundle path: relative prefix stripped,
// path.Clean'd, and NFC-normalized (design doc 0022) — macOS
// filesystems hand names back NFD-decomposed, and the same visible path
// must yield the same entry id and file references.
func cleanPath(p string) string {
	return domain.Normalize(path.Clean(strings.TrimPrefix(p, "./")))
}

// hiddenPath reports whether any segment of the (cleaned) path starts
// with a dot: .git trees, .DS_Store, and the AppleDouble ._* files macOS
// tar interleaves. Never knowledge — skipped without a report, matching
// how directory imports already treat dot entries.
func hiddenPath(clean string) bool {
	for _, seg := range strings.Split(clean, "/") {
		if strings.HasPrefix(seg, ".") {
			return true
		}
	}
	return false
}

func fromBundleFile(clean string, content []byte) (*Doc, []string, error) {
	d, rawType, notes, err := parseDoc(content)
	if err != nil {
		return nil, nil, err
	}
	// Frontmatter is the type's only source: the path no longer claims
	// one, and OKF requires the key — a file without it is not a concept
	// (design doc 0017, no guessing).
	if d.Type == "" {
		return nil, nil, fmt.Errorf("no type: frontmatter type %q is unusable (the type key is required; any single-line value works)", rawType)
	}
	d.ID = strings.TrimSuffix(clean, ".md")
	if !domain.ValidID(d.ID) {
		return nil, nil, fmt.Errorf("path yields invalid id %q", d.ID)
	}
	return d, notes, nil
}
