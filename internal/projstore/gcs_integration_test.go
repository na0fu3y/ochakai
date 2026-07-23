package projstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// TestGCSIntegration exercises GCSObjectStore + the projection against real
// GCS. It is skipped unless OCHAKAI_GCS_TEST_PROJECT names a project the
// caller can create a bucket in (ADC auth). It creates a throwaway
// versioned bucket and deletes it (and every object) at the end.
//
//	OCHAKAI_GCS_TEST_PROJECT=ochakai-example go test ./internal/projstore -run Integration -v
func TestGCSIntegration(t *testing.T) {
	project := os.Getenv("OCHAKAI_GCS_TEST_PROJECT")
	if project == "" {
		t.Skip("set OCHAKAI_GCS_TEST_PROJECT to run the real-GCS integration test")
	}
	ctx := context.Background()
	client, err := storage.NewClient(ctx)
	if err != nil {
		t.Fatalf("storage client (ADC): %v", err)
	}
	defer client.Close()

	name := fmt.Sprintf("%s-projstoretest-%d", project, time.Now().UnixNano())
	bkt := client.Bucket(name)
	if err := bkt.Create(ctx, project, &storage.BucketAttrs{
		Location: "asia-northeast1", StorageClass: "STANDARD", VersioningEnabled: true,
	}); err != nil {
		t.Fatalf("bucket create: %v", err)
	}
	t.Logf("created throwaway bucket %s", name)
	defer func() {
		it := bkt.Objects(ctx, &storage.Query{Versions: true})
		for {
			a, err := it.Next()
			if errors.Is(err, iterator.Done) {
				break
			}
			if err != nil {
				t.Logf("cleanup list: %v", err)
				break
			}
			_ = bkt.Object(a.Name).Generation(a.Generation).Delete(ctx)
		}
		if err := bkt.Delete(ctx); err != nil {
			t.Logf("bucket delete (remove %s manually): %v", name, err)
		} else {
			t.Logf("deleted bucket %s", name)
		}
	}()

	os1 := &GCSObjectStore{bucket: bkt}
	a := New(os1)
	b := New(os1)

	// Create + read-your-writes.
	if err := a.Create(ctx, mkK("metrics/revenue", "売上", "四半期の売上を集計", domain.StatusVerified)); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := a.Create(ctx, mkK("metrics/revenue", "dup", "x", domain.StatusDraft)); err != ErrAlreadyExists {
		t.Errorf("create-only against real GCS: err = %v, want ErrAlreadyExists", err)
	}
	got, err := a.Get(ctx, "metrics/revenue")
	if err != nil || got.Title != "売上" {
		t.Fatalf("get: %v %+v", err, got)
	}

	// CAS: a stale generation loses. Read the object generation directly.
	obj, _ := os1.Get(ctx, "entries/metrics/revenue.json")
	if _, err := os1.PutCAS(ctx, "entries/metrics/revenue.json", []byte(`{}`), obj.Generation); err != nil {
		t.Fatalf("winning CAS: %v", err)
	}
	if _, err := os1.PutCAS(ctx, "entries/metrics/revenue.json", []byte(`{}`), obj.Generation); !errors.Is(err, ErrConflict) {
		t.Errorf("stale CAS against real GCS: err = %v, want ErrConflict", err)
	}

	// Instance B projects A's write via LIST refresh.
	if err := a.Create(ctx, mkK("models/churn", "churn", "ベクトル検索の対象", domain.StatusDraft)); err != nil {
		t.Fatalf("create churn: %v", err)
	}
	hits, err := b.SearchLexical(ctx, "ベクトル検索", Filter{}, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != "models/churn" {
		t.Errorf("B did not project A's write from real GCS: %+v", hits)
	}

	// Vector round-trip through vectors/<id>.f32.
	if err := a.SetVector(ctx, "models/churn", []float32{1, 0, 0}); err != nil {
		t.Fatalf("setvector: %v", err)
	}
	vhits, err := b.SearchVector(ctx, []float32{1, 0, 0}, Filter{}, 10)
	if err != nil {
		t.Fatalf("searchvector: %v", err)
	}
	if len(vhits) != 1 || vhits[0].ID != "models/churn" {
		t.Errorf("B did not project A's vector: %+v", vhits)
	}
}
