// Package blob stores file bytes: on GCS when the deployment names a
// bucket (design doc 0011), in PostgreSQL when it does not (decision
// 0156). Content is addressed by SHA-256 and immutable: Put is
// create-only, and a blob leaves only through the store's sweep, once
// nothing references its hash any more — a purge's promise that the
// record is gone (design doc 0031, condition C1) reaches the bytes this
// way. The interface is for those two and for test fakes, not to grow
// other backends (design doc 0003).
package blob

import (
	"context"
	"errors"
)

// ErrNotFound is what Get wraps when no bytes are stored under the hash.
var ErrNotFound = errors.New("blob not found")

// Fallback reads from primary and, for bytes primary does not hold, from
// secondary; it writes to primary only and deletes from both. It is how a
// deployment that names a bucket after keeping files in PostgreSQL goes
// on reading what it kept there (decision 0156).
func Fallback(primary, secondary Store) Store { return fallback{primary, secondary} }

type fallback struct{ primary, secondary Store }

func (f fallback) Put(ctx context.Context, sha256, mediaType string, data []byte) error {
	return f.primary.Put(ctx, sha256, mediaType, data)
}

func (f fallback) Get(ctx context.Context, sha256 string) ([]byte, error) {
	data, err := f.primary.Get(ctx, sha256)
	if errors.Is(err, ErrNotFound) {
		return f.secondary.Get(ctx, sha256)
	}
	return data, err
}

func (f fallback) Delete(ctx context.Context, sha256 string) error {
	if err := f.primary.Delete(ctx, sha256); err != nil {
		return err
	}
	return f.secondary.Delete(ctx, sha256)
}

// Store holds immutable, content-addressed blobs.
type Store interface {
	// Put stores data under its hex SHA-256. Storing the same sum twice
	// is a no-op — content-addressed names guarantee identical bytes.
	Put(ctx context.Context, sha256, mediaType string, data []byte) error
	// Get returns the bytes stored under the hex SHA-256.
	Get(ctx context.Context, sha256 string) ([]byte, error)
	// Delete removes the bytes stored under the hex SHA-256. Deleting a
	// blob that is already gone is success, not an error: the sweep that
	// calls this retries until the bytes are gone, and must be able to
	// find its work already done.
	Delete(ctx context.Context, sha256 string) error
}
