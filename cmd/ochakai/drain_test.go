package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"runtime/pprof"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Guard: serveAndDrain must not return before in-flight requests have
// finished. Serve returns ErrServerClosed as soon as Shutdown is called;
// returning then would let main close the store (final usage flush, pool
// close) under requests that are still running — the exact loss the
// SIGTERM drain exists to prevent.
func TestServeAndDrainWaitsForInFlightRequests(t *testing.T) {
	// Taken before anything starts, so what `leaked` compares against is
	// this test's own doing rather than the binary's.
	before, _ := leakProfile(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	inHandler := make(chan struct{})
	release := make(chan struct{})
	var handlerDone atomic.Bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(inHandler)
		<-release
		handlerDone.Store(true)
		w.WriteHeader(http.StatusOK)
	})

	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() {
		served <- serveAndDrain(ctx, &http.Server{Handler: handler}, ln)
	}()

	resp := make(chan error, 1)
	go func() {
		r, err := http.Get("http://" + ln.Addr().String())
		if err == nil {
			_, _ = io.Copy(io.Discard, r.Body)
			r.Body.Close()
		}
		resp <- err
	}()

	<-inHandler
	cancel() // SIGTERM arrives while the request is in flight

	select {
	case err := <-served:
		t.Fatalf("serveAndDrain returned before the in-flight request finished (err=%v)", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("serveAndDrain: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serveAndDrain did not return after the drain completed")
	}
	if !handlerDone.Load() {
		t.Fatal("handler did not run to completion before serveAndDrain returned")
	}
	if err := <-resp; err != nil {
		t.Fatalf("in-flight request failed: %v", err)
	}
	leaked(t, before)
}

// leaked fails if the drain left a goroutine blocked forever.
//
// The assertions above say the drain waited; this says it did not wait by
// parking something and walking away. That is the failure the drain's own
// shape invites — a shutdown that hands a request to a goroutine nobody
// joins looks identical from the outside until the process will not exit
// — and until Go 1.27 there was no way to ask about it. `goroutineleak`
// is a profile of goroutines the runtime can prove are permanently
// blocked: the ones whose channel or mutex is no longer reachable by
// anybody who could wake them. A goroutine merely waiting on something
// live is not in it.
//
// The comparison is against a count taken before, not against zero. The
// profile is process-wide, so zero would make this test fail for a leak
// another test in this binary left — reporting a real problem in the
// wrong place, which is the kind of alarm that gets muted rather than
// read.
func leaked(t *testing.T, before int) {
	t.Helper()
	now, stacks := leakProfile(t)
	if now > before {
		t.Errorf("the drain leaked %d goroutine(s) (%d before, %d after):\n\n%s",
			now-before, before, now, stacks)
	}
}

// leakProfile runs the leak detection and returns what it found.
//
// Writing the profile is what runs it. `Count` alone does not: it returns
// the number the runtime reported the *last* time somebody asked, so a
// test that only counted would compare two stale readings and pass
// through any leak it was written to catch — which is what this one did
// until a deliberately parked goroutine failed to fail it.
func leakProfile(t *testing.T) (int, string) {
	t.Helper()
	p := pprof.Lookup("goroutineleak")
	if p == nil {
		t.Fatal("no goroutineleak profile: this check now guards nothing")
	}
	var stacks strings.Builder
	if err := p.WriteTo(&stacks, 1); err != nil {
		t.Fatalf("write the goroutineleak profile: %v", err)
	}
	return p.Count(), stacks.String()
}
