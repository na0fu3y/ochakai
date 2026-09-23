package restapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/llm"
	"github.com/na0fu3y/ochakai/internal/service"
)

type onlyText string

func (onlyText) Name() string { return "only-text" }

func (t onlyText) Generate(context.Context, llm.Request) (*llm.Turn, error) {
	return &llm.Turn{Content: llm.Content{Role: "model", Parts: []llm.Part{{Text: string(t)}}}}, nil
}

func postAgent(t *testing.T, svc *service.Service, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	Handler(svc).ServeHTTP(rec, req)
	return rec
}

func TestAgentAnswersTheLastQuestion(t *testing.T) {
	rec := postAgent(t, &service.Service{Model: onlyText("答え")},
		`{"messages":[{"role":"user","text":"売上は?"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var out struct {
		Text string   `json:"text"`
		Read []string `json:"read"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Text != "答え" || out.Read == nil {
		t.Errorf("answer = %+v; read must be present even when empty", out)
	}
}

// A deployment with no agent says so with the status a file operation
// gets on a deployment with no bucket (design doc 0131), naming the
// variable that would change it.
func TestNoAgentIsA501(t *testing.T) {
	rec := postAgent(t, &service.Service{}, `{"messages":[{"role":"user","text":"q"}]}`)
	if rec.Code != http.StatusNotImplemented || !strings.Contains(rec.Body.String(), "OCHAKAI_AGENT") {
		t.Errorf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestAgentRefusesWhatItCannotRead(t *testing.T) {
	svc := &service.Service{Model: onlyText("x")}
	for _, body := range []string{
		`{"messages":[]}`,
		`{"messages":[{"role":"user","text":"q"}],"model":"other"}`,
		`{"messages":[{"role":"agent","text":"a"}]}`,
	} {
		if rec := postAgent(t, svc, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", body, rec.Code)
		}
	}
}
