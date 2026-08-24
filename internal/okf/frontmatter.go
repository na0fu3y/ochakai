package okf

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// The structured face of a document: its frontmatter read as JSON, and
// the same frontmatter written back one key at a time.
//
// This is the surface design doc 0130 §3.2 named as the way back to a
// form editor — "サーバ側に「文書 → 構造」の面を足す", the clause the
// record it folds had written down in advance. What it is not is a second stored form: the
// document stays the concept (0075 §3), this reads one and hands one
// back, and nothing here touches the store.
//
// Why it lives on the server at all, when a form could be filled by a
// parser in the page: the reading half needs YAML, and a hand-written
// subset parser's failure mode is a form that silently drops a producer
// key (0130 §3.1, 0043 §3.6). The writing half needs YAML too, for the
// opposite reason — a browser could emit JSON, which is a subset of
// YAML and therefore always valid, but a `sources:` list flattened onto
// one line is a document nobody wants to read in a diff. Both halves
// are the format's business, and the format's business is here.

// ErrNoFrontmatter is what the structured face answers for text that is
// not an OKF document. Every other operation in this package leaves such
// text alone rather than failing — a move rewrites what it can and stores
// the rest untouched — but there is nothing to project here, and a caller
// filling a form has to be told that rather than shown an empty one.
var ErrNoFrontmatter = errors.New("document has no YAML frontmatter")

// ErrFrontmatterNotAMapping is what it answers for a frontmatter block
// that parses as something other than a mapping: a list, or a bare
// scalar. SPEC §4.1 makes frontmatter a mapping of keys, so this is a
// document no key can be set on.
var ErrFrontmatterNotAMapping = errors.New("frontmatter is not a mapping")

// Structure is one document's frontmatter as structure: the mapping
// decoded to the shapes JSON has, and the key order the document itself
// carries — which JSON objects do not preserve and a form redrawing the
// document would otherwise scramble.
//
// Values are what YAML said, not what ochakai makes of them: no envelope,
// no defaults filled in, no type coerced. A form drawn from this draws
// the document, which is the whole reason the projection (Summary) was
// not widened to do this job instead (0130 §3.1).
type Structure struct {
	Keys   []string       `json:"keys"`
	Values map[string]any `json:"values"`
}

// StructureOf reads a document's frontmatter as structure.
func StructureOf(raw []byte) (*Structure, error) {
	fm, _, ok := splitFrontmatter(string(NormalizeText(raw)))
	if !ok {
		return nil, ErrNoFrontmatter
	}
	// Exactly empty, not merely blank: a frontmatter block holding a
	// lone tab is one YAML refuses to tokenize, and treating it as empty
	// would append a key to a document that does not parse.
	if fm == "" {
		return &Structure{Keys: []string{}, Values: map[string]any{}}, nil
	}
	// mappingKeys, which is the precondition the line surgery rests on:
	// this face reads a document in order to write it back, so a block
	// the removal declines to address is one this must decline to draw
	// as fields. Drawing them and refusing the first save would be a
	// form that can only lie.
	pairs, ok := mappingKeys(fm)
	if !ok {
		return nil, ErrFrontmatterNotAMapping
	}
	out := &Structure{Keys: []string{}, Values: map[string]any{}}
	// keepTimestampText for the reason it exists on the claim path: a
	// date decoded to time.Time has lost the spelling its writer chose,
	// and a form that redraws `stale_after: 2026-12-31` as an instant is
	// one that edited a key nobody touched.
	for _, n := range pairs {
		keepTimestampText(n)
	}
	for i := 0; i+1 < len(pairs); i += 2 {
		key := pairs[i].Value
		var v any
		if err := pairs[i+1].Decode(&v); err != nil {
			continue
		}
		if _, dup := out.Values[key]; !dup {
			out.Keys = append(out.Keys, key)
		}
		out.Values[key] = yamlScalar(v)
	}
	return out, nil
}

