package projstore

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// ErrAlreadyExists is returned by Create when the id is already live.
var ErrAlreadyExists = errors.New("projstore: knowledge already exists")

// Store is the 0026 store: GCS objects are the source of truth, an in-RAM
// projection is the index. Writes go straight to objects with CAS; reads
// by id read objects (strong, read-your-writes); search reads the
// projection, which each instance refreshes from a LIST on demand.
type Store struct {
	os       ObjectStore
	prefix   string // entry object prefix, e.g. "entries/"
	vprefix  string // vector object prefix, e.g. "vectors/"
	interval time.Duration

	mu      sync.RWMutex
	entries map[string]*indexed // id -> projection
	vgens   map[string]int64    // id -> generation of its loaded vector
	last    time.Time
}

// New returns a Store over os. The projection starts empty; the first
// search (or an explicit Refresh) loads it.
func New(os ObjectStore) *Store {
	return &Store{
		os:      os,
		prefix:  "entries/",
		vprefix: "vectors/",
		entries: map[string]*indexed{},
		vgens:   map[string]int64{},
	}
}

// SetRefreshInterval throttles projection refresh: a search refreshes at
// most once per interval (0026 §5.2/§10). Zero (the default) refreshes on
// every search — convenient for tests and low-traffic instances.
func (s *Store) SetRefreshInterval(d time.Duration) { s.interval = d }

func (s *Store) entryName(id string) string  { return s.prefix + id + ".json" }
func (s *Store) vectorName(id string) string { return s.vprefix + id + ".f32" }

func (s *Store) idFromEntry(name string) (string, bool) {
	if !strings.HasPrefix(name, s.prefix) || !strings.HasSuffix(name, ".json") {
		return "", false
	}
	return strings.TrimSuffix(strings.TrimPrefix(name, s.prefix), ".json"), true
}

// --- writes (objects are the truth) ----------------------------------------

// Create writes a new entry create-only: a colliding id returns
// ErrAlreadyExists (0026 §6.2 rule 2). A previously soft-deleted id is
// free to reuse — its history survives as noncurrent object versions.
func (s *Store) Create(ctx context.Context, k *domain.Knowledge) error {
	now := time.Now().UTC()
	k.CreatedAt, k.UpdatedAt = now, now
	data, err := marshalEntry(k)
	if err != nil {
		return err
	}
	gen, err := s.os.PutCAS(ctx, s.entryName(k.ID), data, CreateOnly)
	if errors.Is(err, ErrExists) {
		return ErrAlreadyExists
	}
	if err != nil {
		return err
	}
	s.project(k, gen)
	return nil
}

// Update overwrites the live entry under compare-and-swap. When ifMatch
// is non-nil it must equal the stored updated_at (the #108 optimistic
// lock); independently the write is conditioned on the object's current
// generation, so a concurrent writer loses with ErrConflict (0026 §6.2
// rule 1). Missing entry → ErrNotFound.
func (s *Store) Update(ctx context.Context, k *domain.Knowledge, actor domain.Actor, ifMatch *time.Time) error {
	cur, err := s.os.Get(ctx, s.entryName(k.ID))
	if errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	var curK domain.Knowledge
	if err := json.Unmarshal(cur.Data, &curK); err != nil {
		return err
	}
	if ifMatch != nil && !curK.UpdatedAt.Equal(*ifMatch) {
		return ErrConflict
	}
	k.CreatedAt = curK.CreatedAt
	k.UpdatedAt = time.Now().UTC()
	data, err := marshalEntry(k)
	if err != nil {
		return err
	}
	gen, err := s.os.PutCAS(ctx, s.entryName(k.ID), data, cur.Generation)
	if err != nil {
		return err // ErrConflict on a lost CAS race
	}
	s.project(k, gen)
	return nil
}

// SoftDelete removes the entry from reads; bucket versioning keeps its
// history (0026 §6.2). Deleting an absent id is a no-op.
func (s *Store) SoftDelete(ctx context.Context, id string, actor domain.Actor) error {
	if err := s.os.Delete(ctx, s.entryName(id)); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.entries, id)
	s.mu.Unlock()
	return nil
}

// SetVector writes an entry's embedding to vectors/<id>.f32 (0026 §6.2
// rule 3 — embeddings are confirmed at write time, never rebuilt). The
// caller supplies the vector (the service owns the Vertex call).
func (s *Store) SetVector(ctx context.Context, id string, vec []float32) error {
	gen, err := s.os.PutCAS(ctx, s.vectorName(id), encodeF32(vec), Unconditional)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if e := s.entries[id]; e != nil {
		e.vec = append([]float32(nil), vec...)
	}
	s.vgens[id] = gen
	s.mu.Unlock()
	return nil
}

// --- reads -----------------------------------------------------------------

