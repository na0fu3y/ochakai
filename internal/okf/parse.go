package okf

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// Parse is the inverse of Document: it reads an OKF concept document
// (YAML frontmatter + markdown body) into a knowledge entry, so
// `ochakai get` output round-trips through `ochakai put`. Server-owned
// keys (generated, verified, created_by, rejected_*, and their v0.1
// spellings) never reach a ledger — provenance comes from authentication,
// never from the payload (design doc 0009) — but they are not thrown away
// either: what the document said about itself is kept as a claim under
// ClaimKey (MoveServerKeys). Links are not read here either:
// they are derived from the body's markdown links when the entry is
// written (design doc 0024), so a "# Links" section is just prose that
// happens to contain links, and is picked up as such.
//
// notes carries what the parse decided, for the caller to report: nothing
// here rejects a document over a field value, per SPEC §11, but a
// reinterpreted field should not be silent either.
func Parse(doc []byte) (d *Doc, notes []string, err error) {
	d, rawType, notes, err := parseDoc(doc)
	if err != nil {
		return nil, nil, err
	}
	if d.Type == "" {
		return nil, nil, fmt.Errorf(`unusable type %q (one line, no "/", up to 128 bytes; recommended: %s)`, rawType, domain.TypesHint())
	}
	return d, notes, nil
}

// Doc is one parsed OKF document: the knowledge it carries, plus what the
// trust family it arrived with turned into.
type Doc struct {
	domain.Knowledge
	// Verified reports that the document carried a verified key — or, in a
	// v0.1 document, verified_at. Under SPEC §5.3 the presence of that key
	// is the entire trust signal, and it is what decides how "stable"
	// lands here (design doc 0036 §3.4). Who verified and when stay out of
	// this instance's ledger: they are an assertion the document makes
	// about itself, kept as such under ClaimKey (design doc 0046 §2.2).
	Verified bool
	// Claimed names the server-owned keys that moved under ClaimKey, in
	// document order, and is empty when none did. A caller that can tell a
	// claim from its own observation (IsObservation) reports it — SPEC §11
	// asks a consumer to surface what it read differently than written.
	Claimed []string
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

// scalarText renders a decoded YAML scalar as the text a producer wrote,
// reporting false for a mapping or a list — a collection is not a scalar
// misspelled, so there is nothing to read it as.
//
// It exists because SPEC §4.1 gives title, description, resource, status,
// status_note, stale_after, runtime and computation no YAML type, and
// §11 lets a consumer refuse a document only for unparseable frontmatter
// or a missing type. `title: 2026` and `status: 3` are documents ochakai
// must take (design doc 0079); this is what it takes them as.
func scalarText(v any) (string, bool) {
	switch v := v.(type) {
	case string:
		return v, true
	case bool:
		return strconv.FormatBool(v), true
	case int:
		return strconv.Itoa(v), true
	case int64:
		return strconv.FormatInt(v, 10), true
	case uint64:
		return strconv.FormatUint(v, 10), true
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64), true
	case nil:
		return "", true
	}
	return "", false
}

