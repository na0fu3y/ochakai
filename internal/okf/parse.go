package okf

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// Parse is the inverse of Document: it reads an OKF concept document
// (YAML frontmatter + markdown body) into a knowledge entry, so
// `ochakai get` output round-trips through `ochakai update`. Server-owned
// keys (generated, verified, created_by, rejected_*, and their v0.1
// spellings) are ignored — provenance comes from authentication, never
// from the payload (design doc 0009). Links are not read here either:
// they are derived from the body's markdown links when the entry is
// written (design doc 0024), so a "# Links" section is just prose that
// happens to contain links, and is picked up as such.
//
// notes carries what the parse decided, for the caller to report: nothing
// here rejects a document over a field value, per SPEC §11, but a
// reinterpreted field should not be silent either.
func Parse(doc []byte) (k *domain.Knowledge, notes []string, err error) {
	k, rawType, notes, err := parseDoc(doc)
	if err != nil {
		return nil, nil, err
	}
	if k.Type == "" {
		return nil, nil, fmt.Errorf(`unusable type %q (one line, no "/", up to 128 bytes; recommended: %s)`, rawType, domain.TypesHint())
	}
	return k, notes, nil
}

// yamlScalar renders decoded YAML timestamps back as text, recursing into
// lists and mappings. YAML types an unquoted date, so `stale_after:
// 2026-12-31` arrives as a time.Time — as do the envelope's other dates
// (sources[].last_modified, the usage_window bounds) and any extension key
// holding one. Left alone, such a value would be stored as JSON and come
// back out as a full RFC 3339 timestamp, quietly turning the producer's
// date into something it did not write. A value with no clock component
// is a date; anything else keeps its instant.
func yamlScalar(v any) any {
	switch v := v.(type) {
	case time.Time:
		if v.Hour() == 0 && v.Minute() == 0 && v.Second() == 0 && v.Nanosecond() == 0 {
			return v.Format(domain.StaleAfterLayout)
		}
		return v.Format(time.RFC3339)
	case []any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = yamlScalar(v[i])
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, e := range v {
			out[k] = yamlScalar(e)
		}
		return out
	}
	return v
}

