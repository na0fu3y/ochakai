package service

import (
	"context"
	"strings"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/store"
)

// Directory browsing (design docs 0014, 0016): the human-facing
// complement to search. IDs are full bundle paths, and this reads that
// hierarchy one level at a time; the root is the top-level segments.
// Not a search — no usage is recorded: walking the tree to see what
// exists is not the demand signal search hits measure.

// BrowseResult is one level of the tree: the subdirectories and entries
// directly under the prefix.
type BrowseResult struct {
	Dirs      []store.DirCount    `json:"dirs,omitempty"`
	Entries   []store.BrowseEntry `json:"entries,omitempty"`
	Truncated bool                `json:"truncated,omitempty"`
}

// Browse lists one level of the knowledge hierarchy: the subdirectories
// and entries directly under prefix ("" is the root). prefix accepts
// "a/b" or "a/b/".
func (s *Service) Browse(ctx context.Context, prefix string) (*BrowseResult, error) {
	prefix, err := normalizePrefix(prefix)
	if err != nil {
		return nil, err
	}
	if prefix != "" {
		prefix += "/"
	}
	dirs, entries, truncated, err := s.Store.Browse(ctx, prefix)
	if err != nil {
		return nil, err
	}
	return &BrowseResult{Dirs: dirs, Entries: entries, Truncated: truncated}, nil
}

// normalizePrefix cleans one path prefix, for every surface that takes
// one — browsing a directory and scoping a search to it (design doc 0041)
// must agree on what a path is, or a directory the tree can open would be
// rejected as a filter. NFC because stored ids are (design doc 0022) and
// a path pasted from a macOS filesystem arrives decomposed; no trailing
// slash because "a/b/" and "a/b" name the same directory. The empty
// prefix is the root, and valid.
func normalizePrefix(p string) (string, error) {
	p = domain.Normalize(strings.TrimSuffix(p, "/"))
	if !domain.ValidIDPrefix(p) {
		return "", Invalidf(`invalid prefix %q (slug segments separated by "/")`, p)
	}
	return p, nil
}
