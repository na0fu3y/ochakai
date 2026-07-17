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
// index.md files are navigation that Bundle regenerates, so they are
// skipped silently. Anything else that cannot become an entry — non-
// markdown files, unparsable documents, invalid slugs — is reported in
// skipped as "path: reason" lines rather than failing the whole bundle.
func FromBundle(files map[string][]byte) (entries []domain.Knowledge, skipped []string) {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, p := range paths {
		clean := path.Clean(strings.TrimPrefix(p, "./"))
		if path.Base(clean) == "index.md" {
			continue
		}
		if !strings.HasSuffix(clean, ".md") {
			skipped = append(skipped, p+": not a markdown concept")
			continue
		}
		k, err := fromBundleFile(clean, files[p])
		if err != nil {
			skipped = append(skipped, p+": "+err.Error())
			continue
		}
		entries = append(entries, *k)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Type != entries[j].Type {
			return entries[i].Type < entries[j].Type
		}
		return entries[i].ID < entries[j].ID
	})
	return entries, skipped
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
