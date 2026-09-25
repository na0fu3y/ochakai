package agent

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/config"
	"github.com/na0fu3y/ochakai/internal/llm"
	"github.com/na0fu3y/ochakai/internal/service"
)

// scripted answers each Generate with the next turn it was given, and
// records the requests it saw.
type scripted struct {
	turns []*llm.Turn
	seen  []llm.Request
}

func (s *scripted) Name() string { return "scripted" }

func (s *scripted) Generate(_ context.Context, req llm.Request) (*llm.Turn, error) {
	s.seen = append(s.seen, req)
	if len(s.turns) == 0 {
		return nil, errors.New("script ran out")
	}
	t := s.turns[0]
	s.turns = s.turns[1:]
	return t, nil
}

func text(s string) *llm.Turn {
	return &llm.Turn{Content: llm.Content{Role: "model", Parts: []llm.Part{{Text: s}}}}
}

func call(name string, a map[string]any) *llm.Turn {
	return &llm.Turn{Content: llm.Content{Role: "model", Parts: []llm.Part{
		{FunctionCall: &llm.FunctionCall{Name: name, Args: a}}}}}
}

func TestADeploymentWithoutAModelSaysSo(t *testing.T) {
	_, err := Run(context.Background(), &service.Service{}, []Message{{Role: "user", Text: "hi"}})
	var unsupported *service.UnsupportedError
	if !errors.As(err, &unsupported) || !strings.Contains(err.Error(), "OCHAKAI_AGENT") {
		t.Errorf("err = %v, want the unsupported error naming OCHAKAI_AGENT", err)
	}
}

func TestMessagesAreChecked(t *testing.T) {
	svc := &service.Service{Model: &scripted{}}
	for name, msgs := range map[string][]Message{
		"empty":      nil,
		"bad role":   {{Role: "system", Text: "x"}},
		"blank text": {{Role: "user", Text: "  "}},
		"agent last": {{Role: "user", Text: "q"}, {Role: "agent", Text: "a"}},
		"too long":   {{Role: "user", Text: strings.Repeat("あ", maxTextBytes)}},
		"too many":   make([]Message, maxMessages+1),
	} {
		_, err := Run(context.Background(), svc, msgs)
		if _, ok := errors.AsType[*service.InvalidInputError](err); !ok {
			t.Errorf("%s: err = %v, want invalid input", name, err)
		}
	}
}

