package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/na0fu3y/ochakai/internal/apiclient"
)

func TestTheReferenceIsTheKeptAnswersConversation(t *testing.T) {
	at := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	turns := []apiclient.AgentTurn{
		// The kept answer: written after the result, so it proposed
		// nothing and read one concept itself.
		{ID: "t2", At: at.Add(5 * time.Minute), By: "human:a", Producer: "ochakai/v1", Asked: "売上は?", Read: []string{"insights/seasonality"}, Keep: true},
		// The same conversation's first answer, which proposed the query.
		{ID: "t1", At: at, By: "human:a", Producer: "ochakai/v1", Asked: "売上は?", Read: []string{"metrics/revenue"}, ProposedSQL: "SELECT 1"},
		// Not the conversation: another person, another agent, too long ago.
		{ID: "o1", At: at, By: "human:b", Producer: "ochakai/v1", Asked: "売上は?", Read: []string{"x"}, ProposedSQL: "SELECT 2"},
		{ID: "o2", At: at, By: "human:a", Producer: "app/1", Asked: "売上は?", Read: []string{"y"}},
		{ID: "o3", At: at.Add(-3 * time.Hour), By: "human:a", Producer: "ochakai/v1", Asked: "売上は?", Read: []string{"z"}, ProposedSQL: "SELECT 3"},
		// After the keep: not part of what was kept.
		{ID: "o4", At: at.Add(time.Hour), By: "human:a", Producer: "ochakai/v1", Asked: "売上は?", Read: []string{"w"}, ProposedSQL: "SELECT 4"},
	}
	ref := reference(turns, turns[0])
	if strings.Join(ref.Read, ",") != "metrics/revenue,insights/seasonality" || ref.SQL != "SELECT 1" {
		t.Errorf("reference = %+v", ref)
	}
}

func TestAQuestionKeptTwiceIsReplayedOnce(t *testing.T) {
	at := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	turns := []apiclient.AgentTurn{
		{ID: "old", At: at, By: "human:a", Asked: "q", Keep: true},
		{ID: "new", At: at.Add(time.Hour), By: "human:a", Asked: "q", Keep: true},
		{ID: "other", At: at.Add(time.Minute), By: "human:a", Asked: "p", Keep: true},
		{ID: "unkept", At: at.Add(2 * time.Hour), By: "human:a", Asked: "r"},
	}
	got := keptTurns(turns, 0)
	if len(got) != 2 || got[0].ID != "new" || got[1].ID != "other" {
		t.Errorf("kept = %+v", got)
	}
	if got := keptTurns(turns, 1); len(got) != 1 || got[0].ID != "new" {
		t.Errorf("limit 1 = %+v", got)
	}
}

func TestResultsCompareAsRowsNotAsText(t *testing.T) {
	res := func(fields []string, rows ...[]string) *queryResult {
		return &queryResult{Fields: fields, Rows: rows, Total: int64(len(rows))}
	}
	want := res([]string{"channel", "revenue"}, []string{"web", "100"}, []string{"app", "0.1"})
	for name, tc := range map[string]struct {
		got  *queryResult
		want string
	}{
		"same rows in another order":    {res([]string{"c", "r"}, []string{"app", "0.10000000001"}, []string{"web", "100.0"}), resultMatch},
		"same columns in another order": {res([]string{"r", "c"}, []string{"100", "web"}, []string{"0.1", "app"}), resultMatch},
		"a different value":             {res([]string{"c", "r"}, []string{"web", "101"}, []string{"app", "0.1"}), resultMismatch},
		"a row missing":                 {res([]string{"c", "r"}, []string{"web", "100"}), resultMismatch},
		"a column missing":              {res([]string{"c"}, []string{"web"}, []string{"app"}), resultMismatch},
		"only the first rows":           {&queryResult{Fields: []string{"c", "r"}, Rows: want.Rows, Total: 5000}, resultTooLarge},
	} {
		if got, _ := compareResults(want, tc.got); got != tc.want {
			t.Errorf("%s: %s, want %s", name, got, tc.want)
		}
	}
}

func TestATimestampReadsAsATime(t *testing.T) {
	if got := bqCell("1.7589312E9", "TIMESTAMP"); got != "2025-09-27 00:00:00 UTC" {
		t.Errorf("TIMESTAMP = %q", got)
	}
	if got := bqCell(nil, "INTEGER"); got != "NULL" {
		t.Errorf("NULL = %q", got)
	}
}

// evalServers stands up the deployment and BigQuery a replay talks to.
// The agent proposes query, then answers once the result comes back.
type evalServers struct {
	mu      sync.Mutex
	asks    []string // the query string of each agent call
	queries []string // the SQL that reached jobs.query
	rows    map[string]string
}

