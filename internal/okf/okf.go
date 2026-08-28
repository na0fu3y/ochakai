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
	// OKF vocabulary: draft | stable | deprecated (design doc 0036 §3.4).
	// omitempty because a concept whose status is unset is a document that
	// said nothing about its lifecycle, and ochakai does not write a key
	// its writer left out (design doc 0046 §3.9). Every stored concept has
	// a status — the write path fills OKF's default in (§5.4) — so the key
	// is absent only where it was absent to begin with: the rendering
	// `ochakai put` and `ochakai import` send for a document that carries
	// no status, which used to reach the store, and a third party's OKF
	// reader, as `status: ""`.
	Status      text        `yaml:"status,omitempty"`
	StatusNote  text        `yaml:"status_note,omitempty"`
	StaleAfter  text        `yaml:"stale_after,omitempty"`
	Runtime     text        `yaml:"runtime,omitempty"`
	Parameters  []parameter `yaml:"parameters,omitempty"`
	Computation text        `yaml:"computation,omitempty"`
	Executor    *executor   `yaml:"executor,omitempty"`
	Attester    *attester   `yaml:"attester,omitempty"`
	CreatedBy   text        `yaml:"created_by,omitempty"` // ochakai extension: OKF records only who produced the current content
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
	Resource text `yaml:"resource"`
	// omitempty because SPEC §10.2 does not require a receipt: a contract
	// that named none is a document that said nothing about the evidence
	// a run returns, and ochakai does not write a key its writer left out
	// (design docs 0046 §3.9, 0079).
	Receipt []text         `yaml:"receipt,omitempty"`
	Extra   map[string]any `yaml:",inline"`
}

type attester struct {
	Resource text           `yaml:"resource"`
	Extra    map[string]any `yaml:",inline"`
}

