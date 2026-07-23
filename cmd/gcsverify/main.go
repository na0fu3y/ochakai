// Command gcsverify exercises the real-GCS assumptions in design doc 0026
// (§16 未解決/リスク) against a live bucket. Throwaway verification harness,
// not shipped code. Auth is ADC (same as internal/blob/gcs.go).
//
// It measures/confirms:
//   - ifGenerationMatch CAS: the losing writer gets 412 (§6.2 rule 1)
//   - create-only (DoesNotExist): 412 on an existing object (§6.2 rule 2)
//   - read-after-write strong consistency (§6.1)
//   - objects.list returns Generation without a GET (§5.2 diff detection)
//   - object versioning retains prior generations = history (§6.2)
//   - LIST paging: pages == ceil(n/1000), Class-A op count, latency (§10)
//   - projection load: LIST + N concurrent GET wall time + throughput (§5.1)
//
// Cost is dominated by the one-time seed (N Class-A PUTs). The seed is
// idempotent (skips objects that already exist), so re-running the
// measurement phases is cheap. Cleanup deletes every object+version and
// the bucket (deletes are free).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/iterator"
)

var (
	flagN           = flag.Int("n", 20000, "number of entry objects to seed/measure")
	flagProject     = flag.String("project", "ochakai-example", "GCP project for bucket creation")
	flagLocation    = flag.String("location", "asia-northeast1", "bucket location")
	flagBucket      = flag.String("bucket", "", "use an existing bucket with a prefix instead of creating one (skips the versioning test)")
	flagConcurrency = flag.Int("c", 64, "concurrent GET/PUT workers")
	flagKeep        = flag.Bool("keep", false, "do not delete bucket/objects at the end")
	flagPrefix      = flag.String("prefix", "entries/", "object prefix")
	flagCleanup     = flag.String("cleanup-bucket", "", "delete all versions + this bucket, then exit (recovery)")
)

func main() {
	flag.Parse()
	log.SetFlags(0)
	ctx := context.Background()

	client, err := storage.NewClient(ctx)
	if err != nil {
		log.Fatalf("storage.NewClient (ADC): %v", err)
	}
	defer client.Close()

	if *flagCleanup != "" {
		b := client.Bucket(*flagCleanup)
		fmt.Printf("[recovery] deleting all versions + bucket %s\n", *flagCleanup)
		deleteAll(ctx, b, "", true)
		if err := b.Delete(ctx); err != nil {
			log.Fatalf("[recovery] bucket delete: %v", err)
		}
		fmt.Println("[recovery] bucket deleted")
		return
	}

	dedicated := *flagBucket == ""
	bucketName := *flagBucket
	if dedicated {
		bucketName = fmt.Sprintf("ochakai-example-gcsverify-%d", time.Now().UnixNano())
	}
	bkt := client.Bucket(bucketName)

	fmt.Printf("== gcsverify ==\nbucket=%s (dedicated=%v) location=%s N=%d concurrency=%d\n\n",
		bucketName, dedicated, *flagLocation, *flagN, *flagConcurrency)

	if dedicated {
		attrs := &storage.BucketAttrs{
			Location:          *flagLocation,
			StorageClass:      "STANDARD",
			VersioningEnabled: true,
		}
		if err := bkt.Create(ctx, *flagProject, attrs); err != nil {
			log.Fatalf("bucket create (need storage.buckets.create on %s, or pass -bucket): %v", *flagProject, err)
		}
		fmt.Printf("created versioned bucket %s\n\n", bucketName)
		if !*flagKeep {
			defer cleanup(ctx, bkt, dedicated)
		}
	} else if !*flagKeep {
		defer cleanupPrefix(ctx, bkt, *flagPrefix)
	}

	seed(ctx, bkt)
	testCAS(ctx, bkt)
	testCreateOnly(ctx, bkt)
	testReadAfterWrite(ctx, bkt)
	testListGeneration(ctx, bkt)
	if dedicated {
		testVersioning(ctx, bkt)
	} else {
		fmt.Println("[versioning] skipped (existing-bucket mode)")
	}
	measureList(ctx, bkt)
	measureProjection(ctx, bkt)

	fmt.Println("\n== done ==")
	fmt.Println("note: GET latencies below are measured over WAN from this machine.")
	fmt.Println("production is Cloud Run→GCS same-region (asia-northeast1); real cold-start is FASTER.")
}

// --- payload ---------------------------------------------------------------

func objName(i int) string { return fmt.Sprintf("%sentry-%06d.json", *flagPrefix, i) }

