package projstore

import (
	"context"
	"sort"
	"strings"
	"sync"
)

// MemObjectStore is an in-memory ObjectStore that mirrors the GCS
// semantics 0026 relies on: a monotonic per-write generation, CAS via
// ifGeneration, strongly-consistent Get/List. It backs the package tests.
type MemObjectStore struct {
	mu      sync.Mutex
	nextGen int64
	objs    map[string]memObj
}

type memObj struct {
	data []byte
	gen  int64
}

func NewMemObjectStore() *MemObjectStore {
	return &MemObjectStore{nextGen: 1, objs: map[string]memObj{}}
}

func (m *MemObjectStore) Get(_ context.Context, name string) (Object, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.objs[name]
	if !ok {
		return Object{}, ErrNotFound
	}
	return Object{Data: append([]byte(nil), o.data...), Generation: o.gen}, nil
}

func (m *MemObjectStore) PutCAS(_ context.Context, name string, data []byte, ifGeneration int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, exists := m.objs[name]
	switch {
	case ifGeneration == CreateOnly:
		if exists {
			return 0, ErrExists
		}
	case ifGeneration == Unconditional:
		// always allowed
	default: // positive: must match current generation
		if !exists || cur.gen != ifGeneration {
			return 0, ErrConflict
		}
	}
	gen := m.nextGen
	m.nextGen++
	m.objs[name] = memObj{data: append([]byte(nil), data...), gen: gen}
	return gen, nil
}

func (m *MemObjectStore) Delete(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objs, name)
	return nil
}

func (m *MemObjectStore) List(_ context.Context, prefix string) ([]ObjectMeta, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []ObjectMeta
	for name, o := range m.objs {
		if strings.HasPrefix(name, prefix) {
			out = append(out, ObjectMeta{Name: name, Generation: o.gen})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
