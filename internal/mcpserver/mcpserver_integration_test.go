package mcpserver

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/na0fu3y/ochakai/internal/config"
	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/httpauth"
	"github.com/na0fu3y/ochakai/internal/service"
	"github.com/na0fu3y/ochakai/internal/store"
	"github.com/na0fu3y/ochakai/internal/testdb"
)

// headerSwitcher injects the delegation header into every outgoing MCP
// request, with a value that the test can change between calls — the
// shape of an embedding host serving several end users over one session.
type headerSwitcher struct {
	mu    sync.Mutex
	value string
	next  http.RoundTripper
}

func (h *headerSwitcher) RoundTrip(r *http.Request) (*http.Response, error) {
	h.mu.Lock()
	v := h.value
	h.mu.Unlock()
	if v != "" {
		r.Header.Set(httpauth.OnBehalfOfHeader, v)
	}
	return h.next.RoundTrip(r)
}

func (h *headerSwitcher) set(v string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.value = v
}

// TestIntegrationDelegatedActorFollowsEachCall pins the 0027 contract on
// the MCP surface: the recorded actor comes from the headers of the POST
// that carried the tool call, not from whoever initialized the session.
// A delegating host reuses one session across end users; before the fix,
// every write was attributed to the first user's identity.
func TestIntegrationDelegatedActorFollowsEachCall(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := t.Context()
	s, err := store.New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{InsecureDev: true}
	svc := &service.Service{Store: s, Config: cfg, Log: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	srv := httptest.NewServer(httpauth.Middleware(cfg, Handler(svc, "test")))
	defer srv.Close()

	sw := &headerSwitcher{value: "human:alice@example.co.jp", next: http.DefaultTransport}
	transport := &mcp.StreamableClientTransport{
		Endpoint:   srv.URL,
		HTTPClient: &http.Client{Transport: sw},
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "host", Version: "0"}, nil).Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect (as alice): %v", err)
	}
	defer cs.Close()

	// The session now exists, initialized under alice's identity. The
	// next call arrives on the same session but on behalf of bob.
	sw.set("human:bob@example.co.jp")
	id := testdb.Unique(t, "mcpit") + "/delegation"
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "put_concept",
		Arguments: map[string]any{"id": id, "document": "---\ntype: glossary\n---\n\nwritten on behalf of bob\n"},
	})
	if err != nil {
		t.Fatalf("put_concept: %v", err)
	}
	if res.IsError {
		t.Fatalf("put_concept failed: %+v", res.Content)
	}

	k, err := svc.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	// The producer is the session's clientInfo — that one is a property of
	// the connection, not of the call, so unlike the identity it is the
	// same for alice's call and bob's (design doc 0052 §3.3).
	want := domain.Actor{Kind: domain.ActorHuman, Name: "bob@example.co.jp", Via: "human:anonymous",
		Producer: "host/0"}
	if k.CreatedBy != want {
		t.Errorf("created_by = %+v, want %+v (actor pinned to the session initializer?)", k.CreatedBy, want)
	}

	// report_outcome takes no actor argument — the service reads it from
	// the context — so it is the tool where a session-pinned actor
	// survives longest. Its report is evidence in the re-verification
	// feed, and evidence attributed to the wrong person is worse than
	// none.
	sw.set("human:carol@example.co.jp")
	res, err = cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "report_outcome",
		Arguments: map[string]any{"target": id, "outcome": "failed", "note": "did not run"},
	})
	if err != nil {
		t.Fatalf("report_outcome: %v", err)
	}
	if res.IsError {
		t.Fatalf("report_outcome failed: %+v", res.Content)
	}
	conn, err := pgx.Connect(context.Background(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	var kind, name string
	if err := conn.QueryRow(context.Background(),
		`SELECT actor_kind, actor_name FROM knowledge_event
		  WHERE knowledge_id = $1 AND event = 'failed' ORDER BY at DESC LIMIT 1`, id).
		Scan(&kind, &name); err != nil {
		t.Fatalf("outcome event: %v", err)
	}
	if kind != string(domain.ActorHuman) || name != "carol@example.co.jp" {
		t.Errorf("outcome recorded %s:%s, want human:carol@example.co.jp (actor pinned to the session initializer?)", kind, name)
	}
}

// put_concept creates on a free id and replaces on a taken one, and
// the guards that used to belong to two tools still apply to the right
// half: reviving a curated tombstone is refused on the create side, and
// replacing a curated entry on the other (design docs 0015 §3.1, 0046
// §3.14).
func TestIntegrationPutKnowledgeCreatesThenReplaces(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := t.Context()
	s, err := store.New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{InsecureDev: true}
	svc := &service.Service{Store: s, Config: cfg, Log: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	srv := httptest.NewServer(httpauth.Middleware(cfg, Handler(svc, "test")))
	defer srv.Close()
	transport := &mcp.StreamableClientTransport{Endpoint: srv.URL}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "host", Version: "0"}, nil).Connect(ctx, transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	id := testdb.Unique(t, "mcpput") + "/revenue"
	defer func() {
		_ = svc.Delete(context.Background(), id, domain.Actor{Kind: domain.ActorHuman, Name: "t"}, nil, "")
	}()

	put := func(t *testing.T, doc string) *mcp.CallToolResult {
		t.Helper()
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{
			Name:      "put_concept",
			Arguments: map[string]any{"id": id, "document": doc},
		})
		if err != nil {
			t.Fatalf("put_concept: %v", err)
		}
		if res.IsError {
			t.Fatalf("put_concept failed: %+v", res.Content)
		}
		return res
	}

	// A title of this test's own: the test database is shared with the
	// packages running beside it, and a live entry called 売上 would
	// crowd their search assertions while this one runs.
	title := "mcp put " + id
	first := put(t, "---\ntype: Metric\ntitle: "+title+"\nstatus: draft\n---\n\n受注合計。\n")
	k, err := svc.Get(ctx, id)
	if err != nil {
		t.Fatalf("the entry was not created: %v", err)
	}
	if k.Title != title {
		t.Errorf("title = %q", k.Title)
	}

	// The same id again is a replace, not a conflict: a write says what
	// the entry should say, and the id being taken is not the agent's
	// question to answer.
	second := "---\ntype: Metric\ntitle: " + title + "(改)\nstatus: draft\n---\n\n受注合計。返品を除く。\n"
	put(t, second)
	k, err = svc.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if k.Title != title+"(改)" {
		t.Errorf("the second write did not replace: title = %q", k.Title)
	}

	// And the answer says which of the three it was, because this
	// surface has no headers to carry it (design doc 0097): an agent
	// that wrote something could not otherwise tell a replacement from a
	// write that changed nothing without reading the concept back.
	planOf := func(t *testing.T, res *mcp.CallToolResult) string {
		t.Helper()
		return answerOf[knowledgeOut](t, res).Knowledge.Plan
	}
	if got := planOf(t, first); got != domain.PlanCreated {
		t.Errorf("the first write answered plan = %q, want %q", got, domain.PlanCreated)
	}
	if got := planOf(t, put(t, second)); got != domain.PlanUnchanged {
		t.Errorf("an identical write answered plan = %q, want %q", got, domain.PlanUnchanged)
	}
	if got := planOf(t, put(t, "---\ntype: Metric\ntitle: "+title+"(改2)\nstatus: draft\n---\n\n受注合計。\n")); got != domain.PlanUpdated {
		t.Errorf("a changed write answered plan = %q, want %q", got, domain.PlanUpdated)
	}

	// A human ruling still stops the replace half, with the advice the
	// two tools used to carry between them.
	if _, err := svc.Verify(ctx, id, domain.Actor{Kind: domain.ActorHuman, Name: "na0"}); err != nil {
		t.Fatal(err)
	}
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "put_concept",
		Arguments: map[string]any{"id": id, "document": "---\ntype: Metric\ntitle: 上書き\n---\n\nx\n"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("put_concept replaced a verified entry")
	}
	var said string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			said += tc.Text
		}
	}
	for _, want := range []string{"report_outcome failed", "verified"} {
		if !strings.Contains(said, want) {
			t.Errorf("the refusal does not mention %q: %s", want, said)
		}
	}
}

