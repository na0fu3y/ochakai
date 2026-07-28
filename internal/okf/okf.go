// Package okf exports the knowledge base as an Open Knowledge Format
// bundle (https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf):
// a directory of markdown files with YAML frontmatter, one concept per
// knowledge entry at its id's path (the layout is the user's, design doc
// 0017), with index.md files for progressive disclosure. Your knowledge
// is yours — bundles are plain files that live happily in git.
package okf

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// The type key needs no translation here: ochakai's types are the OKF
// vocabulary verbatim ("BigQuery Table", not a slug), so export writes
// k.Type and import reads it back unchanged (design doc 0023). The
// mapping table, the alias table, the slugifier, and the okf_type
// spelling-preservation attr that earlier revisions needed are all gone —
// nothing is lost on the way out, so nothing has to be restored.

// Version is the OKF spec version ochakai produces, declared in the
// bundle-root index.md (SPEC §12).
const Version = "0.2"

// frontmatter is the OKF frontmatter for one concept: the required "type",
// the recommended keys, the v0.2 trust and lifecycle families (SPEC §5),
// and ochakai extension keys. Entry attrs are not nested here — Document
// emits them as producer-defined top-level keys (SPEC §4.1), so foreign
// extension keys round-trip in place.
//
// The v0.1 spellings are gone, following SPEC §13.1: timestamp is now
// generated.at, and verified_by/verified_at are now a verified list. Parse
// still reads the old keys so bundles written by ochakai 0.12 and earlier
// import unchanged — but nothing writes them again.
// The key order follows the spec's own families — base (§4.1), provenance
// (§5.1), trust (§5.2), lifecycle (§5.4/§5.5), Attested Computation
// (§10.2) — so a document reads in the order the spec introduces it, with
// ochakai's extension keys last.
type frontmatter struct {
	Type        text         `yaml:"type"`
	Resource    text         `yaml:"resource,omitempty"` // right after type, matching the knowledge-catalog reference bundles
	Title       text         `yaml:"title,omitempty"`    // empty means the filename is the name (design doc 0022) — omitted, so titleless documents round-trip unchanged
	Description text         `yaml:"description,omitempty"`
	Tags        []text       `yaml:"tags,omitempty"`
	Sources     []source     `yaml:"sources,omitempty"`
	UsageWindow *usageWindow `yaml:"usage_window,omitempty"`
	Generated   *event       `yaml:"generated,omitempty"` // omitted only by Canonical, which writes no server-owned key
	Verified    []event      `yaml:"verified,omitempty"`  // absent means unverified — the trust tier is read from this key's presence (SPEC §5.3)
	Status      text         `yaml:"status"`              // OKF vocabulary: draft | stable | deprecated (design doc 0036 §3.4)
	StatusNote  text         `yaml:"status_note,omitempty"`
	StaleAfter  text         `yaml:"stale_after,omitempty"`
	Runtime     text         `yaml:"runtime,omitempty"`
	Parameters  []parameter  `yaml:"parameters,omitempty"`
	Computation text         `yaml:"computation,omitempty"`
	Executor    *executor    `yaml:"executor,omitempty"`
	Attester    *attester    `yaml:"attester,omitempty"`
	CreatedBy   text         `yaml:"created_by,omitempty"` // ochakai extension: OKF records only who produced the current content
	RejectedBy  text         `yaml:"rejected_by,omitempty"`
	RejectedAt  text         `yaml:"rejected_at,omitempty"`
}

