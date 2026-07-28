package okf

import (
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// This file holds the operations ochakai performs on a document *as
// text*, rather than by parsing it into a struct and rendering it back.
//
// The distinction is the whole of design doc 0046 §2.2: what is stored is
// the document as it was handed over. Re-serializing loses frontmatter
// comments and key order, and OKF asks for neither — what SPEC §4.1 asks
// is that unknown keys be preserved and round-tripped, which a byte-for-
// byte store satisfies by construction. The canonical form (Canonical)
// stays, as a derived value: it is what SameContent compares and what a
// document ochakai composed itself is written in.
//
// So the only edits made to a received document are the ones that have a
// reason: newlines are normalized, the keys this instance owns are taken
// out on the way in and put back on the way out, and a move rewrites the
// body. Each is line- or block-wise, and leaves every byte it is not
// about exactly where it was.

// serverOwnedKeys are the top-level frontmatter keys ochakai writes and
// never reads back: the v0.2 trust family (SPEC §5.2), ochakai's own
// created_by / rejected_* extensions, and the v0.1 spellings (SPEC
// §13.1). Provenance is what this instance observed, never a document's
// claim (design doc 0009), so they are stripped from what a writer sends
// and injected into what a reader gets.
var serverOwnedKeys = map[string]bool{
	"generated": true, "verified": true, "created_by": true,
	"rejected_by": true, "rejected_at": true,
	"timestamp": true, "verified_by": true, "verified_at": true,
}

// NormalizeText is every change made to a received document's bytes
// before they are stored: the BOM is dropped, CRLF becomes LF (OKF
// specifies UTF-8 markdown but not line endings), and the text ends in
// exactly one newline. Nothing else — no NFC normalization of the
// content, which design doc 0022 applies to keys (ids, filenames, link
// targets, queries) and deliberately not to what a writer wrote.
func NormalizeText(raw []byte) []byte {
	s := strings.TrimPrefix(string(raw), "\ufeff")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return []byte(s + "\n")
}

// splitFrontmatter cuts a normalized document into the text inside its
// frontmatter delimiters and everything after them. ok is false when the
// text is not an OKF document, in which case the caller leaves it alone —
// nothing here is worth failing a write over, since a document that does
// not parse never reaches these functions.
func splitFrontmatter(s string) (fm, rest string, ok bool) {
	if !strings.HasPrefix(s, "---\n") {
		return "", s, false
	}
	body := s[len("---\n"):]
	if strings.HasPrefix(body, "---\n") { // empty frontmatter
		return "", body[len("---\n"):], true
	}
	end := strings.Index(body, "\n---\n")
	if end < 0 {
		if strings.HasSuffix(body, "\n---") {
			return body[:end+1], "", true
		}
		return "", s, false
	}
	return body[:end+1], body[end+len("\n---\n"):], true
}

// StripServerKeys removes the server-owned keys from a document's
// frontmatter, leaving every other byte — key order, quoting, comments,
// blank lines — where it was. This is what a write path does with a
// document that carries them: `ochakai get` hands out the export form, so
// a writer must be able to send back what it received, and the values are
// still not read (design doc 0043 §3.5).
//
// Which lines to remove is decided from the parsed frontmatter, not from
// a scan for "key:" at the start of a line. YAML has more than one way to
// write the same key — "generated :", quoted, or with the block indented
// under it — and a spelling the scan missed would survive the strip and
// then collide with the key WithServerKeys appends, turning the export
// form into a document that no longer parses. That is the failure the
// round-trip fuzz property found, and reading the key positions out of
// the parse is what makes it unreachable: what the parser calls a key is
// exactly what is taken out.
func StripServerKeys(raw []byte) []byte {
	s := string(raw)
	fm, rest, ok := splitFrontmatter(s)
	if !ok || fm == "" {
		return raw
	}
	lines := strings.Split(strings.TrimSuffix(fm, "\n"), "\n")
	drop := make([]bool, len(lines)+1) // 1-based, as YAML counts
	for _, r := range ownedKeyRanges(fm, len(lines)) {
		for i := r.from; i <= r.to; i++ {
			drop[i] = true
		}
	}
	var kept []string
	for i, line := range lines {
		if !drop[i+1] {
			kept = append(kept, line)
		}
	}
	if len(kept) == 0 {
		return []byte("---\n---\n" + rest)
	}
	return []byte("---\n" + strings.Join(kept, "\n") + "\n---\n" + rest)
}

type lineRange struct{ from, to int }

// ownedKeyRanges returns the 1-based line ranges the server-owned keys
// occupy in a frontmatter block. A key's block runs from its own line to
// the line before the next key's, minus any trailing blank or comment
// lines — those introduce the key that follows, and belong to it. Unless
// that key is going away too, in which case the blank line between them
// separates nothing and goes with them.
//
// Frontmatter that does not parse as a mapping yields nothing: a document
// that cannot be read is one no write path stores, and guessing at its
// shape here could only damage it.
func ownedKeyRanges(fm string, lines int) []lineRange {
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(fm), &root); err != nil ||
		len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	pairs := root.Content[0].Content
	all := strings.Split(fm, "\n")
	var out []lineRange
	for i := 0; i+1 < len(pairs); i += 2 {
		if !serverOwnedKeys[pairs[i].Value] {
			continue
		}
		r := lineRange{from: pairs[i].Line, to: lines}
		nextOwned := false
		if i+2 < len(pairs) {
			r.to = pairs[i+2].Line - 1
			nextOwned = serverOwnedKeys[pairs[i+2].Value]
		}
		for !nextOwned && r.to > r.from {
			line := strings.TrimSpace(all[r.to-1])
			if line != "" && !strings.HasPrefix(line, "#") {
				break
			}
			r.to--
		}
		out = append(out, r)
	}
	return out
}