func (e *evalServers) start(t *testing.T, proposed string) string {
	t.Helper()
	at := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	api := http.NewServeMux()
	api.HandleFunc("GET /api/v1/stats", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"agent":{"enabled":true,"model":"gemini-2.5-flash","bigquery_project":"billing"}}`)
	})
	api.HandleFunc("GET /api/v1/agent/turns", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"turns": []apiclient.AgentTurn{
			{ID: "t2", At: at.Add(time.Minute), By: "human:a", Asked: "チャネル別の売上は?", Read: []string{}, Keep: true, Verdict: "good"},
			{ID: "t1", At: at, By: "human:a", Asked: "チャネル別の売上は?", Read: []string{"metrics/revenue"}, ProposedSQL: "SELECT ref"},
		}})
	})
	api.HandleFunc("POST /api/v1/agent", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Messages []apiclient.AgentMessage `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		e.mu.Lock()
		e.asks = append(e.asks, r.URL.RawQuery)
		e.mu.Unlock()
		if len(in.Messages) == 1 {
			_ = json.NewEncoder(w).Encode(apiclient.AgentAnswer{Text: "確かめます", Read: []string{"metrics/revenue"}, SQL: &apiclient.AgentProposal{Query: proposed}})
			return
		}
		_ = json.NewEncoder(w).Encode(apiclient.AgentAnswer{Text: "web が最大です", Read: []string{}})
	})
	apiSrv := httptest.NewServer(api)
	t.Cleanup(apiSrv.Close)

	bq := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/jobs") {
			_, _ = io.WriteString(w, `{"statistics":{"query":{"statementType":"SELECT"}}}`)
			return
		}
		sql, _ := body["query"].(string)
		e.mu.Lock()
		e.queries = append(e.queries, sql)
		e.mu.Unlock()
		_, _ = io.WriteString(w, `{"jobComplete":true,"schema":{"fields":[{"name":"channel","type":"STRING"},{"name":"revenue","type":"INTEGER"}]},"rows":`+e.rows[sql]+`,"totalRows":"2","totalBytesProcessed":"10"}`)
	}))
	t.Cleanup(bq.Close)
	oldUp, oldTok := bigQueryUpstream, evalTokens
	bigQueryUpstream = bq.URL
	evalTokens = func(context.Context) (oauth2.TokenSource, error) {
		return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "person"}), nil
	}
	t.Cleanup(func() { bigQueryUpstream, evalTokens = oldUp, oldTok })
	return apiSrv.URL
}

func runEval(t *testing.T, args ...string) (string, error) {
	t.Helper()
	orig := os.Stdout
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = pw
	runErr := cmdEval(context.Background(), args)
	pw.Close()
	os.Stdout = orig
	out, _ := io.ReadAll(pr)
	return string(out), runErr
}

func TestEvalReplaysAKeptQuestionAndWritesNothing(t *testing.T) {
	e := &evalServers{rows: map[string]string{
		"SELECT ref":    `[{"f":[{"v":"web"},{"v":"100"}]},{"f":[{"v":"app"},{"v":"50"}]}]`,
		"SELECT replay": `[{"f":[{"v":"app"},{"v":"50"}]},{"f":[{"v":"web"},{"v":"100"}]}]`,
	}}
	target := e.start(t, "SELECT replay")
	out, err := runEval(t, "--url", target, "--exit-code")
	if err != nil {
		t.Fatalf("eval: %v\n%s", err, out)
	}
	for _, want := range []string{
		"pass\tmatch\tread 1/1\tチャネル別の売上は?\n",
		"replayed 1 kept questions with gemini-2.5-flash",
		"1 passed, 0 failed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	for _, q := range e.asks {
		if q != "dry_run=true" {
			t.Errorf("agent called with %q: a replay must be a dry run, or it counts itself", q)
		}
	}
	if len(e.queries) != 2 || e.queries[0] != "SELECT replay" || e.queries[1] != "SELECT ref" {
		t.Errorf("queries run = %q, want the replay's then the reference's", e.queries)
	}
}

func TestEvalGoesRedWhenAnAnswerStopsStanding(t *testing.T) {
	e := &evalServers{rows: map[string]string{
		"SELECT ref":    `[{"f":[{"v":"web"},{"v":"100"}]},{"f":[{"v":"app"},{"v":"50"}]}]`,
		"SELECT replay": `[{"f":[{"v":"web"},{"v":"90"}]},{"f":[{"v":"app"},{"v":"50"}]}]`,
	}}
	target := e.start(t, "SELECT replay")
	out, err := runEval(t, "--url", target, "--exit-code")
	if !errors.Is(err, errWorkPending) {
		t.Fatalf("err = %v, want the exit-2 answer\n%s", err, out)
	}
	if !strings.Contains(out, "FAIL\tmismatch\t") || !strings.Contains(out, "0 passed, 1 failed") {
		t.Errorf("output:\n%s", out)
	}
}