// parseDoc parses one OKF document, also returning the frontmatter type
// verbatim so the caller can report an unusable one. k.Type is "" when the
// frontmatter carries no type a knowledge entry can hold.
//
// Frontmatter keys fall into three groups. Envelope keys \u2014 every key OKF
// v0.2 defines (domain.EnvelopeKeys: the base fields, sources and
// usage_window, the lifecycle pair, and the Attested Computation contract)
// \u2014 map to Knowledge fields. Server-owned keys (generated, verified,
// created_by, rejected_*, and the v0.1 spellings timestamp / verified_by /
// verified_at) are ignored \u2014 provenance comes from authentication (design
// doc 0009). Everything else is a producer-defined extension key (OKF SPEC
// §4.1) and is kept as-is in attrs, so a foreign key survives a round-trip
// at its original top-level position. A nested "attrs" map (the shape
// older ochakai exports wrote) is folded in for backward compatibility.
//
// A malformed envelope value never costs the document: it is dropped and
// reported as a note, which the importer prints separately from its skip
// count (design doc 0036 §3.4).
//
// The one thing read out of the trust family is whether a verified key is
// present at all: under SPEC §5.3 that, not the status, is what says a
// concept was confirmed, so it decides how "stable" lands here (design doc
// 0036 §3.4). Its actors and timestamps are still not read.
func parseDoc(doc []byte) (*domain.Knowledge, string, []string, error) {
	s := strings.TrimPrefix(string(doc), "\ufeff")
	// OKF specifies UTF-8 markdown but not line endings; normalize CRLF so
	// the delimiter scan below works on bundles authored on Windows.
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return nil, "", nil, fmt.Errorf("not an OKF document: missing --- frontmatter")
	}
	rest := strings.TrimPrefix(s, "---\n")
	end := strings.Index(rest, "\n---\n")
	var fmPart, body string
	switch {
	case strings.HasPrefix(rest, "---\n"): // empty frontmatter
		body = rest[len("---\n"):]
	case end >= 0:
		fmPart, body = rest[:end+1], rest[end+len("\n---\n"):]
	case strings.HasSuffix(rest, "\n---"): // document ends at the delimiter
		fmPart = rest[:len(rest)-len("---")]
	default:
		return nil, "", nil, fmt.Errorf("not an OKF document: unterminated frontmatter")
	}

	var raw map[string]any
	if err := yaml.Unmarshal([]byte(fmPart), &raw); err != nil {
		return nil, "", nil, fmt.Errorf("invalid frontmatter: %w", err)
	}
	var str = func(key string) (string, error) {
		v, ok := raw[key]
		if !ok || v == nil {
			return "", nil
		}
		s, ok := yamlScalar(v).(string)
		if !ok {
			return "", fmt.Errorf("invalid frontmatter: %s is not a string", key)
		}
		return s, nil
	}
	var fm struct {
		typ, resource, id, title, description, status, statusNote, staleAfter string
		runtime, computation                                                  string
	}
	for _, f := range []struct {
		key string
		dst *string
	}{
		{"type", &fm.typ}, {"resource", &fm.resource}, {"id", &fm.id},
		{"title", &fm.title}, {"description", &fm.description},
		{"status", &fm.status}, {"status_note", &fm.statusNote},
		{"stale_after", &fm.staleAfter},
		{"runtime", &fm.runtime}, {"computation", &fm.computation},
	} {
		var err error
		if *f.dst, err = str(f.key); err != nil {
			return nil, "", nil, err
		}
	}
	var tags []string
	if v, ok := raw["tags"]; ok && v != nil {
		switch v := v.(type) {
		case []any:
			for _, t := range v {
				s, ok := t.(string)
				if !ok {
					return nil, "", nil, fmt.Errorf("invalid frontmatter: tags is not a list of strings")
				}
				tags = append(tags, s)
			}
		case string:
			// The knowledge-catalog reference bundles sometimes write tags as
			// one comma-separated string ("tags: sales, orders"); OKF's
			// permissive consumption model (SPEC §11) says take it, not
			// reject the document.
			for _, s := range strings.Split(v, ",") {
				if s = strings.TrimSpace(s); s != "" {
					tags = append(tags, s)
				}
			}
		default:
			return nil, "", nil, fmt.Errorf("invalid frontmatter: tags is not a list")
		}
	}

	var attrs map[string]any
	setAttr := func(key string, v any) {
		if attrs == nil {
			attrs = map[string]any{}
		}
		attrs[key] = yamlScalar(v)
	}
	for key, v := range raw {
		switch key {
		case "type", "resource", "id", "title", "description", "tags", "status", "status_note", "stale_after",
			"sources", "usage_window", "runtime", "parameters", "computation", "executor", "attester":
			// envelope, extracted above and below
		case "generated", "verified", "created_by", "rejected_by", "rejected_at",
			"timestamp", "verified_by", "verified_at":
			// server-owned, never from the payload — the last three are the
			// v0.1 spellings, still read (and still ignored) so bundles
			// written before v0.2 import unchanged (SPEC §13.1)
		default:
			setAttr(key, v)
		}
	}
	// The frontmatter type is the type, verbatim (design doc 0023).
	typ := domain.Type(strings.TrimSpace(fm.typ))
	if !domain.ValidType(typ) {
		typ = ""
	}

	var notes []string
	_, hasVerified := raw["verified"]
	if !hasVerified {
		// A v0.1 document says the same thing with the flat keys.
		_, hasVerified = raw["verified_at"]
	}
	status, known := domain.StatusFromOKF(strings.TrimSpace(fm.status), hasVerified)
	if !known {
		notes = append(notes, fmt.Sprintf("status %q is not an OKF lifecycle value (draft, stable, deprecated); read as %s", fm.status, status))
	}
	if !domain.ValidStaleAfter(fm.staleAfter) {
		notes = append(notes, fmt.Sprintf("stale_after %q is not a YYYY-MM-DD date; dropped", fm.staleAfter))
		fm.staleAfter = ""
	}

	sources, n := sourcesFrom(raw["sources"])
	notes = append(notes, n...)
	window, n := windowFrom(raw["usage_window"], "usage_window")
	notes = append(notes, n...)
	params, n := parametersFrom(raw["parameters"])
	notes = append(notes, n...)
	exec, n := executorFrom(raw["executor"])
	notes = append(notes, n...)
	att, n := attesterFrom(raw["attester"])
	notes = append(notes, n...)
	// SPEC §10.2 requires runtime on an Attested Computation, but the
	// conformance rule tells consumers to take the document anyway. The
	// write path is where a missing runtime is an error (design doc 0036
	// §3.4); here it is worth saying and nothing more.
	if domain.EqualType(typ, domain.TypeComputations) && fm.runtime == "" {
		notes = append(notes, "an Attested Computation without a runtime cannot be executed by a consumer (SPEC §10.2)")
	}

	return &domain.Knowledge{
		Type:        typ,
		ID:          fm.id,
		Title:       fm.title,
		Description: fm.description,
		Resource:    fm.resource,
		Tags:        tags,
		Sources:     sources,
		UsageWindow: window,
		Status:      status,
		StatusNote:  fm.statusNote,
		StaleAfter:  fm.staleAfter,
		Runtime:     fm.runtime,
		Parameters:  params,
		Computation: fm.computation,
		Executor:    exec,
		Attester:    att,
		Attrs:       attrs,
		Body:        strings.TrimSpace(body),
	}, fm.typ, notes, nil
}

