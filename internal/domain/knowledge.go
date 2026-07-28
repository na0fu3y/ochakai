// Package domain defines the knowledge model shared by the store, MCP
// server, and REST API. See docs/design/0001-architecture.md §3.
package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Type is the kind of a knowledge entry. Any single-line string is a valid
// type (design doc 0005): the built-in types below are recommendations,
// never a closed set — users' own document types are first-class. The
// values are the OKF type vocabulary verbatim, so import and export are
// identity on the type key (design doc 0023); the list is a vocabulary,
// not a registry of server behavior (design doc 0028 §3).
type Type string

const (
	TypeMetrics      Type = "Metric"               // semantic metric definition, synonyms
	TypeComputations Type = "Attested Computation" // sanctioned computation and the means to check a run of it (OKF SPEC §10, design doc 0036 §3.6)
	TypeSkills       Type = "Skill"                // the procedure an executor.resource points at (OKF SPEC §10.2)
	TypePlaybooks    Type = "Playbook"             // an operational procedure people and agents follow (OKF SPEC §4.4)
	TypeInsights     Type = "Insight"              // how to read a metric: baselines, caveats
	TypePolicies     Type = "Policy"               // the rule that decides a number; what a sources[].resource cites
	TypeTerms        Type = "Glossary Term"        // glossary term
	TypeDatasets     Type = "BigQuery Dataset"     // dataset: a container grouping tables
	TypeTables       Type = "BigQuery Table"       // table catalog entry
	TypeEndpoints    Type = "API Endpoint"         // catalog entry for an asset that is not a BigQuery one
	TypeReferences   Type = "Reference"            // mirror of external material (enums, licenses, schema docs)
)

// Types lists the recommended (built-in) knowledge types, in display order:
// definitions, then the things you run, then interpretation and the rules
// behind it, then catalog assets (containers before their contents), then
// mirrors of outside material.
//
// No server behavior hangs off any of them — the list is a vocabulary, not
// a registry (design doc 0028 §3). Attested Computation is the clearest
// case: ochakai records the contract (runtime, parameters, executor,
// attester) as envelope fields and never runs it. Holding a field is not
// acting on it — executing the computation and checking the receipt belong
// to the consumer (OKF SPEC §10.5), which is the same line design doc 0028
// drew when compile_sql went away.
//
// What earns a place is SPEC §4.1's one requirement of a producer — that
// the spelling be descriptive and self-explanatory — read against what OKF
// itself spells out (design doc 0038). "Semantic Model" and "Golden Query"
// left on the first count: the one carried its meaning in a foreign spec
// rather than in its spelling, and the other is what an Attested
// Computation with a runtime already is. Skill, Playbook, Policy and API
// Endpoint joined on the second — Skill and Policy are what an entry's own
// executor.resource and sources[].resource point at, so leaving them out
// left both ends of an edge ochakai already stores without a name.
//
// The retired spellings stay first-class as free types; only the
// recommendation is gone.
var Types = []Type{TypeMetrics, TypeComputations, TypeSkills, TypePlaybooks, TypeInsights,
	TypePolicies, TypeTerms, TypeDatasets, TypeTables, TypeEndpoints, TypeReferences}

// TypesHint renders the recommended vocabulary for help and error text,
// so no surface keeps its own copy of the list to fall behind.
func TypesHint() string {
	names := make([]string, len(Types))
	for i, t := range Types {
		names[i] = string(t)
	}
	return strings.Join(names, ", ")
}

// TypesQuoted renders the recommended vocabulary as shell words, wrapping
// the types that contain a space in q so the shell sees one word per type.
// Completion scripts interpolate this rather than spelling the list out,
// so the vocabulary has one home (design doc 0038 §4.4); q differs per
// shell because each already uses the other quote around the whole list.
func TypesQuoted(q string) string {
	names := make([]string, len(Types))
	for i, t := range Types {
		names[i] = string(t)
		if strings.Contains(names[i], " ") {
			names[i] = q + names[i] + q
		}
	}
	return strings.Join(names, " ")
}

// BuiltinType reports whether t is one of the recommended types, matched
// case-insensitively (EqualType), the same way search filters match
// (FoldType).
func BuiltinType(t Type) bool {
	for _, v := range Types {
		if EqualType(t, v) {
			return true
		}
	}
	return false
}

