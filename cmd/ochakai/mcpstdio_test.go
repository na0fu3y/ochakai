package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/oauth2"
)

// remoteMCP starts an MCP server over streamable HTTP and returns its base
// URL and the names of the tools it offers. The bridge is supposed to be
// ignorant of what it carries, so the test asserts against whatever this
// server happens to expose rather than against ochakai's real tool set.
func remoteMCP(t *testing.T) (base string, tools []string) {
	t.Helper()
	s := mcp.NewServer(&mcp.Implementation{Name: "remote", Version: "test"}, nil)
	type args struct {
		Q string `json:"q" jsonschema:"a query"`
	}
	mcp.AddTool(s, &mcp.Tool{Name: "search_concepts", Description: "search"},
		func(ctx context.Context, _ *mcp.CallToolRequest, in args) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "hit:" + in.Q}}}, nil, nil
		})
	mcp.AddTool(s, &mcp.Tool{Name: "get_pack", Description: "context"},
		func(ctx context.Context, _ *mcp.CallToolRequest, in args) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "ctx:" + in.Q}}}, nil, nil
		})

	srv := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, nil))
	t.Cleanup(srv.Close)
	return srv.URL, []string{"get_pack", "search_concepts"}
}

// The bridge carries whatever the remote offers. It must not hold a copy
// of the tool list: what a client sees through it is what the server
// said, including a tool the bridge has never heard of (design doc
// 0039 §2.2, §3).
func TestBridgePassesMessagesThrough(t *testing.T) {
	base, want := remoteMCP(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// The client end of an in-memory pair stands in for the stdio client;
	// the bridge is handed the other end.
	clientSide, bridgeSide := mcp.NewInMemoryTransports()
	done := make(chan error, 1)
	go func() {
		done <- bridge(ctx, bridgeSide, &mcp.StreamableClientTransport{Endpoint: base + "/mcp"})
	}()

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil).
		Connect(ctx, clientSide, nil)
	if err != nil {
		t.Fatalf("connecting through the bridge: %v", err)
	}
	defer cs.Close()

	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list through the bridge: %v", err)
	}
	var got []string
	for _, tool := range res.Tools {
		got = append(got, tool.Name)
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("tools through the bridge = %v, remote offers %v", got, want)
	}

	// A call has to survive the round trip too, arguments and all.
	out, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search_concepts",
		Arguments: map[string]any{"q": "revenue"},
	})
	if err != nil {
		t.Fatalf("tools/call through the bridge: %v", err)
	}
	text, _ := out.Content[0].(*mcp.TextContent)
	if text == nil || text.Text != "hit:revenue" {
		t.Errorf("tool result through the bridge = %+v, want hit:revenue", out.Content[0])
	}
}

// In stdio mode stdout *is* the wire. One stray human-readable line
// breaks the session, so the bridge must put every diagnostic on stderr
// (design doc 0039 §2.4). Not parallel: it swaps the process's own
// stdin and stdout.
func TestBridgeStdoutCarriesOnlyProtocol(t *testing.T) {
	base, _ := remoteMCP(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origIn, origOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = inR, outW
	t.Cleanup(func() { os.Stdin, os.Stdout = origIn, origOut })

	done := make(chan error, 1)
	go func() {
		done <- bridge(ctx, &mcp.StdioTransport{}, &mcp.StreamableClientTransport{Endpoint: base + "/mcp"})
	}()

	send := func(msg string) {
		if _, err := io.WriteString(inW, msg+"\n"); err != nil {
			t.Errorf("writing to stdin: %v", err)
		}
	}
	send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`)
	send(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	send(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)

	// Read until the reply to id 2 arrives, checking every line on the way.
	lines := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(outR)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()

	sawList := false
	for !sawList {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("stdout closed before the tools/list reply")
			}
			if strings.TrimSpace(line) == "" {
				continue
			}
			var msg map[string]any
			if err := json.Unmarshal([]byte(line), &msg); err != nil {
				t.Fatalf("stdout carried a non-JSON line %q: %v", line, err)
			}
			if msg["jsonrpc"] != "2.0" {
				t.Fatalf("stdout carried a line that is not a JSON-RPC message: %q", line)
			}
			if id, ok := msg["id"].(float64); ok && id == 2 {
				if _, hasResult := msg["result"]; !hasResult {
					t.Fatalf("tools/list did not succeed through the bridge: %q", line)
				}
				sawList = true
			}
		case <-ctx.Done():
			t.Fatal("timed out waiting for the tools/list reply")
		}
	}

	inW.Close()
	outW.Close()
	cancel()
	<-done
}

// The bridge reaches a public deployment (design doc 0066 §3) from a
// machine whose gcloud cannot mint a token — reauthentication due, no
// account selected, a prompt nothing can answer. oauth2.Transport fails
// the request in that case; sending it bare is what lets the server be
// the one to say whether it wanted an identity.
func TestAuthedHTTPClientSendsBareWhenTheTokenWillNotMint(t *testing.T) {
	var sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := authedHTTPClient(brokenTokenSource{})
	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatalf("a token that would not mint failed the request: %v", err)
	}
	resp.Body.Close()
	if sawAuth != "" {
		t.Errorf("sent Authorization %q; want none", sawAuth)
	}
}

// And a token that mints still rides on the request.
func TestAuthedHTTPClientSendsAMintedToken(t *testing.T) {
	var sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := authedHTTPClient(oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "tok", TokenType: "Bearer"}))
	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if sawAuth != "Bearer tok" {
		t.Errorf("Authorization = %q, want %q", sawAuth, "Bearer tok")
	}
}

type brokenTokenSource struct{}

func (brokenTokenSource) Token() (*oauth2.Token, error) {
	return nil, errors.New("gcloud auth print-identity-token: ERROR: Reauthentication failed")
}
