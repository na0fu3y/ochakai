package restapi

// The contract test beside this one runs every request and response past
// api/openapi.yaml, which catches a server that stopped answering what
// the spec promises. It cannot catch the opposite drift: JSON Schema
// validates what is present, so a schema may declare a field the server
// has never once written and every response still passes.
//
// Three had. `View` advertised created_at, updated_at, trust and
// content_changed_at — four fields no release ever sent, three of them
// living in `summary` and the fourth in `observed.generated.at`.
// `ContextRank` declared `verified: boolean` while the server sent
// `trust`, so the spec's own example disagreed with the schema above it
// and a generated client waited for a field that never came. Both had
// been wrong for releases.
//
// This closes that direction: the schemas ochakai owns are compared to
// the Go types that marshal into them, in both directions, so a field
// renamed in Go without the spec — or promised in the spec without Go —
// fails here rather than in somebody's generated client (design doc 0035:
// read the invariant from outside rather than trust the convention).
//
// additionalProperties: false would have been the other way to say this,
// and it does not work here: `SearchHit` is an allOf over `Summary`, and
// closing either member makes the other's fields "additional". The
// footgun is in JSON Schema rather than in the spec, so the check lives
// in Go.

import (
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	yaml "go.yaml.in/yaml/v3"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/service"
)

// specSchema is as much of a schema as this check reads: the keys that
// decide which property names a shape has.
type specSchema struct {
	Ref        string                 `yaml:"$ref"`
	Properties map[string]*specSchema `yaml:"properties"`
	Items      *specSchema            `yaml:"items"`
	AllOf      []*specSchema          `yaml:"allOf"`
}

type specDoc struct {
	Components struct {
		Schemas map[string]*specSchema `yaml:"schemas"`
	} `yaml:"components"`
}

func loadSpecSchemas(t *testing.T) *specDoc {
	t.Helper()
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read %s: %v", specPath, err)
	}
	doc := &specDoc{}
	if err := yaml.Unmarshal(raw, doc); err != nil {
		t.Fatalf("parse %s: %v", specPath, err)
	}
	if len(doc.Components.Schemas) == 0 {
		t.Fatalf("%s declares no component schemas: this check now guards nothing", specPath)
	}
	return doc
}

// deref follows a local $ref. Only components/schemas targets exist here,
// and a $ref anywhere else is a mistake worth failing on rather than
// skipping quietly.
func (d *specDoc) deref(t *testing.T, s *specSchema) *specSchema {
	t.Helper()
	for s != nil && s.Ref != "" {
		name, ok := strings.CutPrefix(s.Ref, "#/components/schemas/")
		if !ok {
			t.Fatalf("$ref %q is not a component schema", s.Ref)
		}
		next, ok := d.Components.Schemas[name]
		if !ok {
			t.Fatalf("$ref %q names no schema", s.Ref)
		}
		s = next
	}
	return s
}

// properties is every property name a shape has, with allOf members
// unioned in — the composition `SearchHit` uses to say "a Summary, plus
// what ranked it".
func (d *specDoc) properties(t *testing.T, s *specSchema) map[string]bool {
	t.Helper()
	s = d.deref(t, s)
	out := map[string]bool{}
	if s == nil {
		return out
	}
	for name := range s.Properties {
		out[name] = true
	}
	for _, member := range s.AllOf {
		for name := range d.properties(t, member) {
			out[name] = true
		}
	}
	return out
}

// lookup resolves a dotted path into the schemas: "Stats" is the named
// schema, "Stats.concepts" the shape of that property, and a trailing "[]"
// steps into an array's items. Nested shapes are spelled inline in the
// spec, so this is how they get checked without giving each one a name it
// does not need.
func (d *specDoc) lookup(t *testing.T, path string) *specSchema {
	t.Helper()
	parts := strings.Split(path, ".")
	schema, ok := d.Components.Schemas[strings.TrimSuffix(parts[0], "[]")]
	if !ok {
		t.Fatalf("%s declares no schema %q", specPath, parts[0])
	}
	for _, part := range parts[1:] {
		name, array := strings.CutSuffix(part, "[]")
		props := d.deref(t, schema)
		next, ok := props.Properties[name]
		if !ok {
			t.Fatalf("%s: schema path %q has no property %q", specPath, path, name)
		}
		schema = next
		if array {
			if schema = d.deref(t, schema).Items; schema == nil {
				t.Fatalf("%s: schema path %q is not an array", specPath, path)
			}
		}
	}
	return schema
}

