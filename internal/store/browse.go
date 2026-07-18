package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// Directory browsing (design doc 0014). IDs have been hierarchical
// slug paths since design doc 0005; these queries read that hierarchy
// one level at a time — the subdirectories and entries directly under a
// prefix — so the web UI can render a file-tree without loading the
// whole knowledge base. Soft-deleted and rejected entries stay out, the
// same default as search.

// TypeCount is one knowledge type with its live entry count.
type TypeCount struct {
	Type  domain.Type `json:"type"`
	Count int         `json:"count"`
}

// DirCount is one subdirectory (ID segment) with the number of entries
// anywhere beneath it.
type DirCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// BrowseEntry is the light projection of an entry for tree listings:
// no body, no links, no attrs.
type BrowseEntry struct {
	Type      domain.Type   `json:"type"`
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	Status    domain.Status `json:"status"`
	UpdatedAt time.Time     `json:"updated_at"`
}

// browseNotRejected mirrors Filter's default: rejected entries are
// knowledge that was never accepted and must not resurface while
// browsing, exactly as in search.
const browseNotRejected = `deleted_at IS NULL AND status <> 'rejected'`

// ListTypes returns every type that has live entries, with counts,
// in type order — the root level of the browse tree.
func (s *Store) ListTypes(ctx context.Context) ([]TypeCount, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT type, count(*) FROM knowledge WHERE `+browseNotRejected+` GROUP BY type ORDER BY type`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (TypeCount, error) {
		var t TypeCount
		err := row.Scan(&t.Type, &t.Count)
		return t, err
	})
}

// maxBrowseEntries bounds one directory listing. A directory this wide
// is a modeling smell, not a paging problem — the caller renders a
// truncation note instead of paginating.
const maxBrowseEntries = 1000

// Browse returns what sits directly under prefix within one type: the
// subdirectories (with entry counts beneath them) and the entries at
// this level, both in name order; truncated reports that the entry list
// hit maxBrowseEntries. prefix is "" for the type root, or segments
// with a trailing slash ("sales/"). Prefix matching is by string, not
// LIKE — IDs may contain "_", which LIKE would treat as a wildcard.
func (s *Store) Browse(ctx context.Context, typ domain.Type, prefix string) (dirs []DirCount, entries []BrowseEntry, truncated bool, err error) {
	rows, err := s.pool.Query(ctx, `
		SELECT split_part(substr(id, length($2::text)+1), '/', 1) AS dir, count(*)
		FROM knowledge
		WHERE `+browseNotRejected+` AND type = $1
		  AND left(id, length($2::text)) = $2
		  AND strpos(substr(id, length($2::text)+1), '/') > 0
		GROUP BY dir ORDER BY dir`, typ, prefix)
	if err != nil {
		return nil, nil, false, err
	}
	dirs, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (DirCount, error) {
		var d DirCount
		err := row.Scan(&d.Name, &d.Count)
		return d, err
	})
	if err != nil {
		return nil, nil, false, err
	}
	rows, err = s.pool.Query(ctx, fmt.Sprintf(`
		SELECT type, id, title, status, updated_at
		FROM knowledge
		WHERE `+browseNotRejected+` AND type = $1
		  AND left(id, length($2::text)) = $2
		  AND strpos(substr(id, length($2::text)+1), '/') = 0
		ORDER BY id LIMIT %d`, maxBrowseEntries+1), typ, prefix)
	if err != nil {
		return nil, nil, false, err
	}
	entries, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (BrowseEntry, error) {
		var e BrowseEntry
		err := row.Scan(&e.Type, &e.ID, &e.Title, &e.Status, &e.UpdatedAt)
		return e, err
	})
	if err != nil {
		return nil, nil, false, err
	}
	if len(entries) > maxBrowseEntries {
		entries = entries[:maxBrowseEntries]
		truncated = true
	}
	return dirs, entries, truncated, nil
}
