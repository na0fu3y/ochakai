package main

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	yaml "go.yaml.in/yaml/v3"
)

// REST stops at /api/v1 (design doc 0064): api/openapi.yaml is the address
// list a client can hold onto, and after the freeze the only thing that may
// still break it is a security defect (§11, docs/compatibility.md). Nothing
// checked that. The counters in surface_test.go read the same file, but they
// count names in a list — a rename that keeps the count is invisible to
// them, and `concepts` → `entries`, `Ochakai-Plan` → `Ochakai-Planned`, a
// 201 becoming a 200 or a required field going optional all pass every other
// check in the repo. The largest promise here was held up by prose alone,
// which is the thing design doc 0035 says not to do: read the invariant from
// outside rather than trust the convention.
//
// api/openapi.frozen.txt is that reading, checked in. Every line is one
// thing a client can observe — an operation and its operationId, a
// parameter or header wire name, a status code, a media type, a schema
// property with its requiredness, type, enum and constraints — derived
// from the spec and sorted, so a diff names exactly what moved.
//
// The constraint facets (schemaFacets) were the first version's blind
// spot, and a large one: `pattern`, `format`, `default`, `maximum` and
// `maxLength` all describe the wire and none of them was read. Design
// doc 0064 §18 had already written down why a response `pattern` cannot
// widen after the freeze — a validating client holds it — and then froze
// it without a check. `default` was worse than unread: the five distinct
// (default, maximum) pairs `limit` carries across five operations are the
// difference between asking for 10 rows and 200, and moving one of them
// moved no line here. operationId is in for the same reason a header name
// is: 0064 §11 treats a code generator as a consumer of this document, so
// the method name it emits is something a client holds onto.
//
// **Prose is deliberately not in it.** description, summary, example and
// examples are dropped on the way through, so the documentation in the
// contract can still be rewritten, corrected and translated freely after the
// freeze. What is frozen is the wire, not the words about it.
//
// The division of labour with the other contract test: checkedServer in
// internal/restapi/openapi_test.go runs every request and response of the
// integration tests past this spec, which keeps the *server* honest to the
// spec. This one keeps the *spec* honest to the freeze. Neither alone pins
// the server — the first would follow the spec anywhere it went, and the
// second cannot see the code — and together they do.
const frozenWirePath = "../../api/openapi.frozen.txt"

// The header the generated file carries, so a reader who opens it knows what
// it is and how to rebuild it before reading a single line of it.
const frozenWireHeader = `# The frozen REST wire, read out of api/openapi.yaml by
# cmd/ochakai/frozenwire_test.go. Generated — do not edit by hand:
#
#	go test ./cmd/ochakai -run TestFrozenWireHasNotMoved -update
#
# One line per thing a client can observe, for the operations the freeze
# holds still: the OKF core — the bundle round trip and the search beside
# it (design doc 0107, narrowing the freeze design doc 0064 announced).
# The rest of /api/v1 is 0.x-unstable the way MCP and the CLI are
# (docs/compatibility.md) and is deliberately not in this file. Prose
# (description, summary, example, examples) is left out on purpose:
# documentation stays free to improve after the freeze. Moving any line
# below is a change to the frozen core, and the only reasons left are a
# security defect (0064 §11) and OKF conformance (design docs 0100, 0102)
# — except for two additions the freeze was never holding still: a property
# added under components/schemas for a schema no requestBody reaches
# (design doc 0082), and an optional query parameter added to an operation
# (design doc 0101).

`

// frozenCorePaths names the addresses whose operations the freeze holds
// still (design doc 0107): the OKF core — the bundle round trip
// (GET/PUT/DELETE, which is also the archive export, the derived
// index.md/log.md and the file half of the address space) and the read
// entry beside it. An operation outside this set is 0.x-unstable the way
// MCP and the CLI are, and freezes when it stops moving, as its own
// decision (docs/compatibility.md). Widening this set is widening the
// promise; a PR that edits it is saying so out loud.
var frozenCorePaths = map[string]bool{
	"/api/v1/bundle/{path}": true,
	"/api/v1/search":        true,
}

