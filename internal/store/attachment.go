package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
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
// files are unsupported — writes fail with errNoBlobStore.
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

// filePath is where a file with this name, written against this entry,
// lives: the path it names when the entry's own body points there, and
// the canonical <id>/<name> otherwise.
//
// The body is what decides, because the body is what attributes the file
// (design doc 0046 §3.3). A caller naming a path the entry says nothing
// about would otherwise write a file nothing points at — an orphan the
// moment it lands, and one no export would put back where it was asked
// for. Under the entry's own namespace it needs no mention: the path
// says whose it is.
func filePath(k *domain.Knowledge, name, at string) string {
	canonical := k.ID + "/" + name
	if at == "" || at == canonical {
		return canonical
	}
	if slices.Contains(domain.FilesFromBody(k.ID, k.Body), at) {
		return at
	}
	return canonical
}

// PutAttachment stores data as an attachment of a live entry, replacing
// any attachment of the same name. mediaType must already be validated
// (domain.DetectFileMediaType). Attach and detach count as changes
// to the entry: updated_at is bumped and a revision (with the attachment
// list in the snapshot) is recorded.
func (s *Store) PutAttachment(ctx context.Context, id, name, mediaType, at string, data []byte, actor domain.Actor) (*domain.File, error) {
	sum := sha256.Sum256(data)
	att := &domain.File{
		Name:      name,
		MediaType: mediaType,
		Size:      int64(len(data)),
		SHA256:    hex.EncodeToString(sum[:]),
		Path:      at,
		CreatedBy: actor,
		CreatedAt: time.Now().UTC(),
	}
	if s.blobs == nil {
		return nil, errNoBlobStore
	}
	// The external upload happens outside (before) the transaction:
	// create-only and content-addressed, so a DB failure afterwards
	// leaves only an unreferenced object the next identical attach reuses.
	if err := s.blobs.Put(ctx, att.SHA256, mediaType, data); err != nil {
		return nil, err
	}
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		k, err := s.Get(ctx, id)
		if err != nil {
			return err
		}
		path := filePath(k, name, at)
		// The blob row is metadata only now — the object row carries the
		// media type and size a read answers with — but it stays the
		// registry of which content exists, and a revision names bytes by
		// hash alone.
		if _, err := tx.Exec(ctx, `INSERT INTO blob (sha256, media_type, size)
			VALUES ($1, $2, $3) ON CONFLICT (sha256) DO NOTHING`,
			att.SHA256, att.MediaType, att.Size); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO object
			(path, type, title, body, blob_hash, size, media_type,
			 created_by_kind, created_by_name, updated_by_kind, updated_by_name,
			 created_at, updated_at, content_changed_at)
			VALUES ($1,'','','',$2,$3,$4,$5,$6,$5,$6,$7,$7,$7)
			ON CONFLICT (path) DO UPDATE SET
				blob_hash=EXCLUDED.blob_hash, size=EXCLUDED.size, media_type=EXCLUDED.media_type,
				updated_by_kind=EXCLUDED.updated_by_kind, updated_by_name=EXCLUDED.updated_by_name,
				updated_at=EXCLUDED.updated_at
			WHERE object.id IS NULL`,
			path, att.SHA256, att.Size, att.MediaType, actor.Kind, actor.Name, att.CreatedAt); err != nil {
			return err
		}
		// A replaced file's vector describes the old bytes; drop it here
		// and let the service re-embed after commit (design doc 0020).
		if err := execTolerateMissingTable(ctx, tx,
			`DELETE FROM attachment_embedding WHERE knowledge_id=$1 AND name=$2`, id, att.Name); err != nil {
			return err
		}
		return s.touchAndRevise(ctx, tx, k, "attach", actor)
	})
	if err != nil {
		return nil, err
	}
	return att, nil
}

// errNoBlobStore is the backstop for attachment operations on an
// instance without a blob store; the service layer checks first and
// wraps the condition in a client-facing error (design doc 0013).
var errNoBlobStore = errors.New("files are not supported without GCS: set OCHAKAI_GCS_BUCKET (design doc 0013)")

// GetAttachment returns one attachment with its bytes. Attachments of
// soft-deleted entries are gone with the entry.
func (s *Store) GetAttachment(ctx context.Context, id, name string) (*domain.File, []byte, error) {
	att, err := s.GetAttachmentMeta(ctx, id, name)
	if err != nil {
		return nil, nil, err
	}
	if s.blobs == nil {
		return nil, nil, errNoBlobStore
	}
	data, err := s.blobs.Get(ctx, att.SHA256)
	if err != nil {
		return nil, nil, err
	}
	return att, data, nil
}

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

// GetAttachmentMeta returns one file's metadata without touching the
// blob store — enough to answer a conditional GET (the ETag is the
// content hash) before deciding whether the bytes are needed.
//
// The name is the last segment of a path, so a file the entry shows from
// elsewhere in the bundle answers to its filename here, which is the
// name every surface has always called it by.
func (s *Store) GetAttachmentMeta(ctx context.Context, id, name string) (*domain.File, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+fileCols+`
		FROM object k JOIN object f ON `+attributedTo+`
		WHERE k.id=$1 AND k.deleted_at IS NULL
		  AND substr(f.path, length(f.path) - length($2)) = '/' || $2
		ORDER BY f.path LIMIT 1`, id, name)
	if err != nil {
		return nil, err
	}
	att, err := pgx.CollectExactlyOneRow(rows, scanFile)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &att, nil
}

// DeleteAttachment removes the entry→blob mapping. The blob itself stays:
// revisions still name its hash, and content-addressed rows are cheap.
// Nothing reclaims a blob that no revision names any more — there is no
// sweep, so the bucket only grows.
func (s *Store) DeleteAttachment(ctx context.Context, id, name string, actor domain.Actor) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		k, err := s.Get(ctx, id)
		if err != nil {
			return err
		}
		att, err := s.GetAttachmentMeta(ctx, id, name)
		if err != nil {
			return err
		}
		path := filePath(k, att.Name, att.Path)
		tag, err := tx.Exec(ctx, `DELETE FROM object WHERE path=$1 AND id IS NULL`, path)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if err := execTolerateMissingTable(ctx, tx,
			`DELETE FROM attachment_embedding WHERE knowledge_id=$1 AND name=$2`, id, att.Name); err != nil {
			return err
		}
		return s.touchAndRevise(ctx, tx, k, "detach", actor)
	})
}

// ExportAttachment is one attachment with its owner and bytes, as the
// OKF exporter consumes it.
type ExportAttachment struct {
	ID   string
	Att  domain.File
	Data []byte
}

// AttachmentBytes returns the content-addressed bytes behind an
// attachment. Paired with ExportSnapshot.FileMeta for streaming
// export.
func (s *Store) AttachmentBytes(ctx context.Context, sha256 string) ([]byte, error) {
	if s.blobs == nil {
		return nil, errNoBlobStore
	}
	return s.blobs.Get(ctx, sha256)
}

// touchAndRevise bumps the entry's updated_at and records a revision
// whose snapshot includes the attachment list after the change —
// attach/detach are changes to the entry, and every change is kept.
func (s *Store) touchAndRevise(ctx context.Context, tx pgx.Tx, k *domain.Knowledge, change string, actor domain.Actor) error {
	// NowStored, like every other write path: time.Now()'s nanoseconds do
	// not survive the round trip through timestamptz, so the revision
	// snapshot would carry a version the stored row never had (design
	// doc 0030).
	k.UpdatedAt = NowStored()
	// deleted_at IS NULL, as on every other write: the entry was read
	// outside this transaction, so a delete can land in the window, and
	// an attachment plus its revision planted on a tombstone would
	// resurface whenever someone revived the entry.
	tag, err := tx.Exec(ctx,
		`UPDATE object SET updated_at=$2 WHERE id=$1 AND deleted_at IS NULL`, k.ID, k.UpdatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	atts, err := listAttachmentsTx(ctx, tx, k.ID)
	if err != nil {
		return err
	}
	k.Files = atts
	return s.addRevision(ctx, tx, k, change, actor)
}

// listAttachmentsTx reads the file list inside the writing transaction,
// so the revision snapshot sees the change it records.
func listAttachmentsTx(ctx context.Context, tx pgx.Tx, id string) ([]domain.File, error) {
	rows, err := tx.Query(ctx, `SELECT `+fileCols+`
		FROM object k JOIN object f ON `+attributedTo+`
		WHERE k.id=$1 ORDER BY f.path`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanFile)
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
		return nil, false, errNoBlobStore
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	at := NowStored()
	// Outside the transaction, and content-addressed, so a failure after
	// it leaves only an unreferenced object the next write reuses.
	if err := s.blobs.Put(ctx, hash, mediaType, data); err != nil {
		return nil, false, err
	}
	err = s.withTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO blob (sha256, media_type, size)
			VALUES ($1, $2, $3) ON CONFLICT (sha256) DO NOTHING`, hash, mediaType, int64(len(data))); err != nil {
			return err
		}
		// (xmax = 0) is Postgres's own tell for "this row was inserted,
		// not updated by the ON CONFLICT arm" — one query, no second
		// round trip to ask created (design doc 0064: a file PUT answers
		// 201 on create the same way a concept PUT already does).
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
			RETURNING (xmax = 0)`,
			p, hash, int64(len(data)), mediaType, actor.Kind, actor.Name, at)
		if err := row.Scan(&created); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: %s is a concept", ErrAlreadyExists, p)
			}
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
		CreatedBy: actor, CreatedAt: at,
	}, created, nil
}

// GetFile returns the file object at a bundle path with its bytes.
func (s *Store) GetFile(ctx context.Context, p string) (*domain.File, []byte, error) {
	att, err := s.GetFileMeta(ctx, p)
	if err != nil {
		return nil, nil, err
	}
	if s.blobs == nil {
		return nil, nil, errNoBlobStore
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
