package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// Files (design docs 0008, 0013, 0046 §§3.3, 3.13). A file is an object
// in the bundle, at a path, beside the concepts — not a property of an
// entry. Bytes stay content-addressed and immutable in the external blob
// store (GCS): attaching the same file twice stores it once, and a
// revision can name any historical content by hash. Blobs are never
// deleted, so the bucket only grows. Without a configured blob store,
// files are unsupported — writes fail with ErrNoBlobStore.
//
// Which entry a file belongs to is derived rather than recorded (0046
// §3.3): it is the entry whose <id>/ namespace the file sits directly
// under, or the entry whose body points at it. A bundle has nowhere to
// write ownership down, and it does not need one — what makes a diagram
// the metric's diagram is that the metric's document shows it.
//
// The two halves are one SQL predicate, attributedTo, which every read
// here goes through. The path is what the wire carries, and the name a
// surface calls a file by is its last segment — derived on the way out,
// the way a concept keeps its id beside its path.

// attributedTo is the predicate joining file objects f to the entry row
// k: living directly under the entry's namespace (no further slash), or
// named by the entry's own body. The containment half is indexed by
// object_files, the GIN index; object_concept keeps concepts out of the
// path scan.
//
// The namespace half is starts_with rather than LIKE, for the reason
// browsing gives (browse.go): an id may contain "_", which LIKE reads as
// a wildcard. "sales_2024" would have claimed every file under
// "salesX2024/" as its own.
const attributedTo = `f.id IS NULL AND f.deleted_at IS NULL
	AND (starts_with(f.path, k.id || '/') AND strpos(substr(f.path, length(k.id) + 2), '/') = 0
	     OR k.files ? f.path)`

// fileCols is one file object as an attachment is read: the path is what
// the name is derived from, so it is selected rather than the name.
const fileCols = `f.path, f.media_type, f.size, f.blob_hash,
	f.created_by_kind, f.created_by_name, f.created_at`

// asFile renders one file object the way the surfaces name it: at
// its path, with the last segment as the name every surface calls it by.
func asFile(path, mediaType string, size int64, hash string,
	by domain.Actor, at time.Time,
) domain.File {
	return domain.File{
		Name: path[strings.LastIndex(path, "/")+1:], MediaType: mediaType,
		Size: size, SHA256: hash, Path: path, CreatedBy: by, CreatedAt: at,
	}
}

// scanFile pairs fileCols with asFile.
func scanFile(row pgx.CollectableRow) (domain.File, error) {
	var path, mediaType, hash string
	var size int64
	var by domain.Actor
	var at time.Time
	if err := row.Scan(&path, &mediaType, &size, &hash, &by.Kind, &by.Name, &at); err != nil {
		return domain.File{}, err
	}
	return asFile(path, mediaType, size, hash, by, at), nil
}

// ErrNoBlobStore is the backstop for attachment operations on an
// instance without a blob store; the service layer checks first and
// wraps the condition in a client-facing error (design doc 0013).
//
// Exported because one caller cannot check first: the archive reads the
// store directly, and whether it needs a blob store is not a property of
// the request but of what the snapshot turned out to contain
// (ExportSnapshot.FileMeta). So the sentinel travels, and writeError
// gives it the 501 the other paths get.
var ErrNoBlobStore = errors.New("files are not supported without GCS: set OCHAKAI_GCS_BUCKET (design doc 0075 §1)")

// ListAttachments returns the metadata (no bytes) of the files a live
// entry is shown by, in name order.
func (s *Store) ListAttachments(ctx context.Context, id string) ([]domain.File, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+fileCols+`
		FROM object k JOIN object f ON `+attributedTo+`
		WHERE k.id=$1 AND k.deleted_at IS NULL ORDER BY f.path`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanFile)
}

// ListAttachmentsBatch returns file metadata for a set of entries in one
// query, keyed by entry id. Entries with no files have no key. The
// callers pass entries they already know are live (search hits,
// backlinks), so no liveness join is needed.
func (s *Store) ListAttachmentsBatch(ctx context.Context, ids []string) (map[string][]domain.File, error) {
	rows, err := s.pool.Query(ctx, `SELECT k.id, `+fileCols+`
		FROM object k
		JOIN unnest($1::text[]) AS want(id) ON k.id = want.id
		JOIN object f ON `+attributedTo+`
		ORDER BY k.id, f.path`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]domain.File{}
	for rows.Next() {
		var owner, path, mediaType, hash string
		var size int64
		var by domain.Actor
		var at time.Time
		if err := rows.Scan(&owner, &path, &mediaType, &size, &hash, &by.Kind, &by.Name, &at); err != nil {
			return nil, err
		}
		out[owner] = append(out[owner], asFile(path, mediaType, size, hash, by, at))
	}
	return out, rows.Err()
}

// AttachmentBytes returns the content-addressed bytes behind an
// attachment. Paired with ExportSnapshot.FileMeta for streaming
// export.
func (s *Store) AttachmentBytes(ctx context.Context, sha256 string) ([]byte, error) {
	if s.blobs == nil {
		return nil, ErrNoBlobStore
	}
	return s.blobs.Get(ctx, sha256)
}

