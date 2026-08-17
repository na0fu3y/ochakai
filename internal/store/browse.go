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
	CreatedAt time.Time `json:"created_at"`
}

// BrowseConcept is the light projection of a concept for tree listings:
// no body, no links, no attrs. Type rides along as display metadata
// (the tree itself is pure path), and Description so a directory
// listing can render as an index page — the same title-plus-description
// lines the OKF export's index.md files carry.
type BrowseConcept struct {
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

// MaxBrowseEntries bounds one page of a directory listing. It used to
// bound the listing itself, on the reasoning that a directory this wide
// is a modeling smell rather than a paging problem — but the smell is
// the base's and the wall was the caller's, and design doc 0068 §2.1
// had already decided what a listing owes: a way past the cap rather
// than a stop at it. The number is unchanged, so a caller that fits
// under it today sees exactly what it sees today (design doc 0101).
const MaxBrowseEntries = 1000

// BrowseAfter is where a level's listing resumes: the last row already
// delivered of each kind that pages, or nil for a kind that has run out.
// A nil *BrowseAfter is the first page — every kind from its beginning,
// subdirectories included.
//
// Two positions rather than one because a level is two ordered runs, not
// one: concepts by id and files by path, each with its own end. Merging
// them into a single order would change what the JSON says (three
// sections, design doc 0046 §3.7) to buy a tidier cursor.
type BrowseAfter struct {
	Concepts *string
	Files    *string
}

// Level is one directory of the bundle as browsing reads it: what sits
// directly under a prefix, in the three kinds SPEC §8 lists.
type Level struct {
	Dirs      []DirCount      `json:"dirs,omitempty"`
	Concepts  []BrowseConcept `json:"concepts,omitempty"`
	Files     []BrowseFile    `json:"files,omitempty"`
	Truncated bool            `json:"truncated,omitempty"`

	// Which kinds have rows past this page. The service turns them into
	// one cursor; they are separate here because the two runs end
	// independently.
	MoreConcepts bool `json:"-"`
	MoreFiles    bool `json:"-"`
}

// Browse returns one page of what sits directly under prefix: the
// subdirectories (with concept counts beneath them), the entries at this
// level and the files at this level, each in name order; Truncated
// reports that a list hit MaxBrowseEntries, and MoreConcepts / MoreFiles
// say the same thing in the shape a cursor is built from. after resumes
// a page (nil is the first). prefix is "" for the root, or
// segments with a trailing slash ("sales/"). Prefix matching is by
// string, not LIKE — paths may contain "_", which LIKE would treat as a
// wildcard.
//
// Three queries because there are three kinds of row and one of them is
// a grouping. The subdirectory query counts concepts only, which is why
// a directory holding nothing but files is not a subdirectory here: OKF
// says nothing about such a directory, and a count that mixed the kinds
// would misdescribe every directory that has both.
// scope, when non-empty, restricts every row and every count to the
// subtrees the caller may read (design doc 0109 §4). It is applied here
// rather than to the rows the service gets back because one of the three
// answers is a count: a directory row that survived a filter in Go would
// still be carrying the number of concepts underneath it, including the
// ones the caller may not see.
func (s *Store) Browse(ctx context.Context, prefix string, after *BrowseAfter, scope []string) (*Level, error) {
	var lvl Level
	// The subdirectories ride on the first page only. They are a grouping
	// rather than a run — one row per name, bounded by how many names a
	// directory has — so there is no position in them to resume from, and
	// repeating them on every page would make a walk report the same
	// directory N times.
	if after == nil {
		args := []any{prefix}
		rows, err := s.pool.Query(ctx, `
			SELECT split_part(substr(id, length($1::text)+1), '/', 1) AS dir, count(*)
			FROM object
			WHERE `+browseNotRejected+`
			  AND left(id, length($1::text)) = $1
			  AND strpos(substr(id, length($1::text)+1), '/') > 0
			  `+browseScope("id", scope, &args)+`
			GROUP BY dir ORDER BY dir`, args...)
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
	}
	// One row past the page is read to tell a full page from the last
	// one — the same trick the review feeds use, and for the same reason:
	// without it a level whose width happens to equal the cap hands out a
	// cursor onto nothing (design doc 0050 §2.1).
	if resumes(after, func(a *BrowseAfter) *string { return a.Concepts }) {
		args := []any{prefix}
		rows, err := s.pool.Query(ctx, fmt.Sprintf(`
			SELECT type, id, title, description, status, updated_at
			FROM object
			WHERE `+browseNotRejected+`
			  AND left(id, length($1::text)) = $1
			  AND strpos(substr(id, length($1::text)+1), '/') = 0
			  %s%s
			ORDER BY id LIMIT %d`, afterKey("id", after, func(a *BrowseAfter) *string { return a.Concepts }, &args),
			browseScope("id", scope, &args), MaxBrowseEntries+1), args...)
		if err != nil {
			return nil, err
		}
		lvl.Concepts, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (BrowseConcept, error) {
			var e BrowseConcept
			err := row.Scan(&e.Type, &e.ID, &e.Title, &e.Description, &e.Status, &e.UpdatedAt)
			return e, err
		})
		if err != nil {
			return nil, err
		}
		if len(lvl.Concepts) > MaxBrowseEntries {
			lvl.Concepts = lvl.Concepts[:MaxBrowseEntries]
			lvl.Truncated, lvl.MoreConcepts = true, true
		}
	}
	// The files at this level. A file has no id, so it is matched on its
	// path — and the same cap applies, because a directory of ten
	// thousand files is as unreadable as a directory of ten thousand
	// concepts.
	if !resumes(after, func(a *BrowseAfter) *string { return a.Files }) {
		return &lvl, nil
	}
	args := []any{prefix}
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		SELECT path, media_type, size, created_at
		FROM object
		WHERE id IS NULL AND deleted_at IS NULL
		  AND left(path, length($1::text)) = $1
		  AND strpos(substr(path, length($1::text)+1), '/') = 0
		  %s%s
		ORDER BY path LIMIT %d`, afterKey("path", after, func(a *BrowseAfter) *string { return a.Files }, &args),
		browseScope("path", scope, &args), MaxBrowseEntries+1), args...)
	if err != nil {
		return nil, err
	}
	lvl.Files, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (BrowseFile, error) {
		var f BrowseFile
		if err := row.Scan(&f.Path, &f.MediaType, &f.Size, &f.CreatedAt); err != nil {
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
		lvl.Truncated, lvl.MoreFiles = true, true
	}
	return &lvl, nil
}

// resumes reports whether one kind still has rows to read: on a first
// page every kind does, and on a later one only those the cursor did not
// mark finished.
func resumes(after *BrowseAfter, key func(*BrowseAfter) *string) bool {
	return after == nil || key(after) != nil
}

// afterKey renders "strictly after the last row delivered" for one kind,
// appending the value to args. It returns "" on a first page, so the
// caller can interpolate it unconditionally. A level orders by one column
// with no ties — an id and a path are each unique — so the keyset
// predicate is one comparison rather than the lexicographic walk the
// review feeds need.
func afterKey(col string, after *BrowseAfter, key func(*BrowseAfter) *string, args *[]any) string {
	if after == nil || key(after) == nil {
		return ""
	}
	*args = append(*args, *key(after))
	return fmt.Sprintf("AND %s > $%d", col, len(*args))
}

// browseScope is prefixScope as a WHERE fragment appended to a browse
// query, with the array bound as the next argument. Empty scope adds
// nothing, which is every deployment without an access policy.
func browseScope(column string, scope []string, args *[]any) string {
	cond, extra := prefixScope(column, scope, len(*args)+1)
	if cond == "" {
		return ""
	}
	*args = append(*args, extra...)
	return " AND " + cond
}