// The v0.2 family readers. All of them follow the same rule: never reject
// the document (SPEC's conformance section tells consumers to accept a
// concept without rejecting it, and design doc 0036 §3.12 fixed the same
// granularity), drop what cannot be read, and return a note so the
// importer can say what it did rather than reinterpreting in silence.
//
// Every value goes through yamlScalar first: YAML types an unquoted date,
// so last_modified and the usage_window bounds arrive as time.Time and
// have to come back as the plain day the producer wrote.

func mapOf(v any) (map[string]any, bool) {
	m, ok := yamlScalar(v).(map[string]any)
	return m, ok
}

func strOf(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return strings.TrimSpace(s)
}

func windowFrom(v any, where string) (*domain.UsageWindow, []string) {
	if v == nil {
		return nil, nil
	}
	m, ok := mapOf(v)
	if !ok {
		return nil, []string{fmt.Sprintf("%s is not a mapping of from/to; dropped", where)}
	}
	w := &domain.UsageWindow{From: strOf(m, "from"), To: strOf(m, "to")}
	var notes []string
	for _, f := range []struct {
		name string
		dst  *string
	}{{"from", &w.From}, {"to", &w.To}} {
		if !domain.ValidDate(*f.dst) {
			notes = append(notes, fmt.Sprintf("%s.%s %q is not a YYYY-MM-DD date; dropped", where, f.name, *f.dst))
			*f.dst = ""
		}
	}
	if w.From == "" && w.To == "" {
		return nil, notes
	}
	return w, notes
}

// sourceKeys is the closed set SPEC §5.1 defines. A producer key inside a
// source mapping is dropped with a note rather than kept: the point of
// modeling sources is that four surfaces can describe one shape, and an
// open map defeats the form editor and the tool schema alike (design doc
// 0036 §3.5).
var sourceKeys = map[string]bool{
	"resource": true, "id": true, "title": true, "author": true,
	"usage_count": true, "last_modified": true, "usage_window": true,
}

