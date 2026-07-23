package objstore

import (
	"context"
	"sort"
	"sync"
)

// fakeObjectStore is an in-memory ObjectStore with GCS's generation
// semantics: every successful Put increments the object's generation, and
// conditional writes/deletes fail with ErrPreconditionFailed on a
// mismatch. It lets the tests exercise the whole store — including the
// optimistic lock and crash-recovery paths — without a real bucket, the
// same way internal/blob is faked (design doc 0013).
type fakeObjectStore struct {
	mu   sync.Mutex
	objs map[string]fakeObj
	// failPutAfter, when > 0, makes the Nth Put (and every Put after it)
	// return errInjected — used to simulate a crash mid-Move.
	failPutAfter int
	putCount     int
}

type fakeObj struct {
	data []byte
	gen  int64
}

type injectedError struct{}

func (injectedError) Error() string { return "objstore: injected crash" }

func newFake() *fakeObjectStore { return &fakeObjectStore{objs: map[string]fakeObj{}} }

func (f *fakeObjectStore) Put(_ context.Context, key string, data []byte, ifGen *int64) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.putCount++
	if f.failPutAfter > 0 && f.putCount >= f.failPutAfter {
		return 0, injectedError{}
	}
	cur, exists := f.objs[key]
	if ifGen != nil {
		if *ifGen == 0 && exists {
			return 0, ErrPreconditionFailed
		}
		if *ifGen != 0 && (!exists || cur.gen != *ifGen) {
			return 0, ErrPreconditionFailed
		}
	}
	gen := cur.gen + 1
	buf := make([]byte, len(data))
	copy(buf, data)
	f.objs[key] = fakeObj{data: buf, gen: gen}
	return gen, nil
}

func (f *fakeObjectStore) Get(_ context.Context, key string) ([]byte, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.objs[key]
	if !ok {
		return nil, 0, ErrObjectNotFound
	}
	buf := make([]byte, len(o.data))
	copy(buf, o.data)
	return buf, o.gen, nil
}

func (f *fakeObjectStore) List(_ context.Context, prefix string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var keys []string
	for k := range f.objs {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func (f *fakeObjectStore) Delete(_ context.Context, key string, ifGen *int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cur, exists := f.objs[key]
	if ifGen != nil && (!exists || cur.gen != *ifGen) {
		return ErrPreconditionFailed
	}
	delete(f.objs, key)
	return nil
}
