// Package domain defines the knowledge model shared by the store, MCP
// server, and REST API. See docs/design/0001-architecture.md §3.
package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Type is the kind of a knowledge entry. Any path-segment slug is a valid
// type (design doc 0005): the built-in types below are recommendations with
// server behavior attached (compile_sql reads metrics), never a closed set —
// users' own document types are first-class. Slugs are plural, matching the
// knowledge-catalog reference bundles (design doc 0016).
type Type string

const (
	TypeMetrics    Type = "metrics"    // semantic metric definition (Apache Ossie)
	TypeQueries    Type = "queries"    // golden query: question + verified SQL
	TypeInsights   Type = "insights"   // how to read a metric: baselines, caveats
	TypeTerms      Type = "terms"      // glossary term
	TypeDatasets   Type = "datasets"   // dataset: a container grouping tables
	TypeTables     Type = "tables"     // table catalog entry
	TypeReferences Type = "references" // mirror of external material (enums, licenses, schema docs)
)

// Types lists the recommended (built-in) knowledge types, in display order
// (datasets before tables: a dataset is the container).
var Types = []Type{TypeMetrics, TypeQueries, TypeInsights, TypeTerms, TypeDatasets, TypeTables, TypeReferences}

// BuiltinType reports whether t is one of the recommended types.
func BuiltinType(t Type) bool {
	for _, v := range Types {
		if t == v {
			return true
		}
	}
	return false
}

// ValidType reports whether t can be a knowledge type: one path segment.
func ValidType(t Type) bool { return segmentRe.MatchString(string(t)) }

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

func ValidStatus(s Status) bool {
	for _, v := range Statuses {
		if s == v {
			return true
		}
	}
	return false
}

// Usage event kinds recorded per knowledge entry (design doc 0001 §9).
// The first three are recorded passively by reads; worked/failed are
// deliberate outcome reports (report_outcome) closing the write-back loop.
const (
	EventSearchHit = "search_hit" // appeared in search results
	EventFetched   = "fetched"    // fetched individually via get
	EventCompiled  = "compiled"   // referenced by compile_sql
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
	Compiles   int64      `json:"compiles"`
	Worked     int64      `json:"worked"`
	Failed     int64      `json:"failed"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

// Actor identifies who created or verified a knowledge entry.
type Actor struct {
	Kind string `json:"kind"` // "human" | "agent"
	Name string `json:"name"`
}

const (
	ActorHuman = "human"
	ActorAgent = "agent"
)

// Link is a typed edge to another knowledge entry, e.g. {rel: measures,
// target: "tables/orders"}.
type Link struct {
	Rel    string `json:"rel"`
	Target string `json:"target"` // "<type>/<id>"
}

// Knowledge is the common envelope for all knowledge types. Type-specific
// structured attributes live in Attrs; prose lives in Body (markdown).
// The envelope maps 1:1 to an OKF document (YAML frontmatter + markdown).
type Knowledge struct {
	Type        Type           `json:"type"`
	ID          string         `json:"id"` // slug, unique within type
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Resource    string         `json:"resource,omitempty"` // canonical URI of the underlying asset (OKF recommended key)
	Tags        []string       `json:"tags,omitempty"`
	Status      Status         `json:"status"`
	StatusNote  string         `json:"status_note,omitempty"` // free-form reason for the current status (why rejected/deprecated)
	CreatedBy   Actor          `json:"created_by"`
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

// URI returns the canonical reference, e.g. "ochakai://metrics/revenue".
func (k *Knowledge) URI() string { return fmt.Sprintf("ochakai://%s/%s", k.Type, k.ID) }

// SameContent reports whether o carries the same authored content as k:
// the fields a writer controls (title, description, resource, tags, status,
// status_note, links, attrs, body). Server-managed provenance and
// timestamps (created_*, verified_*, rejected_*, updated_at) and the
// attachment list are not content. Attrs are compared as canonical JSON,
// so the same value decoded from YAML (int) and from JSONB (float64)
// compares equal; values JSON cannot encode compare as different.
func (k *Knowledge) SameContent(o *Knowledge) bool {
	return k.Type == o.Type && k.ID == o.ID &&
		k.Title == o.Title && k.Description == o.Description &&
		k.Resource == o.Resource &&
		k.Status == o.Status && k.StatusNote == o.StatusNote &&
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

// segmentRe matches one path segment. Lowercase is recommended, but case,
// dots, and underscores are accepted so foreign OKF bundles (table names
// like GA_sessions_2017) import without renaming. The mandatory leading
// alphanumeric rules out "." and ".." — IDs stay safe as file paths.
var segmentRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

// ValidID reports whether id is a valid knowledge ID: slug segments
// separated by "/", mirroring OKF's hierarchical concept IDs (the bundle
// path is "<type>/<id>.md"). The final segment must not be "index" — that
// filename belongs to the generated per-directory index.md.
func ValidID(id string) bool {
	if id == "" || len(id) > 512 {
		return false
	}
	segs := strings.Split(id, "/")
	for _, s := range segs {
		if !segmentRe.MatchString(s) {
			return false
		}
	}
	return segs[len(segs)-1] != "index"
}

// ValidIDPrefix reports whether prefix can lead a knowledge ID: empty
// (the root) or slug segments separated by "/". Unlike ValidID, a final
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
		if !segmentRe.MatchString(s) {
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