// CanonicalType returns the built-in spelling of t when t names one
// (however it was cased), else t unchanged. Write paths use it so the
// recommended types have one spelling in storage while callers may write
// them in any case.
func CanonicalType(t Type) Type {
	for _, v := range Types {
		if EqualType(t, v) {
			return v
		}
	}
	return t
}

// ValidType reports whether t can be a knowledge type: one non-empty line,
// no "/" so a type never reads as an address, within 128 bytes. Types are
// the OKF vocabulary verbatim (design doc 0023) — "BigQuery Table", not a
// slug — and OKF registers no taxonomy, so the spelling is the meaning.
// They are ochakai vocabulary spoken in filters, frontmatter, and tool
// arguments, never file paths, so a space is fine but a separator is not.
func ValidType(t Type) bool {
	s := strings.TrimSpace(string(t))
	if s == "" || len(s) > 128 || strings.Contains(s, "/") {
		return false
	}
	return strings.IndexFunc(s, func(r rune) bool {
		return r == '\n' || r == '\r' || (r < 0x20) || r == 0x7f
	}) < 0
}

// EqualType reports whether two types name the same kind. Types are
// matched case-insensitively (design doc 0023 §3.3) so a filter written
// "bigquery table" finds entries stored as "BigQuery Table"; the stored
// spelling is always the one the writer used.
func EqualType(a, b Type) bool {
	return FoldType(a) == FoldType(b)
}

// FoldType returns the comparison key for t: NFC-normalized, trimmed, and
// lowercased. It is the matching half of design doc 0023 §3.3 ("照合は
// 大小文字非依存、保存は原表記") in the form storage needs — a value to
// compare against a folded column, rather than the pairwise EqualType.
// Free types fold too: §3.3 widens the tolerance to every entry point and
// every comparison, not just the recommended eight.
//
// The store folds the column with SQL lower(), which agrees with this for
// ASCII and for the ordinary cased letters types are written in. The two
// can disagree on exotic Unicode case pairs (final sigma, dotted I under a
// Turkish collation); a type that hinges on those is beyond what either
// side promises.
func FoldType(t Type) string {
	return strings.ToLower(strings.TrimSpace(Normalize(string(t))))
}

// Status is an entry's lifecycle value, spelled as OKF SPEC §5.4 spells
// it: draft is "not yet reviewed, possibly incomplete", stable is "ready
// for consumption", deprecated is "kept for links and history, no longer
// current".
//
// It says nothing about whether anyone confirmed the entry. Under SPEC
// §5.3 that is what the verified key says, and ochakai keeps it in the
// verification ledger — the lifecycle value and the trust signal are
// independent, and a stable entry may be unverified just as a draft may
// be verified (design doc 0043 §3.2). "Was never accepted" is likewise
// not a lifecycle value but this instance's ruling, in the rejection
// ledger (§3.3).
type Status string

const (
	StatusDraft      Status = "draft"
	StatusStable     Status = "stable"
	StatusDeprecated Status = "deprecated"
)

// Statuses lists all statuses in lifecycle order — the single source for
// every user-facing enumeration (CLI help, completions, docs guards).
var Statuses = []Status{StatusDraft, StatusStable, StatusDeprecated}

func ValidStatus(s Status) bool {
	return slices.Contains(Statuses, s)
}

// StatusFromOKF maps a frontmatter status onto ochakai's vocabulary,
// which since design doc 0043 §3.2 is OKF's vocabulary — so the mapping
// is the identity on every value the spec defines, and the whole of what
// remains here is what to do with the two cases the spec leaves open.
//
// An unrecognized value is a draft, never a rejection of the document
// (SPEC §11 forbids rejecting a concept over an unknown field value). ok
// reports whether the value was recognized, so an importer can mention
// what it did rather than silently reinterpreting.
//
// An absent status stays unset ("") rather than becoming a value. OKF
// reads absence as stable (§5.4), but ochakai already has a rule for a
// write that names no status — draft on create, unchanged on update — and
// that rule is what keeps a re-imported document from silently demoting
// the entry it lands on.
//
// Whether the document carried a verified key is deliberately not an
// input. Until 0043 it was: with no way to say "stable but unconfirmed",
// a foreign bundle's stable entry had to be demoted to draft or it would
// have claimed a confirmation nobody made. The verification ledger can
// hold that state, so the lifecycle value now arrives unaltered and the
// trust signal is recorded beside it (§3.2).
func StatusFromOKF(raw string) (s Status, ok bool) {
	if raw == "" {
		return "", true
	}
	if st := Status(raw); ValidStatus(st) {
		return st, true
	}
	return StatusDraft, false
}