// Frontmatter returns a document's frontmatter as a plain map, which is
// what the store indexes (design doc 0046 §3.11). Values come back the
// way the rest of this package reads them — a date with no clock stays a
// date rather than becoming an instant (yamlScalar) — so the index says
// what the document says.
//
// Server-owned keys are dropped: they are not in a stored document
// (design doc 0043 §2.2), and a filter that could match on one would be
// asking the index about provenance, which is the ledgers' answer. The
// claim under ClaimKey is not one of them — it is an ordinary key of the
// stored document, and indexing what a document said about itself is not
// indexing what this instance observed. Nothing queries it: `fm.` names
// the keys OKF defines (design doc 0047), and this is ochakai's own.
//
// A document whose frontmatter will not parse yields an empty map rather
// than an error. Storing an entry never depends on being able to index
// it: the document is the truth, and an index that cannot be built from
// it is a gap in querying, not a reason to refuse a write.
func Frontmatter(doc []byte) map[string]any {
	fm, _, ok := splitFrontmatter(string(NormalizeText(doc)))
	if !ok || fm == "" {
		return map[string]any{}
	}
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(fm), &raw); err != nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(raw))
	for k, v := range raw {
		if serverOwnedKeys[k] {
			continue
		}
		out[k] = yamlScalar(v)
	}
	return out
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
// verified_at) map to no field \u2014 provenance comes from authentication
// (design doc 0009) \u2014 and move under ClaimKey, where they are the
// document's claim rather than this instance's observation
// (MoveServerKeys). Everything else is a producer-defined extension key (OKF SPEC
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
// 0036 §3.4). Its actors and timestamps are still not read into a ledger:
// they become a claim beside it, which nothing derives trust from.
func parseDoc(doc []byte) (*Doc, string, []string, error) {
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
	var notes []string
	// SPEC §4.1 gives none of these keys a YAML type, and §11 makes an
	// unparseable frontmatter or a missing type the only reasons a
	// consumer may refuse a document — so `title: 2026` is a title a
	// producer wrote without quotes, not a broken document (design doc
	// 0079). The scalar is read as the text it is written as, with a
	// note; only a collection where a scalar belongs is dropped, because
	// there is no text to read it as.
	str := func(key string) string {
		v, ok := raw[key]
		if !ok || v == nil {
			return ""
		}
		scalar := yamlScalar(v)
		if s, ok := scalar.(string); ok {
			return s
		}
		s, ok := scalarText(scalar)
		if !ok {
			notes = append(notes, fmt.Sprintf("%s is not a scalar; dropped", key))
			return ""
		}
		notes = append(notes, fmt.Sprintf("%s is not a string; read as %q", key, s))
		return s
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
		*f.dst = str(f.key)
	}
	var tags []string
	if v, ok := raw["tags"]; ok && v != nil {
		switch v := yamlScalar(v).(type) {
		case []any:
			for i, t := range v {
				if _, wasString := t.(string); wasString {
					tags = append(tags, t.(string))
					continue
				}
				// Not a string: a number is the tag its producer wrote, a
				// mapping and an empty entry are not tags at all.
				if s, ok := scalarText(t); ok && s != "" {
					notes = append(notes, fmt.Sprintf("tags[%d] is not a string; read as %q", i, s))
					tags = append(tags, s)
					continue
				}
				notes = append(notes, fmt.Sprintf("tags[%d] is not a tag; dropped", i))
			}
		default:
			// The knowledge-catalog reference bundles sometimes write tags as
			// one comma-separated string ("tags: sales, orders"); OKF's
			// permissive consumption model (SPEC §11) says take it, not
			// reject the document. A bare scalar of any other kind is read
			// the same way, for the same reason.
			s, ok := scalarText(v)
			if !ok {
				notes = append(notes, "tags is neither a list nor a scalar; dropped")
				break
			}
			for _, t := range strings.Split(s, ",") {
				if t = strings.TrimSpace(t); t != "" {
					tags = append(tags, t)
				}
			}
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
		case "generated", "verified", "created_by", "rejected_by", "rejected_at", "rejected_note",
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

	_, hasVerified := raw["verified"]
	if !hasVerified {
		// A v0.1 document says the same thing with the flat keys.
		_, hasVerified = raw["verified_at"]
	}
	// The lifecycle value arrives unaltered: ochakai's vocabulary is OKF's
	// (design doc 0043 §3.2), so a producer's stable stays stable whether
	// or not anyone confirmed it. The trust signal rides beside it in
	// Doc.Verified rather than colliding with it, which is what forced the
	// old reading of an unconfirmed stable as a draft.
	status, known := domain.StatusFromOKF(strings.TrimSpace(fm.status))
	if !known {
		notes = append(notes, fmt.Sprintf("status %q is not an OKF lifecycle value (%s); read as %s",
			fm.status, domain.StatusesHint(), status))
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

	// What was received, with the keys this instance owns moved under
	// ClaimKey: the document is stored as its writer wrote it (design doc
	// 0046 §2.2), the fields above are the index derived from it, and what
	// the document said about who generated and confirmed it stays in the
	// bytes as a claim rather than being destroyed.
	stored, claimed, claim := MoveServerKeys(NormalizeText([]byte(s)))
	if claim != nil {
		setAttr(ClaimKey, claim)
	}

	return &Doc{Verified: hasVerified, Claimed: claimed, Knowledge: domain.Knowledge{
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
		Doc:         string(stored),
	}}, fm.typ, notes, nil
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

// sourceKeys is the closed set SPEC §5.1 defines: the keys ochakai reads
// into fields of its own, which is what lets four surfaces describe one
// shape — the form editor and the tool schema are built from them
// (design doc 0036 §3.5). A producer key inside a source mapping is not
// one of them and is not dropped either: extraKeys carries it through
// untouched, because the document is the stored form and a key discarded
// here is a key no later release can recover (design doc 0043 §3.6).
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
		s.Extra = extraKeys(m, sourceKeys)
		out = append(out, s)
	}
	return out, notes
}

var (
	parameterKeys = map[string]bool{"name": true, "type": true, "required": true}
	executorKeys  = map[string]bool{"resource": true, "receipt": true}
	attesterKeys  = map[string]bool{"resource": true}
)

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
		p.Extra = extraKeys(m, parameterKeys)
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
	e := &domain.Executor{Resource: strOf(m, "resource"), Extra: extraKeys(m, executorKeys)}
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
	// SPEC §10.2 names only runtime as REQUIRED; of executor it says
	// "`resource` names run instructions or code" and "`receipt` declares
	// the fields a run must return", with no requirement word on either.
	// The resource is what makes the key mean anything at all — an
	// executor naming nothing to run is not a contract — but a missing
	// receipt is a missing optional field, which §11 forbids rejecting
	// for (design doc 0079). ochakai used to cite §10.2 for a rule §10.2
	// does not state.
	if !e.Valid() {
		return nil, []string{"executor needs a resource (the run instructions or code); dropped"}
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
	a := &domain.Attester{Resource: strOf(m, "resource"), Extra: extraKeys(m, attesterKeys)}
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

// extraKeys collects the producer-defined keys of a structured object —
// everything the spec does not name (SPEC §4.1). They are kept rather
// than dropped: the document is the stored form, so a key discarded here
// is a key no later release can recover (design doc 0043 §3.6).
//
// Nothing is reported, because nothing was reinterpreted. A note is for a
// value read differently than written (design doc 0036 §3.4), and a key
// carried through untouched is not one.
func extraKeys(m map[string]any, known map[string]bool) domain.Extra {
	var extra domain.Extra
	for k, v := range m {
		if known[k] {
			continue
		}
		if extra == nil {
			extra = domain.Extra{}
		}
		extra[k] = yamlScalar(v)
	}
	return extra
}

// CarriesType reports whether a document's frontmatter names a type,
// which is what makes it a concept (SPEC §11 requires the key). A
// markdown file without one is not a malformed concept — it is a file
// that happens to be markdown, and a bundle that carried one gets it
// back (design doc 0046 §3.2).
//
// It parses rather than greps: "type:" inside a fenced block in the body
// is prose, and the answer decides whether the bytes are stored as an
// entry or as a file.
func CarriesType(doc []byte) bool {
	d, _, err := Parse(doc)
	return err == nil && strings.TrimSpace(string(d.Type)) != ""
}
