package okf

import (
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

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
// 2026-12-31` arrives as a time.Time — and an extension key holding one
// (OKF's sources[].last_modified, usage_window) would be stored as JSON
// and come back out as a full RFC 3339 timestamp, quietly turning the
// producer's date into something it did not write. A value with no clock
// component is a date; anything else keeps its instant.
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
// Frontmatter keys fall into three groups. Envelope keys (type, id, title,
// description, tags, status, status_note, stale_after) map to Knowledge
// fields. Server-owned keys (generated, verified, created_by, rejected_*,
// and the v0.1 spellings timestamp / verified_by / verified_at) are
// ignored \u2014 provenance comes from authentication (design doc 0009).
// Everything else is a producer-defined extension key (OKF SPEC \u00a74.1) and
// is kept as-is in attrs, so foreign keys like "sources" or "runtime"
// survive a round-trip at their original top-level position. A nested
// "attrs" map (the shape older ochakai exports wrote) is folded in for
// backward compatibility.
//
// The one thing read out of the trust family is whether a verified key is
// present at all: under SPEC \u00a75.3 that, not the status, is what says a
// concept was confirmed, so it decides how "stable" lands here (design doc
// 0034 \u00a73.4). Its actors and timestamps are still not read.
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
	var fm struct{ typ, resource, id, title, description, status, statusNote, staleAfter string }
	for _, f := range []struct {
		key string
		dst *string
	}{
		{"type", &fm.typ}, {"resource", &fm.resource}, {"id", &fm.id},
		{"title", &fm.title}, {"description", &fm.description},
		{"status", &fm.status}, {"status_note", &fm.statusNote},
		{"stale_after", &fm.staleAfter},
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
		case "type", "resource", "id", "title", "description", "tags", "status", "status_note", "stale_after":
			// envelope, extracted above
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

	return &domain.Knowledge{
		Type:        typ,
		ID:          fm.id,
		Title:       fm.title,
		Description: fm.description,
		Resource:    fm.resource,
		Tags:        tags,
		Status:      status,
		StatusNote:  fm.statusNote,
		StaleAfter:  fm.staleAfter,
		Attrs:       attrs,
		Body:        strings.TrimSpace(body),
	}, fm.typ, notes, nil
}