// DateLayout is the date form OKF fixes for its absolute dates — stale_after
// (SPEC §5.5), sources[].last_modified and usage_window (SPEC §5.1). An
// absolute day, not a relative TTL, so staleness stays a plain date
// comparison with no reference to when the entry was read — and with no
// timezone to argue about.
const DateLayout = "2006-01-02"

// StaleAfterLayout is the older name for DateLayout, kept because the date
// rule was written for stale_after first.
const StaleAfterLayout = DateLayout

// ValidDate reports whether s can be one of OKF's absolute dates: unset,
// or a YYYY-MM-DD date that exists. Every date field in the envelope is
// held to this one rule.
func ValidDate(s string) bool {
	if s == "" {
		return true
	}
	_, err := time.Parse(DateLayout, s)
	return err == nil
}

// ValidStaleAfter reports whether s can be a stale_after.
func ValidStaleAfter(s string) bool { return ValidDate(s) }

// Usage event kinds recorded per knowledge entry (design doc 0001 §9).
// The first two are recorded passively by reads; worked/failed are
// deliberate outcome reports (report_outcome) closing the write-back loop.
//
// Retired: "compiled", written by compile_sql until 0.13.0 (design doc
// 0028). Existing rows are kept — history stays history — but nothing
// writes or aggregates them any more.
const (
	EventSearchHit = "search_hit" // appeared in search results
	EventFetched   = "fetched"    // fetched individually via get
	EventWorked    = "worked"     // caller reports the entry led to a correct result
	EventFailed    = "failed"     // caller reports the entry led to a wrong or unusable result
)

// ListSorts are the listing modes: the values of ?sort that replace
// searching with a feed. verified_at and failed rank by what the server
// observed and are emptied by verifying (design doc 0025); usage ranks by
// demand; stale_after ranks by the expiry a writer declared and is
// emptied by editing, not verifying (design doc 0037 §2.2). The single
// source for every surface's validation and help text.
var ListSorts = []string{"verified_at", "usage", "failed", "stale_after"}

func ValidListSort(s string) bool { return slices.Contains(ListSorts, s) }

// Outcomes lists the reportable outcome kinds — the single source for
// every user-facing enumeration (tool schema, CLI help, completions).
var Outcomes = []string{EventWorked, EventFailed}

func ValidOutcome(o string) bool {
	for _, v := range Outcomes {
		if o == v {
			return true
		}
	}
	return false
}

