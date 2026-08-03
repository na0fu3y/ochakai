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
// thing a client can observe — an operation, a parameter or header wire
// name, a status code, a media type, a schema property with its
// requiredness, type and enum — derived from the spec and sorted, so a diff
// names exactly what moved.
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
# One line per thing a client can observe. Prose (description, summary,
# example, examples) is left out on purpose: documentation stays free to
# improve after the freeze. Moving any line below is a change to a contract
# design doc 0064 froze at /api/v1, and §11 says the only reason left is a
# security defect.

`

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
	t.Errorf(`the frozen REST wire moved:

%s
REST is frozen at /api/v1 (design doc 0064, docs/compatibility.md): this
contract is the address list a client holds onto, and after the freeze the
only reason to move a line above is a security defect (0064 §11). Prose is
not fingerprinted, so a documentation edit never lands here.

If this change is meant, regenerate the golden in the same PR and say in the
description which security defect it closes:

	go test ./cmd/ochakai -run TestFrozenWireHasNotMoved -update`,
		wireDiff(want, got))
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
	for _, line := range strings.Split(content, "\n") {
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

func (f *wireFingerprint) walkPaths() {
	for path, item := range wireMap(f.doc["paths"]) {
		pathItem := wireMap(item)
		shared := wireSeq(pathItem["parameters"])
		for method, node := range pathItem {
			if !httpMethods[method] {
				continue
			}
			op := wireMap(node)
			loc := strings.ToUpper(method) + " " + path
			f.add(loc)
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
	components := wireMap(f.doc["components"])
	for name, node := range wireMap(components["parameters"]) {
		p := wireMap(node)
		loc := "components/parameters/" + name
		f.add(loc, "name="+wireStr(p["name"]), "in="+wireStr(p["in"]), requiredToken(p["required"]))
		f.schema(loc+" schema", p["schema"], "")
	}
	for name, node := range wireMap(components["headers"]) {
		loc := "components/headers/" + name
		f.add(loc)
		f.schema(loc+" schema", wireMap(node)["schema"], "")
	}
	for name, node := range wireMap(components["responses"]) {
		f.response("components/responses/"+name, wireMap(node))
	}
	for name, node := range wireMap(components["schemas"]) {
		f.schema("components/schemas/"+name, node, "")
	}
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
	f.add(loc, required, typeToken(s["type"]), enumToken(s["enum"]))

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
