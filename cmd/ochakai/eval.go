// `ochakai eval` replays the questions people kept for comparison against
// the deployment's own agent and says, for each, whether the answer still
// stands (design doc 0146).
//
// It runs where the person runs it: the agent answers on the server with
// ?dry_run=true, so the replay writes nothing and counts nothing there,
// and every query the agent proposes runs from this machine as the person
// at the keyboard, through the same guard `ochakai ui` puts in front of
// BigQuery (a dry run, a SELECT only, the byte cap). The server executes
// no SQL here either (design doc 0142 §4).
//
// A kept turn is one answer, and the answer a person keeps is usually the
// last of a conversation: the one written after a query's result came
// back, which proposed nothing and read little itself. So the reference
// is the conversation up to that answer, gathered from the turns the
// caller can read: the same person, the same opening question, in the
// hours before. What it read is the union of what those turns read, and
// its query is the last one proposed.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/na0fu3y/ochakai/internal/apiclient"
)

const (
	// conversationWindow is how far back a kept answer's conversation is
	// looked for. The web UI keeps a conversation for the life of a tab,
	// and a conversation that stops for longer than this is two.
	conversationWindow = 2 * time.Hour
	// maxReplayRounds bounds one replay. The web UI stops running a
	// conversation's queries by itself after six in a row; a replay gets
	// two more, for the answer to be written after the last.
	maxReplayRounds = 8
	// compareRows is how many rows of each result are compared. Past it
	// the two results are said to be too large to compare, not guessed at.
	compareRows = 1000
	// The shape a result travels back to the agent in: the web UI's own
	// (internal/webui/static/js/sql.js), so a replay reads what the
	// person's answer read.
	messageRows  = 50
	messageChars = 8000
	// maxConversationBytes is the server's bound on one conversation
	// (internal/agent); older results fold first, as the page does.
	maxConversationBytes = 64 << 10
)

// evalTokens is where a replay's queries get the person's Google token:
// the same source `ochakai ui` runs a query with. A variable so tests can
// stand in.
var evalTokens = apiclient.QueryTokenSource

// The outcomes of comparing one replay's result with its reference.
const (
	resultMatch       = "match"
	resultMismatch    = "mismatch"
	resultNoQuery     = "no-query"     // the reference ran a query and the replay did not
	resultNoReference = "no-reference" // nothing to compare the replay's result with
	resultTooLarge    = "too-large"
	resultRefFailed   = "reference-failed"
	resultFailed      = "replay-failed"
)

type evalReference struct {
	Read []string `json:"read"`
	SQL  string   `json:"sql,omitempty"`
}

type evalReplay struct {
	Read   []string `json:"read"`
	SQL    string   `json:"sql,omitempty"`
	Rounds int      `json:"rounds"`
	Answer string   `json:"answer,omitempty"`
	Error  string   `json:"error,omitempty"`
}

type evalCase struct {
	Turn      string        `json:"turn"`
	Asked     string        `json:"asked"`
	By        string        `json:"by"`
	Reference evalReference `json:"reference"`
	Replay    evalReplay    `json:"replay"`
	// ReadHit counts the reference's concepts the replay read too.
	ReadHit int    `json:"read_hit"`
	Result  string `json:"result"`
	// Detail says why a result is not a match, in a sentence.
	Detail string `json:"detail,omitempty"`
	Pass   bool   `json:"pass"`
}

type evalReport struct {
	Model   string     `json:"model"`
	Client  string     `json:"client"`
	At      time.Time  `json:"at"`
	Cases   []evalCase `json:"questions"`
	Passed  int        `json:"passed"`
	Failed  int        `json:"failed"`
	Skipped int        `json:"skipped"`
}

