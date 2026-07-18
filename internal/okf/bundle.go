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
// Following OKF's "concept ID = file path" rule, structure wins over
// frontmatter: the first path segment becomes the type and the remaining
// segments the hierarchical ID; a frontmatter type spelled differently is
// preserved as attrs[AttrOKFType] so re-export reproduces the original.
// index.md files are navigation that Bundle regenerates, and log.md files
// are the other OKF-reserved name (update history, SPEC §3) — both are
// skipped silently, as are hidden paths (.git trees, macOS tar's
// AppleDouble ._* siblings, .DS_Store).
//
// Files referenced by a concept's body markdown links become that
// entry's attachments — attribution is by reference first, so any
// producer's layout works (design doc 0008); the original path is
// preserved for re-export. Unreferenced non-markdown files sitting in an
// entry's canonical namespace ("<type>/<id>/<name>") attach to that entry
// (design doc 0013). Anything else that cannot become an entry or an
// attachment — orphaned non-markdown files, unparsable documents, invalid
// slugs — is reported in skipped as "path: reason" lines rather than
// failing the whole bundle.
func FromBundle(files map[string][]byte) (entries []domain.Knowledge, atts []BundleAttachment, skipped []string) {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var docPaths []string // parallel to entries: the bundle path each was read from
	var nonMarkdown []string
	for _, p := range paths {
		clean := path.Clean(strings.TrimPrefix(p, "./"))
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
		docPaths = append(docPaths, clean)
	}

	concepts := make([]conceptRef, len(entries))
	for i := range entries {
		concepts[i] = conceptRef{k: &entries[i], docPath: docPaths[i]}
	}
	atts, used := resolveAttachments(files, concepts)
	for _, p := range nonMarkdown {
		if !used[path.Clean(strings.TrimPrefix(p, "./"))] {
			skipped = append(skipped, p+": not a markdown concept (referenced by no entry body, and not in an entry's directory)")
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Type != entries[j].Type {
			return entries[i].Type < entries[j].Type
		}
		return entries[i].ID < entries[j].ID
	})
	return entries, atts, skipped
}

// StripWrapper unwraps a bundle packed inside a single top-level
// directory, the shape `tar czf bundle.tar.gz ga4/` produces. Without
// unwrapping, the wrapper name would silently become every entry's type
// (first path segment = type). A bundle rooted at the archive top —
// anything with a top-level file, such as the root index.md every real
// bundle carries — is returned unchanged. The wrapper name is returned so
// callers can tell the user what happened ("" when nothing was stripped).
func StripWrapper(files map[string][]byte) (map[string][]byte, string) {
	root := ""
	for p := range files {
		clean := path.Clean(strings.TrimPrefix(p, "./"))
		if hiddenPath(clean) {
			continue // tar noise (._*, .DS_Store) must not defeat detection
		}
		i := strings.Index(clean, "/")
		if i < 0 {
			return files, ""
		}
		if root == "" {
			root = clean[:i]
		} else if root != clean[:i] {
			return files, ""
		}
	}
	if root == "" {
		return files, ""
	}
	out := make(map[string][]byte, len(files))
	for p, content := range files {
		clean := path.Clean(strings.TrimPrefix(p, "./"))
		if hiddenPath(clean) {
			continue
		}
		out[strings.TrimPrefix(clean, root+"/")] = content
	}
	return out, root
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

	segs := strings.Split(strings.TrimSuffix(clean, ".md"), "/")
	if len(segs) == 1 {
		// A concept at the bundle root has no directory to name its type;
		// fall back to the frontmatter type (the file moves under that
		// directory on re-export).
		if k.Type == "" {
			return nil, fmt.Errorf("no type: not inside a type directory and frontmatter type %q yields no slug", rawType)
		}
		k.ID = segs[0]
	} else {
		pathType := domain.Type(segs[0])
		if !domain.ValidType(pathType) {
			return nil, fmt.Errorf("directory %q is not a valid type slug", segs[0])
		}
		if k.Type != pathType {
			// The path names the type; keep the frontmatter spelling for
			// round-trips ("tables/users.md" with "type: Table" exports
			// back exactly, stored as type "tables").
			delete(k.Attrs, AttrOKFType)
			if rawType != "" && rawType != string(pathType) {
				if k.Attrs == nil {
					k.Attrs = map[string]any{}
				}
				k.Attrs[AttrOKFType] = rawType
			}
			k.Type = pathType
		}
		k.ID = strings.Join(segs[1:], "/")
	}
	if !domain.ValidID(k.ID) {
		return nil, fmt.Errorf("path yields invalid id %q", k.ID)
	}
	return k, nil
}