// Get reads the entry directly from its object: strongly consistent, so
// "save then open" always sees the latest (0026 §7).
func (s *Store) Get(ctx context.Context, id string) (*domain.Knowledge, error) {
	o, err := s.os.Get(ctx, s.entryName(id))
	if errors.Is(err, ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var k domain.Knowledge
	if err := json.Unmarshal(o.Data, &k); err != nil {
		return nil, err
	}
	return &k, nil
}

// ListAll returns every live entry from the projection (after a refresh).
func (s *Store) ListAll(ctx context.Context) ([]domain.Knowledge, error) {
	if err := s.Refresh(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.Knowledge, 0, len(s.entries))
	for _, e := range s.entries {
		out = append(out, e.k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// ListLinkingTo returns entries whose body-derived links (design doc 0024)
// point at id — the backlinks 0026 folds in-RAM from the full entry set.
func (s *Store) ListLinkingTo(ctx context.Context, id string, limit int) ([]domain.Knowledge, error) {
	if err := s.Refresh(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []domain.Knowledge
	for _, e := range s.entries {
		for _, l := range e.k.Links {
			if l.Target == id {
				out = append(out, e.k)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// SearchLexical reproduces store.SearchLexical in-process: similarity() +
// a 0.3 substring floor + a 0.05 verified boost, kept when > 0.05, ranked
// desc. The substring floor is a literal Contains, so the ILIKE wildcard
// bug (design doc 0026 §13-3) cannot occur here.
func (s *Store) SearchLexical(ctx context.Context, query string, f Filter, limit int) ([]domain.SearchHit, error) {
	if err := s.Refresh(ctx); err != nil {
		return nil, err
	}
	qtrig := trigramSet(query)
	qlike := strings.ToLower(query)
	s.mu.RLock()
	defer s.mu.RUnlock()
	var hits []domain.SearchHit
	for _, e := range s.entries {
		if !f.match(&e.k) {
			continue
		}
		score := similaritySets(e.simTrig, qtrig)
		if strings.Contains(e.likeText, qlike) {
			score += 0.3
		}
		if e.k.Status == domain.StatusVerified {
			score += 0.05
		}
		if score > 0.05 {
			hits = append(hits, domain.SearchHit{Knowledge: e.k, Score: score})
		}
	}
	return topHits(hits, limit), nil
}

// SearchVector ranks entries by cosine similarity against a flat scan of
// stored embeddings (0026 §4.1) — bit-for-bit the full scan migrate.go
// already deems sufficient, moved in-process.
func (s *Store) SearchVector(ctx context.Context, vec []float32, f Filter, limit int) ([]domain.SearchHit, error) {
	if err := s.Refresh(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var hits []domain.SearchHit
	for _, e := range s.entries {
		if e.vec == nil || !f.match(&e.k) {
			continue
		}
		hits = append(hits, domain.SearchHit{Knowledge: e.k, Score: cosine(vec, e.vec)})
	}
	return topHits(hits, limit), nil
}

// --- projection refresh (0026 §5.2) ----------------------------------------

// Refresh brings the in-RAM projection up to the current object set: a
// strongly-consistent LIST, diffed by generation, GETs only what changed,
// and drops what vanished. Throttled by the refresh interval. This is the
// whole "builder" — request-driven, in-process, no separate job.
func (s *Store) Refresh(ctx context.Context) error {
	s.mu.Lock()
	if s.interval > 0 && !s.last.IsZero() && time.Since(s.last) < s.interval {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	metas, err := s.os.List(ctx, s.prefix)
	if err != nil {
		return err
	}
	seen := make(map[string]bool, len(metas))
	for _, m := range metas {
		id, ok := s.idFromEntry(m.Name)
		if !ok {
			continue
		}
		seen[id] = true
		s.mu.RLock()
		cur, have := s.entries[id]
		s.mu.RUnlock()
		if have && cur.gen == m.Generation {
			continue // unchanged
		}
		o, err := s.os.Get(ctx, m.Name)
		if errors.Is(err, ErrNotFound) {
			continue // raced with a delete; the next LIST reconciles
		}
		if err != nil {
			return err
		}
		var k domain.Knowledge
		if err := json.Unmarshal(o.Data, &k); err != nil {
			return err
		}
		ind := buildIndexed(k, o.Generation)
		s.mu.Lock()
		if old := s.entries[id]; old != nil {
			ind.vec = old.vec // keep the loaded embedding across an entry edit
		}
		s.entries[id] = ind
		s.mu.Unlock()
	}

	// Drop entries whose objects are gone.
	s.mu.Lock()
	for id := range s.entries {
		if !seen[id] {
			delete(s.entries, id)
			delete(s.vgens, id)
		}
	}
	s.mu.Unlock()

	if err := s.refreshVectors(ctx); err != nil {
		return err
	}

	s.mu.Lock()
	s.last = time.Now()
	s.mu.Unlock()
	return nil
}

// refreshVectors pulls changed embeddings (vectors/<id>.f32) into the
// projection, so an instance sees vectors other instances wrote.
func (s *Store) refreshVectors(ctx context.Context) error {
	metas, err := s.os.List(ctx, s.vprefix)
	if err != nil {
		return err
	}
	for _, m := range metas {
		id := strings.TrimSuffix(strings.TrimPrefix(m.Name, s.vprefix), ".f32")
		s.mu.RLock()
		_, live := s.entries[id]
		known := s.vgens[id]
		s.mu.RUnlock()
		if !live || known == m.Generation {
			continue
		}
		o, err := s.os.Get(ctx, m.Name)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		vec := decodeF32(o.Data)
		s.mu.Lock()
		if e := s.entries[id]; e != nil {
			e.vec = vec
		}
		s.vgens[id] = o.Generation
		s.mu.Unlock()
	}
	return nil
}

// project applies a just-written entry to the local projection so the
// writing instance reflects its own write without waiting for a refresh.
func (s *Store) project(k *domain.Knowledge, gen int64) {
	ind := buildIndexed(*k, gen)
	s.mu.Lock()
	if old := s.entries[k.ID]; old != nil {
		ind.vec = old.vec
	}
	s.entries[k.ID] = ind
	s.mu.Unlock()
}

// --- helpers ---------------------------------------------------------------

// marshalEntry serializes k as its object body. Attachments are managed
// through their own objects, never stored in the entry (they are read-only
// metadata on the domain type), so they are cleared first.
func marshalEntry(k *domain.Knowledge) ([]byte, error) {
	c := *k
	c.Attachments = nil
	return json.Marshal(&c)
}

func topHits(hits []domain.SearchHit, limit int) []domain.SearchHit {
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].ID < hits[j].ID
	})
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}
