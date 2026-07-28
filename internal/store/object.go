package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// File persistence (design doc 0046 §§3.1-3.3). A file is a row in
// `object` addressed by its bundle path; the bytes behind it are a
// content-addressed blob in GCS (design doc 0013's decision, kept).
//
// It replaces the attachment table, where a file was keyed by (entry,
// name) and carried an okf_path to remember where it had really come
// from — two addresses for one file, the authoritative one used only by
// export — and where a file belonging to no entry had no representation
// at all. Now the path is the address, and belonging is derived
// (domain.FilesOf).
//
// Writing a file touches no entry. It is an object in its own right, and
// putting one beside an entry does not change what the entry says: its
// version, its generated stamp and its revisions all stay where they are
// (design doc 0046 §3.4). The attach/detach revisions are gone with the
// relationship they recorded.
//
// Blobs are never deleted, as under 0008: content addressing means a
// hash names historical bytes for as long as anything remembers it.

const objectCols = `o.path, b.media_type, b.size, o.sha256,
	o.created_by_kind, o.created_by_name, o.created_at`

func scanFile(row pgx.CollectableRow) (domain.File, error) {
	var f domain.File
	err := row.Scan(&f.Path, &f.MediaType, &f.Size, &f.SHA256,
		&f.CreatedBy.Kind, &f.CreatedBy.Name, &f.CreatedAt)
	return f, err
}

// errNoBlobStore is the backstop for file operations on an instance
// without a blob store; the service layer checks first and wraps the
// condition in a client-facing error (design doc 0013).
var errNoBlobStore = errors.New("files are not supported without GCS: set OCHAKAI_GCS_BUCKET (design doc 0013)")

// PutFile stores data at path, replacing whatever was there. mediaType is
// what the bytes sniffed as (domain.DetectMediaType) — never what a
// client claimed.
func (s *Store) PutFile(ctx context.Context, path, mediaType string, data []byte, actor domain.Actor) (*domain.File, error) {
	if s.blobs == nil {
		return nil, errNoBlobStore
	}
	sum := sha256.Sum256(data)
	f := &domain.File{
		Path:      path,
		MediaType: mediaType,
		Size:      int64(len(data)),
		SHA256:    hex.EncodeToString(sum[:]),
		CreatedBy: actor,
		CreatedAt: NowStored(),
	}
	// The upload happens before the transaction: create-only and
	// content-addressed, so a failure afterwards leaves an unreferenced
	// object that the next identical write reuses.
	if err := s.blobs.Put(ctx, f.SHA256, mediaType, data); err != nil {
		return nil, err
	}
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		// One address space: a path that a concept already occupies is
		// not free for a file (design doc 0046 §2.1). The check is inside
		// the transaction because the alternative is a window in which
		// both exist.
		if err := refuseIfConcept(ctx, tx, path); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO blob (sha256, media_type, size)
			VALUES ($1, $2, $3) ON CONFLICT (sha256) DO NOTHING`,
			f.SHA256, f.MediaType, f.Size); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO object
			(path, sha256, created_by_kind, created_by_name, created_by_via, created_at, deleted_at)
			VALUES ($1,$2,$3,$4,$5,$6,NULL)
			ON CONFLICT (path) DO UPDATE SET
				sha256=EXCLUDED.sha256,
				created_by_kind=EXCLUDED.created_by_kind,
				created_by_name=EXCLUDED.created_by_name,
				created_by_via=EXCLUDED.created_by_via,
				created_at=EXCLUDED.created_at,
				deleted_at=NULL`,
			f.Path, f.SHA256, actor.Kind, actor.Name, actor.Via, f.CreatedAt); err != nil {
			return err
		}
		// A replaced file's vector describes the old bytes; drop it here
		// and let the service re-embed after commit (design doc 0020).
		return execTolerateMissingTable(ctx, tx, `DELETE FROM file_embedding WHERE path=$1`, f.Path)
	})
	if err != nil {
		return nil, err
	}
	return f, nil
}

// ErrPathTaken is a write to an address the other kind of object holds.
var ErrPathTaken = errors.New("a concept holds that path")

// refuseIfConcept fails a file write whose path a live concept occupies.
// Concepts live at "<id>.md", so this can only bite a markdown file —
// which is exactly the case worth catching, since a .md is a concept or
// a file depending on what is in it.
func refuseIfConcept(ctx context.Context, tx pgx.Tx, path string) error {
	id, isMarkdown := trimMarkdown(path)
	if !isMarkdown {
		return nil
	}
	var exists bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM knowledge WHERE id=$1 AND deleted_at IS NULL)`, id).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return ErrPathTaken
	}
	return nil
}

// ConceptPathFree reports whether a concept may take an id — false when a
// file already sits at "<id>.md". The write path calls it for the same
// reason refuseIfConcept exists, from the other side.
func (s *Store) ConceptPathFree(ctx context.Context, id string) (bool, error) {
	var taken bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM object WHERE path=$1 AND deleted_at IS NULL)`, id+".md").Scan(&taken)
	return !taken, err
}

func trimMarkdown(path string) (string, bool) {
	if len(path) > 3 && path[len(path)-3:] == ".md" {
		return path[:len(path)-3], true
	}
	return path, false
}

// GetFile returns one file's metadata and its bytes.
func (s *Store) GetFile(ctx context.Context, path string) (*domain.File, []byte, error) {
	f, err := s.GetFileMeta(ctx, path)
	if err != nil {
		return nil, nil, err
	}
	if s.blobs == nil {
		return nil, nil, errNoBlobStore
	}
	data, err := s.blobs.Get(ctx, f.SHA256)
	if err != nil {
		return nil, nil, err
	}
	return f, data, nil
}

