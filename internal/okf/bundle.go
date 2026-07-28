package okf

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// FromBundle is the inverse of Bundle: it reads a path→content map (a
// bundle directory or an unpacked archive) into the objects it holds.
// Following OKF's "concept ID = file path" rule, a concept's path minus
// ".md" is its id, verbatim — the layout is the user's (design doc
// 0017). The type comes from frontmatter alone, verbatim (design doc
// 0023).
//
// **What goes into a bundle comes out of it** (design doc 0046 §3.2).
// Everything that is not a concept is a file at its path: a `.md` whose
// frontmatter carries no usable type, a data file no body links to, an
// image in a layout ochakai would never have chosen. The attachment
// model had no representation for any of those, so an import dropped
// them and a round trip came out smaller than it went in.
//
// index.md and log.md are generated (design doc 0046 §§3.7-3.8) and
// skipped silently, as are hidden paths (.git trees, macOS tar's
// AppleDouble ._* siblings, .DS_Store). What is left in skipped is what
// no bundle can hold: a path that is not a valid bundle path, and a file
// past the size limit.
//
// There is no archive unwrapping: `tar czf ga4.tgz ga4/` imports under
// "ga4/" — the packed shape is the structure, and a wrapper directory is
// how a bundle keeps its own namespace (design doc 0017 §4.3).
func FromBundle(files map[string][]byte) (entries []Doc, out []BundleFile, skipped, notes []string) {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, p := range paths {
		clean := cleanPath(p)
		if hiddenPath(clean) || ReservedName(clean) {
			continue
		}
		data := files[p]
		if strings.HasSuffix(strings.ToLower(clean), ".md") {
			d, docNotes, err := fromBundleFile(clean, data)
			if err == nil {
				for _, n := range docNotes {
					notes = append(notes, p+": "+n)
				}
				entries = append(entries, *d)
				continue
			}
			// Not a concept, so it is a file — and saying so is a note
			// rather than a skip, because the document is kept. SPEC §11
			// asks a consumer to accept what it cannot fully read; §3.2
			// asks it not to lose the bytes either.
			notes = append(notes, p+": "+err.Error()+"; kept as a file")
		}
		if err := domain.ValidateFile(clean, len(data)); err != nil {
			skipped = append(skipped, p+": "+err.Error())
			continue
		}
		out = append(out, BundleFile{Path: clean, Data: data})
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return entries, out, skipped, notes
}

// BundleFile is one non-concept object found in a bundle, at the path it
// was found at — which is the path it will be stored at, and the one an
// export writes it back to.
type BundleFile struct {
	Path string
	Data []byte
}

// cleanPath canonicalizes one bundle path: relative prefix stripped,
// path.Clean'd, and NFC-normalized (design doc 0022) — macOS
// filesystems hand names back NFD-decomposed, and the same visible path
// must yield the same entry id and attachment references.
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
