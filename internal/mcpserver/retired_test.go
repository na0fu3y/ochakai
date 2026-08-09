package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/na0fu3y/ochakai/internal/service"
)

// connectRetiring is connect(t) for a server that retired the given
// names. The fixture is what keeps this mechanism working while
// RetiredToolNames is empty, which is most of the time: a window that
// only ever runs the release after somebody renames something is a
// window nobody can trust on the day they need it.
func connectRetiring(t *testing.T, retired ...RetiredToolName) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()
	if _, err := newServer(&service.Service{}, "test", retired).Connect(ctx, st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

var fixtureRetired = RetiredToolName{
	Old: "search_knowledge", Instead: "search_concepts", Release: "9.9.9",
}

// The half of design doc 0088 that costs nothing: a retired name is
// callable and never listed. If it were listed, every agent would spend
// context on a schema for a name that is about to stop existing — the
// price MCP's default-no is there to refuse (0067 §1).
func TestARetiredToolNameIsNotListed(t *testing.T) {
	cs := connectRetiring(t, fixtureRetired)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	found := false
	for _, tool := range res.Tools {
		if tool.Name == fixtureRetired.Old {
			t.Errorf("tools/list offers %q, which is retired: the window forwards calls, it does not advertise", tool.Name)
		}
		if tool.Name == fixtureRetired.Instead {
			found = true
		}
	}
	if !found {
		t.Fatalf("tools/list does not offer %q: this check now guards nothing", fixtureRetired.Instead)
	}
}

// The other half: the call works, and the answer says which name to use
// next time. The arguments here are deliberately invalid — the point is
// that the call reached search_concepts at all, and the validation error
// proves it did without needing a database.
func TestARetiredToolNameStillAnswers(t *testing.T) {
	cs := connectRetiring(t, fixtureRetired)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: fixtureRetired.Old, Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool under the retired name: %v", err)
	}
	if len(res.Content) == 0 {
		t.Fatal("the answer carries no content: the notice is missing")
	}
	first, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("the answer opens with %T, not the notice", res.Content[0])
	}
	for _, want := range []string{"search_knowledge", "search_concepts", "9.9.9", "next release"} {
		if !strings.Contains(first.Text, want) {
			t.Errorf("the notice %q does not name %q", first.Text, want)
		}
	}
	text := ""
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text += tc.Text
		}
	}
	if !strings.Contains(text, "needs a query") {
		t.Errorf("the call did not reach %s: %q", fixtureRetired.Instead, text)
	}
}

// A name nobody retired is still unknown. The middleware rewrites one
// map's worth of names and forwards everything else untouched, which is
// what stops it from becoming a place where typos quietly succeed.
func TestAnUnknownToolNameIsStillUnknown(t *testing.T) {
	cs := connectRetiring(t, fixtureRetired)
	_, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "search_wisdom", Arguments: map[string]any{},
	})
	if err == nil {
		t.Fatal("a tool that never existed was answered")
	}
	if !strings.Contains(err.Error(), "search_wisdom") {
		t.Errorf("error %q does not name the tool that was called", err)
	}
}

// Every live entry has to point at a tool that exists and must not
// shadow one. Both are ways of writing an entry that turns a rename into
// the outage it was supposed to prevent, and neither shows up until
// somebody calls the old name.
func TestRetiredToolNamesPointAtToolsThatExist(t *testing.T) {
	res, err := connect(t).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	live := make(map[string]bool, len(res.Tools))
	for _, tool := range res.Tools {
		live[tool.Name] = true
	}
	for _, r := range RetiredToolNames {
		if live[r.Old] {
			t.Errorf("%q is retired and also registered: a live tool is not a retired name", r.Old)
		}
		if !live[r.Instead] {
			t.Errorf("%q forwards to %q, which this server does not offer", r.Old, r.Instead)
		}
	}
}
