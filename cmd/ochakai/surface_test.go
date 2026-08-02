package main

import (
	"context"
	"fmt"
	"io/fs"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	yaml "go.yaml.in/yaml/v3"

	"github.com/na0fu3y/ochakai/internal/domain"
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

// The contract, parsed once for the three checks that read it. Only the
// keys those checks need are declared: this is a counter, not a second
// OpenAPI implementation.
type wireSpec struct {
	Paths      map[string]map[string]yaml.Node `yaml:"paths"`
	Components struct {
		Parameters map[string]wireParam `yaml:"parameters"`
		Headers    map[string]yaml.Node `yaml:"headers"`
	} `yaml:"components"`
}

type wireParam struct {
	Ref  string `yaml:"$ref"`
	Name string `yaml:"name"`
	In   string `yaml:"in"`
}

type wireOperation struct {
	Parameters []wireParam `yaml:"parameters"`
	Responses  map[string]struct {
		Headers map[string]yaml.Node `yaml:"headers"`
	} `yaml:"responses"`
}

var httpMethods = map[string]bool{"get": true, "put": true, "post": true, "delete": true, "patch": true}

func readWireSpec(t *testing.T) *wireSpec {
	t.Helper()
	content, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatalf("read openapi.yaml: %v", err)
	}
	spec := &wireSpec{}
	if err := yaml.Unmarshal(content, spec); err != nil {
		t.Fatalf("parse openapi.yaml: %v", err)
	}
	if len(spec.Paths) == 0 {
		t.Fatal("no paths found in openapi.yaml: these checks now guard nothing")
	}
	return spec
}

// resolve follows a $ref into components/parameters, so a parameter
// counts the same whether an operation spells it out or shares it.
func (s *wireSpec) resolve(t *testing.T, p wireParam) wireParam {
	t.Helper()
	if p.Ref == "" {
		return p
	}
	name, ok := strings.CutPrefix(p.Ref, "#/components/parameters/")
	if !ok {
		t.Fatalf("parameter $ref %q is not a component parameter", p.Ref)
	}
	target, ok := s.Components.Parameters[name]
	if !ok {
		t.Fatalf("parameter $ref %q names no component", p.Ref)
	}
	return target
}

// operations decodes every method under every path, carrying the
// path-item's own parameters down into each — OpenAPI lets a path declare
// what all its methods take, and /api/v1/review/{id} does.
func (s *wireSpec) operations(t *testing.T) map[string]wireOperation {
	t.Helper()
	out := map[string]wireOperation{}
	for path, item := range s.Paths {
		var shared []wireParam
		if node, ok := item["parameters"]; ok {
			if err := node.Decode(&shared); err != nil {
				t.Fatalf("%s: parameters: %v", path, err)
			}
		}
		for method, node := range item {
			if !httpMethods[method] {
				continue
			}
			var op wireOperation
			if err := node.Decode(&op); err != nil {
				t.Fatalf("%s %s: %v", method, path, err)
			}
			op.Parameters = append(append([]wireParam{}, shared...), op.Parameters...)
			out[strings.ToUpper(method)+" "+path] = op
		}
	}
	return out
}

// REST is the only contract; the other three surfaces are its clients
// (design doc 0015 §2), so its operations are read from the contract
// itself rather than from the router.
func TestSurfaceDocCountsRESTOperations(t *testing.T) {
	ops := readWireSpec(t).operations(t)
	names := make([]string, 0, len(ops))
	for name := range ops {
		names = append(names, name)
	}
	if len(names) == 0 {
		t.Fatal("no operations found in openapi.yaml: this check now guards nothing")
	}
	compareSurface(t, "REST", names)
}

// Counting operations alone measured the dimension the pressure escapes
// through. Folding an endpoint into a parameter or a content type always
// lowers the operation count and can never raise it, so every fold read
// as a pure win: 0046 §3.5's 19 → 14 moved three endpoints into
// `links_to=`, `Accept: application/gzip` and a path suffix, and the
// document recorded only the five that went away.
//
// A query parameter is a thing a reader has to learn and can get wrong,
// which is the same currency the endpoints are counted in. Counting it
// beside them makes a fold show as a trade — REST 15 → 14 and PARAM
// 17 → 18 — rather than as a subtraction.
//
// Distinct names, not occurrences: `limit` means the same thing on five
// operations and is learned once, so reusing the vocabulary is free and
// inventing a word is what costs. That is the incentive worth having.
func TestSurfaceDocCountsRESTParameters(t *testing.T) {
	spec := readWireSpec(t)
	seen := map[string]bool{}
	for _, op := range spec.operations(t) {
		for _, p := range op.Parameters {
			if p := spec.resolve(t, p); p.In == "query" {
				seen[p.Name] = true
			}
		}
	}
	if len(seen) == 0 {
		t.Fatal("no query parameters found in openapi.yaml: this check now guards nothing")
	}
	compareSurface(t, "PARAM", sortedKeys(seen))
}

