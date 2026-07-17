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

// TestSearchRejectsQueryWithSort mirrors the CLI and REST rule: a search
// query combined with sort=verified_at is an error, not silently ignored.
func TestSearchRejectsQueryWithSort(t *testing.T) {
	cs := connect(t)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "search_knowledge",
		Arguments: map[string]any{"sort": "verified_at", "query": "revenue"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected a tool error for query combined with sort")
	}
	text := ""
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text += tc.Text
		}
	}
	if !strings.Contains(text, "cannot be combined") {
		t.Errorf("error %q does not mention the combination rule", text)
	}
}
