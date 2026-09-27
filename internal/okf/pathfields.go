package okf

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// RewritePathFields passes every path a document's frontmatter names
// (domain.PathFieldKeys: resource, sources[].resource, computation,
// executor.resource, attester.resource — OKF SPEC §6.2) through rewrite,
// and returns the document with the ones rewrite changed. The bool says
// whether any did.
//
// It is how a move keeps its promise that nothing it carries is left
// pointed at (design doc 0075 §5): a body link was always repaired, and a
// path in the frontmatter is the same pointer spelled in a key.
//
// The edit is the smallest one the document allows. Where each changed
// value is a scalar on one line — which is how a path is written — only
// that token is swapped, in the quoting it was written in, and every
// other byte stays: indentation, comments, key order. Where one is not,
// the blocks of the keys that changed are rendered again from the parse
// of the writer's own YAML (the splice SetFrontmatterKeys uses), which
// keeps their flow style and comments but not their indentation. A block
// this package cannot address by key (mappingKeys) is returned as it is.
func RewritePathFields(raw []byte, rewrite func(string) (string, bool)) ([]byte, bool, error) {
	doc := NormalizeText(raw)
	fm, rest, ok := splitFrontmatter(string(doc))
	if !ok {
		return raw, false, nil
	}
	pairs, ok := mappingKeys(fm)
	if !ok {
		return raw, false, nil
	}
	var edits []pathEdit
	for i := 0; i+1 < len(pairs); i += 2 {
		if key := pairs[i].Value; slices.Contains(domain.PathFieldKeys, key) {
			edits = append(edits, pathEdits(key, pairs[i+1], rewrite)...)
		}
	}
	if len(edits) == 0 {
		return raw, false, nil
	}
	want := pathValues(pairs, edits)
	if out, ok := swapTokens(fm, edits); ok {
		if back, ok := mappingKeys(out); ok && slices.Equal(pathValues(back, nil), want) {
			return NormalizeText([]byte("---\n" + out + "---\n" + rest)), true, nil
		}
	}
	return renderPathBlocks(fm, rest, pairs, edits, want)
}

// pathEdit is one path scalar and the value rewrite gave it.
type pathEdit struct {
	node  *yaml.Node
	value string
}

// pathEdits finds the path scalars under one path-valued key that rewrite
// changes: the value itself for resource and computation, the resource of
// the mapping for executor and attester, and the resource of each entry
// for sources. A shape other than the one SPEC gives the key is left
// alone — a pointer is only rewritten where it is unambiguously one.
func pathEdits(key string, value *yaml.Node, rewrite func(string) (string, bool)) []pathEdit {
	var edits []pathEdit
	add := func(n *yaml.Node) {
		if n == nil || n.Kind != yaml.ScalarNode {
			return
		}
		if v, ok := rewrite(n.Value); ok && v != n.Value {
			edits = append(edits, pathEdit{n, v})
		}
	}
	for _, n := range pathScalars(key, value) {
		add(n)
	}
	return edits
}

// pathScalars is every node a path-valued key holds a path in, in
// document order.
func pathScalars(key string, value *yaml.Node) []*yaml.Node {
	switch key {
	case "resource", "computation":
		return []*yaml.Node{value}
	case "executor", "attester":
		return []*yaml.Node{mappingValue(value, "resource")}
	case "sources":
		if value.Kind != yaml.SequenceNode {
			return nil
		}
		out := make([]*yaml.Node, 0, len(value.Content))
		for _, entry := range value.Content {
			out = append(out, mappingValue(entry, "resource"))
		}
		return out
	}
	return nil
}

// pathValues lists the path scalars of a frontmatter in document order,
// with edits applied — what the document must read as after the rewrite.
func pathValues(pairs []*yaml.Node, edits []pathEdit) []string {
	var out []string
	for i := 0; i+1 < len(pairs); i += 2 {
		if !slices.Contains(domain.PathFieldKeys, pairs[i].Value) {
			continue
		}
		for _, n := range pathScalars(pairs[i].Value, pairs[i+1]) {
			if n == nil || n.Kind != yaml.ScalarNode {
				continue
			}
			v := n.Value
			for _, e := range edits {
				if e.node == n {
					v = e.value
				}
			}
			out = append(out, v)
		}
	}
	return out
}

