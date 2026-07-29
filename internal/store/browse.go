package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// Directory browsing (design docs 0014, 0016). IDs are full bundle
// paths; these queries read that hierarchy one level at a time — the
// subdirectories and entries directly under a prefix — so the web UI can
// render a file-tree without loading the whole knowledge base. The root
// is prefix "": the top-level segments themselves. Soft-deleted and
// rejected entries stay out, the same default as search.

// DirCount is one subdirectory (ID segment) with the number of entries
// anywhere beneath it.
//
// Concepts are what it counts, and a directory holding only files is not
// one of these: OKF's §8 listing is navigation between concept
// documents, and a count that mixed the two would answer "31 concepts"
// for a directory holding four. What sits in a directory is what that
// directory's own index.md says (BrowseFile), which is the level where
// the format has a place for it.
type DirCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// BrowseFile is one file sitting directly in a directory: the third
// section of the index.md OKF SPEC §8 describes (design doc 0046 §3.7).
//
// A file is an object in the bundle rather than a property of an entry
// (§3.3), so a directory can hold one that no concept shows — a
// producer's seed data, a diagram two entries link — and a listing that
// omits it describes a directory that is not there. The name is the last
// segment, which is what the listing links to; the path is what the
// object is addressed by.
type BrowseFile struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	MediaType string    `json:"media_type"`
	Size      int64     `json:"size"`
	UpdatedAt time.Time `json:"updated_at"`
}

// BrowseEntry is the light projection of an entry for tree listings:
// no body, no links, no attrs. Type rides along as display metadata
// (the tree itself is pure path), and Description so a directory
// listing can render as an index page — the same title-plus-description
// lines the OKF export's index.md files carry.
type BrowseEntry struct {
	Type        domain.Type   `json:"type"`
	ID          string        `json:"id"`
	Title       string        `json:"title,omitempty"` // display-name override; empty means the id's last segment (design doc 0022)
	Description string        `json:"description,omitempty"`
	Status      domain.Status `json:"status"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

// browseNotRejected mirrors Filter's default: rejected entries are
// knowledge that was never accepted and must not resurface while
// browsing, exactly as in search. A rejection is a ledger row rather than
// a status (design doc 0043 §3.3), so the id is qualified — the ledger
// has an id column of its own, and a bare one inside the subquery would
// bind to it and match everything.
// id IS NOT NULL keeps files out: browsing lists the concepts in a
// directory, and a file in the same directory is listed as a file
// (design doc 0046 §3.7), not as an entry with no title.
const browseNotRejected = `deleted_at IS NULL AND id IS NOT NULL
	AND NOT EXISTS (SELECT 1 FROM knowledge_rejection r WHERE r.id = object.id)`

// MaxBrowseEntries bounds one directory listing. A directory this wide
// is a modeling smell, not a paging problem — the caller renders a
// truncation note instead of paginating.
const MaxBrowseEntries = 1000

// Level is one directory of the bundle as browsing reads it: what sits
// directly under a prefix, in the three kinds SPEC §8 lists.
type Level struct {
	Dirs      []DirCount    `json:"dirs,omitempty"`
	Entries   []BrowseEntry `json:"entries,omitempty"`
	Files     []BrowseFile  `json:"files,omitempty"`
	Truncated bool          `json:"truncated,omitempty"`
}

// Browse returns what sits directly under prefix: the subdirectories
// (with concept counts beneath them), the entries at this level and the
// files at this level, each in name order; Truncated reports that the
// entry list hit MaxBrowseEntries. prefix is "" for the root, or
// segments with a trailing slash ("sales/"). Prefix matching is by
// string, not LIKE — paths may contain "_", which LIKE would treat as a
// wildcard.
//
// Three queries because there are three kinds of row and one of them is
// a grouping. The subdirectory query counts concepts only, which is why
// a directory holding nothing but files is not a subdirectory here: OKF
// says nothing about such a directory, and a count that mixed the kinds
// would misdescribe every directory that has both.
func (s *Store) Browse(ctx context.Context, prefix string) (*Level, error) {
	var lvl Level
	rows, err := s.pool.Query(ctx, `
		SELECT split_part(substr(id, length($1::text)+1), '/', 1) AS dir, count(*)
		FROM object
		WHERE `+browseNotRejected+`
		  AND left(id, length($1::text)) = $1
		  AND strpos(substr(id, length($1::text)+1), '/') > 0
		GROUP BY dir ORDER BY dir`, prefix)
	if err != nil {
		return nil, err
	}
	lvl.Dirs, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (DirCount, error) {
		var d DirCount
		err := row.Scan(&d.Name, &d.Count)
		return d, err
	})
	if err != nil {
		return nil, err
	}
	rows, err = s.pool.Query(ctx, fmt.Sprintf(`
		SELECT type, id, title, description, status, updated_at
		FROM object
		WHERE `+browseNotRejected+`
		  AND left(id, length($1::text)) = $1
		  AND strpos(substr(id, length($1::text)+1), '/') = 0
		ORDER BY id LIMIT %d`, MaxBrowseEntries+1), prefix)
	if err != nil {
		return nil, err
	}
	lvl.Entries, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (BrowseEntry, error) {
		var e BrowseEntry
		err := row.Scan(&e.Type, &e.ID, &e.Title, &e.Description, &e.Status, &e.UpdatedAt)
		return e, err
	})
	if err != nil {
		return nil, err
	}
	if len(lvl.Entries) > MaxBrowseEntries {
		lvl.Entries = lvl.Entries[:MaxBrowseEntries]
		lvl.Truncated = true
	}
	// The files at this level. A file has no id, so it is matched on its
	// path — and the same cap applies, because a directory of ten
	// thousand files is as unreadable as a directory of ten thousand
	// concepts.
	rows, err = s.pool.Query(ctx, fmt.Sprintf(`
		SELECT path, media_type, size, updated_at
		FROM object
		WHERE id IS NULL AND deleted_at IS NULL
		  AND left(path, length($1::text)) = $1
		  AND strpos(substr(path, length($1::text)+1), '/') = 0
		ORDER BY path LIMIT %d`, MaxBrowseEntries+1), prefix)
	if err != nil {
		return nil, err
	}
	lvl.Files, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (BrowseFile, error) {
		var f BrowseFile
		if err := row.Scan(&f.Path, &f.MediaType, &f.Size, &f.UpdatedAt); err != nil {
			return f, err
		}
		f.Name = f.Path[strings.LastIndex(f.Path, "/")+1:]
		return f, nil
	})
	if err != nil {
		return nil, err
	}
	if len(lvl.Files) > MaxBrowseEntries {
		lvl.Files = lvl.Files[:MaxBrowseEntries]
		lvl.Truncated = true
	}
	return &lvl, nil
}