// Headers are the third place surface hides. `Ochakai-Read-Only` and
// `Ochakai-Unchanged` are protocol a client has to implement, and
// `If-Match` carries the whole of design doc 0030 — none of it visible in
// an endpoint count. Request and response headers are counted together,
// under the names that travel on the wire, because that is what a reader
// meets.
//
// Path parameters are deliberately not counted anywhere: `{path}` and
// `{id}` are the address of the thing being operated on, not an option
// beside it.
func TestSurfaceDocCountsRESTHeaders(t *testing.T) {
	spec := readWireSpec(t)
	seen := map[string]bool{}
	for _, op := range spec.operations(t) {
		for _, p := range op.Parameters {
			if p := spec.resolve(t, p); p.In == "header" {
				seen[p.Name] = true
			}
		}
		for _, resp := range op.Responses {
			for name := range resp.Headers {
				seen[name] = true
			}
		}
	}
	if len(seen) == 0 {
		t.Fatal("no headers found in openapi.yaml: this check now guards nothing")
	}
	compareSurface(t, "HEADER", sortedKeys(seen))
}

// A component nothing references is surface that was written down and
// then lost: `components/headers/Note`, `components/parameters/IfMatch`
// and `schemas/Revision` were each declared and reachable from no
// operation, so the counters above could not see them and a reader of the
// spec could not find them. The two header/parameter families are checked
// here; schemas are checked in internal/restapi against the Go types that
// write them.
func TestOpenAPIComponentsAreReachable(t *testing.T) {
	spec := readWireSpec(t)
	usedParams, usedHeaders := map[string]bool{}, map[string]bool{}
	for _, op := range spec.operations(t) {
		for _, p := range op.Parameters {
			if name, ok := strings.CutPrefix(p.Ref, "#/components/parameters/"); ok {
				usedParams[name] = true
			}
		}
		for _, resp := range op.Responses {
			for _, node := range resp.Headers {
				var ref struct {
					Ref string `yaml:"$ref"`
				}
				if err := node.Decode(&ref); err != nil {
					continue
				}
				if name, ok := strings.CutPrefix(ref.Ref, "#/components/headers/"); ok {
					usedHeaders[name] = true
				}
			}
		}
	}
	for name := range spec.Components.Parameters {
		if !usedParams[name] {
			t.Errorf("components/parameters/%s is declared and referenced by no operation.\n\n"+
				"Surface nothing points at is surface nobody can find, and the counters\n"+
				"in %s cannot see it. Reference it, or delete it.", name, surfaceDoc)
		}
	}
	for name := range spec.Components.Headers {
		if !usedHeaders[name] {
			t.Errorf("components/headers/%s is declared and referenced by no operation.\n\n"+
				"Surface nothing points at is surface nobody can find, and the counters\n"+
				"in %s cannot see it. Reference it, or delete it.", name, surfaceDoc)
		}
	}
}

// A cap line: "- REST: 14". Deliberately not the backticked shape an
// item takes, so the ceiling section cannot be mistaken for a surface.
//
// A cap whose name ends in -LINES caps a measurement rather than a list:
// "- DOC-LINES: 5800" is the reading volume of the section DOC counts. A
// -LINES cap may carry its own -LINES-SLACK cap beside it — the only
// tolerance this file grants, and stated for the same reason the ceiling
// itself is: so it is a number somebody agreed to rather than a habit.
// Every other section is counted in names, because a name is learned once
// however often it appears; reading is not paid that way.
var surfaceCapRe = regexp.MustCompile(`^- ([A-Z-]+): (\d+)$`)

// capMeasures is the suffix that marks a cap on an amount, and the
// section whose amount it caps.
const capMeasures = "-LINES"

// slackSuffix marks the tolerance declared beside a -LINES cap, and
// capSlack is that suffix appended to capMeasures: "DOC-LINES-SLACK"
// says how much of DOC-LINES's ceiling may sit unspent before that is
// itself a failure, rather than headroom nobody has to return.
const (
	slackSuffix = "-SLACK"
	capSlack    = capMeasures + slackSuffix
)