// The YAML spelling of the v0.2 families lives here rather than on the
// domain types, the way frontmatter and event already do: domain carries
// the JSON wire shape, and the bundle format is this package's business.
//
// Dates are written as strings. yaml.v3 quotes a string that would
// otherwise resolve to a timestamp, so last_modified goes out as
// "2026-05-30" and comes back the plain day the producer wrote — the same
// treatment stale_after gets (design doc 0036 §3.7).
// The Extra fields below are yaml ",inline": a producer key comes back
// out beside the keys the spec defines, exactly where its writer put it
// (SPEC §4.1, design doc 0043 §3.6). yaml.v3 sorts an inlined map's
// keys, so the rendering stays deterministic — which the content hash
// depends on.
type source struct {
	Resource     text           `yaml:"resource"`
	ID           text           `yaml:"id,omitempty"`
	Title        text           `yaml:"title,omitempty"`
	Author       text           `yaml:"author,omitempty"`
	UsageCount   *int           `yaml:"usage_count,omitempty"` // a pointer: zero uses and no count at all are different claims
	LastModified text           `yaml:"last_modified,omitempty"`
	UsageWindow  *usageWindow   `yaml:"usage_window,omitempty"`
	Extra        map[string]any `yaml:",inline"`
}

type usageWindow struct {
	From text `yaml:"from,omitempty"`
	To   text `yaml:"to,omitempty"`
}

type parameter struct {
	Name     text           `yaml:"name"`
	Type     text           `yaml:"type"`
	Required bool           `yaml:"required,omitempty"` // absent and false mean the same thing (SPEC §10.2)
	Extra    map[string]any `yaml:",inline"`
}

type executor struct {
	Resource text           `yaml:"resource"`
	Receipt  []text         `yaml:"receipt"`
	Extra    map[string]any `yaml:",inline"`
}

type attester struct {
	Resource text           `yaml:"resource"`
	Extra    map[string]any `yaml:",inline"`
}

// event is one entry of the trust family: who, when, and — as a producer
// extension — on whose behalf. OKF's actor convention (SPEC §7) has no
// room for delegation, and folding "A via B" into by would leave a string
// that is not an actor; a sibling key keeps by parseable and the human:
// prefix intact, so trust tiers still read correctly (design docs 0027,
// 0036 §3.2).
type event struct {
	By  text `yaml:"by,omitempty"` // required by SPEC §5.2; omitted only for an entry that carries no actor at all
	Via text `yaml:"via,omitempty"`
	At  text `yaml:"at,omitempty"`
}

// text is a frontmatter string that never goes out as a block scalar the
// parser cannot read back.
//
// A block scalar takes its indentation from its first content line, so a
// value opening with whitespace — or with a blank line — needs an
// explicit indentation indicator. The emitter does not reliably write
// one, and the document it then produces fails to parse: "found a tab
// character where an indentation space is expected", or, inside a
// sequence, "did not find expected '-' indicator". An entry holding such
// a value exported into a bundle whose own document ochakai skipped on
// re-import (design doc 0019) instead of round-tripping (0036 §3.12).
//
// Which shapes break varies with the yaml version, and with whether the
// value sits in a mapping or a sequence. Every attempt to enumerate them
// found one more, so this covers the family: a multi-line value opening
// with a space, a tab or a newline goes out quoted. Anything else keeps
// the style the emitter picks, so ordinary multi-line prose stays a block
// scalar — which is how an exported bundle wants to read in a Git diff
// (design doc 0009).
//
// The style has to be set here rather than on the marshaled node tree:
// yaml.Node.Encode emits and re-parses internally, so it hits the same
// fault before there is a tree to correct.
type text string

func (s text) MarshalYAML() (any, error) {
	// Everything but the risky shape goes out as a plain string, exactly
	// as it did before this type existed. That keeps the handling yaml
	// already gets right — a date string quoted so it does not resolve
	// back as a timestamp (0036 §3.7), bytes that are not valid UTF-8
	// written as !!binary — out of this decision.
	if !blockScalarRisk(string(s)) {
		return string(s), nil
	}
	// !!str because an explicit node no longer gets its tag resolved from
	// the value: without it a quoted 2026-12-31 would lose its quotes.
	return &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   "!!str",
		Style: yaml.DoubleQuotedStyle,
		Value: string(s),
	}, nil
}

// blockScalarRisk reports whether s is the shape described on text: a
// value spanning lines that opens with a space, a tab or a newline.
// Invalid UTF-8 is left alone — yaml has its own answer for that, and it
// is not this one's business to override it.
func blockScalarRisk(s string) bool {
	return strings.Contains(s, "\n") && strings.TrimLeft(s, " \t\n") != s &&
		utf8.ValidString(s)
}