// WithServerKeys is the inverse: it appends this instance's observations
// to a stored document's frontmatter, producing the export form — what a
// bundle reader sees, what `ochakai get` hands to an editor, and what a
// GET with Accept: text/markdown returns.
//
// They are appended rather than merged into SPEC's key families because
// merging means re-serializing, and the point of storing the bytes is
// that nobody re-serializes them. SPEC constrains neither key order nor
// position (§4.1), and a reader that cares reads keys, not offsets.
func WithServerKeys(raw []byte, k *domain.Knowledge) ([]byte, error) {
	// generated is SPEC §5.2's "who the content stands by, and when it
	// last meaningfully changed" — so the timestamp is the content's, not
	// the row's (design doc 0046 §3.4).
	g := actorEvent(&k.UpdatedBy, &k.ContentChangedAt)
	own := serverKeysOnly{Generated: &g, CreatedBy: text(k.CreatedBy.String())}
	for i := range k.Verifications {
		v := &k.Verifications[i]
		own.Verified = append(own.Verified, actorEvent(&v.By, &v.At))
	}
	if k.Rejection != nil {
		own.RejectedBy = text(k.Rejection.By.String())
		own.RejectedAt = text(k.Rejection.At.UTC().Format(time.RFC3339))
	}
	owned, err := yaml.Marshal(&own)
	if err != nil {
		return nil, err
	}
	s := string(raw)
	fmText, rest, ok := splitFrontmatter(s)
	if !ok {
		// Not a document with frontmatter — nothing this instance owns
		// has a place to go, and inventing one would corrupt the file.
		return raw, nil
	}
	return []byte("---\n" + fmText + string(owned) + "---\n" + rest), nil
}

// serverKeysOnly is the frontmatter subset WithServerKeys emits. It
// repeats the field types rather than reusing frontmatter so that adding
// a writer-owned key to the renderer cannot silently start appending it
// to every stored document.
type serverKeysOnly struct {
	Generated  *event  `yaml:"generated,omitempty"`
	Verified   []event `yaml:"verified,omitempty"`
	CreatedBy  text    `yaml:"created_by,omitempty"`
	RejectedBy text    `yaml:"rejected_by,omitempty"`
	RejectedAt text    `yaml:"rejected_at,omitempty"`
}

// ReplaceBody swaps a stored document's body, leaving its frontmatter
// byte for byte. A move rewrites the links in a referring entry's body
// (design doc 0021); that is a change to the body and to nothing else, so
// it must not reformat the frontmatter of every entry that happened to
// cite the moved one.
func ReplaceBody(raw []byte, body string) []byte {
	s := string(raw)
	fm, _, ok := splitFrontmatter(s)
	if !ok {
		return NormalizeText([]byte(body))
	}
	head := "---\n" + fm + "---\n"
	if body = strings.TrimSpace(body); body == "" {
		return []byte(head)
	}
	return []byte(head + "\n" + body + "\n")
}