// event is one entry of the trust family: who, when, and — as producer
// extensions — on whose behalf and with what software. OKF's actor
// convention (SPEC §7) has room for exactly one of those three at a time,
// and folding "A via B" into by would leave a string that is not an
// actor; sibling keys keep by parseable and the human: prefix intact, so
// trust tiers still read correctly (design docs 0027, 0036 §3.2, 0052).
//
// producer holds SPEC §7's "<producer>/<version>" form. It is a sibling
// rather than the value of by for the same reason via is: by is the
// authenticated identity, and moving a self-declared name into it would
// put the one unverified thing here in the one place readers trust
// (design doc 0052 §3.1).
type event struct {
	By       text `yaml:"by,omitempty"` // required by SPEC §5.2; omitted only for an entry that carries no actor at all
	Via      text `yaml:"via,omitempty"`
	Producer text `yaml:"producer,omitempty"`
	At       text `yaml:"at,omitempty"`
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
		"created_by": true, "rejected_by": true, "rejected_at": true, "rejected_note": true,
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
//
// files are the bundle's file objects, listed in the directory each one
// sits in (design doc 0046 §3.7). A file in a directory that holds no
// concept is not listed: the index.md tree is navigation between concept
// documents, and OKF says nothing about a directory of pure data, so
// there is no index.md there to list it in. The file is in the archive
// either way, at its own path — what enters the bundle leaves it (§3.2).
// Indexes generates the index.md of every directory in the bundle.
//
// scope names the subtree when this is an archive of one, so the root
// index can say so (design doc 0134); "" is a whole base.
func Indexes(entries []domain.Knowledge, files []domain.File, scope string) map[string][]byte {
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	root := &dir{}
	for _, k := range entries {
		root.insert(strings.Split(k.ID, "/"), k)
	}
	sorted := append([]domain.File(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	for _, f := range sorted {
		root.insertFile(strings.Split(f.Path, "/"), f)
	}
	out := map[string][]byte{}
	root.writeIndexes(out, "", scope)
	return out
}

// dir is one directory level of a bundle, used to generate index.md files.
type dir struct {
	subdirs map[string]*dir
	entries []dirEntry    // concepts directly in this directory, in bundle order
	files   []domain.File // file objects directly in this directory, by path
	count   int           // concepts in this directory and below
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

// insertFile files one file object under the directory it sits in, if
// that directory is in the tree. It creates none: a directory exists
// here because a concept put it there, and count stays the number of
// concepts beneath — a subdirectory line saying "4 concepts" for four
// data files would misdescribe every directory that has both.
func (d *dir) insertFile(segs []string, f domain.File) {
	if len(segs) == 1 {
		d.files = append(d.files, f)
		return
	}
	if sub := d.subdirs[segs[0]]; sub != nil {
		sub.insertFile(segs[1:], f)
	}
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
func (d *dir) writeIndexes(files map[string][]byte, prefix, scope string) {
	var dirs, concepts []IndexLine
	for _, name := range d.subdirNames() {
		noun := "concepts"
		if d.subdirs[name].count == 1 {
			noun = "concept"
		}
		dirs = append(dirs, IndexLine{Text: name + "/", Target: name + "/index.md",
			Description: fmt.Sprintf("%d %s", d.subdirs[name].count, noun)})
	}
	for _, e := range d.entries {
		title := e.k.Title
		if title == "" {
			title = e.name
		}
		concepts = append(concepts, IndexLine{Text: title, Target: e.name + ".md", Description: e.k.Description})
	}
	// One renderer for both callers: an export writes the whole tree,
	// a read generates one directory, and a bundle whose index.md
	// changed shape depending on which asked would be two formats
	// (design doc 0046 §3.7).
	var fileLines []IndexLine
	for _, f := range d.files {
		fileLines = append(fileLines, FileIndexLine(f.Name, f.MediaType, f.Size))
	}
	sections := []IndexSection{{Lines: dirs}, {Lines: concepts}, {Heading: FileSection, Lines: fileLines}}
	if prefix == "" && scope != "" {
		files["index.md"] = SubtreeIndexDocument(scope, sections)
	} else {
		files[prefix+"index.md"] = IndexDocument(strings.TrimSuffix(prefix, "/"), sections)
	}
	for name, sub := range d.subdirs {
		sub.writeIndexes(files, prefix+name+"/", scope)
	}
}

// actorEvent renders one trust event. The actor's kind:name form is SPEC
// §7 verbatim on both kinds — "human:tanaka@example.co.jp" and
// "process:sync@example.iam.gserviceaccount.com" (design doc 0043 §3.8).
// The convention's third form, "<producer>/<version>", goes out beside it
// on the producer key when the caller named one (design doc 0052 §3.5) —
// never in by, which is the authenticated identity.
//
// Like the rest of the trust family, none of this is read back on import:
// a bundle carries knowledge, and provenance is the receiving instance's
// own observation (design doc 0009).
// actorText renders an actor as the created_by value, and an actor that
// is no actor as nothing at all: Actor.String would write a bare ":",
// which is a value no reader can parse back into one and which OKF's
// convention (SPEC §7) does not have a form for. It is the guard
// actorEvent already applies to `by`, applied to the one other place an
// actor reaches frontmatter. Unreachable for a stored row since
// migration 0022_actor_process.sql; the entries that can still be
// rendered without one are composed in memory.
func actorText(a domain.Actor) text {
	if a.Kind == "" && a.Name == "" {
		return ""
	}
	return text(a.String())
}

func actorEvent(a *domain.Actor, at *time.Time) event {
	var e event
	if a != nil && (a.Kind != "" || a.Name != "") {
		e.By, e.Via, e.Producer = text(a.Kind+":"+a.Name), text(a.Via), text(a.Producer)
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
// returns, and what a revision records.
//
// The editing surfaces read ViewOf instead, which hands back the same
// stored bytes without the owned keys: a document beside an Observed
// that already says who wrote and confirmed the entry.
//
// The stored document is the writer's own bytes (design doc 0046 §2.2),
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
// in it. It is no longer the stored shape (design doc 0046 §2.2 stores
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
		fm.CreatedBy = actorText(k.CreatedBy)
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
	// No rejection keys. A rejection is a deletion (design doc 0135), so
	// there is no live concept carrying one to render — and OKF has no
	// negative signal anywhere in its frontmatter (SPEC §5.3's ladder
	// runs unverified to human-reviewed with no rung below it), so a key
	// here would be this instance's observation travelling as a claim,
	// which 0009 §3.2 and 0075 §3.1 refuse. What travels is the §9 log
	// entry the removal wrote. The reader still ignores the keys on the
	// way in: bundles written before this carry them, and they are the
	// document's own claim rather than truth.

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

// ViewOf assembles a read of one entry: the document it is, the
// projection to read it by, and what this instance observed about it
// (design doc 0043 §3.5).
//
// The document is the stored one — the writer's own bytes, comments, key
// order and all (design doc 0046 §2.2). It has to be: this is the shape
// the web UI's editor and `ochakai get` hand to whoever edits the entry,
// so a rendering here would be a rewrite there, and the promise that a
// document survives a round trip would hold only for the callers that
// asked for text/markdown.
//
// Server-owned keys stay out of it. They are what Observed carries, and
// putting them in the document too would hand an editor keys it is not
// allowed to set — Document is the export form, and this is not it.
//
// The canonical rendering is the fallback for an entry with no stored
// bytes: one composed in memory, or a row read for a listing.
func ViewOf(k *domain.Knowledge) (domain.View, error) {
	doc := []byte(k.Doc)
	if len(doc) == 0 {
		var err error
		if doc, err = Canonical(k); err != nil {
			return domain.View{}, err
		}
	}
	return domain.View{
		ID:         k.ID,
		Document:   string(doc),
		Summary:    domain.SummaryOf(k),
		Observed:   domain.ObservedOf(k),
		Files:      k.Files,
		LinkedFrom: k.LinkedFrom,
	}, nil
}

// TarGzWriter writes a bundle one file at a time. A caller that can
// produce its files lazily — the export endpoint, pulling file
// bytes from the blob store as it goes — then never holds more than one
// file in memory, instead of the whole knowledge base plus every
// file.
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
