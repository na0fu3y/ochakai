package restapi

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/httpauth"
)

// logLines decodes what a handler wrote to its logger.
func logLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		out = append(out, m)
	}
	return out
}

func TestRequestLogRecordsTheRouteNotThePath(t *testing.T) {
	buf := &bytes.Buffer{}
	log := slog.New(slog.NewJSONHandler(buf, nil))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/bundle/{path...}", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("body"))
	})
	h := requestLog(log, func(r *http.Request) string {
		_, pattern := mux.Handler(r)
		return pattern
	}, mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/bundle/metrics/quarterly-revenue.md", nil)
	req = req.WithContext(httpauth.WithActor(context.Background(),
		domain.Actor{Kind: domain.ActorProcess, Name: "analysis_agent/gemini"}))
	h.ServeHTTP(httptest.NewRecorder(), req)

	lines := logLines(t, buf)
	if len(lines) != 1 {
		t.Fatalf("logged %d lines, want one per request", len(lines))
	}
	line := lines[0]
	if line["route"] != "GET /api/v1/bundle/{path...}" {
		t.Errorf("route = %v, want the pattern the mux matched", line["route"])
	}
	// The knowledge is the user's, and a log is not a second place to keep
	// a copy of what it is called.
	if raw := buf.String(); strings.Contains(raw, "quarterly-revenue") {
		t.Errorf("the log carries a concept id:\n%s", raw)
	}
	// Nor is it a second copy of who wrote what: the kind is a rate, the
	// name is provenance and lives on the concept.
	if raw := buf.String(); strings.Contains(raw, "gemini") {
		t.Errorf("the log carries an actor name:\n%s", raw)
	}
	if line["actor"] != string(domain.ActorProcess) {
		t.Errorf("actor = %v, want the kind", line["actor"])
	}
	if line["status"] != float64(http.StatusOK) || line["bytes"] != float64(4) {
		t.Errorf("status/bytes = %v/%v, want 200/4", line["status"], line["bytes"])
	}
	if _, ok := line["duration_ms"]; !ok {
		t.Error("no duration_ms: answering \"search got slow\" is why this line exists")
	}
	if _, ok := line["code"]; ok {
		t.Errorf("a success carries code %v; there is no condition to name", line["code"])
	}
}

// A failure logs the condition beside the status, because that is the
// half a status cannot say: three conditions answer 409 (design doc
// 0083), and an operator watching a rate wants to know which one moved.
func TestRequestLogRecordsTheErrorCode(t *testing.T) {
	buf := &bytes.Buffer{}
	log := slog.New(slog.NewJSONHandler(buf, nil))

	h := requestLog(log, func(*http.Request) string { return "PUT /api/v1/bundle/{path...}" },
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeErrorBody(w, http.StatusConflict, domain.CodeAlreadyExists, "taken")
		}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPut, "/api/v1/bundle/x.md", nil))

	lines := logLines(t, buf)
	if len(lines) != 1 {
		t.Fatalf("logged %d lines, want one", len(lines))
	}
	if lines[0]["status"] != float64(http.StatusConflict) {
		t.Errorf("status = %v, want 409", lines[0]["status"])
	}
	if lines[0]["code"] != domain.CodeAlreadyExists {
		t.Errorf("code = %v, want %q", lines[0]["code"], domain.CodeAlreadyExists)
	}
}

// An export streams a gzip of the whole bundle, so the wrapper has to
// stay a Flusher: holding a stream in a buffer until the end is the way
// a request log breaks the one response that cannot wait.
func TestRequestLogKeepsAStreamStreaming(t *testing.T) {
	log := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
	flushed := false
	h := requestLog(log, func(*http.Request) string { return "GET /api/v1/bundle/{path...}" },
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			f, ok := w.(http.Flusher)
			if !ok {
				t.Error("the wrapped writer is not a Flusher")
				return
			}
			_, _ = w.Write([]byte("chunk"))
			f.Flush()
			flushed = true
		}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/bundle/", nil))
	if !flushed {
		t.Error("the handler could not flush through the request log")
	}
}
