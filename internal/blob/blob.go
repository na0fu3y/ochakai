// Package blob stores attachment bytes outside PostgreSQL (design doc
// 0011). Content is addressed by SHA-256 and immutable: Put is
// create-only, and blobs are never deleted — like knowledge revisions,
// history is retained. The interface exists for the store and for test
// fakes, not to grow non-GCP backends (design doc 0003).
package blob

import "context"

// Store holds immutable, content-addressed blobs.
type Store interface {
	// Put stores data under its hex SHA-256. Storing the same sum twice
	// is a no-op — content-addressed names guarantee identical bytes.
	Put(ctx context.Context, sha256, mediaType string, data []byte) error
	// Get returns the bytes stored under the hex SHA-256.
	Get(ctx context.Context, sha256 string) ([]byte, error)
}
