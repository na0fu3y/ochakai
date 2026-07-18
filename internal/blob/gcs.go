package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"
)

// GCS stores blobs as objects named blob/<sha256> in one bucket.
// Authentication is ADC — the same pattern as Vertex embeddings, no keys
// to issue or rotate.
type GCS struct {
	bucket *storage.BucketHandle
}

// NewGCS opens the bucket via Application Default Credentials. It does
// not probe the bucket: Cloud Run starts must not depend on a GCS round
// trip, and a misconfigured bucket surfaces on first use.
func NewGCS(ctx context.Context, bucket string) (*GCS, error) {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("gcs client: %w", err)
	}
	return &GCS{bucket: client.Bucket(bucket)}, nil
}

func objectName(sha256 string) string { return "blob/" + sha256 }

// Put uploads create-only (DoesNotExist precondition): an object that
// already exists has identical content by construction, so the 412 the
// precondition produces is success, and existing objects are never
// rewritten.
func (g *GCS) Put(ctx context.Context, sha256, mediaType string, data []byte) error {
	w := g.bucket.Object(objectName(sha256)).If(storage.Conditions{DoesNotExist: true}).NewWriter(ctx)
	w.ContentType = mediaType
	if _, err := w.Write(data); err != nil {
		_ = w.Close()
		if alreadyExists(err) {
			return nil
		}
		return fmt.Errorf("gcs put %s: %w", sha256, err)
	}
	if err := w.Close(); err != nil {
		if alreadyExists(err) {
			return nil
		}
		return fmt.Errorf("gcs put %s: %w", sha256, err)
	}
	return nil
}

func (g *GCS) Get(ctx context.Context, sha256 string) ([]byte, error) {
	r, err := g.bucket.Object(objectName(sha256)).NewReader(ctx)
	if err != nil {
		return nil, fmt.Errorf("gcs get %s: %w", sha256, err)
	}
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("gcs get %s: %w", sha256, err)
	}
	return data, nil
}

func alreadyExists(err error) bool {
	var apiErr *googleapi.Error
	return errors.As(err, &apiErr) && apiErr.Code == http.StatusPreconditionFailed
}