// jsonFields is what encoding/json will actually write for a type:
// embedded structs flattened the way the marshaller flattens them, and
// fields it never emits left out.
func jsonFields(t *testing.T, typ reflect.Type) map[string]bool {
	t.Helper()
	for typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		t.Fatalf("%s is not a struct", typ)
	}
	out := map[string]bool{}
	for i := range typ.NumField() {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		tag := field.Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name == "-" {
			continue
		}
		if field.Anonymous && name == "" {
			for embedded := range jsonFields(t, field.Type) {
				out[embedded] = true
			}
			continue
		}
		if name == "" {
			name = field.Name
		}
		out[name] = true
	}
	return out
}

// The response shapes ochakai owns, against the Go values that become
// them. A schema absent from this table is one nothing checks, so a new
// one belongs here in the same PR that adds it.
//
// Request bodies are not here: they are read into anonymous structs at
// the handler, and the contract test already fails when a request the
// integration tests send does not validate.
func ownedSchemas() map[string]any {
	return map[string]any{
		"Summary":              domain.Summary{},
		"View":                 domain.View{},
		"SearchHit":            domain.SearchHit{},
		"ContextRank":          domain.ContextRank{},
		"ContextOutline":       domain.ContextOutline{},
		"Usage":                domain.Usage{},
		"QueueCounts":          domain.QueueCounts{},
		"File":                 domain.File{},
		"Actor":                domain.Actor{},
		"Link":                 domain.Link{},
		"Verification":         domain.Verification{},
		"Rejection":            domain.Rejection{},
		"Generated":            domain.Generated{},
		"Observed":             domain.Observed{},
		"Revision":             domain.Revision{},
		"BundleListing":        service.BrowseResult{},
		"Change":               change{},
		"Stats":                domain.Stats{},
		"Stats.concepts":       domain.StatsConcepts{},
		"Stats.review":         domain.StatsReview{},
		"Stats.outcomes":       domain.StatsOutcomes{},
		"Stats.misses":         domain.StatsMisses{},
		"Stats.misses.queries": domain.MissedQuery{},
	}
}

func TestOpenAPISchemasMatchGoTypes(t *testing.T) {
	doc := loadSpecSchemas(t)
	for path, value := range ownedSchemas() {
		t.Run(path, func(t *testing.T) {
			typ := reflect.TypeOf(value)
			schema := doc.lookup(t, path)
			// A property whose value is an array of the shape is spelled
			// "queries" in the spec and checked against the element type,
			// so step into items when the Go side is the element.
			if items := doc.deref(t, schema).Items; items != nil && typ.Kind() == reflect.Struct {
				if len(doc.properties(t, schema)) == 0 {
					schema = items
				}
			}
			want := doc.properties(t, schema)
			got := jsonFields(t, typ)
			for name := range want {
				if !got[name] {
					t.Errorf("%s declares %q, which %s never writes.\n\n"+
						"A schema may not advertise a field the server does not send: a client\n"+
						"generated from this spec waits for it forever. Remove it, or write it.",
						path, name, typ)
				}
			}
			for name := range got {
				if !want[name] {
					t.Errorf("%s writes %q, which the %s schema does not declare.\n\n"+
						"api/openapi.yaml is the public wire surface (design doc 0015 §2), so a\n"+
						"field reaching a caller undeclared is a surface nobody counted.\n"+
						"Declare it there, or drop the json tag.",
						typ, name, path)
				}
			}
			if t.Failed() {
				t.Logf("spec: %s\nGo:   %s", sortedNames(want), sortedNames(got))
			}
		})
	}
}

func sortedNames(set map[string]bool) string {
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, " ")
}
