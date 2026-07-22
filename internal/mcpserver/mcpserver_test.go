package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

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

// TestSearchQueryNotRequired pins the search_knowledge input schema:
// query must stay optional so sort="verified_at" calls can omit it
// (the handler rejects the combination of both).
func TestSearchQueryNotRequired(t *testing.T) {
	cs := connect(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range res.Tools {
		if tool.Name != "search_knowledge" {
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
				t.Errorf("search_knowledge schema requires %q; it must stay optional for sort mode", r)
			}
		}
		return
	}
	t.Fatal("search_knowledge tool not found")
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
		"search_knowledge": {"default 10", "max 50", "default 100", "max 1000"},
		"get_context":      {"default 5", "max 20"},
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

// TestSearchSortValidation mirrors the CLI and REST rules: a search query
// combined with a sort mode is an error (not silently ignored), and an
// unknown sort is rejected — for verified_at, usage, and failed.
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
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
				Name: "search_knowledge", Arguments: c.args,
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
		{"ochakai://overview", "overview", true}, // root-level ids are entries too
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

// TestResourceTemplateAdvertised pins that entries are addressable as MCP
// resources via the ochakai:// template — and that we advertise only the
// template, not an enumeration of every entry (resources/list stays empty).
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
		"search_knowledge":    {readOnly: true},
		"get_context":         {readOnly: true},
		"get_knowledge":       {readOnly: true},
		"get_attachment":      {readOnly: true},
		"get_knowledge_usage": {readOnly: true},
		"compile_sql":         {readOnly: true},
		"create_knowledge":    {destructive: &no},
		"update_knowledge":    {destructive: &no},
		"delete_knowledge":    {destructive: &yes},
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
// that is not a valid entry id and an unknown outcome are tool errors
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
