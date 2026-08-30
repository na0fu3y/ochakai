package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/na0fu3y/ochakai/internal/config"
	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/httpauth"
	"github.com/na0fu3y/ochakai/internal/service"
)

// answerOf decodes a tool result the way an agent reads one, and holds
// the result to carrying exactly one copy of itself (design doc 0103):
// JSON in a text block, and no structuredContent beside it saying the
// same thing again. Every test that reads a result goes through here, so
// the day something starts sending two the assertion is already
// everywhere rather than in one test somebody has to remember to write.
func answerOf[T any](t *testing.T, res *mcp.CallToolResult) T {
	t.Helper()
	if res.StructuredContent != nil {
		t.Fatalf("the result carried a second copy under structuredContent: %v", res.StructuredContent)
	}
	var text *mcp.TextContent
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			if text != nil {
				t.Fatalf("the result carried two text blocks:\n%s\n%s", text.Text, tc.Text)
			}
			text = tc
		}
	}
	if text == nil {
		t.Fatalf("the result carried no text block: %+v", res.Content)
	}
	var out T
	if err := json.Unmarshal([]byte(text.Text), &out); err != nil {
		t.Fatalf("decoding %T from the result: %v\n%s", out, err, text.Text)
	}
	return out
}

func connect(t *testing.T) *mcp.ClientSession {
	t.Helper()
	return connectWith(t, nil)
}

// connectWith is connect against a deployment of a stated posture — the
// config is what decides which tools get registered (design doc 0040
// §2.3), so a test about a posture needs to set it.
func connectWith(t *testing.T, cfg *config.Config) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()
	if _, err := newServer(&service.Service{Config: cfg}, "test", RetiredToolNames).Connect(ctx, st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// TestTheSchemaSaysWhichToolTakesWhat pins what the split bought
// (design doc 0096): the rules that were prose in one schema are the
// shape of two. search_concepts requires a query and knows nothing about
// sort or cursor, so a listing call sent to it is refused by schema
// validation before any handler runs — and list_concepts has no query to
// combine with a feed.
func TestTheSchemaSaysWhichToolTakesWhat(t *testing.T) {
	cs := connect(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	want := map[string]struct {
		required []string
		absent   []string
	}{
		"search_concepts": {required: []string{"query"}, absent: []string{"sort", "cursor"}},
		"list_concepts":   {absent: []string{"query"}},
	}
	for _, tool := range res.Tools {
		w, ok := want[tool.Name]
		if !ok {
			continue
		}
		delete(want, tool.Name)
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal %s schema: %v", tool.Name, err)
		}
		var schema struct {
			Required   []string                   `json:"required"`
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("unmarshal %s schema: %v", tool.Name, err)
		}
		for _, r := range w.required {
			if !slices.Contains(schema.Required, r) {
				t.Errorf("%s does not require %q, so the constraint is prose again", tool.Name, r)
			}
		}
		for _, a := range w.absent {
			if _, ok := schema.Properties[a]; ok {
				t.Errorf("%s still takes %q; that is the other tool's argument", tool.Name, a)
			}
		}
		// The filters are the half that is deliberately on both, and the
		// bytes the split cost (docs/surface.md, MCP-BYTES).
		for _, f := range []string{"types", "statuses", "trusts", "tags", "source", "prefixes"} {
			if _, ok := schema.Properties[f]; !ok {
				t.Errorf("%s lost the %q filter; both halves take the same filters", tool.Name, f)
			}
		}
	}
	for name := range want {
		t.Errorf("tool %s not found", name)
	}
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
	// One tool, one pair of numbers: the two contracts used to share a
	// description, which is the sentence design doc 0096 split apart.
	// "out-of-range is an error" pins the actual behavior (design doc
	// 0064): a prior version of this prose said the opposite — that an
	// out-of-range limit "falls back to the default" — and nothing here
	// caught it because the check only looked for the numbers, never the
	// fallback claim itself (issue #600).
	want := map[string][]string{
		"search_concepts": {"default 10", "max 50", "out-of-range is an error"},
		"list_concepts":   {"default 100", "max 1000", "out-of-range is an error"},
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
		if strings.Contains(desc, "falls back") {
			t.Errorf("%s limit description %q claims an out-of-range limit falls back to the "+
				"default; the service refuses it instead (design doc 0064)", tool.Name, desc)
		}
	}
	for name := range want {
		t.Errorf("tool %s not found", name)
	}
}

// TestFMIsNotOnTheToolSchemas keeps the frontmatter filter off MCP
// (design doc 0058 §2.2). It was here, and the description that made it
// usable had to name all eleven askable keys and explain why `fm.status`
// and `status=` answer differently — 944 characters, on each of two
// tools, or 14% of the schema text every connecting agent pays for.
// Nothing went through it: not the web UI, not the examples, not the
// hooks.
//
// The filter itself is untouched on REST (`?fm.<key>=`) and the CLI
// (`--fm key=value`), where the caller is a program somebody wrote or a
// person at a shell — both of whom already know which key they mean, and
// neither of whom is charged for the explanation.
func TestFMIsNotOnTheToolSchemas(t *testing.T) {
	cs := connect(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(res.Tools) == 0 {
		t.Fatal("the server offered no tools: this check now guards nothing")
	}
	for _, tool := range res.Tools {
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal %s schema: %v", tool.Name, err)
		}
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("unmarshal %s schema: %v", tool.Name, err)
		}
		if _, ok := schema.Properties["fm"]; ok {
			t.Errorf("%s exposes fm: a frontmatter filter is REST and CLI vocabulary, "+
				"and its schema costs every agent that connects (design doc 0058 §2.2)", tool.Name)
		}
	}
}

