package apiclient

import (
	"time"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// The compile types mirror the wire contract in api/openapi.yaml rather
// than importing internal/service: a client speaks the API, not server
// internals, and this keeps apiclient (and any future standalone CLI
// binary) free of the server's store/embedding dependency tree.
// TestCompileTypesMatchServerWire pins the two shapes together.

type CompileRequest struct {
	// Model is the id of a models entry (design doc 0018); empty means
	// resolved from the first metric's entry attrs.model.
	Model      string     `json:"model,omitempty"`
	Metrics    []string   `json:"metrics"`
	Dimensions []string   `json:"dimensions,omitempty"`
	Filters    []Filter   `json:"filters,omitempty"`
	TimeGrain  *TimeGrain `json:"time_grain,omitempty"`
	Limit      int        `json:"limit,omitempty"`
}

type Filter struct {
	Field string `json:"field"` // "dataset.field"
	Op    string `json:"op"`    // = != > >= < <= in not_in
	Value any    `json:"value"` // scalar, or list for in/not_in
}

type TimeGrain struct {
	Field string `json:"field"` // "dataset.field", a time column
	Grain string `json:"grain"` // day | week | month | quarter | year
}

// BrowseResult mirrors GET /api/v1/browse (design docs 0014, 0016): one
// level of the ID hierarchy — the subdirectories and entries directly
// under the prefix ("" is the root). TestBrowseResultMatchesServerWire
// pins it to service.BrowseResult.
type BrowseResult struct {
	Dirs      []BrowseDir   `json:"dirs,omitempty"`
	Entries   []BrowseEntry `json:"entries,omitempty"`
	Truncated bool          `json:"truncated,omitempty"`
}

// BrowseDir is one subdirectory (ID segment) with the number of entries
// anywhere beneath it.
type BrowseDir struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// BrowseEntry is the light projection of an entry in a tree listing:
// no body, no links, no attrs. Description rides along so a directory
// listing can render as an index page.
type BrowseEntry struct {
	Type        string        `json:"type"`
	ID          string        `json:"id"`
	Title       string        `json:"title,omitempty"` // empty means the id's last segment (design doc 0022)
	Description string        `json:"description,omitempty"`
	Status      domain.Status `json:"status"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

type CompileResult struct {
	SQL          string   `json:"sql"`
	DatasetsUsed []string `json:"datasets_used"`
	Notes        []string `json:"notes,omitempty"`
	// Model is the id of the models entry the SQL was compiled from, and
	// ModelStatus its verification status — compile does not gate on
	// status; judge trust from provenance (design doc 0018 §4.3).
	Model       string        `json:"model"`
	ModelStatus domain.Status `json:"model_status"`
	// VerifiedQueries are golden queries about the requested metrics;
	// prefer one when it answers the question.
	VerifiedQueries []domain.SearchHit `json:"verified_queries,omitempty"`
}