func cmdEval(ctx context.Context, args []string) error {
	fs, target := newFlagSet(
		"eval",
		"Usage: ochakai eval [flags]\n\nReplay the questions people kept for comparison (a 👍 with \"use this\nquestion for comparison\") against this deployment's agent, and say for\neach whether the answer still stands: whether the replay read the\nconcepts the kept answer read, and whether its query returned the same\nrows as the kept answer's query.\n\nNothing is written, and nothing is counted: the agent answers with\n?dry_run=true, so a replay keeps no turn, writes no draft and adds no\nusage event. Every query runs from this machine as you — the same\nidentity `ochakai ui` runs one with — as a SELECT only, capped at\n10 GiB billed; the server runs none (design doc 0142 §4).\n\nA question passes when its query's rows match the reference's (row\norder, and numbers to nine significant digits, do not matter), or, when\nthe kept answer ran no query, when the replay read every concept it\nread. A line per question, then one line saying which model answered —\nthe number to watch when the model, the prompt or the knowledge changes.\nWith --exit-code the command exits 2 when any question fails, and 1 on\nan error, so a scheduled job goes red on a regression.",
		"  ochakai eval\n  ochakai eval --limit 10                 # the ten newest kept questions\n  ochakai eval --project my-billing       # where the deployment names none\n  ochakai eval --json | jq '.questions[] | select(.pass | not)'\n  ochakai eval --exit-code                # in CI: red when an answer stops standing\n")
	project := fs.String("project", "", "the Google Cloud `project` the queries are billed to (default: the deployment's OCHAKAI_BIGQUERY_PROJECT)")
	limit := fs.Int("limit", 0, "replay at most this many kept questions, newest first (default: all of them)")
	exitCode := fs.Bool("exit-code", false, "exit 2 when any question fails (0 when all pass, 1 on error) — for cron and CI")
	asJSON := fs.Bool("json", false, "print JSON")
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}
	c, err := newClient(ctx, *target)
	if err != nil {
		return err
	}
	st, err := c.Stats(ctx, 0, nil)
	if err != nil {
		return err
	}
	if st.Agent == nil || !st.Agent.Enabled {
		return errors.New("this deployment has no agent to replay against: set OCHAKAI_AGENT on it (design doc 0142)")
	}
	turns, err := c.Turns(ctx, false)
	if err != nil {
		return err
	}
	kept := keptTurns(turns, *limit)
	if len(kept) == 0 {
		return errors.New("no question has been kept for comparison yet: give an answer a 👍 with \"この問いを比較に使う\" in the web UI")
	}
	bill := *project
	if bill == "" {
		bill = st.Agent.BigQueryProject
	}
	var q *queryRunner
	if bill != "" {
		tokens, err := evalTokens(ctx)
		if err != nil {
			return err
		}
		q = &queryRunner{tokens: tokens, project: bill}
	}

	rep := evalReport{Model: st.Agent.Model, Client: version, At: time.Now().UTC()}
	for _, k := range kept {
		ec := replayOne(ctx, c, q, k, reference(turns, k))
		switch {
		case ec.Result == resultNoReference && len(ec.Reference.Read) == 0:
			rep.Skipped++
		case ec.Pass:
			rep.Passed++
		default:
			rep.Failed++
		}
		rep.Cases = append(rep.Cases, ec)
		if !*asJSON {
			printEvalCase(ec)
		}
	}
	if *asJSON {
		if err := printJSON(rep); err != nil {
			return err
		}
	} else {
		model := rep.Model
		if model == "" {
			model = "the deployment's model"
		}
		fmt.Printf("replayed %d kept questions with %s (ochakai %s): %d passed, %d failed, %d with nothing to compare\n",
			len(rep.Cases), model, rep.Client, rep.Passed, rep.Failed, rep.Skipped)
	}
	if *exitCode && rep.Failed > 0 {
		return errWorkPending
	}
	return nil
}

func printEvalCase(ec evalCase) {
	status := "pass"
	switch {
	case ec.Result == resultNoReference && len(ec.Reference.Read) == 0:
		status = "skip"
	case !ec.Pass:
		status = "FAIL"
	}
	read := "read -"
	if n := len(ec.Reference.Read); n > 0 {
		read = fmt.Sprintf("read %d/%d", ec.ReadHit, n)
	}
	line := fmt.Sprintf("%s\t%s\t%s\t%s", status, ec.Result, read, clipLine(ec.Asked, 80))
	if ec.Detail != "" {
		line += "\t" + ec.Detail
	}
	fmt.Println(line)
}

func clipLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// keptTurns is the questions kept for comparison, newest first, at most
// limit of them (0: all). A question kept twice is replayed once, from
// its newest keep.
func keptTurns(turns []apiclient.AgentTurn, limit int) []apiclient.AgentTurn {
	var out []apiclient.AgentTurn
	seen := map[string]bool{}
	sorted := slices.Clone(turns)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].At.After(sorted[j].At) })
	for _, t := range sorted {
		key := t.By + "\x00" + t.Asked
		if !t.Keep || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, t)
		if limit > 0 && len(out) == limit {
			break
		}
	}
	return out
}

