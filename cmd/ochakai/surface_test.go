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
var surfaceCapRe = regexp.MustCompile(`^- ([A-Z]+): (\d+)$`)

// The counters above are a thermometer: they say how big each surface is
// and let it be any size at all, as long as the document agrees. This is
// the thermostat. Every surface declares a ceiling, and exceeding it
// fails — so growing one means editing a line whose subject is the
// agreement, not just the arithmetic.
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
		if section.declared > limit {
			t.Errorf(`%s is at %d, over its cap of %d.

The cap is the size this project agreed %s should be. Going past it is a
decision, so it is made by editing the cap — in the same PR, with the
three questions answered in the description, and ideally with something
folded away in exchange.`, name, section.declared, limit, name)
		}
	}
	for name := range caps {
		if _, ok := sections[name]; !ok {
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
func TestSurfaceDocCountsEnvironmentVariables(t *testing.T) {
	varRe := regexp.MustCompile(`"(OCHAKAI_[A-Z_]+)"`)
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
				seen[m[1]] = true
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	if len(seen) == 0 {
		t.Fatal("no OCHAKAI_* variables found: this check now guards nothing")
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
	add("queue", jsonFieldNames(t, domain.QueueCounts{})...)
	compareSurface(t, "VOCAB", words)
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
