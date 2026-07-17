package okf

import (
	"path"
	"sort"
	"strings"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// Attachments in OKF bundles (design doc 0008). The OKF spec is silent
// on non-markdown files — nothing forbids them, consumers must be
// permissive, and only the index.md / log.md *filenames* are reserved.
// Three rules keep ochakai consistent with that world:
//
//   - Attribution is reference-driven, never path-driven: an image
//     belongs to the entry whose body links to it, wherever the file
//     sits. Foreign bundles keep their own layouts.
//   - ochakai's canonical export layout is the entry-named directory,
//     "<type>/<id>/<name>" next to "<type>/<id>.md" — everything about
//     an entry lives in its OKF namespace, and `<id>/<name>` relative
//     links render on GitHub. Sharing the directory with hierarchical
//     child entries is harmless: concept parsers ignore non-md files,
//     and attachment names may not end in .md.
//   - A foreign file goes back where it came from: okf_path (recorded on
//     import) wins over the canonical layout, so body links that use the
//     original location keep working byte-for-byte.

// AttachmentPath returns the bundle path for one attachment: its
// preserved foreign location if it has one, else the canonical layout.
func AttachmentPath(typ domain.Type, id string, att *domain.Attachment) string {
	if att.OKFPath != "" {
		return att.OKFPath
	}
	return string(typ) + "/" + id + "/" + att.Name
}

// BundleAttachment is one image found in a bundle, attributed to the
// entry whose body references it.
type BundleAttachment struct {
	Type domain.Type
	ID   string
	Name string // filename (last segment of Path)
	Path string // bundle path, preserved as okf_path for round-trips
	Data []byte
}

// conceptRef pairs a parsed entry with the bundle path its document was
// read from — relative body links resolve against that original path,
// which differs from the canonical "<type>/<id>.md" for concepts at the
// bundle root.
type conceptRef struct {
	k       *domain.Knowledge
	docPath string
}

// resolveAttachments walks each entry's body markdown links and collects
// the bundle files they reference: relative links resolve against the
// concept document's directory, /-rooted links against the bundle root
// (both OKF SPEC §5 forms). Only files that exist in the bundle, pass
// the attachment allowlist (image bytes, not *.md), and fit the size
// limit attach; everything else is left for the skip report.
// The returned used set holds the consumed bundle paths.
func resolveAttachments(files map[string][]byte, concepts []conceptRef) (atts []BundleAttachment, used map[string]bool) {
	used = map[string]bool{}
	index := map[string][]byte{}
	for p, data := range files {
		index[path.Clean(strings.TrimPrefix(p, "./"))] = data
	}
	for _, c := range concepts {
		k := c.k
		docDir := path.Dir(c.docPath)
		seen := map[string]bool{}
		for _, target := range bodyLinkTargets(k.Body) {
			p, ok := resolveTarget(docDir, target)
			if !ok {
				continue
			}
			data, exists := index[p]
			if !exists || strings.HasSuffix(strings.ToLower(p), ".md") {
				continue
			}
			name := path.Base(p)
			if seen[name] || !domain.ValidAttachmentName(name) || len(data) > domain.MaxAttachmentSize || len(data) == 0 {
				continue
			}
			if _, err := domain.DetectAttachmentMediaType(data); err != nil {
				continue
			}
			seen[name] = true
			used[p] = true
			atts = append(atts, BundleAttachment{Type: k.Type, ID: k.ID, Name: name, Path: p, Data: data})
		}
	}
	sort.Slice(atts, func(i, j int) bool {
		if atts[i].Type != atts[j].Type {
			return atts[i].Type < atts[j].Type
		}
		if atts[i].ID != atts[j].ID {
			return atts[i].ID < atts[j].ID
		}
		return atts[i].Name < atts[j].Name
	})
	return atts, used
}

// bodyLinkTargets extracts markdown link and image targets from body:
// ![alt](target) and [text](target). Good-enough scanning, not a full
// markdown parser — targets that don't resolve to a bundle file are
// simply not attachments (the spec requires tolerating broken links).
func bodyLinkTargets(body string) []string {
	var targets []string
	for i := 0; i < len(body); i++ {
		if body[i] != '[' {
			continue
		}
		close := strings.IndexByte(body[i:], ']')
		if close < 0 {
			break
		}
		rest := body[i+close+1:]
		if !strings.HasPrefix(rest, "(") {
			continue
		}
		end := strings.IndexByte(rest, ')')
		if end < 0 {
			continue
		}
		target := strings.TrimSpace(rest[1:end])
		// Strip an optional markdown title: (path "title").
		if j := strings.IndexAny(target, " \t"); j >= 0 {
			target = target[:j]
		}
		if target != "" {
			targets = append(targets, target)
		}
	}
	return targets
}

// resolveTarget resolves one link target to a bundle path, rejecting
// external URLs and anything escaping the bundle root.
func resolveTarget(docDir, target string) (string, bool) {
	if strings.Contains(target, "://") || strings.HasPrefix(target, "data:") || strings.HasPrefix(target, "mailto:") {
		return "", false
	}
	target, _, _ = strings.Cut(target, "#")
	if target == "" {
		return "", false
	}
	var p string
	if strings.HasPrefix(target, "/") {
		p = path.Clean(strings.TrimPrefix(target, "/"))
	} else {
		p = path.Clean(path.Join(docDir, target))
	}
	if p == "." || strings.HasPrefix(p, "../") {
		return "", false
	}
	return p, true
}