// reference is what the kept answer's conversation read and ran: every
// turn by the same person, from the same agent, opening with the same
// question, in the window up to and including the kept one.
func reference(turns []apiclient.AgentTurn, kept apiclient.AgentTurn) evalReference {
	var conv []apiclient.AgentTurn
	for _, t := range turns {
		if t.By == kept.By && t.Producer == kept.Producer && t.Asked == kept.Asked &&
			!t.At.After(kept.At) && kept.At.Sub(t.At) <= conversationWindow {
			conv = append(conv, t)
		}
	}
	sort.SliceStable(conv, func(i, j int) bool { return conv[i].At.Before(conv[j].At) })
	ref := evalReference{Read: []string{}}
	for _, t := range conv {
		for _, id := range t.Read {
			if !slices.Contains(ref.Read, id) {
				ref.Read = append(ref.Read, id)
			}
		}
		if t.ProposedSQL != "" {
			ref.SQL = t.ProposedSQL
		}
	}
	return ref
}

// replayOne asks the kept question again and compares what came back.
func replayOne(ctx context.Context, c *apiclient.Client, q *queryRunner, k apiclient.AgentTurn, ref evalReference) evalCase {
	ec := evalCase{Turn: k.ID, Asked: k.Asked, By: k.By, Reference: ref}
	rp, last := replay(ctx, c, q, k.Asked)
	ec.Replay = rp
	for _, id := range ref.Read {
		if slices.Contains(rp.Read, id) {
			ec.ReadHit++
		}
	}
	readAll := ec.ReadHit == len(ref.Read)
	switch {
	case rp.Error != "":
		ec.Result, ec.Detail = resultFailed, rp.Error
	case ref.SQL == "":
		ec.Result = resultNoReference
		ec.Pass = len(ref.Read) > 0 && readAll
	case rp.SQL == "":
		ec.Result, ec.Detail = resultNoQuery, "the kept answer ran a query and the replay answered without one"
	case q == nil:
		ec.Result, ec.Detail = resultFailed, "no billing project: pass --project, or set OCHAKAI_BIGQUERY_PROJECT on the deployment"
	case last == nil:
		ec.Result, ec.Detail = resultFailed, "the replay's last query did not run"
	default:
		want, err := q.run(ctx, ref.SQL, compareRows)
		if err != nil {
			ec.Result, ec.Detail = resultRefFailed, err.Error()
			break
		}
		ec.Result, ec.Detail = compareResults(want, last)
		ec.Pass = ec.Result == resultMatch
	}
	return ec
}

// replay runs one conversation to its answer: every query the agent
// proposes runs as the person and goes back as their next message, as
// it would in the web UI with automatic runs agreed. It returns the last
// result that ran.
func replay(ctx context.Context, c *apiclient.Client, q *queryRunner, asked string) (evalReplay, *queryResult) {
	rp := evalReplay{Read: []string{}}
	msgs := []apiclient.AgentMessage{{Role: "user", Text: asked}}
	results := []int{} // which messages carry a result, for folding
	var last *queryResult
	for rp.Rounds < maxReplayRounds {
		rp.Rounds++
		ans, err := c.Ask(ctx, foldConversation(msgs, results), true)
		if err != nil {
			rp.Error = err.Error()
			return rp, last
		}
		for _, id := range ans.Read {
			if !slices.Contains(rp.Read, id) {
				rp.Read = append(rp.Read, id)
			}
		}
		if ans.SQL == nil {
			rp.Answer = ans.Text
			return rp, last
		}
		rp.SQL = ans.SQL.Query
		msgs = append(msgs, apiclient.AgentMessage{Role: "agent", Text: ans.Text + "\n\n```sql\n" + ans.SQL.Query + "\n```"})
		if q == nil {
			rp.Error = "the agent proposed a query and there is no billing project to run it in: pass --project, or set OCHAKAI_BIGQUERY_PROJECT on the deployment"
			return rp, last
		}
		res, err := q.run(ctx, ans.SQL.Query, compareRows)
		if err != nil {
			last = nil
			msgs = append(msgs, apiclient.AgentMessage{Role: "user", Text: "実行できませんでした\n\n```sql\n" + ans.SQL.Query + "\n```\n\nエラー: " + err.Error()})
			continue
		}
		last = res
		results = append(results, len(msgs))
		msgs = append(msgs, apiclient.AgentMessage{Role: "user", Text: resultMessage(ans.SQL.Query, res)})
	}
	rp.Error = fmt.Sprintf("no answer after %d rounds", maxReplayRounds)
	return rp, last
}