var loremWords = strings.Fields(`知識 検索 添付 認証 設計 永続 索引 投影 鮮度 整合
	オブジェクト 正本 スナップショット 差分 ベクトル レキシカル trigram 埋め込み
	CAS generation バージョニング Cloud Run Storage 一本化 要求駆動 builder less`)

// entry builds a ~2KB JSON document shaped loosely like a knowledge entry.
func entry(i int, rng *rand.Rand) []byte {
	var body strings.Builder
	for body.Len() < 1600 {
		body.WriteString(loremWords[rng.Intn(len(loremWords))])
		body.WriteByte(' ')
	}
	m := map[string]any{
		"id":          fmt.Sprintf("entry-%06d", i),
		"title":       fmt.Sprintf("検証エントリ %06d", i),
		"description": "gcsverify synthetic entry for design doc 0026",
		"body":        body.String(),
		"tags":        []string{"gcsverify", loremWords[i%len(loremWords)]},
		"updated_at":  "2026-07-24T00:00:00Z",
	}
	b, _ := json.Marshal(m)
	return b
}

// --- phases ----------------------------------------------------------------

func seed(ctx context.Context, bkt *storage.BucketHandle) {
	fmt.Printf("[seed] ensuring %d objects (create-only, idempotent)...\n", *flagN)
	var created, skipped, bytes int64
	start := time.Now()
	pool(*flagN, *flagConcurrency, func(i int) {
		rng := rand.New(rand.NewSource(int64(i) + 1))
		data := entry(i, rng)
		w := bkt.Object(objName(i)).If(storage.Conditions{DoesNotExist: true}).NewWriter(ctx)
		w.ContentType = "application/json"
		_, werr := w.Write(data)
		cerr := w.Close()
		if is412(werr) || is412(cerr) {
			atomic.AddInt64(&skipped, 1)
			return
		}
		if werr != nil || cerr != nil {
			log.Printf("seed put %d: write=%v close=%v", i, werr, cerr)
			return
		}
		atomic.AddInt64(&created, 1)
		atomic.AddInt64(&bytes, int64(len(data)))
	})
	d := time.Since(start)
	rate := float64(created) / d.Seconds()
	fmt.Printf("[seed] created=%d skipped(existing)=%d in %s (%.0f PUT/s, ~%dKB avg obj)\n\n",
		created, skipped, d.Round(time.Millisecond), rate, avgKB(bytes, created))
}

func testCAS(ctx context.Context, bkt *storage.BucketHandle) {
	obj := bkt.Object(objName(0))
	a, err := obj.Attrs(ctx)
	if err != nil {
		log.Printf("[cas] attrs: %v", err)
		return
	}
	gen := a.Generation
	// Writer A wins with the current generation.
	if err := writeCond(ctx, obj, storage.Conditions{GenerationMatch: gen}, []byte(`{"cas":"A"}`)); err != nil {
		log.Printf("[cas] writer A should win: %v", err)
		return
	}
	// Writer B replays the same (now stale) generation → must 412.
	err = writeCond(ctx, obj, storage.Conditions{GenerationMatch: gen}, []byte(`{"cas":"B"}`))
	if is412(err) {
		fmt.Printf("[cas] PASS  stale ifGenerationMatch=%d rejected with 412 (winner-takes-all)\n", gen)
	} else {
		fmt.Printf("[cas] FAIL  expected 412, got: %v\n", err)
	}
}

func testCreateOnly(ctx context.Context, bkt *storage.BucketHandle) {
	// Existing object → DoesNotExist must 412.
	err := writeCond(ctx, bkt.Object(objName(0)), storage.Conditions{DoesNotExist: true}, []byte(`{}`))
	existing := is412(err)
	// Fresh name → DoesNotExist must succeed.
	fresh := bkt.Object(*flagPrefix + "createonly-probe.json")
	err2 := writeCond(ctx, fresh, storage.Conditions{DoesNotExist: true}, []byte(`{}`))
	freshOK := err2 == nil
	_ = fresh.Delete(ctx)
	if existing && freshOK {
		fmt.Println("[create-only] PASS  DoesNotExist: 412 on existing, OK on absent (revision-first is safe)")
	} else {
		fmt.Printf("[create-only] FAIL  existing412=%v freshOK=%v (err=%v err2=%v)\n", existing, freshOK, err, err2)
	}
}

