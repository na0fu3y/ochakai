package restapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/na0fu3y/ochakai/internal/httpauth"
)

// One line per request, so an operator has something to read.
//
// Until this existed the whole observability surface was /health: a
// deployment could say it was alive and nothing else. "Search got slow"
// had no answer short of attaching to Cloud SQL by hand, "what is
// failing" had none at all, and the counts the loop keeps
// (design doc 0069) are about the knowledge rather than the service
// holding it.
//
// This is not telemetry. Nothing is reported anywhere — the line goes to
// stdout, which on Cloud Run is the deployment's own log in the
// deployment's own project (README, "what it refuses"). What it buys
// there is that a log-based metric can be built from it without ochakai
// growing a metrics endpoint, a second address space, or a scrape
// target: the platform the product already requires is the one that
// aggregates.
//
// **The route, not the path.** A bundle path is a concept's address and
// therefore the user's knowledge — logging it would copy the knowledge
// base's shape into a second place, one line at a time. The pattern the
// mux matched ("GET /api/v1/bundle/{path...}") says what was called
// without saying what was called on.
func requestLog(log *slog.Logger, route func(*http.Request) string, next http.Handler) http.Handler {
	if log == nil {
		// A Service assembled without one is a legitimate construction —
		// several tests build the smallest thing that serves — and a
		// missing logger must not be the reason a request panics.
		log = slog.Default()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &loggingWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		// Milliseconds because a request that takes a whole one is
		// already interesting here, and rounding hides nothing an
		// operator would act on.
		attrs := []any{
			"method", r.Method,
			"route", route(r),
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"bytes", rec.bytes,
			// The kind, not the name: who reached a deployment is
			// provenance and lives on the concepts they wrote (design doc
			// 0065), and a request log is not a second, unasked-for copy
			// of it. Whether the caller was a person or a process is what
			// an operator reads a rate by.
			"actor", httpauth.Actor(r.Context()).Kind,
		}
		if rec.code != "" {
			// The condition, not the sentence: "409 already_exists" is a
			// rate somebody can watch, and the prose beside it moves
			// between releases (design doc 0083).
			attrs = append(attrs, "code", rec.code)
		}
		log.Info("request", attrs...)
	})
}

// RequestLog wraps one handler that is not behind a mux — /mcp, whose
// whole surface is a single address — with the same line.
func RequestLog(log *slog.Logger, route string, next http.Handler) http.Handler {
	return requestLog(log, func(*http.Request) string { return route }, next)
}

// loggingWriter remembers what the response turned out to be. It also
// receives the error code from writeErrorBody, which is the one thing
// about a failure the wire carries and the status does not.
type loggingWriter struct {
	http.ResponseWriter
	status  int
	bytes   int
	code    string
	written bool
}

func (w *loggingWriter) WriteHeader(status int) {
	if !w.written {
		w.status, w.written = status, true
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *loggingWriter) Write(b []byte) (int, error) {
	w.written = true
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

// Flush keeps a streaming response streaming: an export writes a gzip of
// the whole bundle through this wrapper, and swallowing Flush would hold
// it in the buffer until the end.
func (w *loggingWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// codeRecorder is what writeErrorBody looks for: the machine-readable
// condition, handed to whatever is recording this response rather than
// put on the wire a second time.
type codeRecorder interface{ recordCode(string) }

func (w *loggingWriter) recordCode(code string) { w.code = code }
