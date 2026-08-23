package mcpserver

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/na0fu3y/ochakai/internal/config"
)

// A listing says how long it holds, and a knowledge read does not
// (design doc 0118 §7). Both halves matter: the SDK fills this field in on
// the way out whether or not ochakai decides it, so "we set no TTL" is
// not a state the wire has — it is the wire saying "immediately stale".
func TestAListingSaysHowLongItHolds(t *testing.T) {
	cs := connect(t)
	ctx := t.Context()

	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if want := int(listTTL.Milliseconds()); tools.TTLMs != want {
		t.Errorf("tools/list TTLMs = %d, want %d", tools.TTLMs, want)
	}
	// "public" is the SDK's default and the right answer, so this pins
	// it rather than setting it: the list is a property of the
	// deployment's posture, not of who asked — every caller of one
	// deployment is told the same six tools, and authorization happens
	// per call (design doc 0109). An intermediary may serve it to
	// anyone.
	if tools.CacheScope != "public" {
		t.Errorf("tools/list CacheScope = %q, want %q", tools.CacheScope, "public")
	}

	templates, err := cs.ListResourceTemplates(ctx, nil)
	if err != nil {
		t.Fatalf("resources/templates/list: %v", err)
	}
	if want := int(listTTL.Milliseconds()); templates.TTLMs != want {
		t.Errorf("resources/templates/list TTLMs = %d, want %d", templates.TTLMs, want)
	}

	resources, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("resources/list: %v", err)
	}
	if want := int(listTTL.Milliseconds()); resources.TTLMs != want {
		t.Errorf("resources/list TTLMs = %d, want %d", resources.TTLMs, want)
	}
}

// The list a read-only deployment answers is a different list (four
// tools, not six), and it holds for just as long: what makes a listing
// cacheable is that the process cannot change it, and the posture is
// fixed at startup like everything else here.
func TestAReadOnlyListingHoldsToo(t *testing.T) {
	cs := connectWith(t, &config.Config{ReadOnly: true})
	tools, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if want := int(listTTL.Milliseconds()); tools.TTLMs != want {
		t.Errorf("read-only tools/list TTLMs = %d, want %d", tools.TTLMs, want)
	}
}

// A read of knowledge is not a listing, and must keep saying zero — a
// client that cached a concept would show a reader a document the base
// no longer holds. The middleware is exercised directly because the
// distinction it draws is between result types, and that is the thing
// worth pinning: a later hand adding ReadResourceResult to the switch is
// exactly the mistake this catches.
func TestAKnowledgeReadHoldsForNoTime(t *testing.T) {
	var got mcp.Result
	h := answerHowLongAListingHolds(listTTL)(
		func(context.Context, string, mcp.Request) (mcp.Result, error) { return got, nil })

	got = &mcp.ReadResourceResult{}
	res, err := h(t.Context(), "resources/read", &mcp.ReadResourceRequest{})
	if err != nil {
		t.Fatalf("resources/read: %v", err)
	}
	if ttl := res.(*mcp.ReadResourceResult).TTLMs; ttl != 0 {
		t.Errorf("resources/read TTLMs = %d, want 0", ttl)
	}
}

// A page of a listing is a place in a walk, not a whole answer, so it
// carries no lifetime. ochakai's listings fit in one page, which is why
// this is a unit test rather than something a client can reach.
func TestAPagedListingHoldsForNoTime(t *testing.T) {
	var got mcp.Result
	h := answerHowLongAListingHolds(listTTL)(
		func(context.Context, string, mcp.Request) (mcp.Result, error) { return got, nil })

	got = &mcp.ListToolsResult{NextCursor: "more"}
	res, err := h(t.Context(), "tools/list", &mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if ttl := res.(*mcp.ListToolsResult).TTLMs; ttl != 0 {
		t.Errorf("a paged tools/list TTLMs = %d, want 0", ttl)
	}
}
