package mcpserver

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/na0fu3y/ochakai/internal/config"
	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/httpauth"
	"github.com/na0fu3y/ochakai/internal/service"
	"github.com/na0fu3y/ochakai/internal/store"
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
	dbURL := os.Getenv("OCHAKAI_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("OCHAKAI_TEST_DATABASE_URL not set")
	}
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
	id := fmt.Sprintf("mcpit%d/delegation", time.Now().UnixNano())
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "create_knowledge",
		Arguments: map[string]any{"id": id, "document": "---\ntype: glossary\n---\n\nwritten on behalf of bob\n"},
	})
	if err != nil {
		t.Fatalf("create_knowledge: %v", err)
	}
	if res.IsError {
		t.Fatalf("create_knowledge failed: %+v", res.Content)
	}

	k, err := svc.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Actor{Kind: domain.ActorHuman, Name: "bob@example.co.jp", Via: "human:anonymous"}
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
