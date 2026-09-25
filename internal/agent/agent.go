// Package agent is ochakai's own data agent (design doc 0142): a
// generative model that reads the knowledge base through the same service
// every face calls, as the person who asked, and answers — citing what it
// read and whether a person confirmed it. It rules on nothing.
//
// It answers, proposes a query for the person to run, and writes drafts
// for a person to rule on (0142 §3). It never replaces a concept, and a
// draft it writes is recorded as the agent's, on behalf of the person who
// asked — never as theirs.
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
//
// SQL is present when the agent stopped to ask for a query to be run.
// It is a proposal: the server runs nothing (design doc 0142 §4). The
// person who asked reads it, runs it as themselves if they choose, and
// sends what came back as their next message.
//
// Turn names the kept shape of this turn (design doc 0142 §6), which the
// person who asked judges with POST /api/v1/agent/turns/{id}. Absent when
// it could not be kept.
//
// Drafts names the concepts the agent wrote in this turn, each a new
// draft awaiting a person's ruling.
type Answer struct {
	Text   string    `json:"text"`
	Read   []string  `json:"read"`
	SQL    *Proposal `json:"sql,omitempty"`
	Drafts []string  `json:"drafts,omitempty"`
	Turn   string    `json:"turn,omitempty"`
}

// Proposal is one query the agent asks the person to run.
type Proposal struct {
	Query   string `json:"query"`
	Purpose string `json:"purpose"`
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
	// maxDrafts bounds what one turn may write. A person rules on each
	// draft one at a time, and the triage procedure asks for five at most
	// a round for the same reason.
	maxDrafts = 5
)

// ErrOff is the answer on a deployment that has no agent.
var ErrOff = service.Unsupportedf("this deployment has no agent: set OCHAKAI_AGENT to turn it on (design doc 0142)")

// Run answers the last message of msgs, and keeps the turn's shape.
func Run(ctx context.Context, svc *service.Service, msgs []Message) (*Answer, error) {
	ans, err := answer(ctx, svc, msgs)
	if err != nil || svc.Store == nil {
		return ans, err
	}
	sql := ""
	if ans.SQL != nil {
		sql = ans.SQL.Query
	}
	ans.Turn = svc.RecordAgentTurn(ctx, firstAsked(msgs), msgs[len(msgs)-1].Text, ans.Read, sql, ans.Drafts)
	return ans, nil
}

// firstAsked is the conversation's opening question — what a comparison
// set wants, where the latest message may be a query's result.
func firstAsked(msgs []Message) string {
	for _, m := range msgs {
		if m.Role == "user" {
			return m.Text
		}
	}
	return ""
}

func answer(ctx context.Context, svc *service.Service, msgs []Message) (*Answer, error) {
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
	// Every agent may propose a query. Whether the page in front can run
	// it is the page's business — `ochakai ui` runs it itself, the team
	// web UI needs an OAuth client, and anything else shows the SQL for
	// the person to run (design doc 0142 §4) — so the model is not told
	// which it is talking to.
	//
	// It may write drafts unless the deployment writes nothing: a
	// read-only one answers but does not write (0142 §5), and the tool is
	// left out rather than offered and refused.
	sys, all := system+systemSQL, append(append([]llm.Tool(nil), tools...), proposeSQL)
	if svc.Config == nil || !svc.Config.ReadOnly {
		sys += systemDraft
		all = append(all, writeDraft)
	} else {
		sys += systemNoDraft
	}
	req := llm.Request{System: sys, Contents: contents, Tools: all}
	for range maxRounds {
		turn, err := svc.Model.Generate(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("the agent could not answer: %w", err)
		}
		calls := turn.Calls()
		if p := proposal(calls); p != nil {
			// The turn ends here, whatever else the model asked for in
			// it: the next step is the person's.
			text := strings.TrimSpace(turn.Text())
			if text == "" {
				text = p.Purpose
			}
			return &Answer{Text: text, Read: r.read, SQL: p, Drafts: r.drafts}, nil
		}
		if len(calls) == 0 {
			text := strings.TrimSpace(turn.Text())
			if text == "" {
				return nil, fmt.Errorf("the agent could not answer: %w", llm.ErrNoAnswer)
			}
			return &Answer{Text: text, Read: r.read, Drafts: r.drafts}, nil
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

// proposal is the first propose_sql call in a turn, or nil. A call with
// no query is not a proposal; the model is told so by the loop going on.
func proposal(calls []llm.FunctionCall) *Proposal {
	for _, c := range calls {
		if c.Name != proposeSQL.Name {
			continue
		}
		a := args(c.Args)
		if q := a.str("query"); q != "" {
			return &Proposal{Query: q, Purpose: a.str("purpose")}
		}
	}
	return nil
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

// run is one call's state: what it has read, for the answer's `read`,
// and what it has written, for its `drafts`.
type run struct {
	svc    *service.Service
	read   []string
	seen   map[string]bool
	drafts []string
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
	case "list_turns":
		return r.svc.AgentTurns(ctx, a.str("verdict"), a.bool("keep"), a.int("limit", 30))
	case "write_draft":
		return r.writeDraft(ctx, a.str("id"), a.str("document"))
	case "read_log":
		doc, err := r.svc.LogDocument(ctx, a.str("prefix"), a.int("limit", 200))
		if err != nil {
			return nil, err
		}
		return map[string]string{"log": string(doc)}, nil
	}
	return nil, fmt.Errorf("no tool named %q", c.Name)
}

// writeDraft creates one draft (design doc 0142 §3). It only ever
// creates: an id that is taken is refused with what holds it, so the
// agent proposes beside a concept rather than over it — the guard MCP's
// put_concept applies to a ruled concept, applied here to every one.
//
// The draft is the agent's, on behalf of the person who asked:
// process:ochakai via their principal, using this build. The write is
// still made in the person's scope, so an access policy narrows what
// the agent may write exactly as it narrows what they may (0109).
func (r *run) writeDraft(ctx context.Context, id, document string) (any, error) {
	if len(r.drafts) >= maxDrafts {
		return nil, fmt.Errorf("this turn has written %d drafts, the most one turn may; say in the answer what else you would have written", maxDrafts)
	}
	if id == "" || document == "" {
		return nil, errors.New("write_draft needs an id and a document")
	}
	d, notes, err := okf.Parse([]byte(document))
	if err != nil {
		return nil, err
	}
	k := d.Knowledge
	k.ID = id
	switch k.Status {
	case "", domain.StatusDraft:
		k.Status = domain.StatusDraft
	default:
		return nil, fmt.Errorf("the document says status: %s; the agent writes drafts only — leave status out or write draft", k.Status)
	}
	created, err := r.svc.CreateKeepingCurated(ctx, &k, r.svc.AgentActor(ctx))
	if err != nil {
		return nil, err
	}
	r.drafts = append(r.drafts, created.ID)
	out := map[string]any{"id": created.ID, "status": created.Status}
	if len(notes) > 0 {
		out["notes"] = notes
	}
	return out, nil
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

func (a args) bool(k string) bool {
	b, _ := a[k].(bool)
	return b
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