// Usage aggregates how often a knowledge entry was actually used, and
// how often users reported it worked or failed.
type Usage struct {
	SearchHits int64      `json:"search_hits"`
	Fetches    int64      `json:"fetches"`
	Worked     int64      `json:"worked"`
	Failed     int64      `json:"failed"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

// Actor identifies who created or verified a knowledge entry.
//
// Via names the caller that acted on this actor's behalf: an application
// with its own service-account identity, forwarding the identity of the
// person using it (design doc 0027). It is never a replacement for the
// actor — an entry written by tanaka through InsightFlow and one tanaka
// wrote with their own credentials must stay distinguishable, or the
// delegation becomes indistinguishable from a forgery. Empty for the
// ordinary case, where the caller acted as itself.
type Actor struct {
	Kind string `json:"kind"` // "human" | "process"
	Name string `json:"name"`
	Via  string `json:"via,omitempty"` // the delegating caller's identity, "" when there is none
}

// String renders an actor the way every surface shows provenance:
// "human:tanaka@example.co.jp", or "human:tanaka@example.co.jp via
// process:insightflow@example.iam.gserviceaccount.com" when the write came
// through a delegating application.
func (a Actor) String() string {
	s := a.Kind + ":" + a.Name
	if a.Via != "" {
		s += " via " + a.Via
	}
	return s
}

// The actor kinds, spelled as OKF SPEC §7 spells them. ochakai wrote
// "agent:" until 0.15 (design doc 0043 §3.8): the spelling was ochakai's
// own, and the distinction it drew — an LLM agent rather than any other
// automation — is one nothing in the system ever branched on, while SPEC
// §5.3 lumps everything that is not human: into one machine-confirmed
// tier. A distinction that costs conformance and buys nothing is not one
// worth keeping.
//
// SPEC §7's third form, "<producer>/<version>", stays unused: ochakai's
// non-human writers are IAM service accounts, which have no version, and
// inventing one would make the provenance say something the server never
// observed.
const (
	ActorHuman   = "human"
	ActorProcess = "process"
)

// Verification is one recorded confirmation that an entry was checked —
// one element of OKF's verified list (SPEC §5.2), and under §5.3 the only
// thing that makes an entry anything but unverified.
//
// The ledger is append-only and plural. ochakai held a single verifier
// until 0.15, which made re-verification an overwrite: checking an entry
// again replaced the record of the last check, so "when was this last
// looked at" was answerable and "how often has this been confirmed, and
// by whom" was not (design doc 0043 §3.2). Verifying is now an append,
// and the feed that ranks by verification age reads the newest row.
//
// A verification is this instance's observation, never something a
// document asserts: import reads only whether a verified key was present,
// not who or when (design docs 0009, 0036 §2.2).
type Verification struct {
	By Actor     `json:"by"`
	At time.Time `json:"at"`
}

// Rejection is this instance's ruling that an entry was not accepted —
// the memory that stops an agent re-proposing knowledge a human already
// turned down (design doc 0001 §9.1).
//
// It is a ledger rather than a status because it is not a lifecycle
// value: OKF's three statuses say how ready a concept is, and "we said
// no" is a judgment about the concept, not a stage of it. As a status it
// also could not round-trip — export had to fold it onto deprecated,
// which claims the opposite thing ("was correct, no longer current")
// about the entry (design doc 0043 §3.3).
//
// Note is the reason, and belongs to the rejecter. It is distinct from
// the entry's own status_note, which belongs to whoever wrote the entry.
type Rejection struct {
	By   Actor     `json:"by"`
	At   time.Time `json:"at"`
	Note string    `json:"note,omitempty"`
}

// ValidActorKind reports whether kind is one ochakai records.
func ValidActorKind(kind string) bool {
	return kind == ActorHuman || kind == ActorProcess
}

// Link is an edge to another knowledge entry, derived from a markdown
// link in the body — never authored as a field (design doc 0024). Links
// are untyped: OKF SPEC §5 leaves the kind of relationship to the
// surrounding prose, so all ochakai keeps is where the link points and
// what it was called.
type Link struct {
	Target string `json:"target"`         // the target entry's id (its bundle path)
	Text   string `json:"text,omitempty"` // the anchor text; empty for a bare ochakai:// reference
}

// DisplayText returns how the link should read: its anchor text when the
// body gave one, else the target's last segment — the same "filename is
// the name" fallback titles use (design doc 0022).
func (l Link) DisplayText() string {
	if l.Text != "" {
		return l.Text
	}
	return DisplayTitle("", l.Target)
}

// UsageWindow is the date range that frames usage_count values (OKF SPEC
// §5.1): "5000 uses" means nothing without the window it was counted over.
// ochakai records the window and never computes one.
type UsageWindow struct {
	From string `json:"from,omitempty"` // YYYY-MM-DD
	To   string `json:"to,omitempty"`
}

func (w *UsageWindow) Valid() bool {
	return w == nil || (ValidDate(w.From) && ValidDate(w.To))
}

func (w *UsageWindow) sameAs(o *UsageWindow) bool {
	if w == nil || o == nil {
		return w == o
	}
	return *w == *o
}

// Source is one piece of external material a concept derives from (OKF
// SPEC §5.1). ochakai records sources; it never fetches Resource, checks
// that it resolves, or scores the credibility signals — SPEC §5.1 is
// explicit that raw signals are for the consumer to weigh.
//
// The spec's six keys are modeled exhaustively, so a producer key inside a
// source mapping is dropped on import with a note rather than kept
// (design doc 0036 §3.5): the whole point of the promotion is that four
// surfaces can describe one shape.
type Source struct {
	Resource string `json:"resource"` // REQUIRED within an entry: the artifact this came from
	ID       string `json:"id,omitempty"`
	Title    string `json:"title,omitempty"`
	Author   string `json:"author,omitempty"` // an actor (SPEC §7), the source's, not ochakai's
	// UsageCount is a pointer because absent and zero say different things:
	// no count at all versus a source exercised zero times over the window.
	UsageCount   *int         `json:"usage_count,omitempty"`
	LastModified string       `json:"last_modified,omitempty"` // YYYY-MM-DD
	UsageWindow  *UsageWindow `json:"usage_window,omitempty"`  // overrides the entry's window for this source
}

// Valid reports whether s is a usable source. A source that names no
// artifact is not a citation, which is why SPEC §5.1 makes resource
// required inside an entry.
func (s Source) Valid() bool {
	return s.Resource != "" && ValidDate(s.LastModified) && s.UsageWindow.Valid() &&
		(s.UsageCount == nil || *s.UsageCount >= 0)
}

func (s Source) sameAs(o Source) bool {
	if s.Resource != o.Resource || s.ID != o.ID || s.Title != o.Title ||
		s.Author != o.Author || s.LastModified != o.LastModified {
		return false
	}
	if !s.UsageWindow.sameAs(o.UsageWindow) {
		return false
	}
	if s.UsageCount == nil || o.UsageCount == nil {
		return s.UsageCount == o.UsageCount
	}
	return *s.UsageCount == *o.UsageCount
}

// Parameter is one declared input of an Attested Computation (OKF SPEC
// §10.2). An agent supplies parameter values; it must not author or edit
// the computation itself — which is a rule for the consumer running it,
// not something ochakai enforces.
//
// Required is a plain bool because the spec gives absent and false the
// same meaning. UsageCount is a pointer for the opposite reason.
type Parameter struct {
	Name     string `json:"name"` // REQUIRED within an entry
	Type     string `json:"type"` // REQUIRED within an entry
	Required bool   `json:"required,omitempty"`
}

func (p Parameter) Valid() bool { return p.Name != "" && p.Type != "" }

// Executor says how an Attested Computation is run and what a run must
// hand back as evidence (OKF SPEC §10.2). ochakai stores it and never
// runs it: executing the computation and checking the receipt belong to
// the consumer (SPEC §10.5, design docs 0001, 0036 §5).
type Executor struct {
	Resource string   `json:"resource"` // REQUIRED within executor: run instructions or code
	Receipt  []string `json:"receipt"`  // REQUIRED within executor: the fields a run must return
}

func (e *Executor) Valid() bool {
	return e == nil || (e.Resource != "" && len(e.Receipt) > 0)
}

// Attester is the deterministic, LLM-free checker that a run of an
// Attested Computation was performed correctly (OKF SPEC §10.2). As with
// Executor, ochakai records the path and never executes it.
type Attester struct {
	Resource string `json:"resource"` // REQUIRED within attester
}

func (a *Attester) Valid() bool { return a == nil || a.Resource != "" }

// Knowledge is the common envelope for all knowledge types. Every key OKF
// v0.2 defines is a field here; Attrs carries only producer-defined
// extension keys (SPEC §4.1, design doc 0036 §2). Prose lives in Body
// (markdown). The envelope maps 1:1 to an OKF document (YAML frontmatter
// + markdown).
type Knowledge struct {
	Type        Type         `json:"type"`
	ID          string       `json:"id"`              // full bundle path, the sole key (design doc 0017)
	Title       string       `json:"title,omitempty"` // display-name override; empty means the id's last segment is the name (design doc 0022)
	Description string       `json:"description,omitempty"`
	Resource    string       `json:"resource,omitempty"` // canonical URI of the underlying asset (OKF recommended key)
	Tags        []string     `json:"tags,omitempty"`
	Sources     []Source     `json:"sources,omitempty"`      // the material this derives from (OKF SPEC §5.1)
	UsageWindow *UsageWindow `json:"usage_window,omitempty"` // the range the sources' usage_counts were counted over
	Status      Status       `json:"status"`
	StatusNote  string       `json:"status_note,omitempty"` // free-form reason for the current status (why rejected/deprecated)
	StaleAfter  string       `json:"stale_after,omitempty"` // YYYY-MM-DD; the entry is stale on and after this day (OKF SPEC §5.5, design doc 0036 §3.5)
	// The Attested Computation contract (OKF SPEC §10.2). These live in the
	// envelope for every type — Go structs do not change shape per type, and
	// ochakai enforces no per-type schema (design doc 0036 §5). What varies
	// by type is validation of Runtime alone (design doc 0036 §3.4) and
	// which sections the Web UI shows.
	//
	// ochakai records this contract and never acts on it: it does not run
	// Executor, check a receipt, run Attester, or fetch Computation. That
	// is the consumer's procedure (SPEC §10.5, design docs 0001, 0036 §5).
	Runtime     string      `json:"runtime,omitempty"`     // required by SPEC when the type is Attested Computation
	Parameters  []Parameter `json:"parameters,omitempty"`  // inputs an agent may fill in
	Computation string      `json:"computation,omitempty"` // path to the computation; empty means the body's "# Computation" fence
	Executor    *Executor   `json:"executor,omitempty"`
	Attester    *Attester   `json:"attester,omitempty"`
	CreatedBy   Actor       `json:"created_by"`
	UpdatedBy   Actor       `json:"updated_by"` // who last changed the content — OKF's generated.by (design doc 0036 §3.3)
	// Verifications and Rejection are this instance's ledgers, not fields
	// of the document: read-only on every surface, never accepted from a
	// payload, and written only by verify and reject (design doc 0043
	// §§3.2-3.3). Oldest verification first.
	Verifications []Verification `json:"verifications,omitempty"`
	Rejection     *Rejection     `json:"rejection,omitempty"`
	Links         []Link         `json:"links,omitempty"`
	Attrs         map[string]any `json:"attrs,omitempty"`
	Body          string         `json:"body,omitempty"`
	// Attachments is read-only metadata (no bytes), populated on single-
	// entry reads. Attachments are managed through their own endpoints,
	// never through create/update payloads.
	Attachments []Attachment `json:"attachments,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
	// ContentHash is the entry's version: the SHA-256 of its canonical
	// document (design doc 0043 §3.4), which is what the ETag carries and
	// what If-Match is checked against. It is a hash of the content
	// alone, so a verification, a rejection or an attachment leaves it
	// where it was — only an edit moves it.
	ContentHash string `json:"content_hash,omitempty"`
}

