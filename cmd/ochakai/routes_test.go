package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/config"
	"github.com/na0fu3y/ochakai/internal/service"
)

// Guard: the one thing the request log says about who called is whether
// it was a person or a process, and on /mcp it said `process` for
// everybody. The wiring is the whole of it — a log wrapped *around*
// httpauth.Middleware reads the request as it arrived, before the actor
// was resolved into a derived context, so httpauth.Actor hands it the
// process/unknown default and prints that.
//
// Nothing about the middleware or the log line changes when the nesting
// is wrong, which is why this reads the printed line rather than the
// code: /api/v1 has always had its log inside (restapi.Handler builds
// its own mux around it) and was printing the truth all along.
func TestTheRequestLogNamesWhoCalledOnMCP(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	// dev resolves every caller to human:anonymous, which is a person —
	// so a line that says `process` here can only have come from the
	// default nobody set.
	cfg := &config.Config{InsecureDev: true}
	mux := routes(log, cfg, &service.Service{Config: cfg}, "test")

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "routes-test", "version": "0"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	mux.ServeHTTP(httptest.NewRecorder(), r)

	line := buf.String()
	if !strings.Contains(line, `route=/mcp`) {
		t.Fatalf("no request line for /mcp:\n%s", line)
	}
	if !strings.Contains(line, "actor=human") {
		t.Errorf("the /mcp request line does not name the caller as a person:\n%s", line)
	}
}
