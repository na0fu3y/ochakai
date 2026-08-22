package mcpserver

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/config"
	"github.com/na0fu3y/ochakai/internal/service"
)

// statelessServer is the MCP endpoint as a deployment serves it, without
// the auth middleware in front: these tests are about the transport.
func statelessServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(Handler(&service.Service{Config: &config.Config{InsecureDev: true}}, "test"))
	t.Cleanup(srv.Close)
	return srv
}

// rpc posts one JSON-RPC message the way a client does and returns the
// response, decoded out of whichever framing came back — a stateless POST
// may answer as JSON or as a one-event stream, and this is not the test
// for which.
//
// method is the Mcp-Method header, which SEP-2243 requires on a POST from
// 2026-07-28 on so that a gateway can route without reading the body; the
// server refuses a request whose header and body disagree. Empty for a
// request written at an older version, which is what an older client
// sends.
//
// The response headers come back rather than the response itself: the
// body is read and closed here, and handing out a spent *http.Response
// invites a caller to treat it as live.
func rpc(t *testing.T, url, method, body string) (http.Header, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if method != "" {
		req.Header.Set("Mcp-Method", method)
		req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimPrefix(strings.TrimSpace(line), "data: ")
		if line == "" {
			continue
		}
		var msg map[string]any
		if json.Unmarshal([]byte(line), &msg) == nil && msg["jsonrpc"] != nil {
			return resp.Header, msg
		}
	}
	return resp.Header, nil
}

// The protocol ochakai speaks is the current one. It is not a free
// upgrade: the SDK serves 2026-07-28 only from a stateless handler and
// caps a stateful one at 2025-11-25, so this is the assertion that says
// the mode in Handler is the mode design doc 0118 decided on. A client
// asking by name is the honest question, because that is what a client
// that has moved does.
func TestMCPAnswersAtTheCurrentProtocolVersion(t *testing.T) {
	srv := statelessServer(t)
	_, msg := rpc(t, srv.URL, "server/discover", `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{`+
		`"io.modelcontextprotocol/protocolVersion":"2026-07-28",`+
		`"io.modelcontextprotocol/clientCapabilities":{},`+
		`"io.modelcontextprotocol/clientInfo":{"name":"probe","version":"1"}}}}`)
	if msg == nil {
		t.Fatal("server/discover returned no JSON-RPC message")
	}
	if e, ok := msg["error"]; ok {
		t.Fatalf("server/discover: %v", e)
	}
	result, ok := msg["result"].(map[string]any)
	if !ok {
		t.Fatalf("server/discover result = %v, want an object", msg["result"])
	}
	var versions []string
	for _, v := range result["supportedVersions"].([]any) {
		versions = append(versions, v.(string))
	}
	found := false
	for _, v := range versions {
		if v == "2026-07-28" {
			found = true
		}
	}
	if !found {
		t.Errorf("supportedVersions = %v, want 2026-07-28 among them (stateful handler? the SDK caps those at 2025-11-25)", versions)
	}
}

// No session to hand out, and so no session to lose: an instance that
// answers a call never learned anything from the one before it, which is
// what lets a deployment run more than one of them (design doc 0118 §2).
func TestMCPIssuesNoSession(t *testing.T) {
	srv := statelessServer(t)
	header, msg := rpc(t, srv.URL, "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"old-client","version":"1.0"}}}`)
	if msg == nil {
		t.Fatal("initialize returned no JSON-RPC message")
	}
	if e, ok := msg["error"]; ok {
		t.Fatalf("initialize: %v (an older client must still be served)", e)
	}
	if got := header.Get("Mcp-Session-Id"); got != "" {
		t.Errorf("Mcp-Session-Id = %q, want none", got)
	}
}

// The two verbs a session transport needs and a stateless one does not.
// They are 405 rather than 404 because the address is right and the verb
// is not, and a client reading the Allow header learns what to do.
func TestMCPRefusesTheSessionVerbs(t *testing.T) {
	srv := statelessServer(t)
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		req, err := http.NewRequest(method, srv.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Accept", "application/json, text/event-stream")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s /mcp = %d, want 405", method, resp.StatusCode)
		}
		if got := resp.Header.Get("Allow"); got != "POST" {
			t.Errorf("%s /mcp Allow = %q, want POST", method, got)
		}
	}
}