// texts converts a slice of plain strings into frontmatter text.
func texts(in []string) []text {
	if in == nil {
		return nil
	}
	out := make([]text, len(in))
	for i, s := range in {
		out[i] = text(s)
	}
	return out
}

// textify converts every string inside a decoded attr value to text, so
// producer extension keys are emitted under the same rule as the envelope
// (see text). Attrs arrive from JSON or YAML, so these are the only kinds
// that can hold one.
// textifyMap is textify over a producer's extension keys, returning nil
// for an empty map so an inlined field adds nothing to the rendering.
func textifyMap(m domain.Extra) map[string]any {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = textify(v)
	}
	return out
}

func textify(v any) any {
	switch v := v.(type) {
	case string:
		return text(v)
	case []any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = textify(v[i])
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, e := range v {
			out[key] = textify(e)
		}
		return out
	}
	return v
}

// reservedKeys are the frontmatter keys the envelope owns. An attr with a
// reserved name is not exported as an extension key: the envelope value
// wins. It is every envelope key (domain.EnvelopeKeys, which the write
// path also refuses to accept inside attrs) plus the server-owned
// provenance keys and the v0.1 trust spellings — the latter because an
// entry that imported one into attrs before 0.13.0 must not re-emit it
// beside the v0.2 key that replaced it.
var reservedKeys = func() map[string]bool {
	m := map[string]bool{
		"generated": true, "verified": true,
		"created_by": true, "rejected_by": true, "rejected_at": true,
		"timestamp": true, "verified_by": true, "verified_at": true,
	}
	for _, k := range domain.EnvelopeKeys {
		m[k] = true
	}
	return m
}()

// Indexes renders a bundle's directory index.md files and nothing else,
// sorting entries by id on the way (Bundle's ordering). Split out so a
// streaming exporter can render one concept document at a time — the
// indexes need every id, but only the ids.
func Indexes(entries []domain.Knowledge) map[string][]byte {
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	root := &dir{}
	for _, k := range entries {
		root.insert(strings.Split(k.ID, "/"), k)
	}
	files := map[string][]byte{}
	root.writeIndexes(files, "")
	return files
}

// dir is one directory level of a bundle, used to generate index.md files.
type dir struct {
	subdirs map[string]*dir
	entries []dirEntry // concepts directly in this directory, in bundle order
	count   int        // concepts in this directory and below
}

type dirEntry struct {
	name string // filename without .md
	k    domain.Knowledge
}

func (d *dir) insert(segs []string, k domain.Knowledge) {
	d.count++
	if len(segs) == 1 {
		d.entries = append(d.entries, dirEntry{name: segs[0], k: k})
		return
	}
	if d.subdirs == nil {
		d.subdirs = map[string]*dir{}
	}
	sub := d.subdirs[segs[0]]
	if sub == nil {
		sub = &dir{}
		d.subdirs[segs[0]] = sub
	}
	sub.insert(segs[1:], k)
}

