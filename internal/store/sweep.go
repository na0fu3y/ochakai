package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Blob bytes are content-addressed and shared between entries, so no
// single delete or purge can reclaim them as its own side effect — the
// hash it stops referencing may be somebody else's diagram. What can be
// answered is the global question, and SweepBlobs answers it: every blob
// row that no object row and no attachment row references any more is
// deleted, bytes first, row second. The sweep is idempotent and
// self-healing — a failure leaves rows behind for the next run, and the
// blob store's Delete treats already-gone bytes as success — so the
// service runs it after the destructive operations (purge, file delete),
// and an error there is a warning, not a failed request: the rows the
// caller asked to destroy are already gone, and the bytes go on the next
// run. A file overwrite can orphan the replaced hash too; those wait for
// the next destructive operation rather than taxing every import PUT
// with an exclusive-lock scan.
//
// This is what makes a purge's promise reach the bytes (design doc 0031;
// condition C1: the asset leaves whole, returns whole, and is deleted on
// request). Before this sweep existed, purge erased every row and left
// the file bytes in the bucket forever.

// blobLockID keys the advisory lock the sweep and the file writers
// share. The race it closes: PutFile and PutAttachment upload bytes
// create-only and read "already there" as success, so a sweep running
// between that upload and the commit of the row referencing it would
// erase bytes a row is about to name. Writers therefore hold the lock
// shared (they can run beside each other) for the span from upload to
// commit, and the sweep holds it exclusive. The value is arbitrary and
// only has to be one thing in one place.
const blobLockID int64 = 0xb10b // "blob", squinting

// lockBlobsShared marks tx as a writer the sweep must not run beside.
// Transaction-scoped, so commit and rollback both release it.
func lockBlobsShared(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared($1)`, blobLockID)
	return err
}

// SweepBlobs deletes every blob nothing references, returning how many
// left. Safe to call at any time, from any caller, concurrently with
// writes — the lock above is what makes that claim true.
func (s *Store) SweepBlobs(ctx context.Context) (int, error) {
	deleted := 0
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, blobLockID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT sha256 FROM blob b
			WHERE NOT EXISTS (SELECT 1 FROM object o WHERE o.blob_hash = b.sha256)
			  AND NOT EXISTS (SELECT 1 FROM attachment a WHERE a.sha256 = b.sha256)`)
		if err != nil {
			return err
		}
		shas, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		if len(shas) == 0 {
			return nil
		}
		// Bytes before rows: a failure between the two leaves a row whose
		// bytes are gone, which only the next sweep ever reads — the row
		// is unreferenced, and a re-upload of the same content finds the
		// object absent and stores it again. Rows before bytes would
		// instead forget the hash and leak the bytes with nothing left to
		// name them.
		if s.blobs != nil {
			for _, sha := range shas {
				if err := s.blobs.Delete(ctx, sha); err != nil {
					return err
				}
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM blob WHERE sha256 = ANY($1)`, shas); err != nil {
			return err
		}
		deleted = len(shas)
		return nil
	})
	return deleted, err
}
