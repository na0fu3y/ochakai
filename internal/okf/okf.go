// Package okf exports the knowledge base as an Open Knowledge Format
// bundle (https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf):
// a directory of markdown files with YAML frontmatter, one concept per
// knowledge entry at its id's path (the layout is the user's, design doc
// 0017), with index.md files for progressive disclosure. Your knowledge
// is yours — bundles are plain files that live happily in git.
package okf

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// The type key needs no translation here: ochakai's types are the OKF
// vocabulary verbatim ("BigQuery Table", not a slug), so export writes
// k.Type and import reads it back unchanged (design doc 0023). The
// mapping table, the alias table, the slugifier, and the okf_type
// spelling-preservation attr that earlier revisions needed are all gone —
// nothing is lost on the way out, so nothing has to be restored.

// Version is the OKF spec version ochakai produces, declared in the
// bundle-root index.md (SPEC §11).
const Version = "0.1"

// frontmatter is the OKF frontmatter for one concept: the required "type",
// the recommended keys, and ochakai extension keys. Entry attrs are not
// nested here — Document emits them as producer-defined top-level keys
// (SPEC §4.1), so foreign extension keys round-trip in place.
type frontmatter struct {
	Type        string   `yaml:"type"`
	Resource    string   `yaml:"resource,omitempty"` // right after type, matching the knowledge-catalog reference bundles
	Title       string   `yaml:"title,omitempty"`    // empty means the filename is the name (design doc 0022) — omitted, so titleless documents round-trip unchanged
	Description string   `yaml:"description,omitempty"`
	Tags        []string `yaml:"tags,omitempty"`
	Timestamp   string   `yaml:"timestamp"`
	Status      string   `yaml:"status"`
	StatusNote  string   `yaml:"status_note,omitempty"`
	CreatedBy   string   `yaml:"created_by"`
	VerifiedBy  string   `yaml:"verified_by,omitempty"`
	VerifiedAt  string   `yaml:"verified_at,omitempty"`
	RejectedBy  string   `yaml:"rejected_by,omitempty"`
	RejectedAt  string   `yaml:"rejected_at,omitempty"`
}

// reservedKeys are the frontmatter keys the envelope owns. An attr with a
// reserved name is not exported as an extension key: the envelope value
// wins.
var reservedKeys = map[string]bool{
	"type": true, "id": true, "title": true, "description": true,
	"resource": true, "tags": true, "timestamp": true, "status": true,
	"status_note": true, "created_by": true, "verified_by": true,
	"verified_at": true, "rejected_by": true, "rejected_at": true,
}

// Bundle renders every entry into a path→content map. The path is
// "<id>.md" — the OKF concept ID is ochakai's id, and its segments are
// the directories. Every directory level gets an index.md for
// progressive disclosure.
func Bundle(entries []domain.Knowledge) (map[string][]byte, error) {
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })

	files := map[string][]byte{}
	root := &dir{}
	for _, k := range entries {
		doc, err := Document(&k)
		if err != nil {
			return nil, fmt.Errorf("render %s: %w", k.URI(), err)
		}
		files[k.ID+".md"] = doc
		root.insert(strings.Split(k.ID, "/"), k)
	}
	root.writeIndexes(files, "")
	return files, nil
}

// dir is one directory level of a bundle, used to generate index.md files.
type dir struct {
	subdirs map[string]*dir
	entries []dirEntry // concepts directly in this directory, in bundle order
	count   int        // concepts in this directory and below
}

type dirEntry struct {
	name string // filename without .md
	k    domain.Knowledge
}

func (d *dir) insert(segs []string, k domain.Knowledge) {
	d.count++
	if len(segs) == 1 {
		d.entries = append(d.entries, dirEntry{name: segs[0], k: k})
		return
	}
	if d.subdirs == nil {
		d.subdirs = map[string]*dir{}
	}
	sub := d.subdirs[segs[0]]
	if sub == nil {
		sub = &dir{}
		d.subdirs[segs[0]] = sub
	}
	sub.insert(segs[1:], k)
}