// TestArrayFiltersAreNamedInThePlural pins the one naming rule this
// surface has that REST and the CLI do not. A filter here is a JSON array
// of values to OR, so its name says so; REST and the CLI spell the same
// filters singular (type, status, tag, prefix, trust) because they repeat
// a parameter instead. trust was the one that broke it — it stood beside
// types, statuses, tags and prefixes as an array and called itself
// singular, which no number in docs/surface.md moved, since field names
// are counted by no dimension.
//
// The assertion is the exact set rather than "ends in s": status ends in
// one and is singular, so the cheap heuristic would wave through the
// regression this exists to catch.
func TestArrayFiltersAreNamedInThePlural(t *testing.T) {
	want := map[string][]string{
		"search_concepts": {"prefixes", "statuses", "tags", "trusts", "types"},
		"list_concepts":   {"prefixes", "statuses", "tags", "trusts", "types"},
	}
	cs := connect(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	seen := map[string]bool{}
	for _, tool := range res.Tools {
		expect, ok := want[tool.Name]
		if !ok {
			continue
		}
		seen[tool.Name] = true
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal %s schema: %v", tool.Name, err)
		}
		// "type" is a string or a list of them, so it is read loosely: a
		// property that can be an array at all is one whose value the
		// caller writes as a list.
		var schema struct {
			Properties map[string]struct {
				Type json.RawMessage `json:"type"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("unmarshal %s schema: %v", tool.Name, err)
		}
		var got []string
		for name, prop := range schema.Properties {
			if strings.Contains(string(prop.Type), `"array"`) {
				got = append(got, name)
			}
		}
		slices.Sort(got)
		if !slices.Equal(got, expect) {
			t.Errorf("%s takes arrays %v, want %v — a filter this surface reads as a "+
				"list of values is named in the plural, the way types/statuses/tags/prefixes are",
				tool.Name, got, expect)
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("%s is not offered: this check now guards less than it says", name)
		}
	}
}

// TestSortValidation mirrors the CLI and REST rules: an unknown sort is
// rejected rather than guessed at, an empty search is a tool error and
// not a Postgres 500, and a listing with neither a feed nor a reverse
// lookup says which of the two it needed.
//
// The combination that used to be checked here — a query beside a sort —
// cannot be expressed any more: the tools that take them are different
// tools (design doc 0096), and the schema refuses it a step earlier.
func TestSortValidation(t *testing.T) {
	cs := connect(t)
	cases := []struct {
		name       string
		tool       string
		args       map[string]any
		wantSubstr string
	}{
		{"invalid sort", "list_concepts", map[string]any{"sort": "created_at"}, "invalid sort"},
		{"neither feed nor source", "list_concepts", map[string]any{}, "needs sort"},
		{"empty query", "search_concepts", map[string]any{"query": ""}, "needs a query"},
		{"whitespace query", "search_concepts", map[string]any{"query": " \t"}, "needs a query"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
				Name: c.tool, Arguments: c.args,
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

// TestToolAnnotations pins the auto-approval hints: readers are read-only
// and writes are non-destructive (history is kept as revisions). Nothing
// here is destructive — the one tool that was, delete_concept, left the
// surface with design doc 0076. MCP clients gate auto-approval on these,
// so they must be exact.
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
	no := false
	want := map[string]ann{
		"search_concepts": {readOnly: true},
		"get_concept":     {readOnly: true},
		"get_file":        {readOnly: true},
		"put_concept":     {destructive: &no},
		"report_outcome":  {destructive: &no},
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

// TestConceptHint pins the habit the server hands every host, riding on
// the answer to every get_concept — the point where knowledge is
// delivered (design docs 0069, 0108). Claude Code sites can install it as
// a Stop hook; a browser-based host has no shell, so this string is the
// only copy it will ever see.
//
// The habit has to be callable, not merely mentioned. This asked for
// "rejected" alone for as long as the hint spelled the filter
// `statuses=["rejected"]` — the value's own retirement as a status
// (design doc 0043 §3.3) left that call answering 400, and a host with no
// other copy of the habit had no way to recover. So the filter is pinned
// by the spelling searchIn accepts, and the status vocabulary is named as
// what the hint must not reach for.
func TestConceptHint(t *testing.T) {
	for _, want := range []string{"report_outcome", "put_concept"} {
		if !strings.Contains(conceptHint, want) {
			t.Errorf("hint does not mention %q: %s", want, conceptHint)
		}
	}
	// A rejection is a deletion (design doc 0135), so there is no filter
	// left to teach and no listing that could answer one. The refusal an
	// agent meets when it writes the id again is what carries the ruling.
	for _, gone := range []string{"rejected=true", "statuses"} {
		if strings.Contains(conceptHint, gone) {
			t.Errorf("the hint teaches a filter that no longer exists (%q): %s", gone, conceptHint)
		}
	}

	// The linking half, pinned the same way: the hint teaches a spelling,
	// so the spelling is run through the parser that has to accept it
	// rather than eyeballed. A hint carrying a link form design doc 0024
	// does not recognize would teach an agent to write drafts that stay
	// orphans — the exact failure the sentence is there to prevent, and
	// one no reader of the string would notice.
	links := domain.LinksFromBody("drafts/example", conceptHint)
	if len(links) != 1 || links[0].Target != "metrics/revenue" {
		t.Errorf("the hint's example link does not parse as a link to metrics/revenue: %v (hint: %s)",
			links, conceptHint)
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
		// Reviving a curated tombstone is the third way to overwrite a
		// ruling, and an agent that only learns of it from an error has
		// already written the draft (design doc 0067 §5.1). Deleting one
		// was the second until design doc 0076 took the tool away.
		//
		// A rejected id is not one of these and the description has to say
		// so: design doc 0135 §3 made the reason a log entry rather than a
		// wall, and this test pinned the opposite claim for a release —
		// which is how the wrong sentence reached every surface at once.
		"put_concept": {"deleted can be reused", "verified or deprecated", "different id",
			"a log entry rather than a wall", "report_outcome failed"},
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

// Both lookups design doc 0037 adds ride on list_concepts rather than on
// tools of their own — tool count is a budget (0015 §2), and 0096 spent
// one of it on the split and nothing else. An argument an agent cannot
// see is an argument it will not use, so the schema has to carry both,
// and the descriptions have to say what they match.
func TestListToolOffersTheStaleFeedAndSourceLookup(t *testing.T) {
	cs := connect(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range res.Tools {
		if tool.Name != "list_concepts" {
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
			t.Fatal("list_concepts has no source argument")
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
	t.Fatal("list_concepts is gone")
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
	want := map[string]bool{"search_concepts": true, "put_concept": true}
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
		for _, retired := range []string{"Semantic Model", "Golden Query", "Playbook", "API Endpoint"} {
			if strings.Contains(text, retired) {
				t.Errorf("%s still recommends %q, retired by design doc 0038 or 0063", tool.Name, retired)
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
	srv := newServer(&service.Service{}, "test", RetiredToolNames)
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
	// The six the surface carries. delete_concept and get_concept_usage
	// left with design doc 0076 (a ruling and a curator's number), and
	// get_context left with 0108 — the pack retired from every surface,
	// and an agent's read is search_concepts → get_concept, with the
	// pack's reverse hop arriving as linked_from on the concept itself
	// (design doc 0106). list_concepts is 0096's split off
	// search_concepts, which bought no capability.
	want := []string{
		"search_concepts", "list_concepts", "get_concept", "put_concept",
		"report_outcome", "get_file",
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
