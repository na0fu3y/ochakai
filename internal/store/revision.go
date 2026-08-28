// Revisions: the append-only ledger of what changed, and the two ways a
// reader asks for it — one concept's history, and a path's (OKF's
// log.md).
package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/okf"
)

// ListRevisions returns an entry's change history, newest first. It
// reads history, so it works for soft-deleted entries too — the audit
// trail is most interesting exactly when the entry is gone.
func (s *Store) ListRevisions(ctx context.Context, id string, limit int) ([]domain.Revision, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT rev, change, changed_by_kind, changed_by_name, changed_by_via, changed_by_producer, changed_at, doc, note
		 FROM knowledge_revision WHERE id=$1 ORDER BY rev DESC LIMIT $2`,
		id, limit)
	if err != nil {
		return nil, err
	}
	revs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Revision, error) {
		var r domain.Revision
		return r, row.Scan(&r.Rev, &r.Change, &r.ChangedBy.Kind, &r.ChangedBy.Name, &r.ChangedBy.Via,
			&r.ChangedBy.Producer, &r.ChangedAt, &r.Document, &r.Note)
	})
	if err != nil {
		return nil, err
	}
	if len(revs) == 0 {
		// Distinguish "no such entry" from "entry with empty history"
		// (the latter cannot happen: create always writes rev 1).
		return nil, ErrNotFound
	}
	return revs, nil
}

// LogRow is one line of a subtree's history: enough to render log.md
// (SPEC §9) and no more. The document each revision holds is
// deliberately not selected — a history that shipped every past version
// of every entry in a directory would be the whole knowledge base
// several times over, and what log.md publishes is what changed and by
// whom (design doc 0046 §3.8).
//
// The row is keyed by the object's path, because that is what a
// revision is an event about (design doc 0046 §3.1) and because a file
// has no concept id — a log that keyed on the id would leave every
// attach and detach out of the history it belongs in.
type LogRow struct {
	Path      string
	Title     string
	Change    string
	ChangedBy domain.Actor
	ChangedAt time.Time
	// Note is the reason the change was made — a rejection's, and no
	// other change writes one (design doc 0135). log.md carries it,
	// which is what makes the published history say *why* a concept is
	// gone rather than only that somebody removed it.
	Note string
}

// ListRevisionsAt returns the changes to the one object at this path,
// newest first — the ledger behind ?history (design doc 0046 §3.5).
//
// Keyed by path rather than by concept id, so it answers for a file as
// well: a revision is an event about an object (§3.1), and "what
// happened to this object" should not be two questions depending on
// what kind it is. It reads history, so a soft-deleted or purged-around
// object still answers, which is when a history is most worth having.
func (s *Store) ListRevisionsAt(ctx context.Context, path string, limit int) ([]LogRow, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT r.path, COALESCE(k.title, ''), r.change,
		        r.changed_by_kind, r.changed_by_name, r.changed_by_via, r.changed_by_producer, r.changed_at, r.note
		 FROM knowledge_revision r
		 LEFT JOIN object k ON k.path = r.path
		 WHERE r.path = $1
		 ORDER BY r.rev DESC LIMIT $2`, path, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanLogRow)
}

// ListRevisionsUnder returns the newest changes anywhere under a path
// prefix, newest first — concepts and files alike, which is the whole
// point of one ledger keyed by path. An empty prefix is the whole
// bundle.
//
// The title comes from the entry as it stands rather than from the
// revision: a log names what changed, and naming it by a title it has
// since stopped having would send a reader looking for something that is
// not there. An entry that has since been purged keeps its history and
// loses its title, which is why the join is left. A file has no title
// and is named by its path.
//
// starts_with, not LIKE: a path may contain "_", which LIKE reads as a
// wildcard (browse.go says the same about ids).
func (s *Store) ListRevisionsUnder(ctx context.Context, prefix string, limit int) ([]LogRow, error) {
	rows, err := s.pool.Query(ctx, revisionsUnderSQL, prefix, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanLogRow)
}

// revisionsUnderSQL is the query behind both callers: the read at a
// log.md address and the export writing the same file from a snapshot.
// One statement, because two that drifted would be two histories.
const revisionsUnderSQL = `SELECT r.path, COALESCE(k.title, ''), r.change,
	        r.changed_by_kind, r.changed_by_name, r.changed_by_via, r.changed_by_producer, r.changed_at, r.note
	 FROM knowledge_revision r
	 LEFT JOIN object k ON k.path = r.path
	 WHERE $1 = '' OR r.path = $1 || '.md' OR starts_with(r.path, $1 || '/')
	 ORDER BY r.changed_at DESC, r.path, r.rev DESC LIMIT $2`

func scanLogRow(row pgx.CollectableRow) (LogRow, error) {
	var l LogRow
	return l, row.Scan(&l.Path, &l.Title, &l.Change,
		&l.ChangedBy.Kind, &l.ChangedBy.Name, &l.ChangedBy.Via, &l.ChangedBy.Producer, &l.ChangedAt, &l.Note)
}

// note is why the change was made, and only a rejection carries one: a
// delete with a reason is the ruling that a concept was not accepted
// (design doc 0135). It rides on the event rather than on the concept
// because the concept is what the event removed, and it is what the OKF
// SPEC §9 log renders beside the line.
func (s *Store) addRevision(ctx context.Context, tx pgx.Tx, k *domain.Knowledge, change string, actor domain.Actor, note string) error {
	// The full document, server-owned keys included: a revision records
	// what the entry was, and who had confirmed it at the time is part of
	// that (design doc 0043 §3.9).
	doc, err := okf.Document(k)
	if err != nil {
		return err
	}
	// A revision is an event about an object, so the ledger counts by
	// path (design doc 0046 §3.1): when a file's create and delete land
	// here too, they share one history with the concept beside them.
	_, err = tx.Exec(ctx, `INSERT INTO knowledge_revision
		(path, id, rev, change, changed_by_kind, changed_by_name, changed_by_via, changed_by_producer, doc, note)
		VALUES ($1, $2, (SELECT COALESCE(MAX(rev), 0) + 1 FROM knowledge_revision WHERE path=$1), $3, $4, $5, $6, $7, $8, $9)`,
		domain.ConceptPath(k.ID), k.ID, change, actor.Kind, actor.Name, actor.Via, actor.Producer, string(doc), note)
	return err
}