func testReadAfterWrite(ctx context.Context, bkt *storage.BucketHandle) {
	// Distinct object per round: GCS caps mutations on a *single* object at
	// ~1/s (we tripped that 429 at first, itself confirming §9/§10). Real
	// read-your-writes is across distinct entries, which this now mirrors.
	const rounds = 20
	stale := 0
	for r := 0; r < rounds; r++ {
		obj := bkt.Object(fmt.Sprintf("%sraw-probe-%02d.json", *flagPrefix, r))
		want := fmt.Sprintf(`{"round":%d,"nonce":%d}`, r, rand.Int63())
		if err := writeCond(ctx, obj, storage.Conditions{}, []byte(want)); err != nil {
			log.Printf("[raw] write: %v", err)
			return
		}
		got, err := readAll(ctx, obj)
		if err != nil {
			log.Printf("[raw] read: %v", err)
			return
		}
		if string(got) != want {
			stale++
		}
		_ = obj.Delete(ctx)
	}
	if stale == 0 {
		fmt.Printf("[raw] PASS  %d/%d immediate reads returned the just-written value (strong RAW)\n", rounds, rounds)
	} else {
		fmt.Printf("[raw] FAIL  %d/%d reads were stale\n", stale, rounds)
	}
}

func testListGeneration(ctx context.Context, bkt *storage.BucketHandle) {
	// Ask only for the fields diff-detection needs.
	q := &storage.Query{Prefix: *flagPrefix}
	_ = q.SetAttrSelection([]string{"Name", "Generation"})
	it := bkt.Objects(ctx, q)
	a, err := it.Next()
	if err != nil {
		log.Printf("[list-gen] next: %v", err)
		return
	}
	if a.Generation != 0 {
		fmt.Printf("[list-gen] PASS  objects.list returns Generation (%d) without a GET (diff-detection viable)\n", a.Generation)
	} else {
		fmt.Println("[list-gen] FAIL  Generation missing from list result")
	}
}

func testVersioning(ctx context.Context, bkt *storage.BucketHandle) {
	name := *flagPrefix + "version-probe.json"
	obj := bkt.Object(name)
	if err := writeCond(ctx, obj, storage.Conditions{}, []byte(`{"v":1}`)); err != nil {
		log.Printf("[versioning] write v1: %v", err)
		return
	}
	a1, _ := obj.Attrs(ctx)
	if err := writeCond(ctx, obj, storage.Conditions{}, []byte(`{"v":2}`)); err != nil {
		log.Printf("[versioning] write v2: %v", err)
		return
	}
	// Count noncurrent + current versions.
	it := bkt.Objects(ctx, &storage.Query{Prefix: name, Versions: true})
	vers := 0
	for {
		_, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			log.Printf("[versioning] list versions: %v", err)
			return
		}
		vers++
	}
	// Old generation still retrievable.
	old, err := readAll(ctx, obj.Generation(a1.Generation))
	oldOK := err == nil && string(old) == `{"v":1}`
	if vers >= 2 && oldOK {
		fmt.Printf("[versioning] PASS  %d generations retained; prior generation still GET-able (history without knowledge_revision)\n", vers)
	} else {
		fmt.Printf("[versioning] FAIL  versions=%d oldOK=%v (err=%v)\n", vers, oldOK, err)
	}
}

func measureList(ctx context.Context, bkt *storage.BucketHandle) {
	start := time.Now()
	q := &storage.Query{Prefix: *flagPrefix}
	_ = q.SetAttrSelection([]string{"Name", "Generation"})
	it := bkt.Objects(ctx, q)
	n, pages := 0, 0
	pager := iterator.NewPager(it, 1000, "")
	for {
		var page []*storage.ObjectAttrs
		tok, err := pager.NextPage(&page)
		if err != nil {
			log.Printf("[list] page: %v", err)
			return
		}
		pages++
		n += len(page)
		if tok == "" {
			break
		}
	}
	d := time.Since(start)
	fmt.Printf("\n[list] %d objects in %d page(s) (=%d Class-A ops) in %s (%.0f obj/s listed)\n",
		n, pages, pages, d.Round(time.Millisecond), float64(n)/d.Seconds())
}