// EnvelopeKeys names every OKF frontmatter key the envelope owns, in the
// spelling a bundle uses. Attrs carries producer extension keys (SPEC
// §4.1) and nothing else, so a payload naming one of these inside attrs is
// rejected rather than stored where nothing will read it (design doc 0036
// §3.5). Server-owned provenance (generated, verified, created_by,
// rejected_*) is not here: those keys are ignored on the way in, and the
// bundle writer keeps its own longer list.
var EnvelopeKeys = []string{
	"type", "id", "title", "description", "resource", "tags",
	"sources", "usage_window",
	"status", "status_note", "stale_after",
	"runtime", "parameters", "computation", "executor", "attester",
}

// Verified reports whether anyone has confirmed this entry — SPEC §5.3's
// signal, which is the presence of a verification and not a status.
func (k *Knowledge) Verified() bool { return len(k.Verifications) > 0 }

// LastVerified returns the newest verification, or nil when there is
// none. The ledger is stored oldest-first.
func (k *Knowledge) LastVerified() *Verification {
	if len(k.Verifications) == 0 {
		return nil
	}
	return &k.Verifications[len(k.Verifications)-1]
}

// Ruling names the decision a human recorded about an entry, or "" when
// nobody has ruled on it. Two of the three now live in ledgers and one is
// still a lifecycle value (design doc 0043 §§3.2-3.3), so the guards ask
// the entry rather than its status — which is where the question always
// belonged, since what it means is "did somebody decide something about
// this", not "what stage is it at".
//
// A rejection outranks a verification when an entry carries both: Verify
// clears a rejection but Reject keeps the verifications, so the pair
// means somebody vouched for this and somebody later ruled against it,
// and the later ruling is the one in force.
type Ruling string