func TestTheConversationReachesTheModelWithItsRoles(t *testing.T) {
	m := &scripted{turns: []*llm.Turn{text("答え")}}
	ans, err := Run(context.Background(), &service.Service{Model: m}, []Message{
		{Role: "user", Text: "売上は?"}, {Role: "agent", Text: "どの期間?"}, {Role: "user", Text: "先月"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ans.Text != "答え" || len(ans.Read) != 0 {
		t.Errorf("answer = %+v", ans)
	}
	got := m.seen[0].Contents
	if len(got) != 3 || got[0].Role != "user" || got[1].Role != "model" || got[2].Role != "user" {
		t.Errorf("roles = %+v", got)
	}
	if m.seen[0].System == "" || len(m.seen[0].Tools) == 0 {
		t.Error("the model was not handed the system instruction and the tools")
	}
}

// A tool the model misnames is an answer it can recover from, not the end
// of the turn; the answer goes back as a function response.
func TestAToolFailureGoesBackToTheModel(t *testing.T) {
	m := &scripted{turns: []*llm.Turn{call("drop_table", nil), call("search_concepts", map[string]any{}), text("ok")}}
	if _, err := Run(context.Background(), &service.Service{Model: m}, []Message{{Role: "user", Text: "q"}}); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{`no tool named "drop_table"`, "needs a query"} {
		last := m.seen[i+1].Contents[len(m.seen[i+1].Contents)-1]
		resp := last.Parts[0].FunctionResponse
		if last.Role != "user" || resp == nil || !strings.Contains(resp.Response["error"].(string), want) {
			t.Errorf("round %d: handed back %+v, want an error containing %q", i+1, last, want)
		}
	}
}

// The reads write nothing: the agent's one write is write_draft, kept
// apart so a deployment that writes nothing can leave it out (design doc
// 0142 §3, §5).
func TestNoReadWrites(t *testing.T) {
	for _, tl := range tools {
		for _, verb := range []string{"put", "write", "delete", "verify", "reject", "report", "move"} {
			if strings.Contains(tl.Name, verb) {
				t.Errorf("tool %s looks like a write", tl.Name)
			}
		}
	}
}

func TestALoopingModelIsStopped(t *testing.T) {
	turns := make([]*llm.Turn, maxRounds+1)
	for i := range turns {
		turns[i] = call("no_such_tool", nil)
	}
	_, err := Run(context.Background(), &service.Service{Model: &scripted{turns: turns}}, []Message{{Role: "user", Text: "q"}})
	if err == nil || !strings.Contains(err.Error(), "rounds") {
		t.Errorf("err = %v, want the round limit", err)
	}
}

func TestCutKeepsCharactersWhole(t *testing.T) {
	if got := cut("あいう", 4); got != "あ" {
		t.Errorf("cut = %q", got)
	}
}

// A proposal ends the turn and comes back as one: the server runs
// nothing, the person decides (design doc 0142 §4). No OAuth client is
// configured here — that is what lets the team web UI run a proposal,
// not what lets the agent make one.
func TestAProposalEndsTheTurn(t *testing.T) {
	svc := &service.Service{}
	m := &scripted{turns: []*llm.Turn{call("propose_sql", map[string]any{"query": "SELECT 1", "purpose": "確かめる"})}}
	svc.Model = m
	ans, err := Run(context.Background(), svc, []Message{{Role: "user", Text: "q"}})
	if err != nil {
		t.Fatal(err)
	}
	if ans.SQL == nil || ans.SQL.Query != "SELECT 1" || ans.Text != "確かめる" {
		t.Errorf("answer = %+v", ans)
	}
	if !strings.Contains(m.seen[0].System, "propose_sql") {
		t.Error("the model was not told it may propose SQL")
	}
}

// A read-only deployment answers but does not write (design doc 0142 §5):
// the model is not offered the tool, and is told to put the text in the
// answer instead.
func TestAReadOnlyDeploymentOffersNoWrite(t *testing.T) {
	for _, readOnly := range []bool{false, true} {
		m := &scripted{turns: []*llm.Turn{text("ok")}}
		svc := &service.Service{Model: m, Config: &config.Config{ReadOnly: readOnly}}
		if _, err := Run(context.Background(), svc, []Message{{Role: "user", Text: "q"}}); err != nil {
			t.Fatal(err)
		}
		offered := slices.ContainsFunc(m.seen[0].Tools, func(tl llm.Tool) bool { return tl.Name == writeDraft.Name })
		if offered == readOnly {
			t.Errorf("read-only %v: write_draft offered = %v", readOnly, offered)
		}
		if told := strings.Contains(m.seen[0].System, "このデプロイではできない"); told != readOnly {
			t.Errorf("read-only %v: told it cannot write = %v", readOnly, told)
		}
	}
}

// What the model is handed back for a write that cannot happen: the
// refusal, so it can say so, and nothing in drafts.
func TestAWriteThatCannotHappenIsAnAnswer(t *testing.T) {
	m := &scripted{turns: []*llm.Turn{
		call("write_draft", map[string]any{"id": "a/b"}),
		call("write_draft", map[string]any{"id": "a/b", "document": "---\ntype: Metric\nstatus: deprecated\n---\nx\n"}),
		text("書けませんでした"),
	}}
	ans, err := Run(context.Background(), &service.Service{Model: m}, []Message{{Role: "user", Text: "q"}})
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"needs an id and a document", "drafts only"} {
		resp := m.seen[i+1].Contents[len(m.seen[i+1].Contents)-1].Parts[0].FunctionResponse
		if e, _ := resp.Response["error"].(string); !strings.Contains(e, want) {
			t.Errorf("write %d handed back %v, want %q", i+1, resp.Response, want)
		}
	}
	if len(ans.Drafts) != 0 {
		t.Errorf("drafts = %v", ans.Drafts)
	}
}
