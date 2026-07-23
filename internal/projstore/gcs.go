package projstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/iterator"
)

// GCSObjectStore is the production ObjectStore: one GCS bucket, ADC auth
// (same as internal/blob/gcs.go). Its CAS/create-only/list-generation
// semantics were confirmed against real GCS by cmd/gcsverify.
type GCSObjectStore struct {
	bucket *storage.BucketHandle
}

// NewGCSObjectStore opens bucket via Application Default Credentials. It
// does not probe the bucket — a Cloud Run start must not depend on a GCS
// round trip (the 0003/0026 posture).
func NewGCSObjectStore(ctx context.Context, bucket string) (*GCSObjectStore, error) {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("gcs client: %w", err)
	}
	return &GCSObjectStore{bucket: client.Bucket(bucket)}, nil
}

func (g *GCSObjectStore) Get(ctx context.Context, name string) (Object, error) {
	r, err := g.bucket.Object(name).NewReader(ctx)
	if errors.Is(err, storage.ErrObjectNotExist) {
		return Object{}, ErrNotFound
	}
	if err != nil {
		return Object{}, fmt.Errorf("gcs get %s: %w", name, err)
	}
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		return Object{}, fmt.Errorf("gcs read %s: %w", name, err)
	}
	return Object{Data: data, Generation: r.Attrs.Generation}, nil
}

func (g *GCSObjectStore) PutCAS(ctx context.Context, name string, data []byte, ifGeneration int64) (int64, error) {
	obj := g.bucket.Object(name)
	switch {
	case ifGeneration == CreateOnly:
		obj = obj.If(storage.Conditions{DoesNotExist: true})
	case ifGeneration == Unconditional:
		// no precondition
	default:
		obj = obj.If(storage.Conditions{GenerationMatch: ifGeneration})
	}
	w := obj.NewWriter(ctx)
	w.ContentType = "application/json"
	if _, err := w.Write(data); err != nil {
		_ = w.Close()
		return 0, mapPrecondition(err, ifGeneration, name)
	}
	if err := w.Close(); err != nil {
		return 0, mapPrecondition(err, ifGeneration, name)
	}
	return w.Attrs().Generation, nil
}

func (g *GCSObjectStore) Delete(ctx context.Context, name string) error {
	err := g.bucket.Object(name).Delete(ctx)
	if err == nil || errors.Is(err, storage.ErrObjectNotExist) {
		return nil
	}
	return fmt.Errorf("gcs delete %s: %w", name, err)
}

func (g *GCSObjectStore) List(ctx context.Context, prefix string) ([]ObjectMeta, error) {
	q := &storage.Query{Prefix: prefix}
	// Diff detection needs only name+generation (0026 §5.2).
	_ = q.SetAttrSelection([]string{"Name", "Generation"})
	it := g.bucket.Objects(ctx, q)
	var out []ObjectMeta
	for {
		a, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("gcs list %s: %w", prefix, err)
		}
		out = append(out, ObjectMeta{Name: a.Name, Generation: a.Generation})
	}
	return out, nil
}

// mapPrecondition turns a 412 into ErrExists for a create-only write or
// ErrConflict for a generation-match write, matching the 0026 model.
func mapPrecondition(err error, ifGeneration int64, name string) error {
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) && apiErr.Code == http.StatusPreconditionFailed {
		if ifGeneration == CreateOnly {
			return ErrExists
		}
		return ErrConflict
	}
	return fmt.Errorf("gcs put %s: %w", name, err)
}