// swapTokens replaces each edited scalar's token on its own line, in its
// own quoting. ok is false when a value is not a one-line scalar whose
// token can be found where the parser says it starts — a block scalar, a
// plain scalar folded over lines — and the caller renders instead.
func swapTokens(fm string, edits []pathEdit) (string, bool) {
	lines := strings.Split(fm, "\n")
	// Right to left within a line, so an earlier offset is not moved by a
	// later swap.
	slices.SortFunc(edits, func(a, b pathEdit) int {
		if a.node.Line != b.node.Line {
			return a.node.Line - b.node.Line
		}
		return b.node.Column - a.node.Column
	})
	for _, e := range edits {
		i := e.node.Line - 1
		if i < 0 || i >= len(lines) {
			return "", false
		}
		line := lines[i]
		start := byteOffset(line, e.node.Column-1)
		if start < 0 {
			return "", false
		}
		end, ok := tokenEnd(line, start, e.node)
		if !ok {
			return "", false
		}
		lines[i] = line[:start] + spellLike(e.node, e.value) + line[end:]
	}
	return strings.Join(lines, "\n"), true
}

// byteOffset turns the parser's 0-based character column into a byte
// offset in line, or -1 when the line is shorter.
func byteOffset(line string, col int) int {
	off := 0
	for range col {
		if off >= len(line) {
			return -1
		}
		_, size := utf8.DecodeRuneInString(line[off:])
		off += size
	}
	return off
}

// tokenEnd finds where the scalar that starts at start ends on its line.
func tokenEnd(line string, start int, n *yaml.Node) (int, bool) {
	switch n.Style {
	case 0, yaml.TaggedStyle:
		if !strings.HasPrefix(line[start:], n.Value) {
			return 0, false
		}
		return start + len(n.Value), true
	case yaml.DoubleQuotedStyle:
		if !strings.HasPrefix(line[start:], `"`) {
			return 0, false
		}
		for i := start + 1; i < len(line); i++ {
			switch line[i] {
			case '\\':
				i++
			case '"':
				return i + 1, true
			}
		}
	case yaml.SingleQuotedStyle:
		if !strings.HasPrefix(line[start:], `'`) {
			return 0, false
		}
		for i := start + 1; i < len(line); i++ {
			if line[i] != '\'' {
				continue
			}
			if i+1 < len(line) && line[i+1] == '\'' {
				i++
				continue
			}
			return i + 1, true
		}
	case yaml.LiteralStyle, yaml.FoldedStyle, yaml.FlowStyle:
		// A block scalar spans lines, and flow is a collection's style:
		// neither is a token on one line.
	}
	return 0, false
}

// spellLike writes value in the quoting the old token used. A plain value
// that plain YAML would read differently is double-quoted instead; the
// read-back in RewritePathFields is what finally vouches for the result.
func spellLike(n *yaml.Node, value string) string {
	switch n.Style {
	case yaml.SingleQuotedStyle:
		return "'" + strings.ReplaceAll(value, "'", "''") + "'"
	case yaml.DoubleQuotedStyle:
		return doubleQuoted(value)
	case 0, yaml.TaggedStyle, yaml.LiteralStyle, yaml.FoldedStyle, yaml.FlowStyle:
		// Plain, below. tokenEnd has already refused the multi-line ones.
	}
	if value == "" || strings.ContainsAny(value, "#:'\"{}[],&*!|>%@`\\") ||
		strings.TrimSpace(value) != value {
		return doubleQuoted(value)
	}
	return value
}

func doubleQuoted(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
}

// renderPathBlocks is the fallback: each key with an edit is rendered
// again from its parsed node with the new values in it, and spliced in
// place of its block.
func renderPathBlocks(fm, rest string, pairs []*yaml.Node, edits []pathEdit, want []string) ([]byte, bool, error) {
	for _, e := range edits {
		e.node.Value = e.value
		e.node.Style = 0
		e.node.Tag = "!!str"
	}
	replace := map[string]string{}
	for i := 0; i+1 < len(pairs); i += 2 {
		key, value := pairs[i].Value, pairs[i+1]
		if !slices.ContainsFunc(edits, func(e pathEdit) bool { return slices.Contains(pathScalars(key, value), e.node) }) {
			continue
		}
		out, err := yaml.Marshal(&yaml.Node{
			Kind:    yaml.MappingNode,
			Content: []*yaml.Node{{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value},
		})
		if err != nil {
			return nil, false, fmt.Errorf("%s: %w", key, err)
		}
		replace[key] = string(out)
	}
	present, err := topLevelKeys(fm)
	if err != nil {
		return nil, false, err
	}
	out := spliceKeys(fm, rest, present, replace, nil)
	back, ok := mappingKeys(mustFrontmatter(out))
	if !ok || !slices.Equal(pathValues(back, nil), want) {
		return nil, false, fmt.Errorf("the rewritten frontmatter does not read back as the paths it was given")
	}
	return out, true, nil
}

func mappingValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}
