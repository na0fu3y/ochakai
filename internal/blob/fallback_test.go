package blob

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

type mem map[string][]byte

func (m mem) Put(_ context.Context, sha, _ string, data []byte) error { m[sha] = data; return nil }
func (m mem) Get(_ context.Context, sha string) ([]byte, error) {
	if d, ok := m[sha]; ok {
		return d, nil
	}
	return nil, fmt.Errorf("mem get %s: %w", sha, ErrNotFound)
}
func (m mem) Delete(_ context.Context, sha string) error { delete(m, sha); return nil }

// A deployment that names a bucket after keeping files in PostgreSQL
// writes new bytes to the bucket and still reads the old ones (decision
// 0156).
func TestFallbackReadsWhatThePrimaryDoesNotHold(t *testing.T) {
	ctx := context.Background()
	gcs, pg := mem{}, mem{"old": []byte("kept in postgres")}
	f := Fallback(gcs, pg)
	if err := f.Put(ctx, "new", "text/plain", []byte("to the bucket")); err != nil {
		t.Fatal(err)
	}
	if _, ok := pg["new"]; ok {
		t.Error("a new blob was written to the secondary")
	}
	for sha, want := range map[string]string{"new": "to the bucket", "old": "kept in postgres"} {
		got, err := f.Get(ctx, sha)
		if err != nil || string(got) != want {
			t.Errorf("Get(%s) = %q, %v", sha, got, err)
		}
	}
	if _, err := f.Get(ctx, "gone"); !errors.Is(err, ErrNotFound) {
		t.Errorf("a blob neither holds: %v", err)
	}
	if err := f.Delete(ctx, "old"); err != nil || len(pg) != 0 {
		t.Errorf("delete did not reach the secondary: %v %v", err, pg)
	}
}
