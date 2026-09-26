package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fixedToken string

func (t fixedToken) token() (string, error) { return string(t), nil }

// chatHarness is serve-chat in front of a fake ochakai server, with
// Google's key check replaced by one that trusts tokens of the form
// "signed:<email>" for the audience it was asked about.
type chatHarness struct {
	bridge  *chatBridge
	asked   []*http.Request
	bodies  []string
	answer  string
	status  int
	delay   time.Duration
	audSeen string
}

func newChatHarness(t *testing.T) *chatHarness {
	t.Helper()
	h := &chatHarness{status: http.StatusOK, answer: `{"text":"売上は **4,560 万円** です(metrics/revenue、human-reviewed)。","read":["metrics/revenue"]}`}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		h.asked = append(h.asked, r.Clone(context.Background()))
		h.bodies = append(h.bodies, string(b))
		time.Sleep(h.delay)
		w.WriteHeader(h.status)
		_, _ = io.WriteString(w, h.answer)
	}))
	t.Cleanup(srv.Close)
	h.bridge = &chatBridge{
		agentURL: srv.URL + "/api/v1/agent",
		tokens:   fixedToken("bridge"),
		verify: func(_ context.Context, token, audience string) (string, error) {
			h.audSeen = audience
			email, ok := strings.CutPrefix(token, "signed:")
			if !ok {
				return "", errors.New("bad signature")
			}
			return email, nil
		},
		log: slog.New(slog.DiscardHandler),
	}
	return h
}

func (h *chatHarness) post(t *testing.T, token, event string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "https://chat-bridge.example.run.app/", strings.NewReader(event))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.bridge.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

const chatMessageEvent = `{"type":"MESSAGE","user":{"email":"a@example.com","type":"HUMAN"},
  "message":{"text":"@ochakai 先月の売上は?","argumentText":" 先月の売上は?"}}`

func TestChatAsksAsThePersonWhoWrote(t *testing.T) {
	h := newChatHarness(t)
	code, out := h.post(t, "signed:chat@system.gserviceaccount.com", chatMessageEvent)
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if h.audSeen != "https://chat-bridge.example.run.app/" {
		t.Errorf("audience checked = %q, want the endpoint Chat called", h.audSeen)
	}
	if len(h.asked) != 1 {
		t.Fatalf("asked the agent %d times", len(h.asked))
	}
	r := h.asked[0]
	if got := r.Header.Get("Ochakai-On-Behalf-Of"); got != "human:a@example.com" {
		t.Errorf("on behalf of %q", got)
	}
	if got := r.Header.Get("X-Serverless-Authorization"); got != "Bearer bridge" {
		t.Errorf("service identity %q", got)
	}
	if !strings.Contains(h.bodies[0], `"text":"先月の売上は?"`) {
		t.Errorf("asked %s — the @mention should be gone", h.bodies[0])
	}
	text, _ := out["text"].(string)
	if !strings.Contains(text, "*4,560 万円*") || strings.Contains(text, "**") || !strings.Contains(text, "`metrics/revenue`") {
		t.Errorf("reply = %q", text)
	}
}

func TestChatRefusesWhatGoogleChatDidNotSign(t *testing.T) {
	h := newChatHarness(t)
	for name, token := range map[string]string{
		"no token":          "",
		"a forged token":    "forged:chat@system.gserviceaccount.com",
		"somebody else's":   "signed:intruder@example.com",
		"another service's": "signed:service-1@gcp-sa-other.iam.gserviceaccount.com",
	} {
		if code, _ := h.post(t, token, chatMessageEvent); code != http.StatusUnauthorized {
			t.Errorf("%s: status %d, want 401", name, code)
		}
	}
	if len(h.asked) != 0 {
		t.Error("the agent was asked for a request Chat did not sign")
	}
}

func TestChatAnswersAnAddOnEventInItsOwnShape(t *testing.T) {
	h := newChatHarness(t)
	ev := `{"chat":{"user":{"email":"b@example.com","type":"HUMAN"},
	  "messagePayload":{"message":{"text":"粗利は?","argumentText":"粗利は?"}}}}`
	code, out := h.post(t, "signed:service-123@gcp-sa-gsuiteaddons.iam.gserviceaccount.com", ev)
	if code != http.StatusOK || len(h.asked) != 1 || h.asked[0].Header.Get("Ochakai-On-Behalf-Of") != "human:b@example.com" {
		t.Fatalf("status %d, asked %d", code, len(h.asked))
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"createMessageAction"`) {
		t.Errorf("reply = %s, want the add-on shape", b)
	}
}

func TestChatShowsAProposalItCannotRun(t *testing.T) {
	h := newChatHarness(t)
	h.answer = `{"text":"数えます","read":[],"sql":{"query":"SELECT 1","purpose":"x"}}`
	_, out := h.post(t, "signed:chat@system.gserviceaccount.com", chatMessageEvent)
	text, _ := out["text"].(string)
	if !strings.Contains(text, "SELECT 1") || !strings.Contains(text, "Web UI") {
		t.Errorf("reply = %q", text)
	}
}

func TestChatSaysSoWhenItCannotAnswer(t *testing.T) {
	h := newChatHarness(t)
	h.status, h.answer = http.StatusInternalServerError, `{"code":"internal","error":"internal error"}`
	_, out := h.post(t, "signed:chat@system.gserviceaccount.com", chatMessageEvent)
	if text, _ := out["text"].(string); !strings.Contains(text, "もう一度") {
		t.Errorf("reply = %q", text)
	}
	// A bot's message is not answered at all.
	before := len(h.asked)
	_, out = h.post(t, "signed:chat@system.gserviceaccount.com", `{"type":"MESSAGE","user":{"email":"bot@example.com","type":"BOT"},"message":{"text":"hi"}}`)
	if len(h.asked) != before || len(out) != 0 {
		t.Errorf("a bot was answered: %v", out)
	}
}

// A refusal that is the bridge's configuration is said to be one, not
// blamed on a busy model.
func TestChatNamesAConfigurationProblemAsOne(t *testing.T) {
	h := newChatHarness(t)
	h.status, h.answer = http.StatusForbidden, `{"code":"forbidden","error":"delegation not allowed"}`
	_, out := h.post(t, "signed:chat@system.gserviceaccount.com", chatMessageEvent)
	if text, _ := out["text"].(string); !strings.Contains(text, "OCHAKAI_DELEGATING_CALLERS") {
		t.Errorf("reply = %q", text)
	}
	if got := h.asked[0].Header.Get("Ochakai-Producer"); got == "ochakai-chat/" {
		t.Error("the producer names no version")
	}
}
