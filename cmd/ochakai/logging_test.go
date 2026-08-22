package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

// logLine runs one line through the handler main installs and returns it
// decoded, which is how Cloud Logging meets it.
func logLine(t *testing.T, emit func(*slog.Logger)) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	emit(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level:       slog.LevelDebug,
		ReplaceAttr: cloudLoggingNames,
	})))
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("log line %q is not JSON: %v", buf.String(), err)
	}
	return got
}

// An operator asking for the warnings is asking Cloud Logging, and Cloud
// Logging reads one field. Until a line carries `severity` every line
// arrives equal, and the ones worth an alert — search silently fallen
// back to lexical, an export truncated mid-response — are indistinguishable
// from the request log.
func TestLogLevelsArriveAsCloudLoggingSeverities(t *testing.T) {
	for _, tc := range []struct {
		name string
		emit func(*slog.Logger)
		want string
	}{
		{"debug", func(l *slog.Logger) { l.Debug("m") }, "DEBUG"},
		{"info", func(l *slog.Logger) { l.Info("m") }, "INFO"},
		// The one rename: slog spells it WARN, Cloud Logging WARNING. A
		// value it does not know is read as DEFAULT, which is where the
		// warnings would have gone on quietly sitting.
		{"warn", func(l *slog.Logger) { l.Warn("m") }, "WARNING"},
		{"error", func(l *slog.Logger) { l.Error("m") }, "ERROR"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := logLine(t, tc.emit)
			if got["severity"] != tc.want {
				t.Errorf("severity = %v, want %q", got["severity"], tc.want)
			}
			if _, ok := got["level"]; ok {
				t.Errorf("line still carries level=%v beside severity", got["level"])
			}
		})
	}
}

// docs/guides/operating.md builds its saved queries on
// `jsonPayload.message="request"`, and slog's own key is `msg`.
func TestTheMessageIsWhereTheGuideSaysItIs(t *testing.T) {
	got := logLine(t, func(l *slog.Logger) { l.Info("request", "route", "GET /api/v1/search") })
	if got["message"] != "request" {
		t.Errorf("message = %v, want the message under the key Cloud Logging displays", got["message"])
	}
	if _, ok := got["msg"]; ok {
		t.Error("line still carries msg beside message")
	}
	// Everything else is the deployment's own, and stays where it was.
	if got["route"] != "GET /api/v1/search" {
		t.Errorf("route = %v, want the attribute untouched", got["route"])
	}
	if _, ok := got["time"]; !ok {
		t.Error("time is gone: it is left for Cloud Run to stamp, not for this to drop")
	}
}

// The reserved names are the platform's only at the top level. An
// attribute a caller put inside a group named "msg" is that caller's
// word, and renaming it would be this handler editing somebody's data.
func TestOnlyTheTopLevelKeysAreRenamed(t *testing.T) {
	got := logLine(t, func(l *slog.Logger) {
		l.Info("m", slog.Group("payload", slog.String("msg", "inner"), slog.String("level", "high")))
	})
	group, ok := got["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload = %v, want a group", got["payload"])
	}
	if group["msg"] != "inner" || group["level"] != "high" {
		t.Errorf("group = %v, want msg and level left as the caller wrote them", group)
	}
}
