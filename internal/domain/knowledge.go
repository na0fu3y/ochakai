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
	TypeModels       Type = "Semantic Model"       // Apache Ossie semantic model, spec verbatim in attrs.spec (design doc 0018; unvalidated since 0028)
	TypeMetrics      Type = "Metric"               // semantic metric definition (Apache Ossie)
	TypeQueries      Type = "Golden Query"         // golden query: question + verified SQL
	TypeComputations Type = "Attested Computation" // sanctioned computation and the means to check a run of it (OKF SPEC §10, design doc 0034 §3.6)
	TypeInsights     Type = "Insight"              // how to read a metric: baselines, caveats
	TypeTerms        Type = "Glossary Term"        // glossary term
	TypeDatasets     Type = "BigQuery Dataset"     // dataset: a container grouping tables
	TypeTables       Type = "BigQuery Table"       // table catalog entry
	TypeReferences   Type = "Reference"            // mirror of external material (enums, licenses, schema docs)
)

// Types lists the recommended (built-in) knowledge types, in display order
// (containers before their contents: models define metrics, datasets
// group tables).
//
// No server behavior hangs off any of them — the list is a vocabulary, not
// a registry (design doc 0028 §3). Attested Computation is the clearest
// case: ochakai records the contract (runtime, parameters, executor,
// attester) as ordinary attrs and never runs it. Executing the computation
// and checking the receipt belong to the consumer (OKF SPEC §10.5), which
// is the same line design doc 0028 drew when compile_sql went away.
var Types = []Type{TypeModels, TypeMetrics, TypeQueries, TypeComputations, TypeInsights, TypeTerms, TypeDatasets, TypeTables, TypeReferences}

// TypesHint renders the recommended vocabulary for help and error text,
// so no surface keeps its own copy of the list to fall behind.
func TypesHint() string {
	names := make([]string, len(Types))
	for i, t := range Types {
		names[i] = string(t)
	}
	return strings.Join(names, ", ")
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

// Status is the verification status of a knowledge entry. deprecated means
// "was correct, no longer recommended"; rejected means "was never accepted"
// — the record keeps agents from re-proposing the same knowledge.
type Status string

const (
	StatusDraft      Status = "draft"
	StatusVerified   Status = "verified"
	StatusDeprecated Status = "deprecated"
	StatusRejected   Status = "rejected"
)

// Statuses lists all statuses in lifecycle order — the single source for
// every user-facing enumeration (CLI help, completions, docs guards).
var Statuses = []Status{StatusDraft, StatusVerified, StatusDeprecated, StatusRejected}

// CuratedStatuses are the states a human has ruled on. Surfaces with no
// If-Match channel must not replace one in place (design doc 0015 §3.1),
// and the rule is enforced both in the service guards and in the store's
// create transaction — so this list is the one place it is spelled.
var CuratedStatuses = []Status{StatusVerified, StatusDeprecated, StatusRejected}

// Curated reports whether a human has ruled on this status.
func (s Status) Curated() bool {
	for _, v := range CuratedStatuses {
		if s == v {
			return true
		}
	}
	return false
}

func ValidStatus(s Status) bool {
	for _, v := range Statuses {
		if s == v {
			return true
		}
	}
	return false
}

// OKF lifecycle values (SPEC §5.4). Three where ochakai has four: OKF
// spells "a human confirmed this" with the verified key, not with a
// status, so verified maps onto stable plus a verified entry.
const (
	OKFStatusDraft      = "draft"
	OKFStatusStable     = "stable"
	OKFStatusDeprecated = "deprecated"
)

// OKFStatus returns the OKF v0.2 lifecycle value for s (design doc 0034
// §3.4). Rejected exports as deprecated: OKF has no state for "was never
// accepted", and the reason survives in status_note while rejected_by /
// rejected_at stay as ochakai extension keys. The mapping is deliberately
// lossy in that one direction — a rejection is this instance's curation
// record, and a bundle carries knowledge, not instance-local rulings
// (design doc 0009).
func OKFStatus(s Status) string {
	switch s {
	case StatusDraft:
		return OKFStatusDraft
	case StatusVerified:
		return OKFStatusStable
	case StatusDeprecated, StatusRejected:
		return OKFStatusDeprecated
	}
	// A status ochakai does not know cannot have landed in the store, but
	// export must still write something OKF-shaped.
	return OKFStatusDraft
}

// StatusFromOKF maps a frontmatter status onto ochakai's vocabulary.
// hasVerified reports whether the document carried a verified key at all
// — under SPEC §5.3 that key, not the status, is what says a concept was
// confirmed, so a foreign bundle's stable entries land as drafts unless
// they carry one. ochakai's own exports always write verified alongside
// stable, so an ochakai → ochakai round-trip is exact.
//
// The values ochakai wrote before v0.2 (verified, rejected) are still
// accepted verbatim: bundles exported by 0.12 and earlier must keep
// importing as what they say.
//
// An unrecognized value is a draft, never a rejection of the document
// (SPEC §11 forbids rejecting a concept over an unknown field value). ok
// reports whether the value was recognized, so an importer can mention
// what it did rather than silently reinterpreting.
//
// An absent status stays unset ("") rather than becoming a value: OKF
// reads absence as stable (§5.4), but ochakai already has a rule for a
// write that names no status — draft on create, unchanged on update — and
// that rule is what keeps a re-imported document from silently demoting
// the entry it lands on. The one exception is a document that carries a
// verified key with no status: that is a confirmation, and saying so is
// the whole point of §5.3.
func StatusFromOKF(raw string, hasVerified bool) (s Status, ok bool) {
	switch raw {
	case "":
		if hasVerified {
			return StatusVerified, true
		}
		return "", true
	case OKFStatusDraft:
		return StatusDraft, true
	case OKFStatusStable:
		if hasVerified {
			return StatusVerified, true
		}
		return StatusDraft, true
	case OKFStatusDeprecated:
		return StatusDeprecated, true
	case string(StatusVerified), string(StatusRejected):
		return Status(raw), true // written by ochakai 0.12 and earlier
	}
	return StatusDraft, false
}

// StaleAfterLayout is the date form OKF fixes for stale_after (SPEC §5.5):
// an absolute day, not a relative TTL, so staleness stays a plain date
// comparison with no reference to when the entry was read — and with no
// timezone to argue about.
const StaleAfterLayout = "2006-01-02"

// ValidStaleAfter reports whether s can be a stale_after: unset, or a
// YYYY-MM-DD date that exists.
func ValidStaleAfter(s string) bool {
	if s == "" {
		return true
	}
	_, err := time.Parse(StaleAfterLayout, s)
	return err == nil
}

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
	Kind string `json:"kind"` // "human" | "agent"
	Name string `json:"name"`
	Via  string `json:"via,omitempty"` // the delegating caller's identity, "" when there is none
}