// The counters above are a thermometer: they say how big each surface is
// and let it be any size at all, as long as the document agrees. This is
// the thermostat. Every surface declares a ceiling, and the ceiling is
// exact — not a limit not to cross but the number this project agreed the
// surface is, so growing one means editing a line whose subject is the
// agreement, not just the arithmetic, and shrinking one means the same
// PR returns the ceiling it earned.
//
// Without this, a fold could lower what is declared and leave the cap
// where it was: nothing would fail, and the next addition would spend the
// gap in silence rather than moving a number anyone could see in the
// diff. That is what happened to DOC-LINES once (d28c3c8) before the
// habit of lowering it by hand caught back up — this test is that habit,
// checked from outside rather than trusted (design doc 0035).
//
// A list dimension carries no tolerance: adding or removing a name is
// already a visible diff, so there is no amount of drift small enough to
// deserve one. The -LINES measurements are different, and get their own
// stated slack below.
//
// The ceiling is one number in one file and anybody can raise it. That is
// the point rather than a weakness, and it is the same bargain
// docs/surface.md already describes for itself: what this buys is that
// the raise cannot happen quietly. A PR that moves a cap is a PR that
// says out loud it is making ochakai bigger, and the three questions are
// there to be answered when it does.
func TestSurfaceStaysUnderItsCaps(t *testing.T) {
	content, err := os.ReadFile(surfaceDoc)
	if err != nil {
		t.Fatalf("read %s: %v", surfaceDoc, err)
	}
	caps := map[string]int{}
	for _, line := range strings.Split(string(content), "\n") {
		if m := surfaceCapRe.FindStringSubmatch(line); m != nil {
			n, err := strconv.Atoi(m[2])
			if err != nil {
				t.Fatalf("%s: cap %q: %v", surfaceDoc, line, err)
			}
			caps[m[1]] = n
		}
	}
	if len(caps) == 0 {
		t.Fatalf("%s declares no caps: this check now guards nothing", surfaceDoc)
	}
	sections := surfaceSections(t)
	for name, section := range sections {
		limit, ok := caps[name]
		if !ok {
			t.Errorf("%s counts %s but sets it no cap; every counted surface has a ceiling", surfaceDoc, name)
			continue
		}
		switch {
		case section.declared > limit:
			t.Errorf(`%s is at %d, over its cap of %d.

The cap is the size this project agreed %s should be. Going past it is a
decision, so it is made by editing the cap — in the same PR, with the
three questions answered in the description, and ideally with something
folded away in exchange.`, name, section.declared, limit, name)
		case section.declared < limit:
			t.Errorf(`%s is at %d, under its cap of %d.

A list dimension carries no headroom: adding a name is already a visible
diff, so the cap is not a limit not to cross but the count this project
agreed %s is. A fold that lowers what is declared and leaves the cap
where it was banks the difference for the next addition to spend without
moving a number anyone would see. Lower the cap to %d in the same PR that
earned it.`, name, section.declared, limit, name, section.declared)
		}
	}
	for name := range caps {
		if strings.HasSuffix(name, capSlack) {
			base := strings.TrimSuffix(name, slackSuffix)
			if _, ok := caps[base]; !ok {
				t.Errorf("%s declares %s but no %s for it to soften", surfaceDoc, name, base)
			}
			continue
		}
		counted := strings.TrimSuffix(name, capMeasures)
		if _, ok := sections[counted]; !ok {
			t.Errorf("%s caps %s, which it does not count", surfaceDoc, name)
		}
	}
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
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

// Counting commands measured the dimension the pressure escapes from,
// exactly as counting REST operations did before PARAM was added. Folding
// an endpoint into a query parameter always lowers the operation count,
// which is why PARAM had to be counted; the CLI has the same shape and
// nothing was reading it. `ochakai search` alone carries thirteen — more
// choices than the whole REST contract has operations — and every one of
// them is something a reader learns.
//
// Short flags count. `-f` is a name to learn like any other, and the one
// place that already reads the flag sets skipped them because the
// completion scripts do not offer them, which is a fact about the
// scripts rather than about what a user pays.
//
// --url is one word wherever it appears, like `limit` in PARAM: names are
// counted, not occurrences, so reusing a flag across commands is free and
// inventing one is what costs.
func TestSurfaceDocCountsCLIFlags(t *testing.T) {
	seen := map[string]bool{}
	for _, names := range commandFlags(t) {
		for _, name := range names {
			seen[name] = true
		}
	}
	if len(seen) == 0 {
		t.Fatal("no CLI flags found: this check now guards nothing")
	}
	compareSurface(t, "FLAG", sortedKeys(seen))
}

// Configuration is surface for the same reason a command is: somebody
// deploying reads it, and can set it wrong. A project whose fourth
// condition is "no forward-deployed engineer" pays for every knob in
// exactly that currency, so the variables are counted beside the calls.
//
// They are read from the shipped source rather than from a list here: a
// variable exists the moment something asks the environment for it, and
// a second list would be one more thing to keep true. Test files are
// skipped — a variable only a test sets is not something a user
// configures — and so is internal/testdb, which is a test file by every
// measure except its name: nothing outside a _test.go imports it, and
// the only variable it reads is the one that says where the test
// database is. TestNothingShipsTestSupport keeps that true.
//
// What is read is the call, not the prefix. Matching "OCHAKAI_*" was a
// convention standing in for a question, and PORT — which Cloud Run sets,
// which the deploy guide names, and which docs/configuration.md has
// always listed — sat outside it for as long as the check existed. Only
// the variables the operating system defines are skipped, by name and for
// a reason: XDG_CONFIG_HOME and AppData say where any program's config
// lives on that platform, so ochakai reads them rather than defining
// them, and nobody sets one to configure ochakai.
func TestSurfaceDocCountsEnvironmentVariables(t *testing.T) {
	varRe := regexp.MustCompile(`(?:os\.Getenv|envOr)\("([A-Za-z][A-Za-z0-9_]*)"`)
	osOwned := map[string]bool{"XDG_CONFIG_HOME": true, "AppData": true}
	seen := map[string]bool{}
	for _, root := range []string{"../../internal", "../../cmd"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && d.Name() == testSupportPkg {
				return fs.SkipDir
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range varRe.FindAllStringSubmatch(string(content), -1) {
				if !osOwned[m[1]] {
					seen[m[1]] = true
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	if len(seen) == 0 {
		t.Fatal("no environment variables found: this check now guards nothing")
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	compareSurface(t, "ENV", names)
}

// testSupportPkg is the one package under internal that ships with the
// tests rather than with the program.
const testSupportPkg = "testdb"

// The exception TestSurfaceDocCountsEnvironmentVariables makes for
// internal/testdb holds only while that package is what it says it is.
// A guard with a named exception is a place to hide something, so the
// exception is checked rather than trusted (design doc 0035): nothing
// the program ships may import it.
func TestNothingShipsTestSupport(t *testing.T) {
	imported := false
	for _, path := range repoTextFiles(t) {
		if filepath.Ext(path) != ".go" {
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !strings.Contains(string(content), "internal/"+testSupportPkg+`"`) {
			continue
		}
		imported = true
		t.Errorf("%s imports internal/%s, which ships with the tests: "+
			"the environment-variable count skips that package, so anything it reads "+
			"would be a knob nobody counts", path, testSupportPkg)
	}
	if imported {
		return
	}
	// And it is imported by tests, or the exception guards nothing.
	found, err := filepath.Glob("../../internal/*/*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range found {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if strings.Contains(string(content), "internal/"+testSupportPkg+`"`) {
			return
		}
	}
	t.Errorf("no test imports internal/%s: the exception in the environment-variable "+
		"count now covers a package nothing uses", testSupportPkg)
}

// A test that builds its own run-unique id builds one nothing gives
// back. That is how the shared test database grew: every integration
// test wrote under `fmt.Sprintf("name%d", time.Now().UnixNano())`, which
// collides with nobody and is cleaned up by nobody, so the corpus grew
// by seventy entries a run until a bounded ranking assertion tipped over
// and read as a search bug (issue #278, twice).
//
// internal/testdb.Unique is the same token with the sweep attached. This
// reads the tree for the shape it replaced, because nothing else would
// notice the next one: the hand-rolled version works perfectly, right up
// until the run that pushes somebody else's entry out of a top-ten.
func TestNoTestRollsItsOwnNamespace(t *testing.T) {
	unique := regexp.MustCompile(`time\.Now\(\)\.UnixNano\(\)`)
	// Where a unique number is not an address: a model name, a query
	// nobody else asks. Those leave no row keyed by an id, so testdb has
	// nothing to sweep and no claim on them.
	allowed := map[string]bool{
		"internal/store/stats_integration_test.go":        true, // a search query, not an id
		"internal/service/attachment_integration_test.go": true, // an embedding model name
	}
	found, checked := false, 0
	paths, err := filepath.Glob("../../internal/*/*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		checked++
		rel := filepath.ToSlash(strings.TrimPrefix(filepath.ToSlash(path), "../../"))
		if !unique.Match(content) {
			continue
		}
		found = true
		if allowed[rel] {
			continue
		}
		t.Errorf("%s builds a run-unique token by hand — use testdb.Unique, "+
			"which is the same token with the sweep that gives it back", rel)
	}
	if checked == 0 || !found {
		t.Error("no test builds a run-unique token: this check now guards nothing")
	}
}

// The six sections above count mechanisms — what a caller can invoke and
// what a deployer can set. None of them counts the thing a curator
// actually has to hold in their head: the **words**. A type spelling, a
// lifecycle value, a trust tier, a listing mode, a ruling, the name of a
// queue, the verb a revision is filed under — each is something the
// documentation teaches and somebody has to remember, and none of them
// moves a single number in the other six.
//
// That is where the pressure went. 0055 folded three REST operations
// into one and the vocabulary stayed three words; 0054 renamed five MCP
// tools and no count moved, though what changed was exactly the word a
// reader has to know. docs/surface.md said as much about itself —
// "adding a way to count only makes the next escape hatch visible" —
// and this is the next one.
//
// The values are read out of internal/domain, where the surfaces already
// take them from, so a word is counted the moment the product can say
// it. Each is written family-first (`status.draft`, `sort.failed`), for
// two reasons: an alphabetical list of bare values would be unreadable,
// and a spelling that means two things in two families — `failed` is a
// listing mode and an outcome — should be visible as two words to learn
// rather than collapse into one.
func TestSurfaceDocCountsVocabulary(t *testing.T) {
	var words []string
	add := func(family string, values ...string) {
		if len(values) == 0 {
			t.Fatalf("vocabulary %q is empty: this check now guards nothing", family)
		}
		for _, v := range values {
			words = append(words, family+"."+v)
		}
	}
	types := make([]string, 0, len(domain.Types))
	for _, x := range domain.Types {
		types = append(types, string(x))
	}
	statuses := make([]string, 0, len(domain.Statuses))
	for _, x := range domain.Statuses {
		statuses = append(statuses, string(x))
	}
	trusts := make([]string, 0, len(domain.Trusts))
	for _, x := range domain.Trusts {
		trusts = append(trusts, string(x))
	}
	add("type", types...)
	add("status", statuses...)
	add("trust", trusts...)
	add("sort", domain.ListSorts...)
	add("ruling", domain.Rulings...)
	add("change", domain.Changes...)
	add("outcome", domain.Outcomes...)
	add("queue", queueWordsToLearn(t)...)
	compareSurface(t, "VOCAB", words)
}

// The eight sections above count what a caller can invoke, what a deployer
// can set and what a curator has to know. None of them counts what
// somebody reads before any of it means anything.
//
// docs/surface.md put the implementation's line count under "not counted"
// and said why; the Web UI is there too, with its reason. Documentation
// was on neither list — not excluded, but never recognized as surface,
// though the README sends a reader to twenty-odd pages and the deploy
// guide alone is longer than the CLI reference.
//
// It is the dimension that grew fastest. Between v0.10.0 and now the REST
// contract went 19 → 11 while these pages passed 5,600 lines from 2,971: the
// folding was real, and the prose explaining the folding outgrew what it
// folded. Every other counter would have called that a pure win, which is
// the same shape of blind spot PARAM, FLAG and VOCAB each closed.
//
// What counts as one of these pages is decided here rather than listed
// twice: an exception nobody can see is a place to put a page.
func TestSurfaceDocCountsUserDocs(t *testing.T) {
	docs, _ := userDocs(t)
	if len(docs) == 0 {
		t.Fatal("no user-facing documents found: this check now guards nothing")
	}
	compareSurface(t, "DOC", docs)
}

// Counting pages leaves growing one free, and growing one is how the
// manual actually grew: 0059 renamed two words and left paragraphs in four
// files, moving no number anywhere. Names are counted everywhere else
// because a name is learned once however often it appears — reading is not
// paid that way, so this is the one place an amount has a ceiling.
//
// The ceiling alone let headroom bank silently: d28c3c8 shortened the
// deploy guide, raised DOC-LINES 5,753 → 5,790 for the room the fold
// needed, and landed at 5,762 — 28 lines the PR whose whole purpose was
// to make the manual smaller never returned. DOC-LINES-SLACK is the
// stated tolerance for the gap a normal edit leaves; anything wider than
// that is unreturned budget, not editing noise, and fails here instead of
// waiting for somebody to notice by hand.
func TestUserDocsStayUnderTheirLineCap(t *testing.T) {
	content, err := os.ReadFile(surfaceDoc)
	if err != nil {
		t.Fatalf("read %s: %v", surfaceDoc, err)
	}
	limit, slack := 0, -1
	for _, line := range strings.Split(string(content), "\n") {
		m := surfaceCapRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		switch m[1] {
		case "DOC" + capMeasures:
			if limit, err = strconv.Atoi(m[2]); err != nil {
				t.Fatalf("%s: cap %q: %v", surfaceDoc, line, err)
			}
		case "DOC" + capSlack:
			if slack, err = strconv.Atoi(m[2]); err != nil {
				t.Fatalf("%s: cap %q: %v", surfaceDoc, line, err)
			}
		}
	}
	if limit == 0 {
		t.Fatalf("%s sets no DOC%s cap: this check now guards nothing", surfaceDoc, capMeasures)
	}
	if slack < 0 {
		t.Fatalf("%s sets no DOC%s cap: DOC%s would carry silent headroom with no ceiling of its own", surfaceDoc, capSlack, capMeasures)
	}
	// The cap moves in whole blocks, so it cannot be set to whatever the
	// manual happens to measure today. Fitting it to the actual leaves no
	// headroom, which puts the next page's PR back on this same line — and
	// nine translations editing one line is the conflict that widened the
	// slack in the first place. A multiple is also what makes crossing one
	// mean something: it happens when the manual got bigger, not when a
	// paragraph did.
	if slack > 0 && limit%slack != 0 {
		t.Errorf(`DOC%s is %d, which is not a multiple of the %d DOC%s.

The cap moves in blocks: round it up to %d rather than fitting it to what
the manual measures today, which would leave the next PR no room and put
it back on this line.`,
			capMeasures, limit, slack, capSlack, ((limit/slack)+1)*slack)
	}
	_, lines := userDocs(t)
	switch headroom := limit - lines; {
	case headroom < 0:
		t.Errorf(`the manual is %d lines, over its cap of %d.

What a reader has to get through is a cost like any other, and this is the
only ceiling on an amount rather than on a list of names. Going past it is
a decision: raise the cap in the same PR, having answered %s's three
questions — or find the pages that say the same thing twice.`,
			lines, limit, surfaceDoc)
	case headroom > slack:
		t.Errorf(`the manual is %d lines, %d under its cap of %d — more than the %d
DOC%s allows.

A fold that shrinks the manual is meant to lower the cap in the same PR,
not leave headroom for the next addition to spend without moving a
number anyone would see (this is exactly what happened to DOC-LINES
across d28c3c8 and its follow-up). Lower the cap to %d, or close to it.`,
			lines, headroom, limit, slack, capSlack, lines)
	}
}

// userDocs is the manual: every markdown page a reader is sent to in order
// to evaluate, use or run ochakai, with the total number of lines in them.
//
// Five things are markdown and are not the manual, and each is left out
// for a reason docs/surface.md states:
//
//   - docs/design — decision records, read by somebody changing ochakai.
//     What earns a number is already narrowed by design doc 0048; the same
//     thing is not tightened twice.
//   - OKF documents — a file with frontmatter under examples/ is
//     knowledge, the thing ochakai stores, not prose about it. That is
//     examples/demo and the bundle under examples/bigquery-catalog.
//   - internal — fixtures.
//   - CHANGELOG, CONTRIBUTING, CLAUDE.md, the code of conduct, and the
//     dot-directories — a ledger read one entry at a time, and the surface
//     somebody changing ochakai reads.
//   - a generated reference page — one nobody reads front to back, built
//     from the binary and unable to drift from it. The rule stays narrow
//     enough that it cannot become a hiding place: generated from the
//     build, verified by a test, with no hand-written prose beyond a
//     fixed header. docs/cli.md is the only file that qualifies today
//     (TestCLIReferenceIsCurrent in clidocs_test.go, issue #371).
func userDocs(t *testing.T) ([]string, int) {
	t.Helper()
	const root = "../.."
	skipDirs := map[string]bool{
		".git": true, "node_modules": true, "docs/design": true, "internal": true,
	}
	forChangers := map[string]bool{
		"CHANGELOG.md": true, "CONTRIBUTING.md": true,
		"CODE_OF_CONDUCT.md": true, "CLAUDE.md": true,
	}
	generatedReference := map[string]bool{
		"docs/cli.md": true,
	}
	var docs []string
	lines := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if skipDirs[rel] || (strings.HasPrefix(d.Name(), ".") && rel != ".") {
				return fs.SkipDir
			}
			return nil
		}
		if filepath.Ext(rel) != ".md" || forChangers[rel] || generatedReference[rel] {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		// Frontmatter is read as "this is an OKF document" only under
		// examples/, where a real bundle (examples/demo,
		// examples/bigquery-catalog/bundle) sits beside plain README
		// prose. Trusting the first line anywhere else would let a page
		// in docs/ drop out of the manual by starting with "---" for a
		// reason that has nothing to do with OKF — this test caught
		// nothing telling the two apart until it did.
		if strings.HasPrefix(rel, "examples/") && strings.HasPrefix(string(content), "---\n") {
			return nil // an OKF document: knowledge, not documentation
		}
		docs = append(docs, rel)
		lines += strings.Count(string(content), "\n")
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return docs, lines
}

// extraTopLevelCommands are the words after "ochakai" that a reader can
// run but that are not in clientCommands — how the binary is started
// rather than something it knows (TestSurfaceDocCountsCLICommands excludes
// them from CLI for the same reason).
var extraTopLevelCommands = map[string]bool{
	"serve": true, "serve-ui": true, "version": true, "help": true,
}

// commandGuardExempt are DOC pages TestManualNamesNoCommandThatDoesNotExist
// does not read, each for a reason that holds regardless of what the page
// says elsewhere:
//
//   - docs/cli.md is regenerated byte-for-byte from the running binary's
//     own flag usage (TestCLIReferenceIsCurrent), so a wrong command word
//     there is already impossible by construction. Its flag-usage prose
//     also reads as one to a pattern that cannot tell a command from a
//     description: "-url ochakai server URL" is `-url`'s help text, not an
//     invocation of `ochakai server`.
//   - ROADMAP.md is where a command that does not exist belongs: a
//     proposal not yet built (`ochakai tutorial`, issue #212) or a request
//     already declined (`ochakai sync`, issue #43). Naming what ochakai
//     does not do is that page's job, the same reason CHANGELOG.md is
//     exempt from TestRetiredSpellingsAreNotTaught.
var commandGuardExempt = map[string]bool{
	"docs/cli.md": true,
	"ROADMAP.md":  true,
}

// commandGuardExtraDir is the directory TestManualNamesNoCommandThatDoesNotExist
// reads outside the Markdown manual: userDocs() is DOC-page prose only
// (.md, and .github/ skipped as a dot-directory), so issue #399 found
// bug_report.yml still showing the retired `ochakai create` as the
// example every bug reporter copies. Issue #401's fix named that one
// file (commandGuardExtra); issue #412 found the same directory's
// feature_request.yml just as unwatched — its own dropdown option names
// `ochakai ui` — so this reads every file the directory holds instead of
// one chosen by hand, the way repoTextFiles (retired_test.go) walks a
// directory rather than naming files in it.
//
// These are scanned with commandWordsIn rather than commandWordIn: an
// issue form's dropdown options and placeholder text are read literally
// by the GitHub UI, so wrapping the invocation in backticks or a fence to
// isolate it — the way the Markdown manual does — would show the reporter
// a stray backtick instead of the command. Every field in every line is
// checked instead, and prose that merely mentions the product name
// (`ochakai` in backticks) is worded to stay out of the way, the same
// reason a URL segment or `./cmd/ochakai` already does not match.
const commandGuardExtraDir = ".github/ISSUE_TEMPLATE"

var inlineCodeSpanRe = regexp.MustCompile("`([^`]*)`")

// commandWordIn reads "ochakai <word>" out of an already-isolated code
// span — a line inside a fenced block, or the text of an inline `...`
// span — the second word taken as the command. "ochakai" must be the
// span's first word: mid-span it names something else's argument, not the
// program being invoked, which is what excludes `claude mcp add
// --transport http ochakai http://localhost:8080/mcp` (docs/guides/mcp-clients.md,
// where "ochakai" names the server being registered) from reading as a
// command. "/ochakai" is accepted too, the shape a container's exec form
// takes (`docker compose exec ochakai /ochakai import ...`).
func commandWordIn(span string) (string, bool) {
	fields := strings.Fields(strings.TrimPrefix(strings.TrimSpace(span), "$ "))
	if len(fields) < 2 || (fields[0] != "ochakai" && fields[0] != "/ochakai") {
		return "", false
	}
	word := commandWordRe.FindString(fields[1])
	return word, word != ""
}

var commandWordRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*`)

// commandWordsIn finds every "ochakai <word>" in a line where "ochakai" is
// its own field — unlike commandWordIn, which requires "ochakai" to open
// an already-isolated span, this walks all fields so it also catches a
// standalone "ochakai" appearing mid-sentence, while still passing over
// "./cmd/ochakai" or "ochakai.example.com", where "ochakai" is never a
// field by itself. A leading "(" is stripped first — an issue form's
// dropdown option reads "Web UI (ochakai ui / serve-ui)", where
// Fields() leaves the paren stuck to the word — so long as what remains
// is exactly "ochakai" and not, say, "(ochakai.example.com)".
func commandWordsIn(line string) []string {
	fields := strings.Fields(line)
	var words []string
	for i, field := range fields[:max(0, len(fields)-1)] {
		field = strings.TrimPrefix(field, "(")
		// Outside the Markdown manual the raw line is read, so a code
		// span arrives with its backticks attached. A prose mention
		// closes the span on the name — `ochakai` — while an invocation
		// runs on into the command, `ochakai put`. Dropping only the
		// opening backtick tells them apart: the first still carries its
		// closing one and does not match, the second does. Without this
		// a real invocation is exempt exactly where prose is, which is
		// the inverse of the manual, where a span is the only thing read.
		field = strings.TrimPrefix(field, "`")
		if field != "ochakai" && field != "/ochakai" {
			continue
		}
		if word := commandWordRe.FindString(fields[i+1]); word != "" {
			words = append(words, word)
		}
	}
	return words
}

// TestManualNamesNoCommandThatDoesNotExist reads every DOC page for
// "ochakai <word>" and fails on any word that is not a command the binary
// actually has — issue #377's first guard. Issue #363 found `ochakai
// create` written as the first command after a deploy and `update` twice
// in troubleshooting: four pages taught a command that does not exist,
// and nothing but a human rereading every page would have noticed either
// mistake.
//
// A prose sentence can start "ochakai " as easily as an invocation can —
// "ochakai is a small project" sits one line above a real one in
// operating.md, purely because the paragraph wrapped there — so which
// words after "ochakai" are worth checking cannot be read from the words
// alone. What it is read from instead is the markdown around them: this
// project always writes a real invocation either inside a fenced code
// block or inside backticks (true of every DOC page when this guard was
// added), so only those two contexts are searched.
func TestManualNamesNoCommandThatDoesNotExist(t *testing.T) {
	valid := map[string]bool{}
	for name := range clientCommands {
		valid[name] = true
	}
	for name := range extraTopLevelCommands {
		valid[name] = true
	}

	docs, _ := userDocs(t)
	checked := 0
	for _, rel := range docs {
		if commandGuardExempt[rel] {
			continue
		}
		content, err := os.ReadFile(filepath.Join("../..", rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		inFence := false
		for i, line := range strings.Split(string(content), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				inFence = !inFence
				continue
			}
			var spans []string
			if inFence {
				spans = []string{line}
			} else {
				for _, m := range inlineCodeSpanRe.FindAllStringSubmatch(line, -1) {
					spans = append(spans, m[1])
				}
			}
			for _, span := range spans {
				word, ok := commandWordIn(span)
				if !ok {
					continue
				}
				checked++
				if !valid[word] {
					t.Errorf("%s:%d names a command ochakai does not have: %q\n\t%s",
						rel, i+1, word, strings.TrimSpace(span))
				}
			}
		}
	}
	entries, err := os.ReadDir(filepath.Join("../..", commandGuardExtraDir))
	if err != nil {
		t.Fatalf("read %s: %v", commandGuardExtraDir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		rel := commandGuardExtraDir + "/" + e.Name()
		content, err := os.ReadFile(filepath.Join("../..", rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		for i, line := range strings.Split(string(content), "\n") {
			for _, word := range commandWordsIn(line) {
				checked++
				if !valid[word] {
					t.Errorf("%s:%d names a command ochakai does not have: %q\n\t%s",
						rel, i+1, word, strings.TrimSpace(line))
				}
			}
		}
	}
	for _, rel := range terraformOutputFiles(t) {
		content, err := os.ReadFile(filepath.Join("../..", rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		for i, line := range strings.Split(string(content), "\n") {
			m := terraformOchakaiValueRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			checked++
			if !valid[m[1]] {
				t.Errorf("%s:%d names a command ochakai does not have: %q\n\t%s",
					rel, i+1, m[1], strings.TrimSpace(line))
			}
		}
	}
	if checked == 0 {
		t.Fatal("no command invocation found across the manual: this check now guards nothing")
	}
}

// terraformOchakaiValueRe matches a Terraform output's `value` when it is
// a literal string that opens with "ochakai <word>" — deploy/terraform/
// outputs.tf's use_command tells the reader the next command to run, in a
// string `terraform output` prints verbatim, so it cannot be fenced or
// backtick-isolated the way the manual's invocations are without putting
// stray backticks in what the operator copies and pastes. A description
// string (prose about the output, not the output itself) is not this
// shape, which is what keeps main.tf's and variables.tf's free-form
// commentary about ochakai out of this check.
var terraformOchakaiValueRe = regexp.MustCompile(`^\s*value\s*=\s*"ochakai ([A-Za-z][A-Za-z0-9-]*)`)

// terraformOutputFiles lists the .tf files under deploy/terraform, the
// only place a Terraform output's value has ever named a live ochakai
// command — walked generically rather than naming outputs.tf, the same
// reason commandGuardExtraDir reads a directory instead of one issue
// form.
func terraformOutputFiles(t *testing.T) []string {
	t.Helper()
	const dir = "deploy/terraform"
	entries, err := os.ReadDir(filepath.Join("../..", dir))
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".tf" {
			continue
		}
		out = append(out, dir+"/"+e.Name())
	}
	return out
}

// queueWordsToLearn is the queue keys that are words of their own. A
// queue named after the sort that lists it is not a second word — the
// curator learns `failed` once and meets it as an outcome to report, a
// feed to work and a number in `stats` (design doc 0059). Counting it
// again per family would say the folding cost nothing, when what it
// bought was exactly this.
//
// The family-first spelling is still right for the words that stay:
// docs/surface.md counts `family.value` because one spelling can mean two
// things in two families, and two words is what that costs. This is the
// other case — one thing wearing two names — and it is now impossible to
// reintroduce quietly, because a queue key that is not a sort shows up
// here as a word.
func queueWordsToLearn(t *testing.T) []string {
	t.Helper()
	sorts := map[string]bool{}
	for _, s := range domain.ListSorts {
		sorts[s] = true
	}
	var out []string
	for _, key := range jsonFieldNames(t, domain.QueueCounts{}) {
		if !sorts[key] {
			out = append(out, key)
		}
	}
	if len(out) == 0 {
		t.Fatal("every queue key is a sort name: this check now guards nothing")
	}
	return out
}

// jsonFieldNames reads a struct's wire names off its json tags, so a
// vocabulary that is already spelled once — as the keys of a response —
// is not spelled a second time here.
func jsonFieldNames(t *testing.T, v any) []string {
	t.Helper()
	rt := reflect.TypeOf(v)
	names := make([]string, 0, rt.NumField())
	for i := range rt.NumField() {
		tag, _, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ",")
		if tag == "" || tag == "-" {
			t.Fatalf("%s.%s carries no json name", rt.Name(), rt.Field(i).Name)
		}
		names = append(names, tag)
	}
	return names
}
