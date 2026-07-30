package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/na0fu3y/ochakai/internal/config"
	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/httpauth"
	"github.com/na0fu3y/ochakai/internal/service"
)

func connect(t *testing.T) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()
	if _, err := newServer(&service.Service{}, "test").Connect(ctx, st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// TestSearchQueryNotRequired pins the search_concepts input schema:
// query must stay optional so sort="verified_at" calls can omit it
// (the handler rejects the combination of both).
func TestSearchQueryNotRequired(t *testing.T) {
	cs := connect(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range res.Tools {
		if tool.Name != "search_concepts" {
			continue
		}
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal schema: %v", err)
		}
		var schema struct {
			Required []string `json:"required"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("unmarshal schema: %v", err)
		}
		for _, r := range schema.Required {
			if r == "query" {
				t.Errorf("search_concepts schema requires %q; it must stay optional for sort mode", r)
			}
		}
		return
	}
	t.Fatal("search_concepts tool not found")
}

// TestLimitContractsInSchema pins that the tool schemas document the
// limit defaults and maxima — MCP agents see only the schema, so the
// contract that openapi.yaml and the CLI help carry must live here too.
func TestLimitContractsInSchema(t *testing.T) {
	cs := connect(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	want := map[string][]string{
		"search_concepts": {"default 10", "max 50", "default 100", "max 1000"},
		"get_context":     {"default 5", "max 20"},
	}
	for _, tool := range res.Tools {
		substrs, ok := want[tool.Name]
		if !ok {
			continue
		}
		delete(want, tool.Name)
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal %s schema: %v", tool.Name, err)
		}
		var schema struct {
			Properties map[string]struct {
				Description string `json:"description"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("unmarshal %s schema: %v", tool.Name, err)
		}
		desc := schema.Properties["limit"].Description
		for _, s := range substrs {
			if !strings.Contains(desc, s) {
				t.Errorf("%s limit description %q does not mention %q", tool.Name, desc, s)
			}
		}
	}
	for name := range want {
		t.Errorf("tool %s not found", name)
	}
}

// TestFMSchemaListsTheAskableKeys holds the "fm" description to the
// vocabulary the server enforces (design doc 0047 §2). An agent sees only
// the schema, so a key promised here and refused by checkedFilter costs a
// tool call to find out — and a key OKF adds later is only askable once
// this text says so.
func TestFMSchemaListsTheAskableKeys(t *testing.T) {
	cs := connect(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	tools := map[string]bool{"search_concepts": true, "get_context": true}
	for _, tool := range res.Tools {
		if !tools[tool.Name] {
			continue
		}
		delete(tools, tool.Name)
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal %s schema: %v", tool.Name, err)
		}
		var schema struct {
			Properties map[string]struct {
				Description string `json:"description"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("unmarshal %s schema: %v", tool.Name, err)
		}
		desc := schema.Properties["fm"].Description
		for _, key := range domain.AskableFrontmatterKeys() {
			if !strings.Contains(desc, key) {
				t.Errorf("%s fm description does not name the askable key %q", tool.Name, key)
			}
		}
	}
	for name := range tools {
		t.Errorf("tool %s not found", name)
	}
}

// TestSearchSortValidation mirrors the CLI and REST rules: a search query
// combined with a sort mode is an error (not silently ignored), an
// unknown sort is rejected — for verified_at, usage, and failed — and
// omitting both query and sort is a tool error, not a Postgres 500.
func TestSearchSortValidation(t *testing.T) {
	cs := connect(t)
	cases := []struct {
		name       string
		args       map[string]any
		wantSubstr string
	}{
		{"verified_at with query", map[string]any{"sort": "verified_at", "query": "revenue"}, "cannot be combined"},
		{"usage with query", map[string]any{"sort": "usage", "query": "revenue"}, "cannot be combined"},
		{"failed with query", map[string]any{"sort": "failed", "query": "revenue"}, "cannot be combined"},
		{"invalid sort", map[string]any{"sort": "created_at"}, "invalid sort"},
		{"neither query nor sort", map[string]any{}, "needs a query"},
		{"whitespace query", map[string]any{"query": " \t"}, "needs a query"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
				Name: "search_concepts", Arguments: c.args,
			})
			if err != nil {
				t.Fatalf("CallTool: %v", err)
			}
			if !res.IsError {
				t.Fatalf("expected a tool error for %s", c.name)
			}
			text := ""
			for _, ct := range res.Content {
				if tc, ok := ct.(*mcp.TextContent); ok {
					text += tc.Text
				}
			}
			if !strings.Contains(text, c.wantSubstr) {
				t.Errorf("error %q does not mention %q", text, c.wantSubstr)
			}
		})
	}
}

// TestParseKnowledgeURI pins the URI parser feeding the resource
// template: the id is everything after the scheme (slashes are path
// separators), and malformed URIs are rejected before any store lookup.
func TestParseKnowledgeURI(t *testing.T) {
	cases := []struct {
		uri string
		id  string
		ok  bool
	}{
		{"ochakai://metrics/revenue", "metrics/revenue", true},
		{"ochakai://queries/sales/top-customers", "queries/sales/top-customers", true},
		{"ochakai://tables/GA_sessions_2017", "tables/GA_sessions_2017", true},
		{"ochakai://overview", "overview", true}, // root-level ids are concepts too
		{"ochakai://", "", false},                // empty id
		{"ochakai://metrics/", "", false},        // empty segment
		{"ochakai:///revenue", "", false},        // empty segment
		{"file:///metrics/revenue", "", false},
		{"metrics/revenue", "", false},
	}
	for _, c := range cases {
		id, ok := parseKnowledgeURI(c.uri)
		if id != c.id || ok != c.ok {
			t.Errorf("parseKnowledgeURI(%q) = (%q, %v), want (%q, %v)",
				c.uri, id, ok, c.id, c.ok)
		}
	}
}

// TestResourceTemplateAdvertised pins that concepts are addressable as MCP
// resources via the ochakai:// template — and that we advertise only the
// template, not an enumeration of every concept (resources/list stays empty).
func TestResourceTemplateAdvertised(t *testing.T) {
	cs := connect(t)
	ctx := context.Background()

	tmpls, err := cs.ListResourceTemplates(ctx, nil)
	if err != nil {
		t.Fatalf("ListResourceTemplates: %v", err)
	}
	if len(tmpls.ResourceTemplates) != 1 {
		t.Fatalf("got %d resource templates, want 1", len(tmpls.ResourceTemplates))
	}
	rt := tmpls.ResourceTemplates[0]
	// {+id} (reserved expansion) is what lets the slash-separated id match;
	// a plain {id} would stop at the first slash.
	if rt.URITemplate != "ochakai://{+id}" {
		t.Errorf("URITemplate = %q, want ochakai://{+id}", rt.URITemplate)
	}
	if rt.MIMEType != "text/markdown" {
		t.Errorf("MIMEType = %q, want text/markdown", rt.MIMEType)
	}

	res, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(res.Resources) != 0 {
		t.Errorf("got %d static resources, want 0 (discovery is via tools, not enumeration)", len(res.Resources))
	}
}

// TestReadResourceRejectsMalformedURI checks the resource handler refuses a
// URI that matches the template shape but has an empty segment, returning
// not-found before it ever reaches the store.
func TestReadResourceRejectsMalformedURI(t *testing.T) {
	cs := connect(t)
	for _, uri := range []string{"ochakai://metrics/", "ochakai:///revenue", "ochakai://"} {
		_, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uri})
		if err == nil {
			t.Errorf("ReadResource(%q) succeeded, want not-found error", uri)
		}
	}
}

// TestToolAnnotations pins the auto-approval hints: readers are read-only,
// writes are non-destructive (history kept as revisions), only delete is
// destructive. MCP clients gate auto-approval on these, so they must be exact.
func TestToolAnnotations(t *testing.T) {
	cs := connect(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	// want[name] = expected (readOnly, destructiveSet, destructiveValue)
	type ann struct {
		readOnly    bool
		destructive *bool
	}
	yes, no := true, false
	want := map[string]ann{
		"search_concepts":   {readOnly: true},
		"get_context":       {readOnly: true},
		"get_concept":       {readOnly: true},
		"get_attachment":    {readOnly: true},
		"get_concept_usage": {readOnly: true},
		"put_concept":       {destructive: &no},
		"delete_concept":    {destructive: &yes},
	}
	seen := map[string]bool{}
	for _, tool := range res.Tools {
		w, ok := want[tool.Name]
		if !ok {
			continue
		}
		seen[tool.Name] = true
		if tool.Annotations == nil {
			t.Errorf("%s: no annotations", tool.Name)
			continue
		}
		if tool.Annotations.ReadOnlyHint != w.readOnly {
			t.Errorf("%s: ReadOnlyHint = %v, want %v", tool.Name, tool.Annotations.ReadOnlyHint, w.readOnly)
		}
		switch {
		case w.destructive == nil && tool.Annotations.DestructiveHint != nil:
			t.Errorf("%s: DestructiveHint set, want unset", tool.Name)
		case w.destructive != nil && tool.Annotations.DestructiveHint == nil:
			t.Errorf("%s: DestructiveHint unset, want %v", tool.Name, *w.destructive)
		case w.destructive != nil && *tool.Annotations.DestructiveHint != *w.destructive:
			t.Errorf("%s: DestructiveHint = %v, want %v", tool.Name, *tool.Annotations.DestructiveHint, *w.destructive)
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("tool %s not found", name)
		}
	}
}

// TestReportOutcomeValidation pins the tool's input checks: a target
// that is not a valid concept id and an unknown outcome are tool errors
// (not transport failures), and both fire before any store access.
func TestReportOutcomeValidation(t *testing.T) {
	cs := connect(t)
	cases := []struct {
		name       string
		args       map[string]any
		wantSubstr string
	}{
		{"bad target", map[string]any{"target": "queries/", "outcome": "worked"}, "invalid target"},
		{"bad outcome", map[string]any{"target": "queries/q", "outcome": "misleading"}, "invalid outcome"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
				Name: "report_outcome", Arguments: c.args,
			})
			if err != nil {
				t.Fatalf("CallTool: %v", err)
			}
			if !res.IsError {
				t.Fatal("expected a tool error")
			}
			text := ""
			for _, content := range res.Content {
				if tc, ok := content.(*mcp.TextContent); ok {
					text += tc.Text
				}
			}
			if !strings.Contains(text, c.wantSubstr) {
				t.Errorf("error %q does not mention %q", text, c.wantSubstr)
			}
		})
	}
}

// TestContextSchemaBoundsResponse pins the get_context input schema: an
// embedding host that never touches the tool arguments must still get a
// bounded response, so budget is documented with its default and
// min_score — usable only after calibration an agent cannot do — is off
// this surface entirely.
func TestContextSchemaBoundsResponse(t *testing.T) {
	cs := connect(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range res.Tools {
		if tool.Name != "get_context" {
			continue
		}
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal schema: %v", err)
		}
		var schema struct {
			Properties map[string]struct {
				Description string `json:"description"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("unmarshal schema: %v", err)
		}
		if _, ok := schema.Properties["min_score"]; ok {
			t.Error("get_context must not expose min_score: an agent cannot calibrate a score floor")
		}
		budget, ok := schema.Properties["budget"]
		if !ok {
			t.Fatal("get_context must expose budget")
		}
		// "the concepts plus the outline rows" is what the cap actually
		// binds (design doc 0033 §3.2); it is not the response body, and
		// an agent that reads it as one will size it wrong.
		for _, want := range []string{"12000", "outline", "the concepts plus the outline rows"} {
			if !strings.Contains(budget.Description, want) {
				t.Errorf("budget description %q does not mention %q", budget.Description, want)
			}
		}
		return
	}
	t.Fatal("get_context tool not found")
}

// TestContextHint pins the habit the server hands every host. Claude Code
// sites can install it as a Stop hook; a browser-based host has no shell,
// so this string is the only copy it will ever see.
func TestContextHint(t *testing.T) {
	plain := contextHint(0)
	for _, want := range []string{"report_outcome", "put_concept", "rejected"} {
		if !strings.Contains(plain, want) {
			t.Errorf("hint does not mention %q: %s", want, plain)
		}
	}
	if strings.Contains(plain, "outline") {
		t.Errorf("nothing was dropped; hint must not mention the outline: %s", plain)
	}
	truncated := contextHint(3)
	for _, want := range []string{"3 concepts", "outline", "get_concept"} {
		if !strings.Contains(truncated, want) {
			t.Errorf("truncated hint does not mention %q: %s", want, truncated)
		}
	}
}

// TestCuratedGuardIsAdvertised pins the half of the curated-write rule
// that agents can act on: the tool descriptions must say what to do
// instead, or an agent meets the refusal with no next move. The refusal
// itself is exercised in the service integration test.
func TestCuratedGuardIsAdvertised(t *testing.T) {
	cs := connect(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	want := map[string][]string{

		"delete_concept": {"verified, rejected, or deprecated", "erase the record of why"},
		// Reviving a curated tombstone is the third way to overwrite a
		// ruling, and an agent that only learns of it from an error has
		// already written the draft (design doc 0015 §3.1).
		"put_concept": {"deleted can be reused", "verified, rejected, deprecated", "different id",
			"verified, rejected, or deprecated", "report_outcome failed"},
	}
	for _, tool := range res.Tools {
		substrs, ok := want[tool.Name]
		if !ok {
			continue
		}
		delete(want, tool.Name)
		for _, s := range substrs {
			if !strings.Contains(tool.Description, s) {
				t.Errorf("%s description does not mention %q:\n%s", tool.Name, s, tool.Description)
			}
		}
	}
	for name := range want {
		t.Errorf("tool %s not found", name)
	}
}

// The budget governs the whole response or it governs nothing. A hit is
// a ranking — an id, a title and a score — and never a second copy of the
// concept: a concept pushed into outline for being too large would otherwise
// arrive in full through hits anyway, while the outline row told the
// agent to go fetch what it already had (design doc 0033).
func TestContextHitsCarryRankingNotKnowledge(t *testing.T) {
	body := strings.Repeat("x", 4000)
	hits := []domain.SearchHit{{
		Summary: domain.Summary{
			Type: domain.TypeComputations, ID: "queries/monthly-revenue",
			// A summary carries the display title already worked out
			// (design doc 0022), so a rank can read it straight off.
			Title: "monthly-revenue", Status: domain.StatusStable, Description: body,
		},
		Score: 0.9,
	}}
	out := contextOut{Hits: domain.ContextRanks(hits)}
	encoded, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), body) {
		t.Error("hits carried the body; the budget covers only concepts")
	}
	if strings.Contains(string(encoded), "SELECT 1") {
		t.Error("hits carried attrs; the budget covers only concepts")
	}
	got := out.Hits[0]
	if got.ID != "queries/monthly-revenue" || got.Score != 0.9 || got.Status != domain.StatusStable {
		t.Errorf("a hit must still rank and address the concept: %+v", got)
	}
	// Title falls back to the filename when the concept has none (design doc
	// 0022), so a hit is readable without fetching.
	if got.Title != "monthly-revenue" {
		t.Errorf("hit title = %q, want the display title", got.Title)
	}
}

// requestActor must prefer the headers of the call's own HTTP request
// over the session context: the context actor is captured at initialize
// and would misattribute every later call on a shared session.
func TestRequestActorUsesCallHeaders(t *testing.T) {
	cfg := &config.Config{InsecureDev: true}
	h := http.Header{}
	h.Set(httpauth.OnBehalfOfHeader, "human:tanaka@example.co.jp")
	req := &mcp.CallToolRequest{Extra: &mcp.RequestExtra{Header: h}}
	ctx := httpauth.WithActor(context.Background(),
		domain.Actor{Kind: domain.ActorHuman, Name: "initializer@example.co.jp"})

	actor, err := requestActor(ctx, cfg, req)
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Actor{Kind: domain.ActorHuman, Name: "tanaka@example.co.jp", Via: "human:anonymous"}
	if actor != want {
		t.Errorf("actor = %+v, want %+v", actor, want)
	}
}

// Without an HTTP request (in-process transports), the context actor is
// all there is — and correct, since such callers cannot share a session
// across identities.
func TestRequestActorFallsBackToContext(t *testing.T) {
	want := domain.Actor{Kind: domain.ActorProcess, Name: "sa@example.gserviceaccount.com"}
	ctx := httpauth.WithActor(context.Background(), want)
	for _, req := range []*mcp.CallToolRequest{
		{},
		{Extra: &mcp.RequestExtra{}},
	} {
		actor, err := requestActor(ctx, &config.Config{InsecureDev: true}, req)
		if err != nil {
			t.Fatal(err)
		}
		if actor != want {
			t.Errorf("actor = %+v, want %+v", actor, want)
		}
	}
}

// Both lookups design doc 0037 adds ride on search_concepts rather than
// on new tools — tool count is a budget (0015 §2). An argument an agent
// cannot see is an argument it will not use, so the schema has to carry
// both, and the descriptions have to say what they match.
func TestSearchToolOffersTheStaleFeedAndSourceLookup(t *testing.T) {
	cs := connect(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range res.Tools {
		if tool.Name != "search_concepts" {
			continue
		}
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal schema: %v", err)
		}
		var schema struct {
			Properties map[string]struct {
				Description string `json:"description"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("unmarshal schema: %v", err)
		}
		src, ok := schema.Properties["source"]
		if !ok {
			t.Fatal("search_concepts has no source argument")
		}
		if !strings.Contains(src.Description, "sources[].resource") {
			t.Errorf("the source argument does not say what it matches: %q", src.Description)
		}
		if !strings.Contains(schema.Properties["sort"].Description, "stale_after") {
			t.Errorf("sort does not offer stale_after: %q", schema.Properties["sort"].Description)
		}
		// The feed only pays off if the agent knows verifying will not
		// empty it (design doc 0037 §2.2).
		if !strings.Contains(tool.Description, "verifying alone does not clear this feed") {
			t.Error("the tool description no longer warns that verifying does not clear the stale feed")
		}
		return
	}
	t.Fatal("search_concepts is gone")
}

// The listing modes live in one list so no surface can offer a value
// another rejects (design doc 0037 §2.5).
func TestListSortsAreTheSameEverywhere(t *testing.T) {
	for _, want := range []string{"verified_at", "usage", "failed", "stale_after"} {
		if !domain.ValidListSort(want) {
			t.Errorf("domain.ListSorts is missing %q", want)
		}
	}
	if domain.ValidListSort("stale") {
		t.Error("ValidListSort accepts a value no surface implements")
	}
}

// TestToolSchemasCarryTheTypeVocabulary pins what an agent actually sees.
// The tool descriptions interpolate domain.TypesHint(), but the jsonschema
// struct tags cannot — they are compile-time constants — so their copy of
// the vocabulary drifts silently. It already had: before design doc 0038
// the search filters listed seven types, create listed eight, and the
// server's own instructions listed nine. An agent picks its --type value
// from these strings, so a missing type is a type nobody filters on and a
// retired one is a type nobody has (design doc 0038 §4.4).
func TestToolSchemasCarryTheTypeVocabulary(t *testing.T) {
	cs := connect(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	// Only the tools that take a type: the read filters and the writes.
	want := map[string]bool{"search_concepts": true, "get_context": true,
		"put_concept": true}
	seen := 0
	for _, tool := range res.Tools {
		if !want[tool.Name] {
			continue
		}
		seen++
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal %s schema: %v", tool.Name, err)
		}
		text := tool.Description + string(raw)
		for _, ty := range domain.Types {
			if !strings.Contains(text, string(ty)) {
				t.Errorf("%s never names the recommended type %q", tool.Name, ty)
			}
		}
		for _, retired := range []string{"Semantic Model", "Golden Query"} {
			if strings.Contains(text, retired) {
				t.Errorf("%s still recommends %q, retired by design doc 0038", tool.Name, retired)
			}
		}
	}
	if seen != len(want) {
		t.Errorf("checked %d type-taking tools, want %d", seen, len(want))
	}
}

// serverSession returns the server's view of one connected client, so the
// initialize-time clientInfo can be read the way requestActor reads it.
func serverSession(t *testing.T, client *mcp.Implementation) *mcp.ServerSession {
	t.Helper()
	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()
	srv := newServer(&service.Service{}, "test")
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	cs, err := mcp.NewClient(client, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	for ss := range srv.Sessions() {
		return ss
	}
	t.Fatal("server has no session")
	return nil
}

// MCP asks every client to name itself and its version at initialize,
// which is SPEC §7's "<producer>/<version>" arriving for free — so an
// agent's writes say which agent made them without anyone adding a header
// (design doc 0052 §3.3).
func TestRequestActorTakesTheProducerFromClientInfo(t *testing.T) {
	ss := serverSession(t, &mcp.Implementation{Name: "claude-code", Version: "2026.07"})
	ctx := httpauth.WithActor(context.Background(),
		domain.Actor{Kind: domain.ActorHuman, Name: "tanaka@example.co.jp"})

	actor, err := requestActor(ctx, &config.Config{InsecureDev: true}, &mcp.CallToolRequest{Session: ss})
	if err != nil {
		t.Fatal(err)
	}
	if actor.Producer != "claude-code/2026.07" {
		t.Errorf("producer = %q, want it taken from clientInfo", actor.Producer)
	}
	// The identity is untouched: the producer says what wrote, never who.
	if actor.Kind != domain.ActorHuman || actor.Name != "tanaka@example.co.jp" {
		t.Errorf("actor = %+v, want the authenticated identity unchanged", actor)
	}
}

// The header is what this call declares; clientInfo is what opened the
// session. A host proxying several agents through one session tells them
// apart only by the header, so the session must not win.
func TestRequestActorPrefersTheProducerHeaderOverClientInfo(t *testing.T) {
	ss := serverSession(t, &mcp.Implementation{Name: "some-host", Version: "1.0"})
	h := http.Header{}
	h.Set(httpauth.ProducerHeader, "insightflow/1.4.0")

	actor, err := requestActor(context.Background(), &config.Config{InsecureDev: true},
		&mcp.CallToolRequest{Session: ss, Extra: &mcp.RequestExtra{Header: h}})
	if err != nil {
		t.Fatal(err)
	}
	if actor.Producer != "insightflow/1.4.0" {
		t.Errorf("producer = %q, want the call's own header", actor.Producer)
	}
}

// A clientInfo that does not fit SPEC §7's form is dropped, not refused:
// the client was asked for a name, not for provenance, so an unspellable
// version must not fail its tool call.
func TestUnspellableClientInfoIsDroppedNotRefused(t *testing.T) {
	ss := serverSession(t, &mcp.Implementation{Name: "some host", Version: ""})
	actor, err := requestActor(context.Background(), &config.Config{InsecureDev: true},
		&mcp.CallToolRequest{Session: ss})
	if err != nil {
		t.Fatalf("requestActor: %v", err)
	}
	if actor.Producer != "" {
		t.Errorf("producer = %q, want it dropped", actor.Producer)
	}
}

// One write face, and the branch inside it is about the id rather than
// about which tool the agent should have called (design doc 0046 §3.14).
// The two tools it replaces asked a question the document does not
// answer: a write states what the concept should say, and whether the id
// was already taken is not part of that statement (0043 §3.5).
func TestOneWriteFace(t *testing.T) {
	cs := connect(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range res.Tools {
		names[tool.Name] = true
	}
	for _, gone := range []string{"create_knowledge", "update_knowledge"} {
		if names[gone] {
			t.Errorf("%s is back; the write face is put_concept", gone)
		}
	}
	// The eight the surface carries (design doc 0046 §3.14, issue #272).
	// The three at the end were once slated to go, and the measurement
	// went the other way: they are the cheapest tools here, and the
	// saving was in the merge above.
	want := []string{
		"search_concepts", "get_context", "get_concept", "put_concept", "report_outcome",
		"delete_concept", "get_concept_usage", "get_attachment",
	}
	for _, w := range want {
		if !names[w] {
			t.Errorf("tool %s is missing", w)
		}
	}
	if len(names) != len(want) {
		t.Errorf("tools = %d, want %d: %v", len(names), len(want), names)
	}
}
