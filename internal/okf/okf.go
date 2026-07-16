// Package okf exports the knowledge base as an Open Knowledge Format
// bundle (https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf):
// a directory of markdown files with YAML frontmatter, one concept per
// knowledge entry, grouped by type, with index.md files for progressive
// disclosure. Your knowledge is yours — bundles are plain files that live
// happily in git.
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

// okfType maps ochakai knowledge types to descriptive OKF type values
// (OKF registers no taxonomy; values should be self-explanatory).
var okfType = map[domain.Type]string{
	domain.TypeMetric:  "Metric",
	domain.TypeQuery:   "Golden Query",
	domain.TypeInsight: "Insight",
	domain.TypeTerm:    "Glossary Term",
	domain.TypeTable:   "Table",
}

// frontmatter is the OKF frontmatter for one concept. Only "type" is
// required by the spec; the rest are recommended keys plus ochakai
// extension keys (consumers must tolerate unknown keys).
type frontmatter struct {
	Type        string         `yaml:"type"`
	Title       string         `yaml:"title"`
	Description string         `yaml:"description,omitempty"`
	Resource    string         `yaml:"resource,omitempty"`
	Tags        []string       `yaml:"tags,omitempty"`
	Timestamp   string         `yaml:"timestamp"`
	Status      string         `yaml:"status"`
	StatusNote  string         `yaml:"status_note,omitempty"`
	CreatedBy   string         `yaml:"created_by"`
	VerifiedBy  string         `yaml:"verified_by,omitempty"`
	VerifiedAt  string         `yaml:"verified_at,omitempty"`
	RejectedBy  string         `yaml:"rejected_by,omitempty"`
	RejectedAt  string         `yaml:"rejected_at,omitempty"`
	Attrs       map[string]any `yaml:"attrs,omitempty"`
}

// Bundle renders every entry into a path→content map. Paths follow
// "<type>/<id>.md" so the OKF concept ID matches ochakai's type/id.
// index.md files are generated at the root and per type directory.
func Bundle(entries []domain.Knowledge) (map[string][]byte, error) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Type != entries[j].Type {
			return entries[i].Type < entries[j].Type
		}
		return entries[i].ID < entries[j].ID
	})

	files := map[string][]byte{}
	byType := map[domain.Type][]domain.Knowledge{}
	for _, k := range entries {
		doc, err := Document(&k)
		if err != nil {
			return nil, fmt.Errorf("render %s: %w", k.URI(), err)
		}
		files[fmt.Sprintf("%s/%s.md", k.Type, k.ID)] = doc
		byType[k.Type] = append(byType[k.Type], k)
	}

	var root strings.Builder
	root.WriteString("---\ntype: Index\ntitle: ochakai knowledge bundle\n---\n\n# ochakai knowledge bundle\n\n")
	for _, t := range domain.Types {
		group := byType[t]
		if len(group) == 0 {
			continue
		}
		fmt.Fprintf(&root, "- [%s/](/%s/index.md) — %d concepts\n", t, t, len(group))

		var idx strings.Builder
		fmt.Fprintf(&idx, "---\ntype: Index\ntitle: %s\n---\n\n# %s\n\n", t, t)
		for _, k := range group {
			desc := k.Description
			if desc != "" {
				desc = " — " + desc
			}
			fmt.Fprintf(&idx, "- [%s](/%s/%s.md)%s\n", k.Title, k.Type, k.ID, desc)
		}
		files[string(t)+"/index.md"] = []byte(idx.String())
	}
	files["index.md"] = []byte(root.String())
	return files, nil
}

// Document renders one knowledge entry as an OKF concept document.
func Document(k *domain.Knowledge) ([]byte, error) {
	fm := frontmatter{
		Type:        okfType[k.Type],
		Title:       k.Title,
		Description: k.Description,
		Tags:        k.Tags,
		Timestamp:   k.UpdatedAt.UTC().Format(time.RFC3339),
		Status:      string(k.Status),
		StatusNote:  k.StatusNote,
		CreatedBy:   k.CreatedBy.Kind + ":" + k.CreatedBy.Name,
		Attrs:       k.Attrs,
	}
	// "resource" is the canonical URI of the underlying asset; only table
	// entries describe a physical resource.
	if k.Type == domain.TypeTable {
		if src, ok := k.Attrs["source"].(string); ok {
			fm.Resource = src
		}
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

	var b strings.Builder
	b.WriteString("---\n")
	b.Write(fmYAML)
	b.WriteString("---\n")
	if body := strings.TrimSpace(k.Body); body != "" {
		b.WriteString("\n" + body + "\n")
	}
	if len(k.Links) > 0 {
		b.WriteString("\n# Links\n\n")
		for _, l := range k.Links {
			b.WriteString(fmt.Sprintf("- %s: [%s](/%s.md)\n", l.Rel, l.Target, l.Target))
		}
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
