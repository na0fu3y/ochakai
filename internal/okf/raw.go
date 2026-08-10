package okf

import (
	"bytes"
	"encoding/json"
	"fmt"
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
// never reads back into its ledgers: the v0.2 trust family (SPEC §5.2),
// ochakai's own created_by / rejected_* extensions, and the v0.1
// spellings (SPEC §13.1). Provenance is what this instance observed,
// never a document's claim (design doc 0009), so they are taken out of
// what a writer sends and injected into what a reader gets.
//
// Taken out is not thrown away. What the document said about itself is
// kept under ClaimKey (MoveServerKeys) — a claim, held apart from the
// observation, because a key discarded on the way in is a key no later
// release can recover (design docs 0046 §2.2, 0043 §3.6).
var serverOwnedKeys = map[string]bool{
	"generated": true, "verified": true, "created_by": true,
	"rejected_by": true, "rejected_at": true,
	"timestamp": true, "verified_by": true, "verified_at": true,
}

// ClaimKey is the frontmatter key a received document's own trust family
// is kept under. It is an ochakai extension key (SPEC §4.1), sitting
// beside created_by and rejected_* — with the difference that it is the
// one key here holding what the *document* asserted rather than what the
// instance observed.
//
// The nesting is what keeps the two apart in one file: `verified` is
// always this instance's ledger and `received.verified` is always
// somebody else's assertion, so a reader never has to work out which of
// two same-named keys it is looking at, and the export form can carry
// both without colliding.
const ClaimKey = "received"

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

// Body is what a document says after its frontmatter — the prose a
// reader reads. A document that is not OKF is its own body, since there
// is no header to skip.
//
// Exported for the context pack's excerpt (design doc 0093): the front of
// a document is mostly keys the caller already has as fields, so an
// excerpt that started there would spend its bytes repeating the outline
// row above it.
func Body(document string) string {
	_, rest, _ := splitFrontmatter(document)
	return strings.TrimLeft(rest, "\n")
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
	return stripKeys(raw, func(key string) bool { return serverOwnedKeys[key] })
}

// stripKeys is StripServerKeys generalized over which keys go: it also
// takes ClaimKey off again when a claim turns out to be this instance's
// own observation coming home (DropClaim).
func stripKeys(raw []byte, owned func(string) bool) []byte {
	s := string(raw)
	fm, rest, ok := splitFrontmatter(s)
	if !ok || fm == "" {
		return raw
	}
	lines := strings.Split(strings.TrimSuffix(fm, "\n"), "\n")
	drop := make([]bool, len(lines)+1) // 1-based, as YAML counts
	for _, r := range ownedKeyRanges(fm, len(lines), owned) {
		for i := r.from; i <= r.to; i++ {
			drop[i] = true
		}
	}
	var kept []string
	dropped := false
	for i, line := range lines {
		if drop[i+1] {
			dropped = true
			continue
		}
		kept = append(kept, line)
	}
	// Nothing matched: hand back the same bytes rather than a rebuilt
	// copy, so a document with no owned key in it is untouched.
	if !dropped {
		return raw
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
func ownedKeyRanges(fm string, lines int, owned func(string) bool) []lineRange {
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(fm), &root); err != nil ||
		len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	pairs := root.Content[0].Content
	all := strings.Split(fm, "\n")
	var out []lineRange
	for i := 0; i+1 < len(pairs); i += 2 {
		if !owned(pairs[i].Value) {
			continue
		}
		r := lineRange{from: pairs[i].Line, to: lines}
		nextOwned := false
		if i+2 < len(pairs) {
			r.to = pairs[i+2].Line - 1
			nextOwned = owned(pairs[i+2].Value)
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

// MoveServerKeys is what a write path does with the keys this instance
// owns: it takes them out of the received document's frontmatter, and
// puts what they said back under ClaimKey.
//
// claimed names the keys that moved, in document order, and is empty when
// nothing did. claim is what ClaimKey now holds, read back out of the
// document that was produced rather than out of the values that went in —
// so the map a caller indexes and the bytes it stores cannot disagree
// about a value YAML re-types on the way through.
//
// A document that already carries a claim keeps it, and the trust family
// beside it is dropped rather than moved. What rides beside a claim is
// an ochakai instance's export form, so it is an *observation* — and an
// observation is what import has never read back (design docs 0009, 0046
// §2.3). That is also what makes get → edit → PUT and export → review →
// import store the bytes they stored before, for an entry that holds a
// claim: ClaimKey records what the document asserted, not a chain of the
// instances that carried it.
//
// The other case — a trust family with no claim beside it, which is
// every document from outside an ochakai instance and every export form
// of an entry that holds no claim — is the one the caller has to rule
// on: only it knows whether this instance observed it (DropClaim).
func MoveServerKeys(raw []byte) (doc []byte, claimed []string, claim map[string]any) {
	stripped := stripKeys(raw, func(key string) bool { return serverOwnedKeys[key] })
	fm, _, ok := splitFrontmatter(string(NormalizeText(raw)))
	if !ok || fm == "" {
		return stripped, nil, nil
	}
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(fm), &root); err != nil ||
		len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return stripped, nil, nil
	}
	pairs := root.Content[0].Content
	moved := map[string]any{}
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i].Value == ClaimKey {
			return stripped, nil, nil
		}
		if !serverOwnedKeys[pairs[i].Value] {
			continue
		}
		var v any
		if err := pairs[i+1].Decode(&v); err != nil {
			continue
		}
		claimed = append(claimed, pairs[i].Value)
		moved[pairs[i].Value] = yamlScalar(v)
	}
	if len(moved) == 0 {
		return stripped, nil, nil
	}
	// textify for the same reason render does: a value whose shape would
	// go out as a block scalar the parser cannot read back would turn the
	// stored document into one that no longer parses.
	block, err := yaml.Marshal(map[string]any{ClaimKey: textify(moved)})
	if err != nil {
		return stripped, nil, nil
	}
	out := appendFrontmatter(stripped, block)
	return out, claimed, ClaimOf(out)
}

// ClaimOf reads ClaimKey back out of a document, which is how a caller
// gets the map to index beside the bytes it stores — and how one holding
// only the stored text (the CLI, which parses a bundle itself) asks
// whether the claim it sent survived the write. nil when the document
// carries no claim.
func ClaimOf(doc []byte) map[string]any {
	fm, _, ok := splitFrontmatter(string(doc))
	if !ok || fm == "" {
		return nil
	}
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(fm), &raw); err != nil {
		return nil
	}
	m, _ := yamlScalar(raw[ClaimKey]).(map[string]any)
	return m
}

