package apiclient

import "github.com/na0fu3y/ochakai/internal/domain"

// The compile types mirror the wire contract in api/openapi.yaml rather
// than importing internal/service: a client speaks the API, not server
// internals, and this keeps apiclient (and any future standalone CLI
// binary) free of the server's store/embedding dependency tree.
// TestCompileTypesMatchServerWire pins the two shapes together.

type CompileRequest struct {
	Model      string     `json:"model,omitempty"`
	Metrics    []string   `json:"metrics"`
	Dimensions []string   `json:"dimensions,omitempty"`
	Filters    []Filter   `json:"filters,omitempty"`
	TimeGrain  *TimeGrain `json:"time_grain,omitempty"`
	Dialect    string     `json:"dialect,omitempty"` // "bigquery" (default) | "ansi"
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

// ImportReport is the response of POST /api/v1/import/ossie.
// TestImportReportMatchesServerWire pins it to importer.Report.
type ImportReport struct {
	Models  []string `json:"models"`
	Created []string `json:"created"`
	Updated []string `json:"updated"`
}

type CompileResult struct {
	SQL          string   `json:"sql"`
	Dialect      string   `json:"dialect"`
	DatasetsUsed []string `json:"datasets_used"`
	Notes        []string `json:"notes,omitempty"`
	// VerifiedQueries are golden queries about the requested metrics;
	// prefer one when it answers the question.
	VerifiedQueries []domain.SearchHit `json:"verified_queries,omitempty"`
}