const (
	RulingRejected   Ruling = "rejected"
	RulingVerified   Ruling = "verified"
	RulingDeprecated Ruling = "deprecated"
)

func (k *Knowledge) Ruling() Ruling {
	switch {
	case k.Rejection != nil:
		return RulingRejected
	case k.Verified():
		return RulingVerified
	case k.Status == StatusDeprecated:
		return RulingDeprecated
	}
	return ""
}

// Curated reports whether a human has ruled on this entry. Surfaces with
// no If-Match channel must not replace one in place (design doc 0015
// §3.1).
func (k *Knowledge) Curated() bool { return k.Ruling() != "" }

// URI returns the canonical reference, e.g. "ochakai://metrics/revenue" —
// the scheme plus the entry's id (its bundle path, design doc 0017).
func (k *Knowledge) URI() string { return fmt.Sprintf("ochakai://%s", k.ID) }

// DisplayTitle returns the entry's display name: the title when one is
// set, else the id's final segment — with title optional (design doc
// 0022), the filename usually is the name.
func (k *Knowledge) DisplayTitle() string { return DisplayTitle(k.Title, k.ID) }

// DisplayTitle is the package-level form for projections that carry
// title and id without a full Knowledge (browse entries).
func DisplayTitle(title, id string) string {
	if title != "" {
		return title
	}
	if i := strings.LastIndexByte(id, '/'); i >= 0 {
		return id[i+1:]
	}
	return id
}