// resultMessage is a result as the web UI hands it back (sql.js asMessage).
func resultMessage(query string, r *queryResult) string {
	var b strings.Builder
	b.WriteString(strings.Join(r.Fields, "\t"))
	shown := min(len(r.Rows), messageRows)
	for _, row := range r.Rows[:shown] {
		b.WriteString("\n" + strings.Join(row, "\t"))
	}
	table, cut := b.String(), int64(shown) < r.Total
	if len(table) > messageChars {
		table, cut = table[:messageChars], true
	}
	note := ""
	if cut {
		note = "、先頭だけ"
	}
	return fmt.Sprintf("実行 SQL(%d バイト読み取り)\n\n```sql\n%s\n```\n\n結果(全 %d 行%s):\n\n```\n%s\n```", r.Bytes, query, r.Total, note, table)
}

// foldConversation drops the oldest results' rows until the conversation
// fits the server's bound, keeping the SQL that produced each and the
// newest result whole — the page's own fold (sql.js).
func foldConversation(msgs []apiclient.AgentMessage, results []int) []apiclient.AgentMessage {
	out := slices.Clone(msgs)
	size := 0
	for _, m := range out {
		size += len(m.Text)
	}
	for i, at := range results {
		if size <= maxConversationBytes || i == len(results)-1 {
			break
		}
		text := out[at].Text
		if before, _, ok := strings.Cut(text, "\n\n結果(全 "); ok {
			folded := before + "\n\n結果: 会話の長さの上限のため省きました。この SQL は実行済みで、そのあとの答えは結果を読んで書かれています。"
			size += len(folded) - len(text)
			out[at].Text = folded
		}
	}
	return out
}

// compareResults says whether two results hold the same rows: as
// multisets, so row order does not matter, with numbers compared to nine
// significant digits and columns compared by position — or, where the
// positions disagree, as the same values in any column order, since a
// query that names its columns in another order has the same answer.
func compareResults(want, got *queryResult) (string, string) {
	if int64(len(want.Rows)) < want.Total || int64(len(got.Rows)) < got.Total {
		return resultTooLarge, fmt.Sprintf("more than %d rows: compare a narrower question", compareRows)
	}
	if len(want.Rows) != len(got.Rows) {
		return resultMismatch, fmt.Sprintf("%d rows, the reference has %d", len(got.Rows), len(want.Rows))
	}
	if len(want.Fields) != len(got.Fields) {
		return resultMismatch, fmt.Sprintf("%d columns, the reference has %d", len(got.Fields), len(want.Fields))
	}
	if sameRows(want.Rows, got.Rows, false) || sameRows(want.Rows, got.Rows, true) {
		return resultMatch, ""
	}
	return resultMismatch, "the same shape, different values"
}

func sameRows(a, b [][]string, anyColumnOrder bool) bool {
	count := map[string]int{}
	key := func(row []string) string {
		cells := make([]string, len(row))
		for i, c := range row {
			cells[i] = normalCell(c)
		}
		if anyColumnOrder {
			sort.Strings(cells)
		}
		return strings.Join(cells, "\x1f")
	}
	for _, r := range a {
		count[key(r)]++
	}
	for _, r := range b {
		k := key(r)
		if count[k] == 0 {
			return false
		}
		count[k]--
	}
	return true
}

func normalCell(c string) string {
	if f, err := strconv.ParseFloat(c, 64); err == nil {
		return strconv.FormatFloat(f, 'g', 9, 64)
	}
	return c
}

// queryResult is one query's rows as text, the way the page reads them.
type queryResult struct {
	Fields []string
	Types  []string
	Rows   [][]string
	Total  int64
	Bytes  int64
}

// queryRunner runs a query as the person, through `ochakai ui`'s guard.
type queryRunner struct {
	tokens  oauth2.TokenSource
	project string
}

