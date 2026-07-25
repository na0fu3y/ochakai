package main

import (
	"context"
	"io"
	"net"
	"net/http"
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
}