func measureProjection(ctx context.Context, bkt *storage.BucketHandle) {
	// Collect the id→generation set first (the LIST a cold start would do).
	listStart := time.Now()
	var names []string
	q := &storage.Query{Prefix: *flagPrefix}
	_ = q.SetAttrSelection([]string{"Name", "Generation"})
	it := bkt.Objects(ctx, q)
	for {
		a, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			log.Printf("[proj] list: %v", err)
			return
		}
		if strings.HasSuffix(a.Name, "-probe.json") {
			continue
		}
		names = append(names, a.Name)
	}
	listDur := time.Since(listStart)

	lats := make([]time.Duration, len(names))
	var bytes int64
	getStart := time.Now()
	pool(len(names), *flagConcurrency, func(i int) {
		t0 := time.Now()
		data, err := readAll(ctx, bkt.Object(names[i]))
		if err != nil {
			log.Printf("[proj] get %s: %v", names[i], err)
			return
		}
		lats[i] = time.Since(t0)
		atomic.AddInt64(&bytes, int64(len(data)))
	})
	getDur := time.Since(getStart)
	total := listDur + getDur

	sort.Slice(lats, func(a, b int) bool { return lats[a] < lats[b] })
	fmt.Printf("\n[projection] full cold-start fetch of %d objects (LIST + %d concurrent GET):\n", len(names), len(names))
	fmt.Printf("  LIST:        %s\n", listDur.Round(time.Millisecond))
	fmt.Printf("  GET (c=%d):  %s  (%.0f obj/s, %.1f MB/s)\n",
		*flagConcurrency, getDur.Round(time.Millisecond),
		float64(len(names))/getDur.Seconds(), float64(bytes)/1e6/getDur.Seconds())
	fmt.Printf("  per-GET:     p50=%s p95=%s p99=%s (WAN; same-region will be lower)\n",
		pct(lats, 50).Round(time.Millisecond), pct(lats, 95).Round(time.Millisecond), pct(lats, 99).Round(time.Millisecond))
	fmt.Printf("  TOTAL:       %s for ~%dKB avg obj, %.1f MB moved\n",
		total.Round(time.Millisecond), avgKB(bytes, int64(len(names))), float64(bytes)/1e6)
	fmt.Printf("  => real cold-start GET cost = %d Class-B ops; add local index build (0026 §4: 20k≈2.1s)\n", len(names))
}

// --- helpers ---------------------------------------------------------------

func writeCond(ctx context.Context, obj *storage.ObjectHandle, cond storage.Conditions, data []byte) error {
	var o *storage.ObjectHandle = obj
	if cond != (storage.Conditions{}) {
		o = obj.If(cond)
	}
	w := o.NewWriter(ctx)
	w.ContentType = "application/json"
	if _, err := w.Write(data); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}

func readAll(ctx context.Context, obj *storage.ObjectHandle) ([]byte, error) {
	r, err := obj.NewReader(ctx)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

func is412(err error) bool {
	var apiErr *googleapi.Error
	return errors.As(err, &apiErr) && apiErr.Code == http.StatusPreconditionFailed
}

// pool runs fn(0..n-1) with at most c concurrent workers.
func pool(n, c int, fn func(i int)) {
	sem := make(chan struct{}, c)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		sem <- struct{}{}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			fn(i)
		}(i)
	}
	wg.Wait()
}

func pct(sorted []time.Duration, p int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	i := (p * len(sorted)) / 100
	if i >= len(sorted) {
		i = len(sorted) - 1
	}
	return sorted[i]
}

func avgKB(bytes, n int64) int64 {
	if n == 0 {
		return 0
	}
	return bytes / n / 1024
}

func cleanup(ctx context.Context, bkt *storage.BucketHandle, dedicated bool) {
	fmt.Println("\n[cleanup] deleting all objects (incl. versions) + bucket...")
	deleteAll(ctx, bkt, "", true)
	if dedicated {
		if err := bkt.Delete(ctx); err != nil {
			log.Printf("[cleanup] bucket delete: %v (delete manually)", err)
		} else {
			fmt.Println("[cleanup] bucket deleted")
		}
	}
}

func cleanupPrefix(ctx context.Context, bkt *storage.BucketHandle, prefix string) {
	fmt.Printf("\n[cleanup] deleting objects under %s...\n", prefix)
	deleteAll(ctx, bkt, prefix, true)
}

func deleteAll(ctx context.Context, bkt *storage.BucketHandle, prefix string, versions bool) {
	it := bkt.Objects(ctx, &storage.Query{Prefix: prefix, Versions: versions})
	var objs []struct {
		name string
		gen  int64
	}
	for {
		a, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			log.Printf("[cleanup] list: %v", err)
			return
		}
		objs = append(objs, struct {
			name string
			gen  int64
		}{a.Name, a.Generation})
	}
	pool(len(objs), *flagConcurrency, func(i int) {
		_ = bkt.Object(objs[i].name).Generation(objs[i].gen).Delete(ctx)
	})
	fmt.Printf("[cleanup] deleted %d object versions\n", len(objs))
}