// DropClaim takes ClaimKey back off a document and the map derived from
// it, leaving the bytes StripServerKeys would have produced. The caller
// that decides a claim was this instance's own observation coming home
// (IsObservation) uses it to undo the move.
func DropClaim(k *domain.Knowledge) {
	delete(k.Attrs, ClaimKey)
	if len(k.Attrs) == 0 {
		k.Attrs = nil
	}
	if k.Doc == "" {
		return
	}
	k.Doc = string(stripKeys([]byte(k.Doc), func(key string) bool { return key == ClaimKey }))
}

// IsObservation reports whether a claim says exactly what this instance
// observed about k — which means it is not a claim at all, but ochakai's
// own export form handed back to it.
//
// It compares the whole trust family rather than looking for a marker,
// because there is no marker to look for: the export form is a plain OKF
// document, and what makes one of them "ours" is that every actor and
// every timestamp in it is one this instance recorded.
func IsObservation(claim any, k *domain.Knowledge) bool {
	m, ok := claim.(map[string]any)
	if !ok || len(m) == 0 || k == nil {
		return false
	}
	observed, err := observedKeys(k)
	if err != nil {
		return false
	}
	a, errA := json.Marshal(m)
	b, errB := json.Marshal(observed)
	return errA == nil && errB == nil && bytes.Equal(a, b)
}

