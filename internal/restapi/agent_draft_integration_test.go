package restapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/llm"
	"github.com/na0fu3y/ochakai/internal/testdb"
)

// script answers each Generate with the next turn it was given and
// keeps what the model was handed back, so a test can read the tool
// results the agent saw.
type script struct {
	turns []*llm.Turn
	seen  []llm.Request
}

func (s *script) Name() string { return "script" }

func (s *script) Generate(_ context.Context, req llm.Request) (*llm.Turn, error) {
	s.seen = append(s.seen, req)
	t := s.turns[0]
	s.turns = s.turns[1:]
	return t, nil
}

func modelCall(name string, a map[string]any) *llm.Turn {
	return &llm.Turn{Content: llm.Content{Role: "model", Parts: []llm.Part{
		{FunctionCall: &llm.FunctionCall{Name: name, Args: a}}}}}
}

// TestRESTIntegrationAgentWritesADraft walks the agent's one write
// (design doc 0142 §3) through the wire: the draft is created as the
// agent's on behalf of the person who asked, it is named in the answer
// and in the kept turn, and a concept already at an id is left alone —
// the refusal goes back to the model rather than ending the turn.
func TestRESTIntegrationAgentWritesADraft(t *testing.T) {
	svc := newIntegrationService(t)
	srv := checkedServer(t, Handler(svc))
	root := testdb.Unique(t, "agentdraft")
	taken, fresh := root+"/revenue", root+"/gross-margin"
	resp := putDoc(t, srv.URL, taken, docFrom(t, map[string]any{
		"type": "Metric", "id": taken, "title": "Revenue", "status": "stable",
	}), true)
	resp.Body.Close()
	removeEntries(t, srv, taken, fresh)

	doc := "---\ntype: Metric\ntitle: 粗利\n---\n売上から原価を引いたもの。[売上](/" + taken + ".md) を元にする。\n"
	m := &script{turns: []*llm.Turn{
		modelCall("write_draft", map[string]any{"id": taken, "document": doc}),
		modelCall("write_draft", map[string]any{"id": fresh, "document": "---\ntype: Metric\nstatus: stable\n---\nx\n"}),
		modelCall("write_draft", map[string]any{"id": fresh, "document": doc}),
		{Content: llm.Content{Role: "model", Parts: []llm.Part{{Text: "粗利の draft を書きました"}}}},
	}}
	svc.Model = m

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/agent",
		strings.NewReader(`{"messages":[{"role":"user","text":"粗利を足して"}]}`))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("agent = %d: %s", res.StatusCode, body)
	}
	var ans struct {
		Drafts []string `json:"drafts"`
		Turn   string   `json:"turn"`
	}
	if err := json.Unmarshal(body, &ans); err != nil {
		t.Fatal(err)
	}
	if len(ans.Drafts) != 1 || ans.Drafts[0] != fresh {
		t.Errorf("drafts = %v, want only %s", ans.Drafts, fresh)
	}

	// What the model was told for the two refused writes.
	for i, want := range []string{"already", "drafts only"} {
		got := m.seen[i+1].Contents[len(m.seen[i+1].Contents)-1].Parts[0].FunctionResponse.Response
		if e, _ := got["error"].(string); !strings.Contains(e, want) {
			t.Errorf("write %d handed back %v, want an error containing %q", i+1, got, want)
		}
	}

	var kept domain.View
	getJSON(t, srv.URL+"/api/v1/bundle/"+taken+".md", &kept)
	if kept.Summary.Status != domain.StatusStable {
		t.Errorf("the concept already at %s is now %q; the agent must not replace it", taken, kept.Summary.Status)
	}
	var wrote domain.View
	getJSON(t, srv.URL+"/api/v1/bundle/"+fresh+".md", &wrote)
	if wrote.Summary.Status != domain.StatusDraft || wrote.Summary.Trust != domain.TrustUnverified {
		t.Errorf("draft = status %q, trust %q; want an unverified draft", wrote.Summary.Status, wrote.Summary.Trust)
	}
	var page struct {
		Turns []struct {
			ID     string   `json:"id"`
			By     string   `json:"by"`
			Drafts []string `json:"drafts"`
		} `json:"turns"`
	}
	getJSON(t, srv.URL+"/api/v1/agent/turns?limit=100", &page)
	found := false
	for _, tr := range page.Turns {
		if tr.ID == ans.Turn {
			found = true
			if len(tr.Drafts) != 1 || tr.Drafts[0] != fresh {
				t.Errorf("the kept turn names drafts %v, want [%s]", tr.Drafts, fresh)
			}
			// Whoever asked — here the unauthenticated test caller — is
			// who the draft was written on behalf of.
			if c := wrote.Observed.CreatedBy; c.Kind != domain.ActorProcess || c.Name != "ochakai" || c.Via != tr.By || !strings.HasPrefix(c.Producer, "ochakai/") {
				t.Errorf("created_by = %v, want process:ochakai via %s, using this build", c, tr.By)
			}
		}
	}
	if !found {
		t.Errorf("turn %s is not listed", ans.Turn)
	}
}
