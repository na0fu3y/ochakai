// Package domain defines the knowledge model shared by the store, MCP
// server, and REST API. See docs/design/0001-architecture.md §3.
package domain

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Type is the kind of a knowledge entry. Any path-segment slug is a valid
// type (design doc 0005): the built-in types below are recommendations with
// server behavior attached (compile_sql reads metrics, resource export reads
// tables), never a closed set — users' own document types are first-class.
type Type string

const (
	TypeMetric  Type = "metric"  // semantic metric definition (Apache Ossie)
	TypeQuery   Type = "query"   // golden query: question + verified SQL
	TypeInsight Type = "insight" // how to read a metric: baselines, caveats
	TypeTerm    Type = "term"    // glossary term
	TypeTable   Type = "table"   // table catalog entry
)

// Types lists the recommended (built-in) knowledge types, in display order.
var Types = []Type{TypeMetric, TypeQuery, TypeInsight, TypeTerm, TypeTable}

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

func ValidStatus(s Status) bool {
	return s == StatusDraft || s == StatusVerified || s == StatusDeprecated || s == StatusRejected
}

// Usage event kinds recorded per knowledge entry (design doc 0001 §9).
const (
	EventSearchHit = "search_hit" // appeared in search results
	EventFetched   = "fetched"    // fetched individually via get
	EventCompiled  = "compiled"   // referenced by compile_sql
)

// Usage aggregates how often a knowledge entry was actually used.
type Usage struct {
	SearchHits int64      `json:"search_hits"`
	Fetches    int64      `json:"fetches"`
	Compiles   int64      `json:"compiles"`
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
// target: "table/orders"}.
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
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// URI returns the canonical reference, e.g. "ochakai://metric/revenue".
func (k *Knowledge) URI() string { return fmt.Sprintf("ochakai://%s/%s", k.Type, k.ID) }

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

// SearchHit is one search result with its ranking score.
type SearchHit struct {
	Knowledge
	Score float64 `json:"score"`
}
