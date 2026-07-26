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

	"gopkg.in/yaml.v3"

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
	Type        string       `yaml:"type"`
	Resource    string       `yaml:"resource,omitempty"` // right after type, matching the knowledge-catalog reference bundles
	Title       string       `yaml:"title,omitempty"`    // empty means the filename is the name (design doc 0022) — omitted, so titleless documents round-trip unchanged
	Description string       `yaml:"description,omitempty"`
	Tags        []string     `yaml:"tags,omitempty"`
	Sources     []source     `yaml:"sources,omitempty"`
	UsageWindow *usageWindow `yaml:"usage_window,omitempty"`
	Generated   event        `yaml:"generated"`
	Verified    []event      `yaml:"verified,omitempty"` // absent means unverified — the trust tier is read from this key's presence (SPEC §5.3)
	Status      string       `yaml:"status"`             // OKF vocabulary: draft | stable | deprecated (design doc 0036 §3.4)
	StatusNote  string       `yaml:"status_note,omitempty"`
	StaleAfter  string       `yaml:"stale_after,omitempty"`
	Runtime     string       `yaml:"runtime,omitempty"`
	Parameters  []parameter  `yaml:"parameters,omitempty"`
	Computation string       `yaml:"computation,omitempty"`
	Executor    *executor    `yaml:"executor,omitempty"`
	Attester    *attester    `yaml:"attester,omitempty"`
	CreatedBy   string       `yaml:"created_by"` // ochakai extension: OKF records only who produced the current content
	RejectedBy  string       `yaml:"rejected_by,omitempty"`
	RejectedAt  string       `yaml:"rejected_at,omitempty"`
}

// The YAML spelling of the v0.2 families lives here rather than on the
// domain types, the way frontmatter and event already do: domain carries
// the JSON wire shape, and the bundle format is this package's business.
//
// Dates are written as strings. yaml.v3 quotes a string that would
// otherwise resolve to a timestamp, so last_modified goes out as
// "2026-05-30" and comes back the plain day the producer wrote — the same
// treatment stale_after gets (design doc 0036 §3.7).
type source struct {
	Resource     string       `yaml:"resource"`
	ID           string       `yaml:"id,omitempty"`
	Title        string       `yaml:"title,omitempty"`
	Author       string       `yaml:"author,omitempty"`
	UsageCount   *int         `yaml:"usage_count,omitempty"` // a pointer: zero uses and no count at all are different claims
	LastModified string       `yaml:"last_modified,omitempty"`
	UsageWindow  *usageWindow `yaml:"usage_window,omitempty"`
}

type usageWindow struct {
	From string `yaml:"from,omitempty"`
	To   string `yaml:"to,omitempty"`
}

type parameter struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`
	Required bool   `yaml:"required,omitempty"` // absent and false mean the same thing (SPEC §10.2)
}

type executor struct {
	Resource string   `yaml:"resource"`
	Receipt  []string `yaml:"receipt"`
}

type attester struct {
	Resource string `yaml:"resource"`
}

// event is one entry of the trust family: who, when, and — as a producer
// extension — on whose behalf. OKF's actor convention (SPEC §7) has no
// room for delegation, and folding "A via B" into by would leave a string
// that is not an actor; a sibling key keeps by parseable and the human:
// prefix intact, so trust tiers still read correctly (design docs 0027,
// 0036 §3.2).
type event struct {
	By  string `yaml:"by,omitempty"` // required by SPEC §5.2; omitted only for an entry that carries no actor at all
	Via string `yaml:"via,omitempty"`
	At  string `yaml:"at,omitempty"`
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

// actorEvent renders one trust event. The actor's kind:name form already
// matches SPEC §7 for people ("human:tanaka@example.co.jp"); agents keep
// ochakai's "agent:" prefix rather than being dressed up as a versioned
// producer or relabelled process, because §5.3 keys trust tiers off the
// human: prefix alone and nothing else in the convention is load-bearing
// (design doc 0036 §3.11).
func actorEvent(a *domain.Actor, at *time.Time) event {
	var e event
	if a != nil && (a.Kind != "" || a.Name != "") {
		e.By, e.Via = a.Kind+":"+a.Name, a.Via
	}
	if at != nil {
		e.At = at.UTC().Format(time.RFC3339)
	}
	return e
}

func windowOut(w *domain.UsageWindow) *usageWindow {
	if w == nil {
		return nil
	}
	return &usageWindow{From: w.From, To: w.To}
}

func sourcesOut(in []domain.Source) []source {
	var out []source
	for _, s := range in {
		out = append(out, source{
			Resource: s.Resource, ID: s.ID, Title: s.Title, Author: s.Author,
			UsageCount: s.UsageCount, LastModified: s.LastModified,
			UsageWindow: windowOut(s.UsageWindow),
		})
	}
	return out
}

// Document renders one knowledge entry as an OKF concept document.
//
// The trust family follows SPEC §5.2: generated is who the content stands
// by and when it last changed, verified is the list of confirmations (one,
// for now — ochakai keeps the latest). status carries OKF's three-value
// lifecycle, so "verified" leaves the status key entirely and becomes
// stable plus a verified entry, which is where a v0.2 consumer looks for
// it (design doc 0036 §3.4).
func Document(k *domain.Knowledge) ([]byte, error) {
	fm := frontmatter{
		Type:        string(k.Type),
		Resource:    k.Resource,
		Title:       k.Title,
		Description: k.Description,
		Tags:        k.Tags,
		Sources:     sourcesOut(k.Sources),
		UsageWindow: windowOut(k.UsageWindow),
		Status:      domain.OKFStatus(k.Status),
		StatusNote:  k.StatusNote,
		StaleAfter:  k.StaleAfter,
		Runtime:     k.Runtime,
		Computation: k.Computation,
		Generated:   actorEvent(&k.UpdatedBy, &k.UpdatedAt),
		CreatedBy:   k.CreatedBy.String(),
	}
	for _, p := range k.Parameters {
		fm.Parameters = append(fm.Parameters, parameter{Name: p.Name, Type: p.Type, Required: p.Required})
	}
	if k.Executor != nil {
		fm.Executor = &executor{Resource: k.Executor.Resource, Receipt: k.Executor.Receipt}
	}
	if k.Attester != nil {
		fm.Attester = &attester{Resource: k.Attester.Resource}
	}
	// Status verified always writes a verified entry, even for an entry
	// carrying no verification actor. The key's presence is what a v0.2
	// consumer reads the trust tier from (SPEC §5.3), and what carries
	// "verified" back through an import (design doc 0036 §3.4) — an
	// exported status of stable with no verified key means unverified, and
	// would come back as a draft.
	if k.Status == domain.StatusVerified || k.VerifiedAt != nil || k.VerifiedBy != nil {
		fm.Verified = []event{actorEvent(k.VerifiedBy, k.VerifiedAt)}
	}
	if k.RejectedBy != nil {
		fm.RejectedBy = k.RejectedBy.String()
	}
	if k.RejectedAt != nil {
		fm.RejectedAt = k.RejectedAt.UTC().Format(time.RFC3339)
	}

	fmYAML, err := yaml.Marshal(&fm)
	if err != nil {
		return nil, err
	}
	// Extension attrs go out as top-level keys, after the envelope keys.
	// yaml.Marshal sorts map keys, so the output is deterministic.
	extras := map[string]any{}
	for key, v := range k.Attrs {
		if !reservedKeys[key] {
			extras[key] = v
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