// The write-back loop's second half, walked from the six tools alone.
//
// The claim this pins is that an MCP-only agent — a hosted client with no
// shell, which the README calls the primary case for this surface — can
// carry out the instruction put_concept's own description gives it: when
// a verified concept is wrong, "put a better draft at a different id and
// let a human promote it". That instruction is worth only as much as the
// agent's ability to see what became of the draft, and nothing had
// checked that it can.
//
// Three things are needed and all three are already here: the draft can
// name what it would replace (a body link, which is how relationships are
// written at all — design doc 0074 §2), the agent can see the human's
// ruling on its own draft (get_concept carries the verification ledger),
// and it can find out that a proposal was already turned down before
// spending a write on it (rejected=true). What it cannot do is rule —
// verify, reject, delete — and that is design doc 0076's decision rather
// than a gap.
func TestIntegrationMCPAgentSeesTheRulingOnItsDraft(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := t.Context()
	s, err := store.New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{InsecureDev: true}
	svc := &service.Service{Store: s, Config: cfg, Log: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	srv := httptest.NewServer(httpauth.Middleware(cfg, Handler(svc, "test")))
	defer srv.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "host", Version: "0"}, nil).
		Connect(ctx, &mcp.StreamableClientTransport{Endpoint: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	human := domain.Actor{Kind: domain.ActorHuman, Name: "reviewer"}
	run := testdb.Unique(t, "mcploop")
	original, replacement := run+"/metrics/revenue", run+"/metrics/revenue-net"
	defer func() {
		for _, id := range []string{original, replacement} {
			_ = svc.Delete(context.Background(), id, human, nil, "")
		}
	}()

	// A verified concept, as a human left it.
	if _, _, _, err := svc.Put(ctx, &domain.Knowledge{
		Type: domain.TypeMetrics, ID: original, Title: "mcp loop " + original,
		Status: domain.StatusStable, Body: "受注合計。", CreatedBy: human,
	}, human, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Verify(ctx, original, human); err != nil {
		t.Fatal(err)
	}

	call := func(t *testing.T, name string, args map[string]any) *mcp.CallToolResult {
		t.Helper()
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if res.IsError {
			t.Fatalf("%s failed: %+v", name, res.Content)
		}
		return res
	}
	conceptOf := func(t *testing.T, res *mcp.CallToolResult) domain.View {
		t.Helper()
		return answerOf[knowledgeOut](t, res).Knowledge
	}

	// The agent drafts the replacement, naming what it would replace with
	// an ordinary body link. No field carries the relationship: links are
	// written in prose, and the surrounding sentence says what kind it is.
	call(t, "put_concept", map[string]any{
		"id": replacement,
		"document": "---\ntype: Metric\ntitle: mcp loop " + replacement + "\nstatus: draft\n---\n\n" +
			"[" + original + "](/" + original + ".md) の置き換え案: 返品を除いた受注合計。\n",
	})

	// Before the ruling: the agent's own draft reads back as unverified,
	// which is how it knows nothing has happened yet.
	drafted := conceptOf(t, call(t, "get_concept", map[string]any{"id": replacement}))
	if drafted.Summary.Trust != domain.TrustUnverified {
		t.Errorf("a fresh draft reads as trust %q, want %q", drafted.Summary.Trust, domain.TrustUnverified)
	}
	if len(drafted.Observed.Verified) != 0 {
		t.Errorf("a fresh draft carries %d verifications", len(drafted.Observed.Verified))
	}

	// The link is two-way, so the human reviewing the draft reaches the
	// concept it would replace and the concept names its challenger.
	// Asked here the way the human surfaces ask it: the backlink listing
	// is REST, CLI and the web UI, and is deliberately not an MCP tool
	// (design doc 0067 §5.1) — the ruling belongs to the person, and so
	// does the question "what is waiting to replace this?".
	back, err := svc.SearchOrList(ctx, "", "", "", store.Filter{LinksTo: original}, 50)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, h := range back.Hits {
		if h.ID == replacement {
			found = true
		}
	}
	if !found {
		t.Errorf("the draft's body link did not make it findable from %s", original)
	}

	// The human rules. Confirming is not on this surface (design doc
	// 0076) — that is the point of the split, not a missing tool.
	if _, err := svc.Verify(ctx, replacement, human); err != nil {
		t.Fatal(err)
	}

	// And the agent sees it: same tool, same id, the ledger now naming
	// who confirmed it and when.
	promoted := conceptOf(t, call(t, "get_concept", map[string]any{"id": replacement}))
	if promoted.Summary.Trust != domain.TrustHuman {
		t.Errorf("after a human confirmed it the draft reads as trust %q, want %q",
			promoted.Summary.Trust, domain.TrustHuman)
	}
	last := promoted.Observed.LastVerified()
	if last == nil || last.By.Name != human.Name {
		t.Fatalf("the promotion is not visible in the ledger: %+v", promoted.Observed.Verified)
	}

	// And the memory of no. A rejection is a deletion carrying its reason
	// (design doc 0135), so what a curator turned down leaves every
	// listing — and the id stays open, because a recorded reason is
	// information rather than a wall.
	turned := run + "/metrics/revenue-gross"
	call(t, "put_concept", map[string]any{
		"id":       turned,
		"document": "---\ntype: Metric\ntitle: Gross revenue\n---\n\nOrder totals before refunds.\n",
	})
	if err := svc.Delete(ctx, turned, human, nil, "duplicate of the original"); err != nil {
		t.Fatal(err)
	}
	gone := answerOf[searchOut](t, call(t, "search_concepts", map[string]any{
		"query": "gross revenue", "prefixes": []string{run},
	}))
	for _, h := range gone.Hits {
		if h.ID == turned {
			t.Errorf("a rejected proposal is still in the results: %+v", gone.Hits)
		}
	}
	// The agent may propose there again — even from this surface, which
	// has no precondition to offer.
	call(t, "put_concept", map[string]any{
		"id":       turned,
		"document": "---\ntype: Metric\ntitle: Second try\n---\n\nThe same proposal again.\n",
	})
	again := conceptOf(t, call(t, "get_concept", map[string]any{"id": turned}))
	if !strings.Contains(again.Document, "Second try") {
		t.Errorf("the re-proposal did not land: %s", again.Document)
	}
	// The reason it was turned down is still readable, in the history the
	// §9 log publishes.
	revs, rerr := svc.Revisions(ctx, turned, 20)
	if rerr != nil {
		t.Fatal(rerr)
	}
	reasonKept := false
	for _, r := range revs {
		if r.Change == "reject" && r.Note == "duplicate of the original" {
			reasonKept = true
		}
	}
	if !reasonKept {
		t.Errorf("the reason is not in the history: %+v", revs)
	}

	// A verified concept that was deleted is a different case, and it
	// stays guarded: the verification is the ruling there, not the
	// removal (design doc 0015 §3.1).
	if err := svc.Delete(ctx, replacement, human, nil, ""); err != nil {
		t.Fatal(err)
	}
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "put_concept", Arguments: map[string]any{
		"id":       replacement,
		"document": "---\ntype: Metric\ntitle: Over a verified tombstone\n---\n\nx\n",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Error("put_concept revived a verified tombstone")
	}
}

// TestIntegrationListWalksAFeedAndSearchRefusesTo is the split as an
// agent meets it (design doc 0096): a feed is walked a page at a time
// through list_concepts, and the same call aimed at search_concepts does
// not reach a handler at all.
//
// The cursor is the half that only a database can show — a page whose
// cursor did not resume would look identical to a short feed until the
// second call.
func TestIntegrationListWalksAFeedAndSearchRefusesTo(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := t.Context()
	s, err := store.New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{InsecureDev: true}
	svc := &service.Service{Store: s, Config: cfg, Log: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	srv := httptest.NewServer(httpauth.Middleware(cfg, Handler(svc, "test")))
	defer srv.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "host", Version: "0"}, nil).
		Connect(ctx, &mcp.StreamableClientTransport{Endpoint: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	human := domain.Actor{Kind: domain.ActorHuman, Name: "reviewer"}
	run := testdb.Unique(t, "mcpfeed")
	ids := []string{run + "/metrics/a", run + "/metrics/b", run + "/metrics/c"}
	defer func() {
		for _, id := range ids {
			_ = svc.Delete(context.Background(), id, human, nil, "")
		}
	}()
	// Three verified concepts, so the verified_at feed has something to
	// walk and a page of one leaves two behind it.
	for _, id := range ids {
		if _, _, _, err := svc.Put(ctx, &domain.Knowledge{
			Type: domain.TypeMetrics, ID: id, Title: "mcp feed " + id,
			Status: domain.StatusStable, Body: "受注合計。", CreatedBy: human,
		}, human, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Verify(ctx, id, human); err != nil {
			t.Fatal(err)
		}
	}

	page := func(t *testing.T, args map[string]any) listOut {
		t.Helper()
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "list_concepts", Arguments: args})
		if err != nil {
			t.Fatalf("list_concepts: %v", err)
		}
		if res.IsError {
			t.Fatalf("list_concepts failed: %+v", res.Content)
		}
		return answerOf[listOut](t, res)
	}

	first := page(t, map[string]any{
		"sort": "verified_at", "prefixes": []string{run}, "limit": 1,
	})
	if len(first.Hits) != 1 || first.Cursor == "" {
		t.Fatalf("a page of one out of three: %d hits, cursor %q", len(first.Hits), first.Cursor)
	}
	next := page(t, map[string]any{
		"sort": "verified_at", "prefixes": []string{run}, "limit": 1, "cursor": first.Cursor,
	})
	if len(next.Hits) != 1 {
		t.Fatalf("the cursor did not resume the feed: %d hits", len(next.Hits))
	}
	if next.Hits[0].ID == first.Hits[0].ID {
		t.Errorf("the second page repeats the first: %s", next.Hits[0].ID)
	}

	// And the tool that ranks does not list. The refusal comes from
	// schema validation rather than from the handler, which is what the
	// split bought: the agent is told before the call costs anything.
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "search_concepts", Arguments: map[string]any{"sort": "verified_at"},
	})
	if err == nil && !res.IsError {
		t.Fatal("search_concepts accepted a listing call")
	}
	said := ""
	if err != nil {
		said = err.Error()
	} else {
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				said += tc.Text
			}
		}
	}
	if !strings.Contains(said, "sort") {
		t.Errorf("the refusal does not name the argument that does not belong: %q", said)
	}

	// An empty query with a source filter is the other way this tool
	// could quietly become a listing: SearchOrList routes that shape to
	// s.list before it ever checks that a search needs a query (0096
	// §5 put the reverse lookup on list_concepts, which has a cursor
	// parameter and a listing-sized limit — this schema has neither).
	// The handler has to refuse it itself, because no JSON Schema shape
	// says "query required only when source is empty" (issue #602).
	res, err = cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "search_concepts", Arguments: map[string]any{"query": "", "source": "https://example.com/doc"},
	})
	if err == nil && !res.IsError {
		t.Fatal("search_concepts with only a source filter answered as a listing instead of refusing")
	}
	said = ""
	if err != nil {
		said = err.Error()
	} else {
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				said += tc.Text
			}
		}
	}
	if !strings.Contains(said, "list_concepts") {
		t.Errorf("the refusal does not point at the tool that does this: %q", said)
	}
}
