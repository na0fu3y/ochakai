// Package projstore implements the core of design doc 0026: Cloud Storage
// 一本化 (builder を持たない要求駆動). GCS objects are the source of truth
// (entries/<id>.json, versioned, written with compare-and-swap); each
// instance projects them into an in-RAM index — a pg_trgm-faithful lexical
// scorer, a flat float32 vector scan, and a backlink map — refreshed on
// demand from a strongly-consistent LIST.
//
// This package is the 0026 engine, unit-tested against an in-memory object
// store (mem.go) and runnable against real GCS (gcs.go, whose semantics
// cmd/gcsverify confirmed). It does not yet replace internal/store; the
// service-layer cutover is a follow-on change.
package projstore

import (
	"context"
	"errors"
)

var (
	// ErrNotFound is returned by Get when no live object exists.
	ErrNotFound = errors.New("projstore: entry not found")
	// ErrConflict is a CAS miss: the object's generation moved under a
	// positive ifGeneration precondition (0026 §6.2 rule 1). The caller
	// re-reads and retries.
	ErrConflict = errors.New("projstore: entry changed since it was read")
	// ErrExists is a create-only miss: the object already existed under an
	// ifGeneration==0 precondition (0026 §6.2 rule 2).
	ErrExists = errors.New("projstore: entry already exists")
)

// Unconditional and CreateOnly are ifGeneration sentinels for PutCAS.
const (
	Unconditional int64 = -1 // overwrite regardless of current generation
	CreateOnly    int64 = 0  // must not already exist (DoesNotExist)
	// A positive value is a GenerationMatch precondition.
)

// Object is a stored object plus the generation identifying its version.
type Object struct {
	Data       []byte
	Generation int64
}

// ObjectMeta is what a LIST returns without a GET: a name and its current
// generation. Diff detection reads generations only (0026 §5.2).
type ObjectMeta struct {
	Name       string
	Generation int64
}

// ObjectStore is the narrow GCS surface 0026 depends on. Both the GCS
// implementation and the in-memory test double satisfy it.
type ObjectStore interface {
	// Get returns the current object and its generation, or ErrNotFound.
	Get(ctx context.Context, name string) (Object, error)
	// PutCAS writes data under an ifGeneration precondition: Unconditional
	// overwrites; CreateOnly requires absence (miss → ErrExists); a positive
	// value requires that generation (miss → ErrConflict). It returns the
	// new generation.
	PutCAS(ctx context.Context, name string, data []byte, ifGeneration int64) (int64, error)
	// Delete removes the current version; bucket versioning retains history
	// (0026 §6.2). Deleting an absent object is not an error.
	Delete(ctx context.Context, name string) error
	// List returns every object under prefix with its generation in one
	// strongly-consistent pass (0026 §6.1).
	List(ctx context.Context, prefix string) ([]ObjectMeta, error)
}
