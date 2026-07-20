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
// skipped silently, as are hidden paths (.git trees, macOS tar's
// AppleDouble ._* siblings, .DS_Store).
//
// Files referenced by a concept's body markdown links become that
// entry's attachments — attribution is by reference first, so any
// producer's layout works (design doc 0008); the original path is
// preserved for re-export. Unreferenced non-markdown files sitting in an
// entry's canonical namespace ("<id>/<name>") attach to that entry
// (design doc 0013). Anything else that cannot become an entry or an
// attachment — orphaned non-markdown files, unparsable documents, invalid
// ids — is reported in skipped as "path: reason" lines rather than
// failing the whole bundle.
//
// There is no archive unwrapping: `tar czf ga4.tgz ga4/` imports under
// "ga4/" — the packed shape is the structure, and a wrapper directory is
// how a bundle keeps its own namespace (design doc 0017 §4.3).
func FromBundle(files map[string][]byte) (entries []domain.Knowledge, atts []BundleAttachment, skipped []string) {
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
		if base := path.Base(clean); base == "index.md" || base == "log.md" {
			continue
		}
		if !strings.HasSuffix(clean, ".md") {
			nonMarkdown = append(nonMarkdown, p)
			continue
		}
		k, err := fromBundleFile(clean, files[p])
		if err != nil {
			skipped = append(skipped, p+": "+err.Error())
			continue
		}
		entries = append(entries, *k)
	}

	concepts := make([]*domain.Knowledge, len(entries))
	for i := range entries {
		concepts[i] = &entries[i]
	}
	atts, used := resolveAttachments(files, concepts)
	for _, p := range nonMarkdown {
		if !used[cleanPath(p)] {
			skipped = append(skipped, p+": not a markdown concept (referenced by no entry body, and not in an entry's directory)")
		}
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return entries, atts, skipped
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

func fromBundleFile(clean string, content []byte) (*domain.Knowledge, error) {
	k, rawType, err := parseDoc(content)
	if err != nil {
		return nil, err
	}
	// Frontmatter is the type's only source: the path no longer claims
	// one, and OKF requires the key — a file without it is not a concept
	// (design doc 0017, no guessing).
	if k.Type == "" {
		return nil, fmt.Errorf("no type: frontmatter type %q is unusable (the type key is required; any single-line value works)", rawType)
	}
	k.ID = strings.TrimSuffix(clean, ".md")
	if !domain.ValidID(k.ID) {
		return nil, fmt.Errorf("path yields invalid id %q", k.ID)
	}
	return k, nil
}