// Normalize returns s in Unicode NFC. IDs, link targets, attachment
// names, and search queries are compared byte-wise against stored text,
// and macOS filesystems hand paths back NFD-decomposed — the same
// visible path must land on the same entry, so every boundary that
// accepts one normalizes first (design doc 0022). Bodies and titles are
// content, not keys, and are kept as written.
func Normalize(s string) string { return norm.NFC.String(s) }

// SameContent reports whether o carries the same authored content as k:
// the fields a writer controls (title, description, resource, tags,
// sources, usage_window, status, status_note, stale_after, the Attested
// Computation contract, links, attrs, body). Server-managed provenance and
// timestamps (created_*, updated_by, verified_*, rejected_*, updated_at)
// and the attachment list are not content. Attrs are compared as canonical
// JSON, so the same value decoded from YAML (int) and from JSONB (float64)
// compares equal; values JSON cannot encode compare as different.
//
// The list comparisons are order-sensitive on purpose: reordering an
// entry's citations or an Attested Computation's parameters is a change to
// what it says, and a write that reorders them must not be swallowed as a
// no-op.
func (k *Knowledge) SameContent(o *Knowledge) bool {
	return k.Type == o.Type && k.ID == o.ID &&
		k.Title == o.Title && k.Description == o.Description &&
		k.Resource == o.Resource &&
		k.Status == o.Status && k.StatusNote == o.StatusNote &&
		k.StaleAfter == o.StaleAfter &&
		k.Runtime == o.Runtime && k.Computation == o.Computation &&
		k.Body == o.Body &&
		slices.Equal(k.Tags, o.Tags) &&
		slices.EqualFunc(k.Sources, o.Sources, Source.sameAs) &&
		k.UsageWindow.sameAs(o.UsageWindow) &&
		slices.Equal(k.Parameters, o.Parameters) &&
		executorsEqual(k.Executor, o.Executor) &&
		attestersEqual(k.Attester, o.Attester) &&
		slices.Equal(k.Links, o.Links) &&
		attrsEqual(k.Attrs, o.Attrs)
}

func executorsEqual(a, b *Executor) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Resource == b.Resource && slices.Equal(a.Receipt, b.Receipt)
}

func attestersEqual(a, b *Attester) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func attrsEqual(a, b map[string]any) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(ja, jb)
}

// Revision is one entry in an entry's change history: who changed it,
// how, when, and the full snapshot as of that change. The audit trail
// behind "every change kept as a revision".
type Revision struct {
	Rev       int       `json:"rev"`
	Change    string    `json:"change"` // create | update | move | delete | verify | reject | unreject | attach | detach
	ChangedBy Actor     `json:"changed_by"`
	ChangedAt time.Time `json:"changed_at"`
	// Document is the entry as it stood, as an OKF document (design doc
	// 0043 §3.9). It replaced a JSON snapshot of the Go struct: a record
	// whose shape depends on the reader knowing every past release is the
	// distortion, not the fidelity, and OKF is an anchor that outlasts
	// them. It also means a diff between two revisions reads.
	Document string `json:"document"`
}

