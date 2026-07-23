// Package objstore is a verification prototype (design doc 0026): a
// knowledge store backed by an object store (Cloud Storage) with an
// in-memory index, holding no PostgreSQL. It exists to prove the
// load-bearing mechanisms of a Cloud-SQL-free ochakai are sound —
// object-generation optimistic locking, in-memory lexical search,
// in-memory link/attr queries, and crash-safe Move without a
// transaction — not to be a drop-in production replacement for
// internal/store. See design doc 0026 for the honest limits.
//
// The bet is ochakai's deliberate smallness (README: "trust density over
// volume"): a curated base that round-trips through git-friendly OKF
// bundles is single-digit megabytes, so the whole thing fits in memory
// and every query PostgreSQL served with an index runs as an in-process
// scan instead.
package objstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// ObjectStore is the narrow slice of Cloud Storage this store needs. The
// generation preconditions are GCS's native x-goog-if-generation-match:
// generation 0 means "must not exist" (create-only), a positive value
// means "must still be this exact version". This is a stronger primitive
// than the If-Match-on-updated_at optimistic lock the SQL store hand-rolls
// (design doc 0025 §11) — the object store enforces it, not the app.
type ObjectStore interface {
	// Put writes data at key. When ifGenerationMatch is non-nil the write
	// is conditional: 0 requires the object be absent, N requires its
	// current generation be N. A failed precondition returns
	// ErrPreconditionFailed. Returns the new generation on success.
	Put(ctx context.Context, key string, data []byte, ifGenerationMatch *int64) (int64, error)
	// Get returns the bytes and current generation at key, or
	// ErrObjectNotFound.
	Get(ctx context.Context, key string) ([]byte, int64, error)
	// List returns every key under prefix.
	List(ctx context.Context, prefix string) ([]string, error)
	// Delete removes key, conditional on generation when non-nil.
	Delete(ctx context.Context, key string, ifGenerationMatch *int64) error
}

var (
	ErrPreconditionFailed = errors.New("objstore: generation precondition failed")
	ErrObjectNotFound     = errors.New("objstore: object not found")
	// ErrNotFound / ErrAlreadyExists / ErrConflict mirror internal/store so
	// the service layer's error handling is unchanged after a swap.
	ErrNotFound      = errors.New("knowledge not found")
	ErrAlreadyExists = errors.New("knowledge already exists")
	ErrConflict      = errors.New("knowledge changed since it was read")
)

// entryPrefix namespaces knowledge objects. In production these would be
// OKF documents (markdown + YAML frontmatter, design doc 0005) so the
// bucket is a browsable OKF bundle; the prototype stores the domain
// envelope as JSON, which round-trips the same fields.
const entryPrefix = "entries/"

func entryKey(id string) string { return entryPrefix + id + ".json" }

// record is one entry plus the object generation it was loaded at — the
// token the next conditional write must match.
type record struct {
	k   domain.Knowledge
	gen int64
}

// Index is the in-memory view rebuilt from the object store on startup.
// All reads and searches run against it; writes go through to the object
// store first (durability + the generation lock) and then update the map.
//
// One writer at a time: a single Cloud Run instance (max-instances=1)
// serializes writes with mu. That single-instance constraint is the
// central concession of the GCS-only design — see design doc 0026 §5.
type Index struct {
	os    ObjectStore
	mu    sync.RWMutex
	byID  map[string]*record
	usage map[string]*domain.Usage
}

// Open builds an Index by scanning every entry object once. This is the
// cold-start cost that replaces a warm database connection: O(N) GETs on
// boot. At ochakai's scale (hundreds–thousands of KB-sized entries) it is
// seconds; design doc 0026 §5 weighs it against min-instances economics.
func Open(ctx context.Context, os ObjectStore) (*Index, error) {
	ix := &Index{os: os, byID: map[string]*record{}, usage: map[string]*domain.Usage{}}
	keys, err := os.List(ctx, entryPrefix)
	if err != nil {
		return nil, err
	}
	for _, key := range keys {
		data, gen, err := os.Get(ctx, key)
		if err != nil {
			return nil, err
		}
		var k domain.Knowledge
		if err := json.Unmarshal(data, &k); err != nil {
			return nil, fmt.Errorf("objstore: decode %s: %w", key, err)
		}
		// Links are derived from the body on load, never trusted from the
		// stored field (design doc 0024). This is what makes Move
		// crash-safe without a transaction: the in-memory link graph
		// always equals what the bodies say, so a half-finished rewrite is
		// a stale-but-visible edge, never index corruption (design doc
		// 0026 §4).
		k.Links = domain.LinksFromBody(k.ID, k.Body)
		ix.byID[k.ID] = &record{k: k, gen: gen}
	}
	return ix, nil
}

func nowStored() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

// Get returns a copy of the live entry, or ErrNotFound.
func (ix *Index) Get(_ context.Context, id string) (*domain.Knowledge, error) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	r, ok := ix.byID[id]
	if !ok {
		return nil, ErrNotFound
	}
	k := r.k
	return &k, nil
}

// Create inserts a new entry with a create-only object write (generation
// 0). A live id already present is ErrAlreadyExists.
func (ix *Index) Create(ctx context.Context, k *domain.Knowledge) error {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if _, ok := ix.byID[k.ID]; ok {
		return ErrAlreadyExists
	}
	now := nowStored()
	k.CreatedAt, k.UpdatedAt = now, now
	k.Links = domain.LinksFromBody(k.ID, k.Body)
	gen, err := ix.write(ctx, k, int64Ptr(0))
	if err != nil {
		if errors.Is(err, ErrPreconditionFailed) {
			return ErrAlreadyExists // another writer won the race
		}
		return err
	}
	cp := *k
	ix.byID[k.ID] = &record{k: cp, gen: gen}
	return nil
}

// Update writes k over the live entry. When ifMatch is non-nil the write
// is conditional on the entry's updated_at (design doc 0025 §11), enforced
// here through the object generation the timestamp was loaded at: a
// mismatch is ErrConflict. This is the SQL store's optimistic lock, moved
// onto the storage layer's own precondition.
func (ix *Index) Update(ctx context.Context, k *domain.Knowledge, ifMatch *time.Time) error {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	r, ok := ix.byID[k.ID]
	if !ok {
		return ErrNotFound
	}
	if ifMatch != nil && !r.k.UpdatedAt.Equal(ifMatch.UTC()) {
		return ErrConflict
	}
	k.CreatedAt = r.k.CreatedAt
	k.UpdatedAt = nowStored()
	k.Links = domain.LinksFromBody(k.ID, k.Body)
	gen, err := ix.write(ctx, k, &r.gen)
	if err != nil {
		if errors.Is(err, ErrPreconditionFailed) {
			return ErrConflict // the object moved under us
		}
		return err
	}
	cp := *k
	ix.byID[k.ID] = &record{k: cp, gen: gen}
	return nil
}

// write serializes k and puts it, conditional on gen. Callers hold mu.
func (ix *Index) write(ctx context.Context, k *domain.Knowledge, gen *int64) (int64, error) {
	data, err := json.Marshal(k)
	if err != nil {
		return 0, err
	}
	return ix.os.Put(ctx, entryKey(k.ID), data, gen)
}

func int64Ptr(v int64) *int64 { return &v }