// subdirNames lists subdirectories alphabetically — every level the
// same, with no special root ordering (the root is no longer a type
// listing, design doc 0017).
func (d *dir) subdirNames() []string {
	names := make([]string, 0, len(d.subdirs))
	for n := range d.subdirs {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// writeIndexes emits index.md for this directory and recurses. prefix is
// the bundle-relative directory path, "" for the root or "a/b/" below it.
// Index files carry no frontmatter (SPEC §6) — except the bundle root,
// where an okf_version declaration is the one permitted block (§11) — and
// list their entries with relative links, as in the spec's examples.
func (d *dir) writeIndexes(files map[string][]byte, prefix string) {
	var b strings.Builder
	if prefix == "" {
		fmt.Fprintf(&b, "---\nokf_version: %q\n---\n\n# ochakai knowledge bundle\n\n", Version)
	} else {
		fmt.Fprintf(&b, "# %s\n\n", strings.TrimSuffix(prefix, "/"))
	}
	for _, name := range d.subdirNames() {
		noun := "concepts"
		if d.subdirs[name].count == 1 {
			noun = "concept"
		}
		fmt.Fprintf(&b, "* [%s/](%s/index.md) - %d %s\n", name, name, d.subdirs[name].count, noun)
	}
	for _, e := range d.entries {
		title := e.k.Title
		if title == "" {
			title = e.name
		}
		desc := e.k.Description
		if desc != "" {
			desc = " - " + desc
		}
		fmt.Fprintf(&b, "* [%s](%s.md)%s\n", title, e.name, desc)
	}
	files[prefix+"index.md"] = []byte(b.String())
	for name, sub := range d.subdirs {
		sub.writeIndexes(files, prefix+name+"/")
	}
}

// actorEvent renders one trust event. The actor's kind:name form is SPEC
// §7 verbatim on both kinds — "human:tanaka@example.co.jp" and
// "process:sync@example.iam.gserviceaccount.com" (design doc 0043 §3.8).
// The convention's third form, "<producer>/<version>", stays unused: a
// service account has no version to name.
func actorEvent(a *domain.Actor, at *time.Time) event {
	var e event
	if a != nil && (a.Kind != "" || a.Name != "") {
		e.By, e.Via = text(a.Kind+":"+a.Name), text(a.Via)
	}
	if at != nil {
		e.At = text(at.UTC().Format(time.RFC3339))
	}
	return e
}

func windowOut(w *domain.UsageWindow) *usageWindow {
	if w == nil {
		return nil
	}
	return &usageWindow{From: text(w.From), To: text(w.To)}
}

func sourcesOut(in []domain.Source) []source {
	var out []source
	for _, s := range in {
		out = append(out, source{
			Resource: text(s.Resource), ID: text(s.ID), Title: text(s.Title), Author: text(s.Author),
			UsageCount: s.UsageCount, LastModified: text(s.LastModified),
			UsageWindow: windowOut(s.UsageWindow),
			Extra:       textifyMap(s.Extra),
		})
	}
	return out
}

// Document is the export form of one entry: the document as stored, plus
// the keys this instance owns and only ever writes — generated and
// verified (SPEC §5.2), and ochakai's created_by / rejected_* extensions.
// This is what an export writes, what a read with Accept: text/markdown
// returns, and what `ochakai get` hands to an editor.
//
// The stored document is the writer's own bytes (design doc 0044 §2.2),
// so the keys are appended to its frontmatter rather than merged into a
// re-serialization of it. An entry that was composed in memory instead of
// parsed from a document has no such bytes; its canonical form is its
// document, which is also what the fallback below writes for a row read
// without the doc column.
//
// The trust family follows SPEC §5.2: generated is who the content
// stands by and when it last changed, verified is every confirmation the
// instance recorded. status carries OKF's three-value lifecycle and
// nothing else — whether anyone confirmed the entry is the verified
// key's business (§5.3, design doc 0043 §3.2).
func Document(k *domain.Knowledge) ([]byte, error) {
	if k.Doc != "" {
		return WithServerKeys([]byte(k.Doc), k)
	}
	return render(k, true)
}

// Canonical renders an entry in one normal form, with no server-owned key
// in it. It is no longer the stored shape (design doc 0044 §2.2 stores
// the bytes as received); it is the derived value the store compares to
// decide whether a write changed what the entry says, and the form
// ochakai writes when it composes a document itself rather than being
// handed one.
//
// It is a pure function of the fields SameContent compares, so two
// entries render alike if and only if they say the same thing.
func Canonical(k *domain.Knowledge) ([]byte, error) { return render(k, false) }

func render(k *domain.Knowledge, serverKeys bool) ([]byte, error) {
	fm := frontmatter{
		Type:        text(k.Type),
		Resource:    text(k.Resource),
		Title:       text(k.Title),
		Description: text(k.Description),
		Tags:        texts(k.Tags),
		Sources:     sourcesOut(k.Sources),
		UsageWindow: windowOut(k.UsageWindow),
		Status:      text(string(k.Status)),
		StatusNote:  text(k.StatusNote),
		StaleAfter:  text(k.StaleAfter),
		Runtime:     text(k.Runtime),
		Computation: text(k.Computation),
	}
	if serverKeys {
		g := actorEvent(&k.UpdatedBy, &k.ContentChangedAt)
		fm.Generated = &g
		fm.CreatedBy = text(k.CreatedBy.String())
	}
	for _, p := range k.Parameters {
		fm.Parameters = append(fm.Parameters, parameter{
			Name: text(p.Name), Type: text(p.Type), Required: p.Required, Extra: textifyMap(p.Extra)})
	}
	if k.Executor != nil {
		fm.Executor = &executor{Resource: text(k.Executor.Resource), Receipt: texts(k.Executor.Receipt),
			Extra: textifyMap(k.Executor.Extra)}
	}
	if k.Attester != nil {
		fm.Attester = &attester{Resource: text(k.Attester.Resource), Extra: textifyMap(k.Attester.Extra)}
	}
	// The whole verification ledger, oldest first — SPEC §5.2's list, with
	// as many entries as the instance recorded. The key's presence is what
	// a v0.2 consumer reads the trust tier from (§5.3); its absence means
	// unverified, so an entry with no verifications writes no key at all.
	if serverKeys {
		for i := range k.Verifications {
			v := &k.Verifications[i]
			fm.Verified = append(fm.Verified, actorEvent(&v.By, &v.At))
		}
	}
	// A rejection is this instance's ruling and not a portable claim, so
	// it stays an ochakai extension key that import never reads back
	// (design docs 0009, 0043 §3.3). The status beside it is now the
	// entry's real lifecycle value rather than deprecated, which is what
	// the ruling used to be folded onto — an assertion ("this was once
	// current") that a rejection specifically denies.
	if serverKeys && k.Rejection != nil {
		fm.RejectedBy = text(k.Rejection.By.String())
		fm.RejectedAt = text(k.Rejection.At.UTC().Format(time.RFC3339))
	}

	fmYAML, err := yaml.Marshal(&fm)
	if err != nil {
		return nil, err
	}
	// Extension attrs go out as top-level keys, after the envelope keys.
	// yaml sorts map keys, so the output is deterministic.
	extras := map[string]any{}
	for key, v := range k.Attrs {
		if !reservedKeys[key] {
			extras[key] = textify(v)
		}
	}
	var exYAML []byte
	if len(extras) > 0 {
		if exYAML, err = yaml.Marshal(extras); err != nil {
			return nil, err
		}
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.Write(fmYAML)
	b.Write(exYAML)
	b.WriteString("---\n")
	// Links are not rendered: they are derived from the body's markdown
	// links (design doc 0024), so they are already in the text below.
	// Writing a "# Links" section here would duplicate them on re-import.
	if body := strings.TrimSpace(k.Body); body != "" {
		b.WriteString("\n" + body + "\n")
	}
	return []byte(b.String()), nil
}

// TarGzWriter writes a bundle one file at a time. A caller that can
// produce its files lazily — the export endpoint, pulling attachment
// bytes from the blob store as it goes — then never holds more than one
// file in memory, instead of the whole knowledge base plus every
// attachment.
type TarGzWriter struct {
	gz      *gzip.Writer
	tw      *tar.Writer
	modTime time.Time
}

func NewTarGzWriter(w io.Writer, modTime time.Time) *TarGzWriter {
	gz := gzip.NewWriter(w)
	return &TarGzWriter{gz: gz, tw: tar.NewWriter(gz), modTime: modTime.UTC()}
}

// Add appends one file, in the order the caller adds them: nothing is
// sorted here because nothing is collected.
func (t *TarGzWriter) Add(path string, data []byte) error {
	if err := t.tw.WriteHeader(&tar.Header{
		Name:    path,
		Mode:    0o644,
		Size:    int64(len(data)),
		ModTime: t.modTime,
	}); err != nil {
		return err
	}
	_, err := t.tw.Write(data)
	return err
}

func (t *TarGzWriter) Close() error {
	if err := t.tw.Close(); err != nil {
		return err
	}
	return t.gz.Close()
}