// TestFrozenWireHasNotMoved compares the fingerprint of api/openapi.yaml
// with the golden file. The golden is regenerated deliberately, in the PR
// that changes the wire, which is the whole point: the freeze becomes
// something a diff shows rather than something a convention asks for.
func TestFrozenWireHasNotMoved(t *testing.T) {
	got := frozenWire(t)
	if *updateGolden {
		if err := os.WriteFile(frozenWirePath, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	raw, err := os.ReadFile(frozenWirePath)
	if err != nil {
		t.Fatal(err)
	}
	want := string(raw)
	if want == got {
		return
	}
	if onlyAdditionsOutsideTheFreeze(t, want, got) {
		t.Errorf(`the frozen REST wire grew:

%s
Two additions are outside the freeze. A property on a response-only schema
(design doc 0082): a client ignores a response key it does not know, and `+"`required`"+`
in a response is a promise the server makes rather than a demand on the
caller. An optional query parameter (design doc 0101): 0064 §2 made an
unrecognized key a 400 expressly so one could be added later, and a caller
that does not send it reads the same answer it reads today. Both still have
to show in a diff, so regenerate the golden in the
same PR and say in the description what the addition is for:

	go test ./cmd/ochakai -run TestFrozenWireHasNotMoved -update`,
			wireDiff(want, got))
		return
	}
	t.Errorf(`the frozen REST wire moved:

%s
The OKF core of REST is frozen (design docs 0064 and 0107,
docs/compatibility.md): these lines are the part of the contract a client
holds onto forever, and after the freeze the only reasons to move one are
a security defect (0064 §11) and OKF conformance (0100, 0102). Prose is
not fingerprinted, so a documentation edit never lands here. An operation
outside the core does not land here either — a diff showing one means
frozenCorePaths itself was edited, which is a change to the promise.

What 0082 and 0101 leave outside the freeze is narrower than this diff:
properties *added* to a schema no `+"`requestBody`"+` can reach, and *optional query
parameters* added to an operation. A line that disappeared or changed — a
rename, a type, a requiredness, an address — is the freeze itself, and only
a security defect or an OKF conformance defect moves it.

If this change is meant, regenerate the golden in the same PR and say in the
description which security defect it closes:

	go test ./cmd/ochakai -run TestFrozenWireHasNotMoved -update`,
		wireDiff(want, got))
}

// onlyAdditionsOutsideTheFreeze reports whether every difference between
// the golden and the spec is one of the two the freeze was never holding
// still: a line added under a schema no request can reach (design doc
// 0082), or an optional query parameter added to an operation (design doc
// 0101).
//
// A removal is never one of those, and neither is a change: the
// fingerprint is a sorted set of lines, so an edited line arrives here as
// a removal beside an addition, and the removal is what fails.
func onlyAdditionsOutsideTheFreeze(t *testing.T, want, got string) bool {
	t.Helper()
	old := map[string]bool{}
	for _, line := range fingerprintLines(want) {
		old[line] = true
	}
	added := []string{}
	for _, line := range fingerprintLines(got) {
		if !old[line] {
			added = append(added, line)
		}
		delete(old, line)
	}
	if len(old) > 0 || len(added) == 0 {
		return false // something left, or nothing arrived
	}
	responseOnly := responseOnlySchemas(t)
	for _, line := range added {
		// A schema written straight into components/responses is
		// response-only by construction — OpenAPI has no spelling in
		// which a requestBody reaches a responses component — so it
		// needs no reachability test to earn 0082's rule (0082 §3, which
		// names both of the two places a response schema can live).
		// Without this arm the rule reached one of the two, and the
		// error envelope, which lives in the other, would have needed a
		// security defect to grow a field.
		if strings.HasPrefix(line, "components/responses/") {
			continue
		}
		// The third place a response schema can live, and the one 0082
		// §3 did not name: written inline under an operation's
		// `responses`, which is where the search and listing answer is
		// spelled. It is response-only by construction as surely as a
		// responses component is — the fingerprint puts " response "
		// in the line only for a node reached through `responses`, and
		// a requestBody has no spelling that reaches one — so it earns
		// the same rule (design doc 0114 §5). Without this arm the rule
		// reached two of three places, and the third could not gain a
		// field a client ignores without claiming a security defect.
		if isInlineResponseSchema(line) {
			continue
		}
		if isOptionalQueryParam(line) {
			continue
		}
		name, ok := schemaOfLine(line)
		if !ok || !responseOnly[name] {
			return false
		}
	}
	return true
}

// isInlineResponseSchema reports whether a line names a node inside an
// operation's own `responses` — "GET /api/v1/search response 200 content
// application/json.degraded" and the like.
//
// The marker is the fingerprint's own word. Only a node reached through
// `responses` is printed with " response " after the address, and the
// address ahead of it is a URL path, which cannot contain a space; so a
// request-side line cannot spell this and the test does not have to walk
// the contract again to be sure. That is the same argument the
// components/responses arm makes, applied to the other place a response
// schema is written without a name (design doc 0114 §5).
func isInlineResponseSchema(line string) bool {
	method, rest, ok := strings.Cut(line, " ")
	if !ok || !slices.Contains(fingerprintedMethods, method) {
		return false
	}
	_, rest, ok = strings.Cut(rest, " ")
	return ok && strings.HasPrefix(rest, "response ")
}

// isOptionalQueryParam reports whether a line declares a query parameter
// a caller may omit — the addition design doc 0101 places outside the
// freeze, because 0064 §2 closed unrecognized keys with a 400 expressly
// so that one could be added afterwards.
//
// Three narrowings, each load-bearing. Only `query`: a request header can
// be added by a relay the caller does not control (0064 §11), and a path
// parameter is an address. Only `required=false`: a required one is a 400
// for every client that predates it, which 0082 §2 already names as the
// freeze itself. And only a whole parameter line — a line that names a
// key of an existing parameter arrives as a removal beside an addition,
// and the removal fails before this is reached.
func isOptionalQueryParam(line string) bool {
	method, rest, ok := strings.Cut(line, " ")
	if !ok || !slices.Contains(fingerprintedMethods, method) {
		return false
	}
	_, decl, ok := strings.Cut(rest, " query ")
	if !ok {
		return false
	}
	name, facets, ok := strings.Cut(decl, " ")
	if !ok || strings.Contains(name, ".") {
		return false
	}
	// The array members of a repeatable parameter fingerprint as
	// "query type[] ref=Type", which carries no requiredness of its own
	// and rides on the parameter line above it.
	if strings.HasSuffix(name, "[]") {
		return true
	}
	return strings.HasPrefix(facets, "required=false")
}

// fingerprintedMethods are the HTTP methods a fingerprint line can start
// with, so a schema line that happens to contain " query " is never read
// as one.
var fingerprintedMethods = []string{"GET", "PUT", "POST", "DELETE", "PATCH", "HEAD", "OPTIONS"}

// schemaOfLine names the component schema a fingerprint line belongs to,
// for the lines that belong to one: "components/schemas/Stats.dropped
// required=true type=object" is Stats. A line anywhere else — an
// operation, a parameter, a header — has no schema and is never an
// addition this rule covers.
func schemaOfLine(line string) (string, bool) {
	rest, ok := strings.CutPrefix(line, "components/schemas/")
	if !ok {
		return "", false
	}
	name, _, _ := strings.Cut(rest, " ")
	name, _, _ = strings.Cut(name, ".")
	// A schema composed with allOf fingerprints its members under a path
	// — "SearchHit/allOf/1.snippet" — and the schema whose reachability
	// decides the rule is the one at the head of it. Without this cut the
	// name is "SearchHit/allOf/1", which matches no schema, and every
	// composed schema fell outside 0082's rule for a spelling reason.
	name, _, _ = strings.Cut(name, "/")
	return name, name != ""
}

// responseOnlySchemas names the component schemas no requestBody can
// reach, directly or through a $ref.
//
// The distinction is the whole of 0082 §3: adding a required property to
// a schema a client *sends* is the 400 that 0064 §2 introduced, while
// adding one to a schema a client *receives* is a key it ignores. One
// fingerprint line cannot tell the two apart, so the contract is read
// again here to say which schemas are which.
func responseOnlySchemas(t *testing.T) map[string]bool {
	t.Helper()
	out := responseOnlySchemasIn(readSpecTree(t))
	if len(out) == 0 {
		t.Fatal("no component schemas found: the response-only rule now guards nothing")
	}
	return out
}

// responseOnlySchemasIn is the reachability walk, over any contract. It
// takes the document rather than reading the file so the rule can be
// tested on a spec that actually has a request body pointing at a
// component — ochakai's own spells every request body out inline today,
// which makes the distinction correct and invisible.
func responseOnlySchemasIn(doc map[string]any) map[string]bool {
	schemas := wireMap(wireMap(doc["components"])["schemas"])
	reachable := map[string]bool{}
	var walk func(node any)
	walk = func(node any) {
		switch v := node.(type) {
		case map[string]any:
			if ref := wireStr(v["$ref"]); strings.HasPrefix(ref, "#/components/schemas/") {
				name := shortRef(ref)
				if !reachable[name] {
					reachable[name] = true
					walk(schemas[name])
				}
			}
			for key, child := range v {
				if key != "$ref" {
					walk(child)
				}
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	for _, pathItem := range wireMap(doc["paths"]) {
		for method, node := range wireMap(pathItem) {
			if httpMethods[method] {
				walk(wireMap(node)["requestBody"])
			}
		}
	}
	out := map[string]bool{}
	for name := range schemas {
		out[name] = !reachable[name]
	}
	return out
}

// wireDiff names what moved, rather than leaving the author to diff two
// sorted lists by eye. Both sides are sorted already, so a merge walk is
// enough; comment and blank lines are the generated header, which carries no
// contract.
func wireDiff(want, got string) string {
	const shown = 40
	a, b := fingerprintLines(want), fingerprintLines(got)
	var out []string
	for i, j := 0, 0; i < len(a) || j < len(b); {
		switch {
		case j == len(b) || (i < len(a) && a[i] < b[j]):
			out = append(out, "- "+a[i])
			i++
		case i == len(a) || b[j] < a[i]:
			out = append(out, "+ "+b[j])
			j++
		default:
			i, j = i+1, j+1
		}
	}
	if len(out) == 0 {
		return "  (only the generated header differs — regenerate it)\n"
	}
	extra := ""
	if len(out) > shown {
		extra = fmt.Sprintf("  ... and %d more\n", len(out)-shown)
		out = out[:shown]
	}
	return "  " + strings.Join(out, "\n  ") + "\n" + extra
}

func fingerprintLines(content string) []string {
	var out []string
	for line := range strings.SplitSeq(content, "\n") {
		if line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	return out
}

// frozenWire renders the fingerprint: the header, then every observable line
// in sorted order. Sorting is what makes it stable — nothing here depends on
// where in the spec a key happens to be written, so moving a block of YAML
// around is not a wire change and does not read as one.
func frozenWire(t *testing.T) string {
	t.Helper()
	f := &wireFingerprint{t: t, doc: readSpecTree(t)}
	f.walk()
	if len(f.lines) == 0 {
		t.Fatal("no wire found in openapi.yaml: this check now guards nothing")
	}
	slices.Sort(f.lines)
	f.lines = slices.Compact(f.lines)
	return frozenWireHeader + strings.Join(f.lines, "\n") + "\n"
}

func readSpecTree(t *testing.T) map[string]any {
	t.Helper()
	content, err := os.ReadFile(openAPISpec)
	if err != nil {
		t.Fatalf("read openapi.yaml: %v", err)
	}
	var doc any
	if err := yaml.Unmarshal(content, &doc); err != nil {
		t.Fatalf("parse openapi.yaml: %v", err)
	}
	return wireMap(doc)
}

// wireFingerprint accumulates the lines. It reads the spec as a plain tree
// rather than through a typed struct: the point is to see everything a
// client can observe, and a struct only sees the keys somebody thought to
// declare — which is the failure mode this test exists to close.
type wireFingerprint struct {
	t     *testing.T
	doc   map[string]any
	lines []string
}

// add writes one line: a location, then the tokens that describe it. Empty
// tokens drop out, so a caller can pass a value that may not apply.
func (f *wireFingerprint) add(loc string, tokens ...string) {
	for _, tok := range tokens {
		if tok != "" {
			loc += " " + tok
		}
	}
	f.lines = append(f.lines, loc)
}

func (f *wireFingerprint) walk() {
	f.walkPaths()
	f.walkComponents()
	f.walkSecurity()
}

// coreComponents names the components the frozen core can reach — the
// schemas, parameters, headers and responses that travel on a core
// operation, directly or through a $ref. A component only a non-core
// operation uses is that operation's shape, and fingerprinting it would
// freeze half of an endpoint the freeze does not hold (design doc 0107).
// Keys are "<kind>/<name>", matching the fingerprint's own locations.
func (f *wireFingerprint) coreComponents() map[string]bool {
	components := wireMap(f.doc["components"])
	reachable := map[string]bool{}
	var walk func(node any)
	walk = func(node any) {
		switch v := node.(type) {
		case map[string]any:
			if ref := wireStr(v["$ref"]); strings.HasPrefix(ref, "#/components/") {
				key := strings.TrimPrefix(ref, "#/components/")
				if !reachable[key] {
					reachable[key] = true
					kind, name, _ := strings.Cut(key, "/")
					walk(wireMap(components[kind])[name])
				}
			}
			for key, child := range v {
				if key != "$ref" {
					walk(child)
				}
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	for path, item := range wireMap(f.doc["paths"]) {
		if frozenCorePaths[path] {
			walk(item)
		}
	}
	return reachable
}

func (f *wireFingerprint) walkPaths() {
	for path, item := range wireMap(f.doc["paths"]) {
		if !frozenCorePaths[path] {
			continue
		}
		pathItem := wireMap(item)
		shared := wireSeq(pathItem["parameters"])
		for method, node := range pathItem {
			if !httpMethods[method] {
				continue
			}
			op := wireMap(node)
			loc := strings.ToUpper(method) + " " + path
			f.add(loc)
			f.add(loc, "operationId="+wireStr(op["operationId"]))
			for _, p := range append(append([]any{}, shared...), wireSeq(op["parameters"])...) {
				f.parameter(loc, p)
			}
			f.requestBody(loc, wireMap(op["requestBody"]))
			for status, resp := range wireMap(op["responses"]) {
				f.response(loc+" response "+status, wireMap(resp))
			}
		}
	}
}

// parameter records one parameter under the name it travels by, which is the
// name in components/parameters when the operation shares one — an operation
// that says $ref and one that spells the parameter out are the same thing on
// the wire. The shared parameter's own schema is fingerprinted once, where it
// is declared, rather than repeated at every operation that points at it.
func (f *wireFingerprint) parameter(loc string, node any) {
	p := wireMap(node)
	if ref := wireStr(p["$ref"]); ref != "" {
		target := wireMap(wireMap(wireMap(f.doc["components"])["parameters"])[shortRef(ref)])
		if target == nil {
			f.t.Fatalf("%s: parameter $ref %q names no component", loc, ref)
		}
		f.add(f.paramLoc(loc, target), "ref="+shortRef(ref))
		return
	}
	f.schema(f.paramLoc(loc, p), p["schema"], requiredToken(p["required"]))
}

func (f *wireFingerprint) paramLoc(loc string, p map[string]any) string {
	return loc + " " + wireStr(p["in"]) + " " + wireStr(p["name"])
}

func (f *wireFingerprint) requestBody(loc string, body map[string]any) {
	if body == nil {
		return
	}
	for media, node := range wireMap(body["content"]) {
		f.schema(loc+" body "+media, wireMap(node)["schema"], requiredToken(body["required"]))
	}
}

// response records the status code, then the header names and media types
// under it. A response that is a $ref is recorded as the component it names:
// the component itself is fingerprinted once below, so a rename inside it
// shows on one line instead of on every operation that shares it.
func (f *wireFingerprint) response(loc string, resp map[string]any) {
	if ref := wireStr(resp["$ref"]); ref != "" {
		f.add(loc, "ref="+shortRef(ref))
		return
	}
	f.add(loc)
	for name, node := range wireMap(resp["headers"]) {
		f.add(loc+" header "+name, refToken(node))
	}
	for media, node := range wireMap(resp["content"]) {
		f.schema(loc+" content "+media, wireMap(node)["schema"], "")
	}
}

func (f *wireFingerprint) walkComponents() {
	core := f.coreComponents()
	components := wireMap(f.doc["components"])
	for name, node := range wireMap(components["parameters"]) {
		if !core["parameters/"+name] {
			continue
		}
		p := wireMap(node)
		loc := "components/parameters/" + name
		f.add(loc, "name="+wireStr(p["name"]), "in="+wireStr(p["in"]), requiredToken(p["required"]))
		f.schema(loc+" schema", p["schema"], "")
	}
	for name, node := range wireMap(components["headers"]) {
		if !core["headers/"+name] {
			continue
		}
		loc := "components/headers/" + name
		f.add(loc)
		f.schema(loc+" schema", wireMap(node)["schema"], "")
	}
	for name, node := range wireMap(components["responses"]) {
		if !core["responses/"+name] {
			continue
		}
		f.response("components/responses/"+name, wireMap(node))
	}
	for name, node := range wireMap(components["schemas"]) {
		if !core["schemas/"+name] {
			continue
		}
		f.schema("components/schemas/"+name, node, "")
	}
	// Security schemes are named from the document's top-level security
	// requirements rather than by $ref, and those requirements govern the
	// core operations too — so they are all part of what a core caller
	// holds.
	for name, node := range wireMap(components["securitySchemes"]) {
		s := wireMap(node)
		f.add("components/securitySchemes/"+name,
			"type="+wireStr(s["type"]), "scheme="+wireStr(s["scheme"]), "format="+wireStr(s["bearerFormat"]))
	}
}

// walkSecurity records what the API requires of a caller. The empty
// alternative beside GoogleIDToken is the whole of "a public deployment
// answers a caller with no Authorization header" (design docs 0040, 0042),
// and dropping it would be as breaking as dropping an endpoint.
func (f *wireFingerprint) walkSecurity() {
	for _, req := range wireSeq(f.doc["security"]) {
		names := make([]string, 0, 1)
		for name := range wireMap(req) {
			names = append(names, name)
		}
		if len(names) == 0 {
			f.add("security (anonymous)")
			continue
		}
		slices.Sort(names)
		f.add("security " + strings.Join(names, "+"))
	}
}

// schema records one schema node and everything under it: its type, its enum
// verbatim when it has one, and one line per property, fully qualified and
// carrying whether the property is required. Nothing about how the schema is
// described is read — the prose keys are simply never visited.
func (f *wireFingerprint) schema(loc string, node any, required string) {
	s := wireMap(node)
	if s == nil {
		f.add(loc, required)
		return
	}
	if ref := wireStr(s["$ref"]); ref != "" {
		f.add(loc, required, "ref="+shortRef(ref))
		return
	}
	f.add(loc, append([]string{required, typeToken(s["type"]), enumToken(s["enum"])}, facetTokens(s)...)...)

	names := map[string]bool{}
	for _, name := range wireSeq(s["required"]) {
		names[wireStr(name)] = true
	}
	for name, prop := range wireMap(s["properties"]) {
		f.schema(loc+"."+name, prop, requiredToken(names[name]))
		delete(names, name)
	}
	// A required name with no property declared is a hole in the contract,
	// not something to drop silently on the way to the golden file.
	for name := range names {
		f.add(loc+"."+name, "required=true", "(declared required, no such property)")
	}
	if items, ok := s["items"]; ok {
		f.schema(loc+"[]", items, "")
	}
	if extra, ok := s["additionalProperties"]; ok && wireMap(extra) != nil {
		f.schema(loc+".*", extra, "")
	}
	for _, combinator := range []string{"allOf", "anyOf", "oneOf"} {
		for i, sub := range wireSeq(s[combinator]) {
			f.schema(fmt.Sprintf("%s/%s/%d", loc, combinator, i), sub, "")
		}
	}
}

// shortRef is the last segment of a $ref: what the pointer names, without
// the pointer. "#/components/schemas/Summary" is Summary wherever it is
// pointed at from.
func shortRef(ref string) string {
	return ref[strings.LastIndex(ref, "/")+1:]
}

func refToken(node any) string {
	if ref := wireStr(wireMap(node)["$ref"]); ref != "" {
		return "ref=" + shortRef(ref)
	}
	return ""
}

// requiredToken always says which way it went. "absent means optional" would
// make a required field going optional a deletion in the diff rather than a
// change, and that is one of the renames this test exists to catch.
func requiredToken(v any) string {
	if b, ok := v.(bool); ok && b {
		return "required=true"
	}
	return "required=false"
}

func typeToken(v any) string {
	switch t := v.(type) {
	case string:
		return "type=" + t
	case []any:
		names := make([]string, 0, len(t))
		for _, one := range t {
			names = append(names, fmt.Sprint(one))
		}
		return "type=" + strings.Join(names, "|")
	}
	return ""
}

// schemaFacets are the constraint keys, in the order a line carries them.
// Each one narrows or describes what may travel under a name, which makes
// it as much a part of the contract as the name: a `default` decides what
// a caller gets for asking nothing, a `pattern` or `maxLength` decides
// what a validating client refuses, and a `format` tells it how to read
// the string. Listed rather than "every key that is not prose" so a facet
// OpenAPI grows later arrives as a decision to record it, not as a line
// that appears on its own.
var schemaFacets = []string{
	"format", "pattern", "default", "minimum", "maximum", "exclusiveMinimum",
	"exclusiveMaximum", "multipleOf", "minLength", "maxLength", "minItems",
	"maxItems", "uniqueItems", "readOnly", "writeOnly", "nullable",
}

func facetTokens(s map[string]any) []string {
	out := make([]string, 0, len(schemaFacets))
	for _, name := range schemaFacets {
		if v, ok := s[name]; ok {
			out = append(out, name+"="+facetValue(v))
		}
	}
	return out
}

// facetValue quotes a string and leaves a number or a bool bare, so a
// pattern containing a space still reads as one token and `default=0` is
// never confused with `default="0"`.
func facetValue(v any) string {
	if s, ok := v.(string); ok {
		return fmt.Sprintf("%q", s)
	}
	return fmt.Sprint(v)
}

// enumToken quotes each value, so an enum whose values contain a space or
// look like numbers still reads as exactly what the spec wrote. Document
// order is kept: verbatim is the promise.
func enumToken(v any) string {
	values := wireSeq(v)
	if len(values) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(values))
	for _, one := range values {
		quoted = append(quoted, fmt.Sprintf("%q", fmt.Sprint(one)))
	}
	return "enum=[" + strings.Join(quoted, " ") + "]"
}

func wireMap(v any) map[string]any {
	switch m := v.(type) {
	case map[string]any:
		return m
	case map[any]any: // a mapping with a non-string key anywhere in it
		out := make(map[string]any, len(m))
		for k, value := range m {
			out[fmt.Sprint(k)] = value
		}
		return out
	}
	return nil
}

func wireSeq(v any) []any {
	s, _ := v.([]any)
	return s
}

func wireStr(v any) string {
	s, _ := v.(string)
	return s
}

// The rule design doc 0082 leaves outside the freeze rests on two
// judgments this test pins, because both of them fail open: a
// reachability walk that found nothing would call every schema
// response-only, and a diff reader that missed a removal would wave a
// break through as an addition.

// TestResponseOnlySchemasSeparatesRequestFromResponse gives the walk a
// contract with one schema on each side.
//
// Synthetic on purpose. Every request body in api/openapi.yaml is spelled
// out inline, so every component schema there is response-only and the
// real spec cannot show the distinction working — which is exactly the
// shape of a guard that quietly stopped guarding.
func TestResponseOnlySchemasSeparatesRequestFromResponse(t *testing.T) {
	const spec = `
paths:
  /api/v1/thing:
    post:
      requestBody:
        content:
          application/json:
            schema: { $ref: "#/components/schemas/Write" }
      responses:
        "200":
          content:
            application/json:
              schema: { $ref: "#/components/schemas/Read" }
components:
  schemas:
    Write:
      type: object
      properties:
        nested: { $ref: "#/components/schemas/WritePart" }
    WritePart: { type: object }
    Read: { type: object }
`
	var doc any
	if err := yaml.Unmarshal([]byte(spec), &doc); err != nil {
		t.Fatal(err)
	}
	got := responseOnlySchemasIn(wireMap(doc))
	for name, want := range map[string]bool{"Write": false, "WritePart": false, "Read": true} {
		if got[name] != want {
			t.Errorf("%s: response-only = %v, want %v — a schema a client sends cannot grow a "+
				"required property without producing the 400 design doc 0064 §2 introduced",
				name, got[name], want)
		}
	}
	// And the real contract still answers, or the rule guards nothing.
	if real := responseOnlySchemas(t); !real["Stats"] {
		t.Error("Stats reads as request-reachable; it is a response schema")
	}
}

// TestOnlyResponseAdditionsRefusesEverythingElse walks the cases the
// looser message must not cover.
func TestOnlyResponseAdditionsRefusesEverythingElse(t *testing.T) {
	const base = "components/schemas/Stats.misses required=true type=object"
	for _, c := range []struct {
		name   string
		golden string // the checked-in fingerprint
		spec   string // what the contract fingerprints to now
		ok     bool
	}{
		{"an added response property", base,
			base + "\ncomponents/schemas/Stats.dropped required=true type=integer", true},
		{"a removed line", base, "", false},
		{"a changed line", base,
			"components/schemas/Stats.misses required=false type=object", false},
		{"an added operation", base, base + "\nGET /api/v1/new", false},
		// The request half of what 0064 §2 built the unknown-key 400 for
		// (design doc 0101): optional and query, or it is the freeze.
		{"an added optional query parameter", base,
			base + "\nGET /api/v1/stats query weeks required=false type=integer", true},
		{"an added required query parameter", base,
			base + "\nGET /api/v1/stats query weeks required=true type=integer", false},
		{"an added request header", base,
			base + "\nGET /api/v1/stats header Ochakai-Window ref=Window", false},
		// The third place a response schema lives: inline under the
		// operation, with no component to run the reachability walk
		// against (design doc 0114 §5).
		{"an added property on an inline response schema", base,
			base + "\nGET /api/v1/search response 200 content application/json.degraded required=false type=boolean", true},
		{"an added inline response schema on a new operation", base,
			base + "\nGET /api/v1/new\nGET /api/v1/new response 200 content application/json.x required=false type=string", false},
		{"a request body property is not a response one", base,
			base + "\nPUT /api/v1/access requestBody content application/json.rules required=true type=array", false},
		{"an added path parameter", base,
			base + "\nGET /api/v1/stats path id required=false type=string", false},
		{"nothing at all", base, base, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := onlyAdditionsOutsideTheFreeze(t, c.golden, c.spec); got != c.ok {
				t.Errorf("onlyAdditionsOutsideTheFreeze = %v, want %v", got, c.ok)
			}
		})
	}
}