// subdirNames lists subdirectories alphabetically — every level the
// same, with no special root ordering (the root is no longer a type
// listing, design doc 0017).
func (d *dir) subdirNames() []string {
	names := make([]string, 0, len(d.subdirs))
	for n := range d.subdirs {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// writeIndexes emits index.md for this directory and recurses. prefix is
// the bundle-relative directory path, "" for the root or "a/b/" below it.
// Index files carry no frontmatter (SPEC §6) — except the bundle root,
// where an okf_version declaration is the one permitted block (§11) — and
// list their entries with relative links, as in the spec's examples.
func (d *dir) writeIndexes(files map[string][]byte, prefix string) {
	var b strings.Builder
	if prefix == "" {
		fmt.Fprintf(&b, "---\nokf_version: %q\n---\n\n# ochakai knowledge bundle\n\n", Version)
	} else {
		fmt.Fprintf(&b, "# %s\n\n", strings.TrimSuffix(prefix, "/"))
	}
	for _, name := range d.subdirNames() {
		noun := "concepts"
		if d.subdirs[name].count == 1 {
			noun = "concept"
		}
		fmt.Fprintf(&b, "* [%s/](%s/index.md) - %d %s\n", name, name, d.subdirs[name].count, noun)
	}
	for _, e := range d.entries {
		title := e.k.Title
		if title == "" {
			title = e.name
		}
		desc := e.k.Description
		if desc != "" {
			desc = " - " + desc
		}
		fmt.Fprintf(&b, "* [%s](%s.md)%s\n", title, e.name, desc)
	}
	files[prefix+"index.md"] = []byte(b.String())
	for name, sub := range d.subdirs {
		sub.writeIndexes(files, prefix+name+"/")
	}
}

// Document renders one knowledge entry as an OKF concept document.
func Document(k *domain.Knowledge) ([]byte, error) {
	fm := frontmatter{
		Type:        string(k.Type),
		Resource:    k.Resource,
		Title:       k.Title,
		Description: k.Description,
		Tags:        k.Tags,
		Timestamp:   k.UpdatedAt.UTC().Format(time.RFC3339),
		Status:      string(k.Status),
		StatusNote:  k.StatusNote,
		CreatedBy:   k.CreatedBy.Kind + ":" + k.CreatedBy.Name,
	}
	if k.VerifiedBy != nil {
		fm.VerifiedBy = k.VerifiedBy.Kind + ":" + k.VerifiedBy.Name
	}
	if k.VerifiedAt != nil {
		fm.VerifiedAt = k.VerifiedAt.UTC().Format(time.RFC3339)
	}
	if k.RejectedBy != nil {
		fm.RejectedBy = k.RejectedBy.Kind + ":" + k.RejectedBy.Name
	}
	if k.RejectedAt != nil {
		fm.RejectedAt = k.RejectedAt.UTC().Format(time.RFC3339)
	}

	fmYAML, err := yaml.Marshal(&fm)
	if err != nil {
		return nil, err
	}
	// Extension attrs go out as top-level keys, after the envelope keys.
	// yaml.Marshal sorts map keys, so the output is deterministic.
	extras := map[string]any{}
	for key, v := range k.Attrs {
		if !reservedKeys[key] {
			extras[key] = v
		}
	}
	var exYAML []byte
	if len(extras) > 0 {
		if exYAML, err = yaml.Marshal(extras); err != nil {
			return nil, err
		}
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.Write(fmYAML)
	b.Write(exYAML)
	b.WriteString("---\n")
	// Links are not rendered: they are derived from the body's markdown
	// links (design doc 0024), so they are already in the text below.
	// Writing a "# Links" section here would duplicate them on re-import.
	if body := strings.TrimSpace(k.Body); body != "" {
		b.WriteString("\n" + body + "\n")
	}
	return []byte(b.String()), nil
}

// WriteTarGz streams the bundle as a gzipped tarball (the OKF-sanctioned
// archive distribution), with deterministic entry order.
func WriteTarGz(w io.Writer, files map[string][]byte, modTime time.Time) error {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	for _, p := range paths {
		if err := tw.WriteHeader(&tar.Header{
			Name:    p,
			Mode:    0o644,
			Size:    int64(len(files[p])),
			ModTime: modTime.UTC(),
		}); err != nil {
			return err
		}
		if _, err := tw.Write(files[p]); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}
