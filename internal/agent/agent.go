// Package agent is ochakai's own data agent (design doc 0142): a
// generative model that reads the knowledge base through the same service
// every face calls, as the person who asked, and answers — citing what it
// read and whether a person confirmed it. It rules on nothing.
//
// This is the read-only cut: it answers and prepares a ruling packet, and
// writes nothing. Drafts, SQL and the outcome loop arrive in later slices
// (0142 §7-§8).
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/llm"
	"github.com/na0fu3y/ochakai/internal/okf"
	"github.com/na0fu3y/ochakai/internal/service"
	"github.com/na0fu3y/ochakai/internal/store"
)

// Message is one turn of the conversation as the caller holds it. The
// server keeps no conversation: a call carries everything it needs, the
// rule MCP's transport already follows (design doc 0118).
type Message struct {
	Role string `json:"role"` // "user" or "agent"
	Text string `json:"text"`
}

// Answer is what one call returns: the agent's reply, and the concepts it
// read to write it, in the order it read them.
type Answer struct {
	Text string   `json:"text"`
	Read []string `json:"read"`
}

// Limits on what one call may carry and spend. A conversation longer
// than this is one the caller should start again; a turn that needs more
// tool rounds than this is one that is looping.
const (
	maxMessages  = 40
	maxTextBytes = 64 << 10
	maxRounds    = 16
	timeout      = 4 * time.Minute
	// maxResultBytes bounds one tool result handed back to the model: a
	// long concept is cut rather than spent whole out of the context.
	maxResultBytes = 24 << 10
)

// ErrOff is the answer on a deployment that has no agent.
var ErrOff = service.Unsupportedf("this deployment has no agent: set OCHAKAI_AGENT to turn it on (design doc 0142)")

// Run answers the last message of msgs.
func Run(ctx context.Context, svc *service.Service, msgs []Message) (*Answer, error) {
	if svc.Model == nil {
		return nil, ErrOff
	}
	contents, err := validate(msgs)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	r := &run{svc: svc, read: []string{}, seen: map[string]bool{}}
	req := llm.Request{System: system, Contents: contents, Tools: tools}
	for range maxRounds {
		turn, err := svc.Model.Generate(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("the agent could not answer: %w", err)
		}
		calls := turn.Calls()
		if len(calls) == 0 {
			text := strings.TrimSpace(turn.Text())
			if text == "" {
				return nil, fmt.Errorf("the agent could not answer: %w", llm.ErrNoAnswer)
			}
			return &Answer{Text: text, Read: r.read}, nil
		}
		answers := make([]llm.Part, 0, len(calls))
		for _, c := range calls {
			answers = append(answers, llm.Part{FunctionResponse: &llm.FunctionResponse{
				Name: c.Name, Response: r.call(ctx, c),
			}})
		}
		req.Contents = append(req.Contents, turn.Content, llm.Content{Role: "user", Parts: answers})
	}
	return nil, fmt.Errorf("the agent did not finish within %d rounds of reading", maxRounds)
}

func validate(msgs []Message) ([]llm.Content, error) {
	if len(msgs) == 0 {
		return nil, service.Invalidf("messages is empty: send at least the question")
	}
	if len(msgs) > maxMessages {
		return nil, service.Invalidf("messages has %d turns; at most %d — start a new conversation", len(msgs), maxMessages)
	}
	total := 0
	contents := make([]llm.Content, 0, len(msgs))
	for i, m := range msgs {
		role := ""
		switch m.Role {
		case "user":
			role = "user"
		case "agent":
			role = "model"
		default:
			return nil, service.Invalidf("messages[%d].role is %q; it is user or agent", i, m.Role)
		}
		if strings.TrimSpace(m.Text) == "" {
			return nil, service.Invalidf("messages[%d].text is empty", i)
		}
		total += len(m.Text)
		contents = append(contents, llm.Content{Role: role, Parts: []llm.Part{{Text: m.Text}}})
	}
	if total > maxTextBytes {
		return nil, service.Invalidf("messages carry %d bytes of text; at most %d", total, maxTextBytes)
	}
	if msgs[len(msgs)-1].Role != "user" {
		return nil, service.Invalidf("the last message is the agent's; the last one is the question to answer")
	}
	return contents, nil
}

// run is one call's state: what it has read, for the answer's `read`.
type run struct {
	svc  *service.Service
	read []string
	seen map[string]bool
}

// call runs one tool and returns what the model is handed back. A tool's
// failure is an answer too — the model can recover from "not found" by
// searching again — so errors travel as {"error": ...} rather than ending
// the turn.
func (r *run) call(ctx context.Context, c llm.FunctionCall) map[string]any {
	out, err := r.dispatch(ctx, c)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	if len(b) > maxResultBytes {
		return map[string]any{"result": cut(string(b), maxResultBytes), "truncated": true}
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"result": v}
}

func (r *run) dispatch(ctx context.Context, c llm.FunctionCall) (any, error) {
	a := args(c.Args)
	switch c.Name {
	case "search_concepts":
		q := a.str("query")
		if q == "" {
			return nil, errors.New("search_concepts needs a query")
		}
		page, err := r.svc.SearchOrList(ctx, q, "", "", a.filter(), a.int("limit", 8))
		if err != nil {
			return nil, err
		}
		return page.Hits, nil
	case "list_concepts":
		sort := a.str("sort")
		if sort == "" {
			return nil, fmt.Errorf("list_concepts needs sort (%s)", strings.Join(domain.ListSorts, ", "))
		}
		page, err := r.svc.SearchOrList(ctx, "", sort, "", a.filter(), a.int("limit", 20))
		if err != nil {
			return nil, err
		}
		return page.Hits, nil
	case "get_concept":
		id := a.str("id")
		k, err := r.svc.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if !r.seen[k.ID] {
			r.seen[k.ID] = true
			r.read = append(r.read, k.ID)
		}
		return okf.ViewOf(k)
	case "get_stats":
		return r.svc.Stats(ctx, a.int("days", 30), nil)
	case "get_usage":
		return r.svc.Usage(ctx, a.str("id"))
	case "read_log":
		doc, err := r.svc.LogDocument(ctx, a.str("prefix"), a.int("limit", 200))
		if err != nil {
			return nil, err
		}
		return map[string]string{"log": string(doc)}, nil
	}
	return nil, fmt.Errorf("no tool named %q", c.Name)
}

type args map[string]any

func (a args) str(k string) string {
	s, _ := a[k].(string)
	return strings.TrimSpace(s)
}

// int reads a number the model sent, which JSON decoding hands over as a
// float64, and falls back to def when it sent none.
func (a args) int(k string, def int) int {
	if f, ok := a[k].(float64); ok && f > 0 {
		return int(f)
	}
	return def
}

func (a args) filter() store.Filter {
	var f store.Filter
	if t := a.str("type"); t != "" {
		f.Types = []domain.Type{domain.Type(t)}
	}
	if s := a.str("status"); s != "" {
		f.Statuses = []domain.Status{domain.Status(s)}
	}
	return f
}

// cut shortens s to at most n bytes without splitting a character.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
