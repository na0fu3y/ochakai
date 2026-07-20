package okf

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// Parse is the inverse of Document: it reads an OKF concept document
// (YAML frontmatter + markdown body) into a knowledge entry, so
// `ochakai get` output round-trips through `ochakai update`. Server-owned
// keys (timestamp, created_by, verified_*) are ignored — provenance comes
// from authentication, never from the payload. The trailing "# Links"
// section rendered by Document is folded back into structured links.
func Parse(doc []byte) (*domain.Knowledge, error) {
	k, rawType, err := parseDoc(doc)
	if err != nil {
		return nil, err
	}
	if k.Type == "" {
		return nil, fmt.Errorf(`unusable type %q (one line, no "/", up to 128 bytes; recommended: Metric, Golden Query, Insight, Glossary Term, BigQuery Dataset, BigQuery Table, Reference)`, rawType)
	}
	return k, nil
}

// parseDoc parses one OKF document, also returning the frontmatter type
// verbatim so the caller can report an unusable one. k.Type is "" when the
// frontmatter carries no type a knowledge entry can hold.
//
// Frontmatter keys fall into three groups. Envelope keys (type, id, title,
// description, tags, status, status_note) map to Knowledge fields.
// Server-owned keys (timestamp, created_by, verified_*, rejected_*) are
// ignored \u2014 provenance comes from authentication. Everything else is a
// producer-defined extension key (OKF SPEC \u00a74.1) and is kept as-is in
// attrs, so foreign keys like "resource" or "owner" survive a round-trip
// at their original top-level position. A nested "attrs" map (the shape
// older ochakai exports wrote) is folded in for backward compatibility.
func parseDoc(doc []byte) (*domain.Knowledge, string, error) {
	s := strings.TrimPrefix(string(doc), "\ufeff")
	// OKF specifies UTF-8 markdown but not line endings; normalize CRLF so
	// the delimiter scan below works on bundles authored on Windows.
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return nil, "", fmt.Errorf("not an OKF document: missing --- frontmatter")
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
		return nil, "", fmt.Errorf("not an OKF document: unterminated frontmatter")
	}

	var raw map[string]any
	if err := yaml.Unmarshal([]byte(fmPart), &raw); err != nil {
		return nil, "", fmt.Errorf("invalid frontmatter: %w", err)
	}
	var str = func(key string) (string, error) {
		v, ok := raw[key]
		if !ok || v == nil {
			return "", nil
		}
		s, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("invalid frontmatter: %s is not a string", key)
		}
		return s, nil
	}
	var fm struct{ typ, resource, id, title, description, status, statusNote string }
	for _, f := range []struct {
		key string
		dst *string
	}{
		{"type", &fm.typ}, {"resource", &fm.resource}, {"id", &fm.id},
		{"title", &fm.title}, {"description", &fm.description},
		{"status", &fm.status}, {"status_note", &fm.statusNote},
	} {
		var err error
		if *f.dst, err = str(f.key); err != nil {
			return nil, "", err
		}
	}
	var tags []string
	if v, ok := raw["tags"]; ok && v != nil {
		switch v := v.(type) {
		case []any:
			for _, t := range v {
				s, ok := t.(string)
				if !ok {
					return nil, "", fmt.Errorf("invalid frontmatter: tags is not a list of strings")
				}
				tags = append(tags, s)
			}
		case string:
			// The knowledge-catalog reference bundles sometimes write tags as
			// one comma-separated string ("tags: sales, orders"); OKF's
			// permissive consumption model (SPEC §9) says take it, not
			// reject the document.
			for _, s := range strings.Split(v, ",") {
				if s = strings.TrimSpace(s); s != "" {
					tags = append(tags, s)
				}
			}
		default:
			return nil, "", fmt.Errorf("invalid frontmatter: tags is not a list")
		}
	}

	var attrs map[string]any
	setAttr := func(key string, v any) {
		if attrs == nil {
			attrs = map[string]any{}
		}
		attrs[key] = v
	}
	for key, v := range raw {
		switch key {
		case "type", "resource", "id", "title", "description", "tags", "status", "status_note":
			// envelope, extracted above
		case "timestamp", "created_by", "verified_by", "verified_at", "rejected_by", "rejected_at":
			// server-owned, never from the payload
		default:
			setAttr(key, v)
		}
	}
	// The frontmatter type is the type, verbatim (design doc 0023).
	typ := domain.Type(strings.TrimSpace(fm.typ))
	if !domain.ValidType(typ) {
		typ = ""
	}

	trimmedBody, links := splitLinks(strings.TrimSpace(body))
	return &domain.Knowledge{
		Type:        typ,
		ID:          fm.id,
		Title:       fm.title,
		Description: fm.description,
		Resource:    fm.resource,
		Tags:        tags,
		Status:      domain.Status(fm.status),
		StatusNote:  fm.statusNote,
		Links:       links,
		Attrs:       attrs,
		Body:        trimmedBody,
	}, fm.typ, nil
}

var linkLineRe = regexp.MustCompile(`^- ([^:\s]+): \[([^\]]+)\]\([^)]*\)$`)

// splitLinks strips a trailing "# Links" section (as generated by
// Document) and returns it as structured links. A section whose bullets
// don't all match the generated format is treated as ordinary body text.
func splitLinks(body string) (string, []domain.Link) {
	var start int
	switch i := strings.LastIndex(body, "\n# Links\n"); {
	case strings.HasPrefix(body, "# Links\n"):
		start = 0
	case i >= 0:
		start = i + 1
	default:
		return body, nil
	}
	var links []domain.Link
	for _, line := range strings.Split(body[start:], "\n")[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		m := linkLineRe.FindStringSubmatch(line)
		if m == nil {
			return body, nil
		}
		links = append(links, domain.Link{Rel: m[1], Target: m[2]})
	}
	if len(links) == 0 {
		return body, nil
	}
	return strings.TrimSpace(body[:start]), links
}
