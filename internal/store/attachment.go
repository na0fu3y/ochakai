package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// Attachment persistence (design docs 0008, 0013). Bytes are content-
// addressed and immutable — attaching the same file twice stores it once,
// and revisions can name any historical content by hash. The blob row
// keeps the metadata (media_type, size) and the attachment row maps
// entry + filename to a blob; the bytes themselves live only in the
// external blob store (GCS, design doc 0013 — the bytea column of design
// doc 0008 is gone). Blobs are never deleted: like knowledge revisions,
// history is retained. Without a configured blob store, attachments are
// unsupported — writes fail with errNoBlobStore.

const attachmentCols = `a.name, b.media_type, b.size, a.sha256, a.okf_path,
	a.created_by_kind, a.created_by_name, a.created_at`

func scanAttachment(row pgx.CollectableRow) (domain.Attachment, error) {
	var a domain.Attachment
	err := row.Scan(&a.Name, &a.MediaType, &a.Size, &a.SHA256, &a.OKFPath,
		&a.CreatedBy.Kind, &a.CreatedBy.Name, &a.CreatedAt)
	return a, err
}

// PutAttachment stores data as an attachment of a live entry, replacing
// any attachment of the same name. mediaType must already be validated
// (domain.DetectAttachmentMediaType). Attach and detach count as changes
// to the entry: updated_at is bumped and a revision (with the attachment
// list in the snapshot) is recorded.
func (s *Store) PutAttachment(ctx context.Context, id, name, mediaType, okfPath string, data []byte, actor domain.Actor) (*domain.Attachment, error) {
	sum := sha256.Sum256(data)
	att := &domain.Attachment{
		Name:      name,
		MediaType: mediaType,
		Size:      int64(len(data)),
		SHA256:    hex.EncodeToString(sum[:]),
		OKFPath:   okfPath,
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
		var count int
		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM attachment WHERE knowledge_id=$1 AND name<>$2`,
			id, name).Scan(&count); err != nil {
			return err
		}
		if count >= domain.MaxAttachmentsPerEntry {
			return fmt.Errorf("invalid attachment: entry already has %d attachments (max %d)", count, domain.MaxAttachmentsPerEntry)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO blob (sha256, media_type, size)
			VALUES ($1, $2, $3) ON CONFLICT (sha256) DO NOTHING`,
			att.SHA256, att.MediaType, att.Size); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO attachment
			(knowledge_id, name, sha256, okf_path, created_by_kind, created_by_name, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7)
			ON CONFLICT (knowledge_id, name) DO UPDATE SET
				sha256=EXCLUDED.sha256, okf_path=EXCLUDED.okf_path,
				created_by_kind=EXCLUDED.created_by_kind, created_by_name=EXCLUDED.created_by_name,
				created_at=EXCLUDED.created_at`,
			id, att.Name, att.SHA256, att.OKFPath, actor.Kind, actor.Name, att.CreatedAt); err != nil {
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
var errNoBlobStore = errors.New("attachments are not supported without GCS: set OCHAKAI_GCS_BUCKET (design doc 0013)")

// GetAttachment returns one attachment with its bytes. Attachments of
// soft-deleted entries are gone with the entry.
func (s *Store) GetAttachment(ctx context.Context, id, name string) (*domain.Attachment, []byte, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+attachmentCols+`
		FROM attachment a
		JOIN blob b ON b.sha256 = a.sha256
		JOIN knowledge k ON k.id = a.knowledge_id AND k.deleted_at IS NULL
		WHERE a.knowledge_id=$1 AND a.name=$2`, id, name)
	if err != nil {
		return nil, nil, err
	}
	att, err := pgx.CollectExactlyOneRow(rows, scanAttachment)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
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
	return &att, data, nil
}

// ListAttachments returns the metadata (no bytes) of a live entry's
// attachments, in name order.
func (s *Store) ListAttachments(ctx context.Context, id string) ([]domain.Attachment, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+attachmentCols+`
		FROM attachment a
		JOIN blob b ON b.sha256 = a.sha256
		JOIN knowledge k ON k.id = a.knowledge_id AND k.deleted_at IS NULL
		WHERE a.knowledge_id=$1 ORDER BY a.name`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanAttachment)
}

// ListAttachmentsBatch returns attachment metadata for a set of entries
// in one query, keyed by entry id. Entries without attachments have no
// key. The callers pass entries they already know are live (search
// hits, backlinks), so no liveness join is needed.
func (s *Store) ListAttachmentsBatch(ctx context.Context, ids []string) (map[string][]domain.Attachment, error) {
	rows, err := s.pool.Query(ctx, `SELECT a.knowledge_id, `+attachmentCols+`
		FROM attachment a
		JOIN blob b ON b.sha256 = a.sha256
		JOIN unnest($1::text[]) AS want(id) ON a.knowledge_id = want.id
		ORDER BY a.knowledge_id, a.name`, ids)
	if err != nil {
		return nil, err
	}
	out := map[string][]domain.Attachment{}
	for rows.Next() {
		var id string
		var a domain.Attachment
		if err := rows.Scan(&id, &a.Name, &a.MediaType, &a.Size, &a.SHA256, &a.OKFPath,
			&a.CreatedBy.Kind, &a.CreatedBy.Name, &a.CreatedAt); err != nil {
			return nil, err
		}
		out[id] = append(out[id], a)
	}
	return out, rows.Err()
}

// GetAttachmentMeta returns one attachment's metadata without touching
// the blob store — enough to answer a conditional GET (the ETag is the
// content hash) before deciding whether the bytes are needed.
func (s *Store) GetAttachmentMeta(ctx context.Context, id, name string) (*domain.Attachment, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+attachmentCols+`
		FROM attachment a
		JOIN blob b ON b.sha256 = a.sha256
		JOIN knowledge k ON k.id = a.knowledge_id AND k.deleted_at IS NULL
		WHERE a.knowledge_id=$1 AND a.name=$2`, id, name)
	if err != nil {
		return nil, err
	}
	att, err := pgx.CollectExactlyOneRow(rows, scanAttachment)
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
func (s *Store) DeleteAttachment(ctx context.Context, id, name string, actor domain.Actor) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		k, err := s.Get(ctx, id)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx,
			`DELETE FROM attachment WHERE knowledge_id=$1 AND name=$2`, id, name)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return s.touchAndRevise(ctx, tx, k, "detach", actor)
	})
}

// ExportAttachment is one attachment with its owner and bytes, as the
// OKF exporter consumes it.
type ExportAttachment struct {
	ID   string
	Att  domain.Attachment
	Data []byte
}

// ListAllAttachments returns every live entry's attachments with bytes,
// ordered by id then name. Used by the OKF exporter.
func (s *Store) ListAllAttachments(ctx context.Context) ([]ExportAttachment, error) {
	rows, err := s.pool.Query(ctx, `SELECT a.knowledge_id, `+attachmentCols+`
		FROM attachment a
		JOIN blob b ON b.sha256 = a.sha256
		JOIN knowledge k ON k.id = a.knowledge_id AND k.deleted_at IS NULL
		ORDER BY a.knowledge_id, a.name`)
	if err != nil {
		return nil, err
	}
	atts, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ExportAttachment, error) {
		var e ExportAttachment
		err := row.Scan(&e.ID, &e.Att.Name, &e.Att.MediaType, &e.Att.Size, &e.Att.SHA256, &e.Att.OKFPath,
			&e.Att.CreatedBy.Kind, &e.Att.CreatedBy.Name, &e.Att.CreatedAt)
		return e, err
	})
	if err != nil {
		return nil, err
	}
	if len(atts) > 0 && s.blobs == nil {
		return nil, errNoBlobStore
	}
	for i := range atts {
		if atts[i].Data, err = s.blobs.Get(ctx, atts[i].Att.SHA256); err != nil {
			return nil, err
		}
	}
	return atts, nil
}

// touchAndRevise bumps the entry's updated_at and records a revision
// whose snapshot includes the attachment list after the change —
// attach/detach are changes to the entry, and every change is kept.
func (s *Store) touchAndRevise(ctx context.Context, tx pgx.Tx, k *domain.Knowledge, change string, actor domain.Actor) error {
	k.UpdatedAt = time.Now().UTC()
	if _, err := tx.Exec(ctx,
		`UPDATE knowledge SET updated_at=$2 WHERE id=$1`, k.ID, k.UpdatedAt); err != nil {
		return err
	}
	atts, err := listAttachmentsTx(ctx, tx, k.ID)
	if err != nil {
		return err
	}
	k.Attachments = atts
	return s.addRevision(ctx, tx, k, change, actor)
}

// listAttachmentsTx reads the attachment list inside the writing
// transaction, so the revision snapshot sees the change it records.
func listAttachmentsTx(ctx context.Context, tx pgx.Tx, id string) ([]domain.Attachment, error) {
	rows, err := tx.Query(ctx, `SELECT `+attachmentCols+`
		FROM attachment a JOIN blob b ON b.sha256 = a.sha256
		WHERE a.knowledge_id=$1 ORDER BY a.name`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanAttachment)
}