func (q *queryRunner) run(ctx context.Context, sql string, maxRows int) (*queryResult, error) {
	tok, err := q.tokens.Token()
	if err != nil {
		return nil, fmt.Errorf("no Google token to run the query with: %w", err)
	}
	kind, status, body := statementType(ctx, tok.AccessToken, q.project, sql)
	if status != http.StatusOK {
		return nil, bigQueryError(status, body)
	}
	if kind != "SELECT" {
		return nil, fmt.Errorf("only a SELECT is run; this statement is %s", kind)
	}
	req, _ := json.Marshal(map[string]any{
		"query":              sql,
		"useLegacySql":       false,
		"maximumBytesBilled": strconv.Itoa(maxBytesBilled),
		"timeoutMs":          20000,
		"maxResults":         maxRows,
	})
	base := "/bigquery/v2/projects/" + url.PathEscape(q.project) + "/queries"
	status, body = call(ctx, tok.AccessToken, http.MethodPost, base, req)
	for range 12 {
		if status != http.StatusOK {
			return nil, bigQueryError(status, body)
		}
		var r bqResponse
		if err := json.Unmarshal(body, &r); err != nil {
			return nil, fmt.Errorf("reading BigQuery's answer: %w", err)
		}
		if r.JobComplete {
			return r.result(), nil
		}
		path := base + "/" + url.PathEscape(r.JobReference.JobID) + "?" + url.Values{
			"location": {r.JobReference.Location}, "timeoutMs": {"10000"}, "maxResults": {strconv.Itoa(maxRows)},
		}.Encode()
		status, body = call(ctx, tok.AccessToken, http.MethodGet, path, nil)
	}
	return nil, errors.New("the query did not finish within two minutes")
}

func bigQueryError(status int, body []byte) error {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error.Message != "" {
		return errors.New(e.Error.Message)
	}
	return fmt.Errorf("BigQuery answered %d", status)
}

type bqResponse struct {
	JobComplete  bool `json:"jobComplete"`
	JobReference struct {
		JobID    string `json:"jobId"`
		Location string `json:"location"`
	} `json:"jobReference"`
	Schema struct {
		Fields []struct {
			Name string `json:"name"`
			Type string `json:"type"`
			Mode string `json:"mode"`
		} `json:"fields"`
	} `json:"schema"`
	Rows []struct {
		F []struct {
			V any `json:"v"`
		} `json:"f"`
	} `json:"rows"`
	TotalRows           string `json:"totalRows"`
	TotalBytesProcessed string `json:"totalBytesProcessed"`
}

func (r *bqResponse) result() *queryResult {
	out := &queryResult{}
	for _, f := range r.Schema.Fields {
		out.Fields = append(out.Fields, f.Name)
		t := f.Type
		if f.Mode == "REPEATED" {
			t = "REPEATED"
		}
		out.Types = append(out.Types, t)
	}
	for _, row := range r.Rows {
		cells := make([]string, len(row.F))
		for i, c := range row.F {
			t := ""
			if i < len(out.Types) {
				t = out.Types[i]
			}
			cells[i] = bqCell(c.V, t)
		}
		out.Rows = append(out.Rows, cells)
	}
	out.Total, _ = strconv.ParseInt(r.TotalRows, 10, 64)
	if out.Total == 0 {
		out.Total = int64(len(out.Rows))
	}
	out.Bytes, _ = strconv.ParseInt(r.TotalBytesProcessed, 10, 64)
	return out
}

// bqCell is one value as the page shows it (sql.js cell): NULL spelled
// out, a TIMESTAMP as a UTC time rather than epoch seconds, anything
// nested as its JSON.
func bqCell(v any, typ string) string {
	switch x := v.(type) {
	case nil:
		return "NULL"
	case string:
		if typ == "TIMESTAMP" {
			if f, err := strconv.ParseFloat(x, 64); err == nil {
				sec := int64(f)
				t := time.Unix(sec, int64((f-float64(sec))*1e9)).UTC()
				return strings.TrimSuffix(t.Format("2006-01-02 15:04:05.000"), ".000") + " UTC"
			}
		}
		return strings.NewReplacer("\t", " ", "\n", " ").Replace(x)
	default:
		b, _ := json.Marshal(x)
		return string(b)
	}
}