// SetFrontmatterKeys returns raw with each key in set given that value
// and each key in unset removed, and every other byte where it was.
//
// A key already in the document is replaced in place — its whole block,
// found by the parse rather than by a scan for "key:" at the start of a
// line, for the reason StripServerKeys reads positions the same way: YAML
// has more than one way to spell a key, and a spelling a scan missed
// would survive and collide with what is appended. A key the document
// does not carry is appended at the end of the frontmatter, which is
// where every key this instance adds goes (appendFrontmatter).
//
// What is not touched: key order, comments, blank lines, the quoting of
// every value the caller did not name, and the body. That is the
// difference between this and rebuilding a document from a form's
// fields, which is the shape that erased data before (documents.js says
// the same thing about the page's own line edits).
//
// A server-owned key is refused rather than written: provenance is what
// this instance observed (design doc 0009), and a face that accepted
// `verified` here would be offering to write it even though the write
// path ignores it — a button that only ever lies.
func SetFrontmatterKeys(raw []byte, set map[string]json.RawMessage, unset []string) ([]byte, error) {
	for _, key := range sortedKeys(set) {
		if serverOwnedKeys[key] {
			return nil, fmt.Errorf("%q is recorded by this instance, not written by hand", key)
		}
	}
	for _, key := range unset {
		if serverOwnedKeys[key] {
			return nil, fmt.Errorf("%q is recorded by this instance, not written by hand", key)
		}
	}
	doc := NormalizeText(raw)
	fm, rest, ok := splitFrontmatter(string(doc))
	if !ok {
		return nil, ErrNoFrontmatter
	}
	present, err := topLevelKeys(fm)
	if err != nil {
		return nil, err
	}

	// Replace and remove in one pass over the lines, so the ranges the
	// parse produced all refer to the same text. Doing them one key at a
	// time would need a reparse between each, and a caller setting two
	// keys would pay for a document rewritten twice.
	replace := map[string]string{}
	for key, v := range set {
		block, err := renderKey(key, v)
		if err != nil {
			return nil, err
		}
		replace[key] = block
	}
	remove := map[string]bool{}
	for _, key := range unset {
		remove[key] = true
	}

	// A plain split is exact here because mappingKeys has already
	// refused every block whose lines the parser would count differently
	// — a bare carriage return, the Unicode breaks, a document marker.
	// An empty block has no lines at all: splitting "" would yield one
	// empty string, a blank line the writer never wrote.
	var lines []string
	if fm != "" {
		lines = strings.Split(strings.TrimSuffix(fm, "\n"), "\n")
	}
	// 1-based, as YAML counts, and one longer so the last line's index
	// is addressable.
	act := make([]string, len(lines)+2)
	drop := make([]bool, len(lines)+2)
	for _, key := range present {
		_, replacing := replace[key]
		if !replacing && !remove[key] {
			continue
		}
		// Every occurrence goes, and the new block takes the place of
		// the first. A document naming one key twice is one yaml.v3
		// parses and this could otherwise leave half of — a second
		// `title:` under a form that reports it set the title once.
		for i, r := range keyRange(fm, len(lines), key) {
			if i == 0 && replacing {
				act[r.from] = key
			}
			for line := r.from; line <= r.to; line++ {
				drop[line] = true
			}
		}
	}

	var kept []string
	for i := range lines {
		if key := act[i+1]; key != "" {
			kept = append(kept, strings.Split(strings.TrimSuffix(replace[key], "\n"), "\n")...)
			delete(replace, key)
		}
		if drop[i+1] {
			continue
		}
		kept = append(kept, lines[i])
	}
	// Whatever is left in replace is a key the document did not carry.
	// Appended in a stable order, because two runs of the same request
	// must produce the same bytes — the content hash is taken over them.
	for _, key := range sortedKeys(replace) {
		kept = append(kept, strings.Split(strings.TrimSuffix(replace[key], "\n"), "\n")...)
	}
	out := []byte("---\n---\n" + rest)
	if len(kept) > 0 {
		out = []byte("---\n" + strings.Join(kept, "\n") + "\n---\n" + rest)
	}
	// Normalized on the way out as well as on the way in, so that setting
	// the same key twice produces the same bytes: what a caller stores
	// has to be what a caller storing it again would store. One pass is
	// one, since NormalizeText is idempotent.
	out = NormalizeText(out)
	if err := verifySpliced(out, set, unset); err != nil {
		return nil, err
	}
	return out, nil
}

// verifySpliced reads the produced document back and requires it to say
// what the caller asked for: every key set is there, every key unset is
// gone. Presence, not value — YAML retypes a scalar on the way through
// and that is the format's business, not a failure.
//
// It is here because the splice is line arithmetic over text a producer
// wrote, and a frontmatter block can be shaped so that the arithmetic is
// right and the result still does not read back — a line of whitespace
// that swallows what follows it, an anchor, a spelling the parser and
// the line map disagree about. Rather than enumerate those, the function
// checks the one thing that matters: **a document handed back is one the
// next read agrees with.** A form whose save silently did nothing, and
// whose next save then wrote the key twice, is the failure mode design
// doc 0130 §3.1 refused a browser-side parser over — it must not come back
// on this side either.
func verifySpliced(out []byte, set map[string]json.RawMessage, unset []string) error {
	if len(set) == 0 && len(unset) == 0 {
		return nil
	}
	keys, err := topLevelKeys(mustFrontmatter(out))
	if err != nil {
		return fmt.Errorf("the result would not parse: %w", err)
	}
	has := make(map[string]bool, len(keys))
	for _, k := range keys {
		has[k] = true
	}
	for _, k := range sortedKeys(set) {
		if !has[k] {
			return fmt.Errorf("this frontmatter cannot be edited by key: %q did not read back after it was written", k)
		}
	}
	for _, k := range unset {
		if has[k] {
			return fmt.Errorf("this frontmatter cannot be edited by key: %q is still there after it was removed", k)
		}
	}
	return nil
}