func sourcesFrom(v any) ([]domain.Source, []string) {
	if v == nil {
		return nil, nil
	}
	list, ok := yamlScalar(v).([]any)
	if !ok {
		return nil, []string{"sources is not a list; dropped"}
	}
	var out []domain.Source
	var notes []string
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			notes = append(notes, fmt.Sprintf("sources[%d] is not a mapping; dropped", i))
			continue
		}
		s := domain.Source{
			Resource: strOf(m, "resource"),
			ID:       strOf(m, "id"),
			Title:    strOf(m, "title"),
			Author:   strOf(m, "author"),
		}
		if s.Resource == "" {
			notes = append(notes, fmt.Sprintf("sources[%d] names no resource; dropped", i))
			continue
		}
		if lm := strOf(m, "last_modified"); lm != "" {
			if domain.ValidDate(lm) {
				s.LastModified = lm
			} else {
				notes = append(notes, fmt.Sprintf("sources[%d].last_modified %q is not a YYYY-MM-DD date; dropped", i, lm))
			}
		}
		if n, ok := intFrom(m["usage_count"]); ok && n >= 0 {
			s.UsageCount = &n
		} else if m["usage_count"] != nil {
			notes = append(notes, fmt.Sprintf("sources[%d].usage_count is not a non-negative whole number; dropped", i))
		}
		w, wn := windowFrom(m["usage_window"], fmt.Sprintf("sources[%d].usage_window", i))
		s.UsageWindow, notes = w, append(notes, wn...)
		if extra := unknownKeys(m, sourceKeys); len(extra) > 0 {
			notes = append(notes, fmt.Sprintf("sources[%d]: dropped keys OKF does not define: %s", i, strings.Join(extra, ", ")))
		}
		out = append(out, s)
	}
	return out, notes
}

var parameterKeys = map[string]bool{"name": true, "type": true, "required": true}

func parametersFrom(v any) ([]domain.Parameter, []string) {
	if v == nil {
		return nil, nil
	}
	list, ok := yamlScalar(v).([]any)
	if !ok {
		return nil, []string{"parameters is not a list; dropped"}
	}
	var out []domain.Parameter
	var notes []string
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			notes = append(notes, fmt.Sprintf("parameters[%d] is not a mapping; dropped", i))
			continue
		}
		p := domain.Parameter{Name: strOf(m, "name"), Type: strOf(m, "type")}
		if !p.Valid() {
			notes = append(notes, fmt.Sprintf("parameters[%d] needs both a name and a type; dropped", i))
			continue
		}
		p.Required, _ = m["required"].(bool)
		if extra := unknownKeys(m, parameterKeys); len(extra) > 0 {
			notes = append(notes, fmt.Sprintf("parameters[%d]: dropped keys OKF does not define: %s", i, strings.Join(extra, ", ")))
		}
		out = append(out, p)
	}
	return out, notes
}

func executorFrom(v any) (*domain.Executor, []string) {
	if v == nil {
		return nil, nil
	}
	m, ok := mapOf(v)
	if !ok {
		return nil, []string{"executor is not a mapping; dropped"}
	}
	e := &domain.Executor{Resource: strOf(m, "resource")}
	switch r := m["receipt"].(type) {
	case []any:
		for _, f := range r {
			if s, ok := f.(string); ok && strings.TrimSpace(s) != "" {
				e.Receipt = append(e.Receipt, strings.TrimSpace(s))
			}
		}
	case string:
		// A single-field receipt written as a bare scalar rather than a
		// one-item list; SPEC §11's permissive reading says take it.
		if strings.TrimSpace(r) != "" {
			e.Receipt = []string{strings.TrimSpace(r)}
		}
	}
	if !e.Valid() {
		return nil, []string{"executor needs both a resource and a non-empty receipt (SPEC §10.2); dropped"}
	}
	return e, nil
}

func attesterFrom(v any) (*domain.Attester, []string) {
	if v == nil {
		return nil, nil
	}
	m, ok := mapOf(v)
	if !ok {
		return nil, []string{"attester is not a mapping; dropped"}
	}
	a := &domain.Attester{Resource: strOf(m, "resource")}
	if !a.Valid() {
		return nil, []string{"attester needs a resource (SPEC §10.2); dropped"}
	}
	return a, nil
}

// intFrom reads a whole number however the decoder typed it: YAML gives
// int, and a value that has been through JSONB comes back float64.
func intFrom(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case uint64:
		return int(n), true
	case float64:
		if n == float64(int(n)) {
			return int(n), true
		}
	}
	return 0, false
}

func unknownKeys(m map[string]any, known map[string]bool) []string {
	var extra []string
	for k := range m {
		if !known[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra) // map iteration order must not reach a user-visible note
	return extra
}
