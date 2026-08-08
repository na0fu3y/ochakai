// Package blob stores file bytes outside PostgreSQL (design doc
// 0011). Content is addressed by SHA-256 and immutable: Put is
// create-only, and a blob leaves only through the store's sweep, once
// nothing references its hash any more — a purge's promise that the
// record is gone (design doc 0031, condition C1) reaches the bytes this
// way. The interface exists for the store and for test fakes, not to
// grow non-GCP backends (design doc 0003).
package blob

import "context"

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
