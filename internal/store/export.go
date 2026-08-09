package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// ExportSnapshot reads a knowledge base as it stood at one instant.
//
// An export is streamed: the id list and the attachment metadata are read
// first, then the entries in batches while the archive is already on the
// wire. Each of those reads is its own statement, so without a snapshot
// they see different databases. An entry deleted after the id list was
// taken leaves an index.md entry pointing at a file the archive does not
// contain; an entry renamed mid-stream can appear twice or not at all; a
// title edited between the two reads disagrees with itself across the
// bundle. Nothing in the result says any of this happened — the archive
// is well-formed, just not a picture of anything that ever existed.
//
// A REPEATABLE READ transaction costs one pooled connection for the
// length of the download. That is the trade this makes: the endpoint
// exists to produce a backup, and a backup that is not a point in time is
// not a backup. The batching still matters for memory — the snapshot
// bounds what the reads see, not how much of it is held at once.
type ExportSnapshot struct {
	store *Store
	conn  *pgxpool.Conn
	tx    pgx.Tx
}

// BeginExport opens the snapshot. PostgreSQL fixes a REPEATABLE READ
// snapshot at the transaction's first query, not at BEGIN, so the instant
// the archive describes is the first IndexRows call. Close it when the
// archive is done.
func (s *Store) BeginExport(ctx context.Context) (*ExportSnapshot, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		conn.Release()
		return nil, err
	}
	return &ExportSnapshot{store: s, conn: conn, tx: tx}, nil
}

// Close ends the snapshot and returns the connection to the pool. Read-only
// and never committed: there is nothing to keep.
func (e *ExportSnapshot) Close(ctx context.Context) {
	_ = e.tx.Rollback(ctx)
	e.conn.Release()
}

// IndexRows returns the id, type, title and description of every live
// entry, in id order — what index.md files are built from, and the id list
// the rest of the export walks. Never a body: the index names entries, it
// does not carry them.
func (e *ExportSnapshot) IndexRows(ctx context.Context, prefix string) ([]domain.Knowledge, error) {
	rows, err := e.tx.Query(ctx,
		`SELECT type, id, title, description FROM object
		  WHERE deleted_at IS NULL AND id IS NOT NULL`+underPrefix("", "$1")+`
		  ORDER BY id`, prefix)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Knowledge, error) {
		var k domain.Knowledge
		err := row.Scan(&k.Type, &k.ID, &k.Title, &k.Description)
		return k, err
	})
}

// ListByIDs returns the named live entries in id order. The exporter walks
// the id list in batches so the whole knowledge base is never held in
// memory at once.
func (e *ExportSnapshot) ListByIDs(ctx context.Context, ids []string) ([]domain.Knowledge, error) {
	rows, err := e.tx.Query(ctx,
		`SELECT `+knowledgeSelectDoc+` FROM object
		 WHERE deleted_at IS NULL AND id = ANY($1) ORDER BY id`, ids)
	if err != nil {
		return nil, err
	}
	// With the document: an export writes what the entry is stored as,
	// plus this instance's own keys (design doc 0046 §2.2). Composing it
	// from the index columns instead would hand back a reformatted copy
	// of what the writer wrote.
	return pgx.CollectRows(rows, scanKnowledgeDoc)
}

// FileMeta returns the metadata of every file the archive at prefix has
// to carry, in path order; the bytes are fetched one at a time as the
// archive is written (design doc 0013). A bundle holding files in a
// deployment that has no blob store is an error, not an empty archive.
//
// What it has to carry is every file *at* a path under the prefix —
// attributed to an entry or not. A file is an object in the bundle
// (design doc 0046 §§2.1, 3.3), not a property of an entry, and what
// enters the bundle leaves it (§3.2): selecting by attribution would
// hand back an archive missing the producer's seed data and every file
// whose entry has since been deleted, silently, in the one endpoint
// that exists to be a backup.
//
// The second half of the predicate is for a subtree archive only: a file
// an in-scope entry's body points at travels with the entry even when it
// lives outside the subtree, because the body link would otherwise
// dangle in the copy. At the root the first half already holds
// everything.
//
// Blobs live in GCS, outside this snapshot: a blob deleted mid-export
// still fails when it is reached. The metadata being consistent is what
// makes that failure visible as a missing file rather than as an archive
// quietly disagreeing with its own index.
func (e *ExportSnapshot) FileMeta(ctx context.Context, prefix string) ([]domain.File, error) {
	rows, err := e.tx.Query(ctx, `SELECT `+fileCols+`
		FROM object f
		WHERE f.id IS NULL AND f.deleted_at IS NULL
		  AND ($1 = '' OR starts_with(f.path, $1 || '/')
		       OR EXISTS (SELECT 1 FROM object k
		                   WHERE k.id IS NOT NULL AND k.deleted_at IS NULL
		                     AND `+attributedTo+underPrefix("k.", "$1")+`))
		ORDER BY f.path`, prefix)
	if err != nil {
		return nil, err
	}
	atts, err := pgx.CollectRows(rows, scanFile)
	if err != nil {
		return nil, err
	}
	if len(atts) > 0 && e.store.blobs == nil {
		return nil, ErrNoBlobStore
	}
	return atts, nil
}

// RevisionsUnder is ListRevisionsUnder inside the export's snapshot, so
// the log.md files an archive carries describe the same instant as the
// documents beside them (design doc 0046 §3.8). A history read outside
// the snapshot could name a change to an entry the archive does not
// contain, or miss one it does.
func (e *ExportSnapshot) RevisionsUnder(ctx context.Context, prefix string, limit int) ([]LogRow, error) {
	rows, err := e.tx.Query(ctx, revisionsUnderSQL, prefix, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanLogRow)
}

// underPrefix scopes an export to one subtree, matched on segment
// boundaries like every other path scope (design doc 0041 §2.2): a
// "metrics" scope covers metrics itself and everything under "metrics/",
// and leaves "metrics-legacy/churn" alone. An empty prefix is the whole
// bundle and adds no predicate, so the archive of everything runs the
// query it always ran.
//
// The parameter is always bound, empty or not: a query text that changed
// with the argument would be a second prepared statement for the same
// question.
func underPrefix(col, arg string) string {
	return " AND (" + arg + " = '' OR " + col + "id = " + arg +
		" OR left(" + col + "id, length(" + arg + ") + 1) = " + arg + " || '/')"
}
