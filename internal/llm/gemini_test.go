package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEndpointStaysInTheRegion(t *testing.T) {
	got := endpoint("p", "asia-northeast1", "gemini-x")
	want := "https://asia-northeast1-aiplatform.googleapis.com/v1/projects/p/locations/asia-northeast1/publishers/google/models/gemini-x:generateContent"
	if got != want {
		t.Errorf("endpoint = %s, want %s", got, want)
	}
	if got := endpoint("p", "global", "m"); !strings.HasPrefix(got, "https://aiplatform.googleapis.com/") {
		t.Errorf("global endpoint = %s", got)
	}
}

func TestGenerateSendsToolsAndReadsACall(t *testing.T) {
	var seen generateRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &seen); err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"role":"model","parts":[
			{"text":"見てみる"},
			{"functionCall":{"name":"search_concepts","args":{"query":"売上"}}}]},"finishReason":"STOP"}]}`)
	}))
	defer srv.Close()
	g := newGemini(srv.Client(), srv.URL, "gemini-x")

	turn, err := g.Generate(context.Background(), Request{
		System:   "you are",
		Contents: []Content{{Role: "user", Parts: []Part{{Text: "売上は?"}}}},
		Tools:    []Tool{{Name: "search_concepts", Description: "d"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen.SystemInstruction == nil || seen.SystemInstruction.Parts[0].Text != "you are" {
		t.Errorf("system instruction not sent: %+v", seen.SystemInstruction)
	}
	if len(seen.Tools) != 1 || seen.Tools[0].FunctionDeclarations[0].Name != "search_concepts" {
		t.Errorf("tools not sent: %+v", seen.Tools)
	}
	calls := turn.Calls()
	if len(calls) != 1 || calls[0].Name != "search_concepts" || calls[0].Args["query"] != "売上" {
		t.Errorf("calls = %+v", calls)
	}
	if turn.Text() != "見てみる" {
		t.Errorf("text = %q", turn.Text())
	}
}

func TestGenerateRetriesOnlyWhatIsBusy(t *testing.T) {
	defer func(d time.Duration) { backoff = d }(backoff)
	backoff = time.Millisecond

	for _, tc := range []struct {
		status   int
		attempts int
	}{
		{http.StatusTooManyRequests, 3},
		{http.StatusServiceUnavailable, 3},
		{http.StatusBadRequest, 1},
		{http.StatusNotFound, 1}, // a model this region does not carry
	} {
		n := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n++
			w.WriteHeader(tc.status)
		}))
		g := newGemini(srv.Client(), srv.URL, "m")
		if _, err := g.Generate(context.Background(), Request{}); err == nil {
			t.Errorf("%d: no error", tc.status)
		}
		if n != tc.attempts {
			t.Errorf("%d: %d attempts, want %d", tc.status, n, tc.attempts)
		}
		srv.Close()
	}
}

func TestGenerateSaysWhenNothingCameBack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"promptFeedback":{"blockReason":"SAFETY"}}`)
	}))
	defer srv.Close()
	_, err := newGemini(srv.Client(), srv.URL, "m").Generate(context.Background(), Request{})
	if err == nil || !strings.Contains(err.Error(), "SAFETY") {
		t.Errorf("err = %v", err)
	}
}