// observedKeys renders what WithServerKeys would append for k, in the
// shape MoveServerKeys stores a claim in, so the two can be compared.
func observedKeys(k *domain.Knowledge) (map[string]any, error) {
	owned, err := yaml.Marshal(serverKeysFor(k))
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := yaml.Unmarshal(owned, &m); err != nil {
		return nil, err
	}
	out, _ := yamlScalar(m).(map[string]any)
	return out, nil
}

// NoteClaim adds ClaimNote to a write's notes when the claim the parse
// kept survived the write. Whether a trust family was a claim or this
// instance's own export form coming home is the write path's answer
// (IsObservation), so the report waits for it: stored is the entry as it
// ended up, and a claim still on it is one the document made and ochakai
// did not observe.
func NoteClaim(notes, claimed []string, stored *domain.Knowledge) []string {
	if stored == nil {
		return notes
	}
	return noteClaim(notes, claimed, stored.Attrs[ClaimKey] != nil)
}

// NoteStoredClaim is NoteClaim for a caller holding the stored document
// rather than the entry: `ochakai import` parses the bundle itself, so
// the move happens before the server sees the keys and the answer has to
// be read back out of what was stored.
func NoteStoredClaim(notes, claimed []string, stored string) []string {
	return noteClaim(notes, claimed, ClaimOf([]byte(stored)) != nil)
}

func noteClaim(notes, claimed []string, kept bool) []string {
	if len(claimed) == 0 || !kept {
		return notes
	}
	return append(notes, ClaimNote(claimed))
}

// ClaimNote is what a write says about a claim it kept: SPEC §11 asks
// a consumer to surface what it read differently than written, and a
// trust family that became a claim is exactly that.
func ClaimNote(keys []string) string {
	is := "is not"
	if len(keys) > 1 {
		is = "are not"
	}
	return fmt.Sprintf("%s %s this instance's observation; read as the document's own claim and kept "+
		"under %q, which no trust tier and no trust= filter answers from",
		strings.Join(keys, ", "), is, ClaimKey)
}

// appendFrontmatter puts a rendered block at the end of a document's
// frontmatter, which is where every key this instance adds goes: the
// stored document is the writer's own bytes, so nothing already in it
// moves (design doc 0046 §2.2).
func appendFrontmatter(raw, block []byte) []byte {
	fm, rest, ok := splitFrontmatter(string(raw))
	if !ok {
		return raw
	}
	return []byte("---\n" + fm + string(block) + "---\n" + rest)
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
	owned, err := yaml.Marshal(serverKeysFor(k))
	if err != nil {
		return nil, err
	}
	// A document with no frontmatter has nowhere for the keys to go, and
	// inventing a block would corrupt the file — appendFrontmatter hands
	// such a file back untouched.
	return appendFrontmatter(raw, owned), nil
}

// serverKeysFor is the trust family this instance would publish for k —
// the block WithServerKeys appends, and the one IsObservation compares a
// claim against, so the two can never drift apart.
func serverKeysFor(k *domain.Knowledge) *serverKeysOnly {
	// generated is SPEC §5.2's "who the content stands by, and when it
	// last meaningfully changed" — so the timestamp is the content's, not
	// the row's (design doc 0046 §3.4).
	g := actorEvent(&k.UpdatedBy, &k.ContentChangedAt)
	own := &serverKeysOnly{Generated: &g, CreatedBy: actorText(k.CreatedBy)}
	for i := range k.Verifications {
		v := &k.Verifications[i]
		own.Verified = append(own.Verified, actorEvent(&v.By, &v.At))
	}
	if k.Rejection != nil {
		own.RejectedBy = text(k.Rejection.By.String())
		own.RejectedAt = text(k.Rejection.At.UTC().Format(time.RFC3339))
	}
	return own
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
