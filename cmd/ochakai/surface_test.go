package main

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	yaml "go.yaml.in/yaml/v3"

	"github.com/na0fu3y/ochakai/internal/mcpserver"
	"github.com/na0fu3y/ochakai/internal/service"
)

// What ochakai costs the person using it is its surface — the endpoints
// they can call, the tool schemas that eat their agent's context, the
// commands they have to learn — not the lines behind it. Nothing counted
// that surface, so it could grow one reasonable-looking addition at a
// time and no diff ever said so.
//
// [docs/surface.md] is where it is counted. This test reads the three
// enumerable surfaces back out of the code and fails when the document
// and the build disagree, so growing the surface means editing a document
// whose whole subject is how big the surface is (design doc 0035: check
// the invariant from outside rather than trust the convention).
//
// The check holds the arithmetic, not the judgment. Whether an addition
// earned its place is the reviewer's, and docs/surface.md says what to
// ask.
const surfaceDoc = "../../docs/surface.md"

// A heading opens a counted surface: "## REST (19)". The number is
// redundant with the list under it, deliberately — it is the line a
// reader notices moving.
var surfaceHeadingRe = regexp.MustCompile(`^## ([A-Z]+) \((\d+)\)$`)

// An item is a whole line of backticked name, so prose that happens to
// mention `get_context` is not mistaken for an entry.
var surfaceItemRe = regexp.MustCompile("^- `([^`]+)`$")

// surfaceSection is one counted surface as docs/surface.md declares it.
type surfaceSection struct {
	declared int      // the count in the heading
	items    []string // the list under it, in document order
}

func surfaceSections(t *testing.T) map[string]surfaceSection {
	t.Helper()
	content, err := os.ReadFile(surfaceDoc)
	if err != nil {
		t.Fatalf("read %s: %v", surfaceDoc, err)
	}
	sections := map[string]surfaceSection{}
	name := ""
	for _, line := range strings.Split(string(content), "\n") {
		if m := surfaceHeadingRe.FindStringSubmatch(line); m != nil {
			if _, dup := sections[m[1]]; dup {
				t.Fatalf("%s: two %s sections", surfaceDoc, m[1])
			}
			count, err := strconv.Atoi(m[2])
			if err != nil {
				t.Fatalf("%s: heading %q: %v", surfaceDoc, line, err)
			}
			name = m[1]
			sections[name] = surfaceSection{declared: count}
			continue
		}
		if strings.HasPrefix(line, "## ") {
			name = "" // an uncounted section: prose, not a surface
			continue
		}
		if m := surfaceItemRe.FindStringSubmatch(line); m != nil && name != "" {
			s := sections[name]
			s.items = append(s.items, m[1])
			sections[name] = s
		}
	}
	return sections
}

// compareSurface reports the document against what the build actually
// offers. want is sorted, so the failure can print the section as it
// should read rather than leaving the author to diff two lists by eye.
func compareSurface(t *testing.T, name string, want []string) {
	t.Helper()
	section, ok := surfaceSections(t)[name]
	if !ok {
		t.Fatalf("%s has no %q section; every surface ochakai offers is counted there", surfaceDoc, name)
	}
	sort.Strings(want)
	if section.declared != len(want) || !equalStrings(section.items, want) {
		block := &strings.Builder{}
		fmt.Fprintf(block, "## %s (%d)\n\n", name, len(want))
		for _, item := range want {
			fmt.Fprintf(block, "- `%s`\n", item)
		}
		t.Errorf(`%s's %s section does not match the build.

It lists %d (heading says %d); the build offers %d. The surface is what
somebody using ochakai pays for, so it is counted in one place and the
count moves in the diff. If this addition is meant, update the section
and say in the PR which of docs/surface.md's three questions it answers.

The section should read:

%s`, surfaceDoc, name, len(section.items), section.declared, len(want), block)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// REST is the only contract; the other three surfaces are its clients
// (design doc 0015 §2), so its operations are read from the contract
// itself rather than from the router.
func TestSurfaceDocCountsRESTOperations(t *testing.T) {
	content, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatalf("read openapi.yaml: %v", err)
	}
	var spec struct {
		Paths map[string]map[string]yaml.Node `yaml:"paths"`
	}
	if err := yaml.Unmarshal(content, &spec); err != nil {
		t.Fatalf("parse openapi.yaml: %v", err)
	}
	methods := map[string]bool{"get": true, "put": true, "post": true, "delete": true, "patch": true}
	var ops []string
	for path, item := range spec.Paths {
		for method := range item {
			if methods[method] {
				ops = append(ops, strings.ToUpper(method)+" "+path)
			}
		}
	}
	if len(ops) == 0 {
		t.Fatal("no operations found in openapi.yaml: this check now guards nothing")
	}
	compareSurface(t, "REST", ops)
}

// Tool schemas are spent out of the agent's context window, which is why
// the tool count is a budget and not a mirror of REST (design doc 0015
// §3.1). Read from a running server: what an agent sees is what it
// offers, not what the source appears to register.
func TestSurfaceDocCountsMCPTools(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	srv := httptest.NewServer(mcpserver.Handler(&service.Service{}, "test"))
	defer srv.Close()

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "surface-test", Version: "1"}, nil).
		Connect(ctx, &mcp.StreamableClientTransport{Endpoint: srv.URL}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = cs.Close() }()

	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	tools := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		tools = append(tools, tool.Name)
	}
	if len(tools) == 0 {
		t.Fatal("the server offered no tools: this check now guards nothing")
	}
	compareSurface(t, "MCP", tools)
}

// The CLI is the completeness surface (design doc 0015 §3.1), so it is
// the one expected to grow with REST — counted for the same reason, and
// because every command is something a reader has to learn. `serve`,
// `serve-ui`, `version` and `help` are how the binary is run rather than
// things it knows, and are not counted.
func TestSurfaceDocCountsCLICommands(t *testing.T) {
	commands := make([]string, 0, len(clientCommands))
	for name := range clientCommands {
		commands = append(commands, "ochakai "+name)
	}
	if len(commands) == 0 {
		t.Fatal("no client commands: this check now guards nothing")
	}
	compareSurface(t, "CLI", commands)
}
