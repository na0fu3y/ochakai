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
func (e *ExportSnapshot) IndexRows(ctx context.Context) ([]domain.Knowledge, error) {
	rows, err := e.tx.Query(ctx,
		`SELECT type, id, title, description FROM knowledge WHERE deleted_at IS NULL ORDER BY id`)
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
		`SELECT `+knowledgeSelectDoc+` FROM knowledge
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

// Files returns every live file in the bundle, in path order; the bytes
// are fetched one at a time as the archive is written (design doc 0013).
// A bundle with files in a deployment that has no blob store is an
// error, not an archive quietly missing them.
//
// It lists files rather than "each entry's attachments" because a file
// belongs to the bundle, not to an entry (design doc 0046 §3.3) — which
// is also what stops an export from dropping the ones no entry claims,
// the way the attachment query did.
//
// Blobs live in GCS, outside this snapshot: a blob deleted mid-export
// still fails when it is reached. The metadata being consistent is what
// makes that failure visible as a missing file rather than as an archive
// quietly disagreeing with its own index.
func (e *ExportSnapshot) Files(ctx context.Context) ([]domain.File, error) {
	rows, err := e.tx.Query(ctx, `SELECT `+objectCols+`
		FROM object o JOIN blob b ON b.sha256 = o.sha256
		WHERE o.deleted_at IS NULL ORDER BY o.path`)
	if err != nil {
		return nil, err
	}
	files, err := pgx.CollectRows(rows, scanFile)
	if err != nil {
		return nil, err
	}
	if len(files) > 0 && e.store.blobs == nil {
		return nil, errNoBlobStore
	}
	return files, nil
}