// String renders an actor the way every surface shows provenance:
// "human:tanaka@example.co.jp", or "human:tanaka@example.co.jp via
// agent:insightflow@example.iam.gserviceaccount.com" when the write came
// through a delegating application.
func (a Actor) String() string {
	s := a.Kind + ":" + a.Name
	if a.Via != "" {
		s += " via " + a.Via
	}
	return s
}

const (
	ActorHuman = "human"
	ActorAgent = "agent"
)

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

// Knowledge is the common envelope for all knowledge types. Type-specific
// structured attributes live in Attrs; prose lives in Body (markdown).
// The envelope maps 1:1 to an OKF document (YAML frontmatter + markdown).
type Knowledge struct {
	Type        Type           `json:"type"`
	ID          string         `json:"id"`              // full bundle path, the sole key (design doc 0017)
	Title       string         `json:"title,omitempty"` // display-name override; empty means the id's last segment is the name (design doc 0022)
	Description string         `json:"description,omitempty"`
	Resource    string         `json:"resource,omitempty"` // canonical URI of the underlying asset (OKF recommended key)
	Tags        []string       `json:"tags,omitempty"`
	Status      Status         `json:"status"`
	StatusNote  string         `json:"status_note,omitempty"` // free-form reason for the current status (why rejected/deprecated)
	StaleAfter  string         `json:"stale_after,omitempty"` // YYYY-MM-DD; the entry is stale on and after this day (OKF SPEC §5.5, design doc 0034 §3.5)
	CreatedBy   Actor          `json:"created_by"`
	UpdatedBy   Actor          `json:"updated_by"` // who last changed the content — OKF's generated.by (design doc 0034 §3.3)
	VerifiedBy  *Actor         `json:"verified_by,omitempty"`
	VerifiedAt  *time.Time     `json:"verified_at,omitempty"`
	RejectedBy  *Actor         `json:"rejected_by,omitempty"`
	RejectedAt  *time.Time     `json:"rejected_at,omitempty"`
	Links       []Link         `json:"links,omitempty"`
	Attrs       map[string]any `json:"attrs,omitempty"`
	Body        string         `json:"body,omitempty"`
	// Attachments is read-only metadata (no bytes), populated on single-
	// entry reads. Attachments are managed through their own endpoints,
	// never through create/update payloads.
	Attachments []Attachment `json:"attachments,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

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
// the fields a writer controls (title, description, resource, tags, status,
// status_note, stale_after, links, attrs, body). Server-managed provenance
// and timestamps (created_*, updated_by, verified_*, rejected_*,
// updated_at) and the attachment list are not content. Attrs are compared
// as canonical JSON, so the same value decoded from YAML (int) and from
// JSONB (float64) compares equal; values JSON cannot encode compare as
// different.
func (k *Knowledge) SameContent(o *Knowledge) bool {
	return k.Type == o.Type && k.ID == o.ID &&
		k.Title == o.Title && k.Description == o.Description &&
		k.Resource == o.Resource &&
		k.Status == o.Status && k.StatusNote == o.StatusNote &&
		k.StaleAfter == o.StaleAfter &&
		k.Body == o.Body &&
		slices.Equal(k.Tags, o.Tags) &&
		slices.Equal(k.Links, o.Links) &&
		attrsEqual(k.Attrs, o.Attrs)
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
	Change    string    `json:"change"` // create | update | delete | attach | detach
	ChangedBy Actor     `json:"changed_by"`
	ChangedAt time.Time `json:"changed_at"`
	Snapshot  Knowledge `json:"snapshot"`
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
