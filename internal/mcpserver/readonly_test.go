package mcpserver

import (
	"context"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/na0fu3y/ochakai/internal/config"
	"github.com/na0fu3y/ochakai/internal/service"
)

// toolNames lists what a server offers, as a client sees it.
func toolNames(t *testing.T, cfg *config.Config) []string {
	t.Helper()
	srv := httptest.NewServer(Handler(&service.Service{Config: cfg}, "test"))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "1"}, nil).
		Connect(ctx, &mcp.StreamableClientTransport{Endpoint: srv.URL}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })

	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return names
}

// A read-only deployment does not offer the write tools at all (design
// doc 0040 §2.3). Offering one that always fails would spend the agent's
// context on a schema and then refuse the call.
func TestReadOnlyServerOffersNoWriteTools(t *testing.T) {
	got := toolNames(t, &config.Config{ReadOnly: true})
	for _, w := range writeTools {
		for _, name := range got {
			if name == w {
				t.Errorf("read-only server still offers %q", w)
			}
		}
	}
	if len(got) == 0 {
		t.Fatal("read-only server offers no tools at all; it should still serve reads")
	}
	for _, want := range []string{"search_concepts", "get_concept"} {
		found := false
		for _, name := range got {
			if name == want {
				found = true
			}
		}
		if !found {
			t.Errorf("read-only server dropped the read tool %q; it offers %v", want, got)
		}
	}
}

// The removal must be exactly the write tools — a writable server minus
// the read-only one is writeTools and nothing else. Stated as a
// difference so that adding a tool to either side without deciding which
// kind it is fails here.
func TestReadOnlyRemovesExactlyTheWriteTools(t *testing.T) {
	writable := toolNames(t, &config.Config{})
	readonly := toolNames(t, &config.Config{ReadOnly: true})

	keep := map[string]bool{}
	for _, n := range readonly {
		keep[n] = true
	}
	var removed []string
	for _, n := range writable {
		if !keep[n] {
			removed = append(removed, n)
		}
	}
	sort.Strings(removed)
	want := append([]string(nil), writeTools...)
	sort.Strings(want)
	if strings.Join(removed, ",") != strings.Join(want, ",") {
		t.Errorf("read-only removed %v, want exactly %v", removed, want)
	}
}
