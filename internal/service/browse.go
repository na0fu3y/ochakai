package service

import (
	"context"
	"strings"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/store"
)

// Directory browsing (design doc 0014): the human-facing complement to
// search. IDs are hierarchical slug paths, and this reads that hierarchy
// one level at a time. Not a search — no usage is recorded: walking the
// tree to see what exists is not the demand signal search hits measure.

// BrowseResult is one level of the tree. At the root (no type), Types
// carries the type list; inside a type, Dirs and Entries carry what sits
// directly under the prefix.
type BrowseResult struct {
	Types     []store.TypeCount   `json:"types,omitempty"`
	Dirs      []store.DirCount    `json:"dirs,omitempty"`
	Entries   []store.BrowseEntry `json:"entries,omitempty"`
	Truncated bool                `json:"truncated,omitempty"`
}

// Browse lists one level of the knowledge hierarchy. With typ empty it
// returns the types (prefix must be empty too — a prefix without a type
// has nothing to scope to); with a type it returns the subdirectories
// and entries directly under prefix. prefix accepts "a/b" or "a/b/".
func (s *Service) Browse(ctx context.Context, typ domain.Type, prefix string) (*BrowseResult, error) {
	prefix = strings.TrimSuffix(prefix, "/")
	if !domain.ValidIDPrefix(prefix) {
		return nil, Invalidf(`invalid prefix %q (slug segments separated by "/")`, prefix)
	}
	if typ == "" {
		if prefix != "" {
			return nil, Invalidf("prefix requires a type")
		}
		types, err := s.Store.ListTypes(ctx)
		if err != nil {
			return nil, err
		}
		return &BrowseResult{Types: types}, nil
	}
	if !domain.ValidType(typ) {
		return nil, Invalidf("invalid type %q (one slug segment)", typ)
	}
	if prefix != "" {
		prefix += "/"
	}
	dirs, entries, truncated, err := s.Store.Browse(ctx, typ, prefix)
	if err != nil {
		return nil, err
	}
	return &BrowseResult{Dirs: dirs, Entries: entries, Truncated: truncated}, nil
}
