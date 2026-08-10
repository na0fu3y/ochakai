package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The tool count is a budget, and it has been one since design doc 0015
// §3.1 — 0076 took two tools off this surface to protect it. What nobody
// counted is the bytes, and that is where the cost actually is: a client
// holds the tool list and the instructions for the whole conversation,
// so every character of prose here is paid for on every turn, by every
// agent, forever.
//
// Six tools were the budget while one tool's description grew larger
// than the two that were removed. So the bytes are counted too, against
// a ceiling declared beside the other nine in docs/surface.md
// (MCP-BYTES). Raising it is a decision said out loud, which is the same
// arrangement every other surface has.

// surfaceDoc is where the ceilings live, read the way
// cmd/ochakai/surface_test.go reads them.
const surfaceDoc = "../../docs/surface.md"

// The two lines declaring the ceiling and how much of it may sit
// unspent, read the way cmd/ochakai/surface_test.go reads DOC-LINES and
// DOC-LINES-SLACK: over the cap is a decision, and so far under it that
// the slack is gone is a ceiling somebody forgot to return.
var (
	mcpBytesCap   = regexp.MustCompile(`(?m)^- MCP-BYTES: (\d+)$`)
	mcpBytesSlack = regexp.MustCompile(`(?m)^- MCP-BYTES-SLACK: (\d+)$`)
)

func declaredCap(t *testing.T, content []byte, re *regexp.Regexp, name string) int {
	t.Helper()
	m := re.FindSubmatch(content)
	if m == nil {
		t.Fatalf("%s declares no %s: this check guards nothing", surfaceDoc, name)
	}
	n, err := strconv.Atoi(string(m[1]))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// residentBytes is what a client keeps for the whole conversation: the
// instructions, and every tool's name, description and input schema, as
// the wire carries them.
//
// Measured from a real session rather than from the source, for the
// reason design doc 0035 gives: the schema a client receives is
// generated from struct tags, and a count that read the tags would miss
// whatever the generator adds around them.
func residentBytes(t *testing.T) (total int, perTool map[string]int) {
	t.Helper()
	ctx := context.Background()
	cs := connect(t)
	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	perTool = map[string]int{}
	for _, tool := range res.Tools {
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal %s schema: %v", tool.Name, err)
		}
		n := len(tool.Name) + len(tool.Description) + len(schema)
		perTool[tool.Name] = n
		perTool[tool.Name+" (schema)"] = len(schema)
		total += n
	}
	total += len(instructions)
	return total, perTool
}

func TestMCPStaysUnderItsResidentByteCap(t *testing.T) {
	total, perTool := residentBytes(t)

	names := make([]string, 0, len(perTool))
	for name := range perTool {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return perTool[names[i]] > perTool[names[j]] })
	breakdown := &strings.Builder{}
	fmt.Fprintf(breakdown, "  %-16s %6d\n", "instructions", len(instructions))
	for _, name := range names {
		fmt.Fprintf(breakdown, "  %-24s %6d\n", name, perTool[name])
	}
	t.Logf("resident bytes: %d\n%s", total, breakdown)

	content, err := os.ReadFile(surfaceDoc)
	if err != nil {
		t.Fatalf("read %s: %v", surfaceDoc, err)
	}
	ceiling := declaredCap(t, content, mcpBytesCap, "MCP-BYTES")
	slack := declaredCap(t, content, mcpBytesSlack, "MCP-BYTES-SLACK")
	if total > ceiling {
		t.Errorf(`the MCP surface is %d resident bytes, over its cap of %d.

%s
Every one of these is held for the whole conversation, by every agent, on
every turn — the tool count says six either way. Going past the cap is a
decision: raise it in the same PR, having answered %s's three
questions, or find the prose that says twice what a schema already says.`,
			total, ceiling, breakdown, surfaceDoc)
	}
	if ceiling-total > slack {
		t.Errorf(`the MCP surface is %d resident bytes, %d under its cap of %d — more than the %d of slack declared.

%s
Trimming is worth as much as not growing, so the budget it earned goes
back: lower MCP-BYTES in the same PR.`, total, ceiling-total, ceiling, slack, breakdown)
	}
}

// The MCP bundle's manifest lists the tools for the desktop app to show
// before anyone installs it (packaging/mcpb/manifest.json). That list is
// a second copy of the tool names, in a file no compiler reads, so it
// drifts the moment a tool is added, renamed or retired — and it drifts
// into the one place a person looks *while deciding whether to install*.
//
// Read against a real session, like the byte cap above: the names are
// what the wire carries, not what the source says.
//
// The descriptions are deliberately not compared. The manifest's are
// display copy for a human choosing a bundle; the server's are written
// for an agent and paid for out of its context window (MCP-BYTES). Two
// audiences, two sentences, one set of names.
func TestMCPBManifestListsTheToolsTheServerServes(t *testing.T) {
	raw, err := os.ReadFile("../../packaging/mcpb/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}

	listed := map[string]bool{}
	for _, tool := range manifest.Tools {
		if tool.Description == "" {
			t.Errorf("the bundle lists %s with no description; the app shows the list before "+
				"anyone installs it", tool.Name)
		}
		listed[tool.Name] = true
	}

	res, err := connect(t).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	served := map[string]bool{}
	for _, tool := range res.Tools {
		served[tool.Name] = true
		if !listed[tool.Name] {
			t.Errorf("the server serves %s and the bundle's manifest does not list it", tool.Name)
		}
	}
	for name := range listed {
		if !served[name] {
			t.Errorf("the bundle's manifest lists %s, which the server does not serve", name)
		}
	}
}