// PutFile writes a file object at a bundle path, replacing whatever file
// is there (design doc 0046 §§3.2, 3.5). It is the write behind the
// bundle address: an object that belongs to no entry — a producer's seed
// data, a diagram in a shared directory, a markdown file that carries no
// type and so is not a concept.
//
// A concept at that path is not overwritten. The two kinds share one
// address space, so the path is taken, and a caller who meant to edit
// the entry there means PUT on the entry.
func (s *Store) PutFile(ctx context.Context, p, mediaType string, data []byte, actor domain.Actor) (att *domain.File, created bool, err error) {
	if s.blobs == nil {
		return nil, false, ErrNoBlobStore
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	at := NowStored()
	var (
		createdBy domain.Actor
		createdAt time.Time
	)
	err = s.withTx(ctx, func(tx pgx.Tx) error {
		// Under the writers' shared lock (sweep.go), and content-addressed,
		// so a failure after the upload leaves only an unreferenced object
		// the next write reuses — or the sweep reclaims, though never
		// between this Put and the commit that names its hash.
		if err := lockBlobsShared(ctx, tx); err != nil {
			return err
		}
		if err := s.blobs.Put(ctx, hash, mediaType, data); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO blob (sha256, media_type, size)
			VALUES ($1, $2, $3) ON CONFLICT (sha256) DO NOTHING`, hash, mediaType, int64(len(data))); err != nil {
			return err
		}
		// (xmax = 0) is Postgres's own tell for "this row was inserted,
		// not updated by the ON CONFLICT arm" — one query, no second
		// round trip to ask created (design doc 0064: a file PUT answers
		// 201 on create the same way a concept PUT already does).
		// RETURNING reads created_by and created_at off the row rather
		// than assuming them: an overwrite keeps the original creator
		// (created_at is not in the ON CONFLICT arm), and a response
		// saying otherwise would disagree with the next GET (design doc
		// 0064).
		row := tx.QueryRow(ctx, `INSERT INTO object
			(path, type, title, body, blob_hash, size, media_type,
			 created_by_kind, created_by_name, updated_by_kind, updated_by_name,
			 created_at, updated_at, content_changed_at)
			VALUES ($1,'','','',$2,$3,$4,$5,$6,$5,$6,$7,$7,$7)
			ON CONFLICT (path) DO UPDATE SET
				blob_hash=EXCLUDED.blob_hash, size=EXCLUDED.size, media_type=EXCLUDED.media_type,
				updated_by_kind=EXCLUDED.updated_by_kind, updated_by_name=EXCLUDED.updated_by_name,
				updated_at=EXCLUDED.updated_at
			WHERE object.id IS NULL
			RETURNING (xmax = 0), created_by_kind, created_by_name, created_at`,
			p, hash, int64(len(data)), mediaType, actor.Kind, actor.Name, at)
		if err := row.Scan(&created, &createdBy.Kind, &createdBy.Name, &createdAt); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: %s is a concept", ErrAlreadyExists, p)
			}
			return err
		}
		// The vector at this address describes the bytes that were here;
		// the service re-embeds after commit. Dropping it in the write
		// rather than trusting the upsert is what makes the stale case
		// impossible: bytes the model declines — or a provider that is
		// down — would otherwise leave the old vector answering for
		// content that is gone (design doc 0020).
		if err := execTolerateMissingTable(ctx, tx,
			`DELETE FROM attachment_embedding WHERE path=$1`, p); err != nil {
			return err
		}
		return s.addFileRevision(ctx, tx, p, "create", actor)
	})
	if err != nil {
		return nil, false, err
	}
	return &domain.File{
		Name: p[strings.LastIndex(p, "/")+1:], MediaType: mediaType,
		Size: int64(len(data)), SHA256: hash, Path: p,
		CreatedBy: createdBy, CreatedAt: createdAt,
	}, created, nil
}

// GetFile returns the file object at a bundle path with its bytes.
func (s *Store) GetFile(ctx context.Context, p string) (*domain.File, []byte, error) {
	att, err := s.GetFileMeta(ctx, p)
	if err != nil {
		return nil, nil, err
	}
	if s.blobs == nil {
		return nil, nil, ErrNoBlobStore
	}
	data, err := s.blobs.Get(ctx, att.SHA256)
	if err != nil {
		return nil, nil, err
	}
	return att, data, nil
}

// GetFileMeta is GetFile without the bytes, for a conditional request.
func (s *Store) GetFileMeta(ctx context.Context, p string) (*domain.File, error) {
	var a domain.File
	var at time.Time
	err := s.pool.QueryRow(ctx, `SELECT media_type, size, blob_hash,
		created_by_kind, created_by_name, created_at
		FROM object WHERE path=$1 AND id IS NULL AND deleted_at IS NULL`, p).
		Scan(&a.MediaType, &a.Size, &a.SHA256, &a.CreatedBy.Kind, &a.CreatedBy.Name, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	a.Name, a.Path, a.CreatedAt = p[strings.LastIndex(p, "/")+1:], p, at
	return &a, nil
}

// DeleteFile removes the file object at a bundle path. The blob stays:
// revisions still name its hash, and content-addressed rows are cheap.
func (s *Store) DeleteFile(ctx context.Context, p string, actor domain.Actor) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM object WHERE path=$1 AND id IS NULL`, p)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if err := execTolerateMissingTable(ctx, tx,
			`DELETE FROM attachment_embedding WHERE path=$1`, p); err != nil {
			return err
		}
		return s.addFileRevision(ctx, tx, p, "delete", actor)
	})
}

// addFileRevision records a file's own history, in the ledger the
// concepts use (design doc 0046 §3.1). A file has no document, so the
// revision carries the change and who made it and nothing else — which
// is the whole of what happened.
func (s *Store) addFileRevision(ctx context.Context, tx pgx.Tx, p, change string, actor domain.Actor) error {
	_, err := tx.Exec(ctx, `INSERT INTO knowledge_revision
		(path, id, rev, change, changed_by_kind, changed_by_name, changed_by_via, changed_by_producer, doc)
		VALUES ($1, NULL, (SELECT COALESCE(MAX(rev), 0) + 1 FROM knowledge_revision WHERE path=$1), $2, $3, $4, $5, $6, '')`,
		p, change, actor.Kind, actor.Name, actor.Via, actor.Producer)
	return err
}