// GetFileMeta returns one file's metadata without touching the blob
// store — enough to answer a conditional GET (the ETag is the content
// hash) before deciding whether the bytes are needed.
func (s *Store) GetFileMeta(ctx context.Context, path string) (*domain.File, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+objectCols+`
		FROM object o JOIN blob b ON b.sha256 = o.sha256
		WHERE o.path=$1 AND o.deleted_at IS NULL`, path)
	if err != nil {
		return nil, err
	}
	f, err := pgx.CollectExactlyOneRow(rows, scanFile)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// ListFiles returns the live files under a path prefix, in path order.
// An empty prefix is the whole bundle, which is what an export walks.
func (s *Store) ListFiles(ctx context.Context, prefix string) ([]domain.File, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+objectCols+`
		FROM object o JOIN blob b ON b.sha256 = o.sha256
		WHERE o.deleted_at IS NULL AND ($1 = '' OR o.path = $1 OR o.path LIKE $2)
		ORDER BY o.path`, prefix, prefix+"/%")
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanFile)
}

// DeleteFile removes a file from the bundle. The blob stays: bytes are
// content-addressed, and an export somebody took or another path holding
// the same content may still name the hash.
func (s *Store) DeleteFile(ctx context.Context, path string, actor domain.Actor) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`UPDATE object SET deleted_at=$2, created_by_kind=$3, created_by_name=$4, created_by_via=$5
			 WHERE path=$1 AND deleted_at IS NULL`,
			path, time.Now().UTC(), actor.Kind, actor.Name, actor.Via)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return execTolerateMissingTable(ctx, tx, `DELETE FROM file_embedding WHERE path=$1`, path)
	})
}

// FileBytes fetches blob content by hash, for callers that already hold
// the metadata (an export streaming its own listing).
func (s *Store) FileBytes(ctx context.Context, sha256 string) ([]byte, error) {
	if s.blobs == nil {
		return nil, errNoBlobStore
	}
	return s.blobs.Get(ctx, sha256)
}

// ownedBy is the SQL half of domain.FilesOf: a file belongs to the entry
// whose directory it sits in, or to any entry whose body links to it.
// alias names the knowledge table, pathExpr the file's path.
//
// The link half reads the derived file_links column rather than the
// body, for the same reason backlinks read links: the reading of markdown
// happens once, on write.
func ownedBy(alias, pathExpr string) string {
	return "(regexp_replace(" + pathExpr + ", '/[^/]*$', '') = " + alias + ".id OR " +
		alias + ".file_links @> ARRAY[" + pathExpr + "])"
}

// EntriesOwning returns the live entries a file belongs to.
func (s *Store) EntriesOwning(ctx context.Context, path string) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT k.id FROM knowledge k WHERE k.deleted_at IS NULL AND `+ownedBy("k", "$1")+` ORDER BY k.id`, path)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (string, error) {
		var id string
		return id, r.Scan(&id)
	})
}

// FilesForEntries returns each named entry's files in one query, keyed by
// entry id — the batch form of the same derivation, for the listing
// surfaces that carry file metadata.
func (s *Store) FilesForEntries(ctx context.Context, ids []string) (map[string][]domain.File, error) {
	rows, err := s.pool.Query(ctx, `SELECT k.id, `+objectCols+`
		FROM object o
		JOIN blob b ON b.sha256 = o.sha256
		JOIN knowledge k ON k.deleted_at IS NULL AND `+ownedBy("k", "o.path")+`
		JOIN unnest($1::text[]) AS want(id) ON k.id = want.id
		WHERE o.deleted_at IS NULL
		ORDER BY k.id, o.path`, ids)
	if err != nil {
		return nil, err
	}
	out := map[string][]domain.File{}
	for rows.Next() {
		var id string
		var f domain.File
		if err := rows.Scan(&id, &f.Path, &f.MediaType, &f.Size, &f.SHA256,
			&f.CreatedBy.Kind, &f.CreatedBy.Name, &f.CreatedAt); err != nil {
			return nil, err
		}
		out[id] = append(out[id], f)
	}
	return out, rows.Err()
}

// UpsertFileEmbedding stores the document embedding for one file
// (design doc 0020).
func (s *Store) UpsertFileEmbedding(ctx context.Context, path, model string, vec []float32) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO file_embedding (path, model, embedding, updated_at)
		VALUES ($1, $2, $3::vector, now())
		ON CONFLICT (path) DO UPDATE SET model = $2, embedding = $3::vector, updated_at = now()`,
		path, model, encodeVector(vec))
	return err
}

// UnembeddedFile is one file still missing a vector for the configured
// model (design doc 0020).
type UnembeddedFile struct {
	Path      string
	MediaType string
	SHA256    string
}

// ListUnembeddedFiles returns files with no vector for model, in path
// order after the cursor.
func (s *Store) ListUnembeddedFiles(ctx context.Context, model, after string, limit int) ([]UnembeddedFile, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT o.path, b.media_type, o.sha256
		FROM object o
		JOIN blob b ON b.sha256 = o.sha256
		LEFT JOIN file_embedding e ON e.path = o.path AND e.model = $1
		WHERE o.deleted_at IS NULL AND e.path IS NULL AND o.path > $2
		ORDER BY o.path LIMIT $3`, model, after, limit)
	if err != nil {
		if isUndefinedTable(err) {
			return nil, nil
		}
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (UnembeddedFile, error) {
		var u UnembeddedFile
		err := r.Scan(&u.Path, &u.MediaType, &u.SHA256)
		return u, err
	})
}
