package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/na0fu3y/ochakai/internal/blob"
)

// PostgresBlobs keeps file bytes in the database, for a deployment that
// names no bucket (decision 0156). Same contract as blob.GCS: addressed
// by SHA-256, create-only, deleted only by the sweep once nothing
// references the hash.
func (s *Store) PostgresBlobs() blob.Store { return pgBlobs{s} }

type pgBlobs struct{ s *Store }

func (b pgBlobs) Put(ctx context.Context, sha256, _ string, data []byte) error {
	_, err := b.s.pool.Exec(ctx,
		`INSERT INTO blob_bytes (sha256, bytes) VALUES ($1, $2) ON CONFLICT (sha256) DO NOTHING`, sha256, data)
	return err
}

func (b pgBlobs) Get(ctx context.Context, sha256 string) ([]byte, error) {
	var data []byte
	err := b.s.pool.QueryRow(ctx, `SELECT bytes FROM blob_bytes WHERE sha256 = $1`, sha256).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("postgres get %s: %w", sha256, blob.ErrNotFound)
	}
	return data, err
}

func (b pgBlobs) Delete(ctx context.Context, sha256 string) error {
	_, err := b.s.pool.Exec(ctx, `DELETE FROM blob_bytes WHERE sha256 = $1`, sha256)
	return err
}