// validSegment reports whether s can be one ID path segment. OKF
// prescribes no character set — a concept ID is a file path, nothing
// more — so only path safety constrains it (design doc 0019): non-empty
// valid UTF-8, at most 128 bytes, no control characters, and not
// starting with "." (which rules out ".", "..", and hidden files, the
// paths bundle import skips as never-knowledge).
func validSegment(s string) bool {
	if s == "" || len(s) > 128 || strings.HasPrefix(s, ".") || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f || r == '/' {
			return false
		}
	}
	return true
}

// ValidID reports whether id is a valid knowledge ID: path segments
// separated by "/", following OKF's "concept ID = file path" rule (the
// bundle path is "<id>.md", design doc 0017). The final segment must not
// be "index" or "log" — those filenames belong to OKF's reserved
// per-directory index.md (generated navigation) and log.md (history).
func ValidID(id string) bool {
	if id == "" || len(id) > 512 {
		return false
	}
	segs := strings.Split(id, "/")
	for _, s := range segs {
		if !validSegment(s) {
			return false
		}
	}
	last := segs[len(segs)-1]
	return last != "index" && last != "log"
}

// ValidIDPrefix reports whether prefix can lead a knowledge ID: empty
// (the root) or path segments separated by "/". Unlike ValidID, a final
// "index" segment is fine — it only names a directory here, and the
// index.md reservation is about a document's own filename.
func ValidIDPrefix(prefix string) bool {
	if prefix == "" {
		return true
	}
	if len(prefix) > 512 {
		return false
	}
	for _, s := range strings.Split(prefix, "/") {
		if !validSegment(s) {
			return false
		}
	}
	return true
}

// ToTypes converts transport-layer type filters (query parameters, tool
// inputs) into domain types — shared by the REST and MCP surfaces so the
// conversion is written once.
func ToTypes(ss []string) []Type {
	out := make([]Type, 0, len(ss))
	for _, s := range ss {
		out = append(out, Type(s))
	}
	return out
}

// ToStatuses is ToTypes for status filters.
func ToStatuses(ss []string) []Status {
	out := make([]Status, 0, len(ss))
	for _, s := range ss {
		out = append(out, Status(s))
	}
	return out
}

// SearchHit is one search result with its ranking score. Usage is
// populated only by the sort=usage listing (the draft review feed), where
// the promotion signal is the point; it stays nil for search results and
// the verified_at feed so their wire shape is unchanged.
type SearchHit struct {
	Knowledge
	Score float64 `json:"score"`
	Usage *Usage  `json:"usage,omitempty"`
}

// ContextRank is what a hit is worth once the entries travel in the same
// response: an ordering, not a second copy of the knowledge (design doc
// 0033). SearchHit embeds the whole Knowledge — body, attrs and all — so
// a context pack that returned hits verbatim sent every top entry twice
// and left the byte budget governing one of the copies.
//
// The fields are the ones a caller needs to decide whether to spend a
// round trip on an id it was not handed: search results below the pack's
// own cut-off arrive only this way. Search itself keeps returning full
// hits — there the entries are the answer, not a duplicate of one.
type ContextRank struct {
	ID     string  `json:"id"`
	Type   Type    `json:"type"`
	Title  string  `json:"title"`
	Status Status  `json:"status"`
	Score  float64 `json:"score"`
}

// URI is the entry's canonical address, as on Knowledge: a rank is a
// pointer, and the pointer should read the same everywhere.
func (c *ContextRank) URI() string { return fmt.Sprintf("ochakai://%s", c.ID) }

// ContextRanks projects search hits down to the ranking behind a pack.
func ContextRanks(hits []SearchHit) []ContextRank {
	out := make([]ContextRank, len(hits))
	for i := range hits {
		out[i] = ContextRank{
			ID: hits[i].ID, Type: hits[i].Type, Title: hits[i].DisplayTitle(),
			Status: hits[i].Status, Score: hits[i].Score,
		}
	}
	return out
}

// ContextOutline names an entry a context pack could not afford to deliver
// in full: enough for the caller to decide whether to spend a round trip
// fetching it by id, and nothing more. The description carries the weight
// here — an entry with an empty description is nearly invisible in an
// outline, which is one more reason curation pays. It is also the only
// unbounded field, so the packer caps it: these rows are counted against
// the same budget as the entries.
type ContextOutline struct {
	ID          string `json:"id"`
	Type        Type   `json:"type"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Status      Status `json:"status"`
	Bytes       int    `json:"bytes"`
}