func mustFrontmatter(doc []byte) string {
	fm, _, _ := splitFrontmatter(string(doc))
	return fm
}

// keyRange is ownedKeyRanges for one named key — every line range the
// key occupies, in document order.
func keyRange(fm string, lines int, key string) []lineRange {
	return ownedKeyRanges(fm, lines, func(k string) bool { return k == key })
}

// topLevelKeys names the frontmatter's own keys, in document order, or
// reports that the block is not one this face can address (mappingKeys).
func topLevelKeys(fm string) ([]string, error) {
	if fm == "" {
		return nil, nil
	}
	pairs, ok := mappingKeys(fm)
	if !ok {
		return nil, ErrFrontmatterNotAMapping
	}
	out := make([]string, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, pairs[i].Value)
	}
	return out, nil
}

// renderKey renders one key and its value as the YAML lines that stand
// for it — block style, indented as the rest of the frontmatter is, so
// what a form writes reads like what a person writes.
//
// The value arrives as JSON rather than as a decoded map because **JSON
// objects are ordered on the wire and Go maps are not**: yaml.Marshal of
// a map sorts, so a source written from a form came out
// `author, id, last_modified, resource, …` while the bundle writer emits
// SPEC's own order. Walking the JSON keeps whatever order the caller
// wrote, which is the caller's business and not this package's opinion —
// the same posture every other edit here takes toward a document.
func renderKey(key string, v json.RawMessage) (string, error) {
	if key == "" || strings.ContainsAny(key, ":\n#") {
		return "", fmt.Errorf("%q is not a frontmatter key", key)
	}
	value, err := jsonAsYAML(v)
	if err != nil {
		return "", fmt.Errorf("%q: %w", key, err)
	}
	// yaml.Marshal, with no indent set, so a block written from a field
	// is indented exactly as the bundle writer (Canonical) would have
	// written it. A document whose sources block is indented differently
	// from the one beside it is a diff nobody asked for, and the one
	// spelling that cannot be wrong here is this package's own.
	out, err := yaml.Marshal(&yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value,
		},
	})
	if err != nil {
		return "", err
	}
	// The key has to survive its own rendering, which is not something
	// the type system says: a key whose bytes are not valid UTF-8 goes
	// out as `!!binary qA==`, comes back as something else, and is then
	// appended a second time by the next write because the first is no
	// longer found. Asking the parser closes that whole class rather
	// than the one spelling the fuzzer happened to reach.
	if back, err := topLevelKeys(string(out)); err != nil || len(back) != 1 || back[0] != key {
		return "", fmt.Errorf("%q is not a frontmatter key ochakai can write", key)
	}
	return string(out), nil
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// jsonAsYAML turns one JSON value into the YAML node that stands for it,
// in the order the JSON was written.
//
// Strings carry an explicit !!str tag so a value that looks like a date
// or a number keeps its quotes and comes back the string it went in as
// (design doc 0036 §3.7 is the same rule for stale_after), and the one
// shape yaml would emit as a block scalar the parser cannot read back is
// double-quoted instead — which is what the `text` type does for every
// other value this package writes.
func jsonAsYAML(raw json.RawMessage) (*yaml.Node, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	n, err := jsonNode(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("a value must be a single JSON value")
	}
	return n, nil
}

func jsonNode(dec *json.Decoder) (*yaml.Node, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '[':
			seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			for dec.More() {
				item, err := jsonNode(dec)
				if err != nil {
					return nil, err
				}
				seq.Content = append(seq.Content, item)
			}
			_, err := dec.Token() // ]
			return seq, err
		case '{':
			m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			for dec.More() {
				name, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := name.(string)
				if !ok {
					return nil, errors.New("a mapping key must be a string")
				}
				value, err := jsonNode(dec)
				if err != nil {
					return nil, err
				}
				m.Content = append(m.Content,
					&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
			}
			_, err := dec.Token() // }
			return m, err
		}
		return nil, errors.New("unbalanced JSON")
	case string:
		if blockScalarRisk(t) {
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Style: yaml.DoubleQuotedStyle, Value: t}, nil
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: t}, nil
	case json.Number:
		tag := "!!int"
		if strings.ContainsAny(t.String(), ".eE") {
			tag = "!!float"
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: t.String()}, nil
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(t)}, nil
	case nil:
		// A key set to null is a key that says nothing. Written rather
		// than refused: the document is the writer's, and `null` is a
		// value YAML has.
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	}
	return nil, fmt.Errorf("unsupported JSON value %T", tok)
}
