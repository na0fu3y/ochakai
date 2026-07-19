package compiler

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Error is a compilation failure. ochakai never guesses: anything outside
// the supported subset returns an Error with an actionable reason.
type Error struct{ Reason string }

func (e *Error) Error() string { return e.Reason }

// Request selects metrics and the shape of the result set. The output is
// always BigQuery SQL (design doc 0016: ochakai is BigQuery-only).
type Request struct {
	Metrics    []string   `json:"metrics"`              // metric names in the semantic model
	Dimensions []string   `json:"dimensions,omitempty"` // "dataset.field" group-by columns
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

// Result is the compiled BigQuery SQL plus what it was compiled from.
type Result struct {
	SQL          string   `json:"sql"`
	DatasetsUsed []string `json:"datasets_used"`
	Notes        []string `json:"notes,omitempty"`
}

var identRe = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\.([A-Za-z_][A-Za-z0-9_]*)`)

// bareColumn matches a single unquoted SQL column identifier. It gates
// caller-controlled field references (dimensions, filter fields, time
// grain) that pass through as physical columns: those flow verbatim into
// the compiled SQL, which is executed downstream with real warehouse
// credentials, so anything that is not a plain identifier must be rejected
// rather than concatenated (SQL injection).
var bareColumn = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Compile builds a SQL statement for the request against the model.
func Compile(m *Model, req Request) (*Result, error) {
	if len(req.Metrics) == 0 {
		return nil, &Error{Reason: "at least one metric is required"}
	}

	needed := map[string]bool{} // datasets referenced anywhere
	var notes []string

	// Resolve metric expressions: rewrite dataset.field to dataset.<physical
	// expr> and collect referenced datasets.
	type compiledMetric struct{ name, expr string }
	var metrics []compiledMetric
	for _, name := range req.Metrics {
		metric := m.metric(name)
		if metric == nil {
			return nil, &Error{Reason: fmt.Sprintf("metric %q is not defined in semantic model %q", name, m.Name)}
		}
		raw, ok := metric.Expression.ForBigQuery()
		if !ok {
			return nil, &Error{Reason: fmt.Sprintf("metric %q has no BIGQUERY or ANSI_SQL expression", name)}
		}
		expr, err := m.rewriteExpr(raw, needed, &notes)
		if err != nil {
			return nil, err
		}
		metrics = append(metrics, compiledMetric{name: name, expr: expr})
	}

	// Resolve dimensions, time grain, filters.
	type selectCol struct{ expr, alias string }
	var groupCols []selectCol
	if req.TimeGrain != nil {
		expr, err := m.resolveFieldRef(req.TimeGrain.Field, needed, &notes)
		if err != nil {
			return nil, err
		}
		truncated, err := truncateByGrain(expr, req.TimeGrain.Grain)
		if err != nil {
			return nil, err
		}
		groupCols = append(groupCols, selectCol{expr: truncated, alias: strings.ReplaceAll(req.TimeGrain.Field, ".", "_") + "_" + req.TimeGrain.Grain})
	}
	for _, dim := range req.Dimensions {
		expr, err := m.resolveFieldRef(dim, needed, &notes)
		if err != nil {
			return nil, err
		}
		groupCols = append(groupCols, selectCol{expr: expr, alias: strings.ReplaceAll(dim, ".", "_")})
	}

	var whereClauses []string
	for _, f := range req.Filters {
		expr, err := m.resolveFieldRef(f.Field, needed, &notes)
		if err != nil {
			return nil, err
		}
		clause, err := renderFilter(expr, f)
		if err != nil {
			return nil, err
		}
		whereClauses = append(whereClauses, clause)
	}

	// Resolve the join tree: single fact (root) + star joins.
	root := ""
	if len(metrics) > 0 {
		// The first dataset referenced by the first metric anchors the join tree.
		for _, ds := range referencedDatasets(m, mustExpr(m.metric(req.Metrics[0]))) {
			root = ds
			break
		}
	}
	if root == "" {
		for ds := range needed {
			root = ds
			break
		}
	}
	if root == "" {
		return nil, &Error{Reason: "could not determine a root dataset from the metric expression"}
	}
	joins, err := m.resolveJoins(root, needed)
	if err != nil {
		return nil, err
	}

	// Assemble.
	var b strings.Builder
	b.WriteString("SELECT\n")
	var selects []string
	for _, c := range groupCols {
		selects = append(selects, fmt.Sprintf("  %s AS %s", c.expr, c.alias))
	}
	for _, mtr := range metrics {
		selects = append(selects, fmt.Sprintf("  %s AS %s", mtr.expr, mtr.name))
	}
	b.WriteString(strings.Join(selects, ",\n"))
	rootDS := m.dataset(root)
	b.WriteString(fmt.Sprintf("\nFROM %s AS %s", quoteSource(rootDS.Source), root))
	for _, j := range joins {
		b.WriteString("\n" + j)
	}
	if len(whereClauses) > 0 {
		b.WriteString("\nWHERE " + strings.Join(whereClauses, "\n  AND "))
	}
	if len(groupCols) > 0 {
		var nums []string
		for i := range groupCols {
			nums = append(nums, fmt.Sprintf("%d", i+1))
		}
		b.WriteString("\nGROUP BY " + strings.Join(nums, ", "))
		b.WriteString("\nORDER BY 1")
	}
	if req.Limit > 0 {
		b.WriteString(fmt.Sprintf("\nLIMIT %d", req.Limit))
	}

	datasets := make([]string, 0, len(needed))
	for ds := range needed {
		datasets = append(datasets, ds)
	}
	sort.Strings(datasets)
	return &Result{SQL: b.String(), DatasetsUsed: datasets, Notes: dedupe(notes)}, nil
}

// rewriteExpr replaces every dataset.field reference in an expression with
// the field's physical expression qualified by the dataset alias, and
// records referenced datasets.
func (m *Model) rewriteExpr(expr string, needed map[string]bool, notes *[]string) (string, error) {
	var rewriteErr error
	out := identRe.ReplaceAllStringFunc(expr, func(ref string) string {
		parts := strings.SplitN(ref, ".", 2)
		ds := m.dataset(parts[0])
		if ds == nil {
			// Not a dataset reference (e.g. a SQL function namespace); leave as is.
			return ref
		}
		needed[ds.Name] = true
		resolved, err := m.resolveField(ds, parts[1], notes)
		if err != nil {
			rewriteErr = err
			return ref
		}
		return resolved
	})
	if rewriteErr != nil {
		return "", rewriteErr
	}
	return out, nil
}

// resolveFieldRef resolves a "dataset.field" reference used as a dimension,
// filter, or time grain column.
func (m *Model) resolveFieldRef(ref string, needed map[string]bool, notes *[]string) (string, error) {
	parts := strings.SplitN(ref, ".", 2)
	if len(parts) != 2 {
		return "", &Error{Reason: fmt.Sprintf("field reference %q must be dataset.field", ref)}
	}
	ds := m.dataset(parts[0])
	if ds == nil {
		return "", &Error{Reason: fmt.Sprintf("dataset %q is not defined in semantic model %q", parts[0], m.Name)}
	}
	needed[ds.Name] = true
	return m.resolveField(ds, parts[1], notes)
}

// resolveField maps a logical field to "alias.physical_expr". Unknown names
// pass through as physical columns (the Ossie draft does not require every
// column to be declared as a field), with a note so the caller knows the
// column's existence was taken on trust, not checked against the model.
func (m *Model) resolveField(ds *Dataset, name string, notes *[]string) (string, error) {
	f := ds.field(name)
	if f == nil {
		// Undeclared columns pass through as physical columns (the Ossie
		// draft does not require every column to be declared), but only when
		// the name is a bare SQL identifier. This branch is reachable with
		// caller-controlled input (a dimension/filter/time-grain field that
		// names no declared field), so an unchecked pass-through would splice
		// arbitrary SQL into the compiled statement.
		if !bareColumn.MatchString(name) {
			return "", &Error{Reason: fmt.Sprintf("field %q is not defined in dataset %q and is not a bare column name; declare it as a field to use an expression", name, ds.Name)}
		}
		*notes = append(*notes, fmt.Sprintf("%s.%s is not declared in semantic model %q; passed through as a physical column", ds.Name, name, m.Name))
		return ds.Name + "." + name, nil
	}
	expr, ok := f.Expression.ForBigQuery()
	if !ok {
		return "", &Error{Reason: fmt.Sprintf("field %s.%s has no BIGQUERY or ANSI_SQL expression", ds.Name, name)}
	}
	// Field expressions are relative to their dataset: qualify bare
	// identifiers with the dataset alias.
	if bareColumn.MatchString(expr) {
		return ds.Name + "." + expr, nil
	}
	return "(" + expr + ")", nil
}

// resolveJoins connects every needed dataset to the root via relationship
// edges (LEFT JOIN from the fact outward). Unreachable datasets are an error.
func (m *Model) resolveJoins(root string, needed map[string]bool) ([]string, error) {
	if m.dataset(root) == nil {
		return nil, &Error{Reason: fmt.Sprintf("dataset %q is not defined in semantic model %q", root, m.Name)}
	}
	joined := map[string]bool{root: true}
	var joins []string
	remaining := map[string]bool{}
	for ds := range needed {
		if ds != root {
			remaining[ds] = true
		}
	}
	for len(remaining) > 0 {
		progressed := false
		for _, rel := range m.Relationships {
			var newDS string
			switch {
			case joined[rel.From] && remaining[rel.To]:
				newDS = rel.To
			case joined[rel.To] && remaining[rel.From]:
				newDS = rel.From
			default:
				continue
			}
			if len(rel.FromColumns) != len(rel.ToColumns) || len(rel.FromColumns) == 0 {
				return nil, &Error{Reason: fmt.Sprintf("relationship %q has mismatched from_columns/to_columns", rel.Name)}
			}
			ds := m.dataset(newDS)
			if ds == nil {
				return nil, &Error{Reason: fmt.Sprintf("relationship %q references undefined dataset %q", rel.Name, newDS)}
			}
			var conds []string
			for i := range rel.FromColumns {
				conds = append(conds, fmt.Sprintf("%s.%s = %s.%s", rel.From, rel.FromColumns[i], rel.To, rel.ToColumns[i]))
			}
			joins = append(joins, fmt.Sprintf("LEFT JOIN %s AS %s ON %s", quoteSource(ds.Source), newDS, strings.Join(conds, " AND ")))
			joined[newDS] = true
			delete(remaining, newDS)
			progressed = true
		}
		if !progressed {
			var missing []string
			for ds := range remaining {
				missing = append(missing, ds)
			}
			sort.Strings(missing)
			return nil, &Error{Reason: fmt.Sprintf("no relationship path from %q to %s; ochakai only compiles star joins declared in the semantic model", root, strings.Join(missing, ", "))}
		}
	}
	return joins, nil
}

// dedupe drops repeated notes (the same undeclared field may be resolved
// as a dimension, a filter, and a grain in one request), keeping order.
func dedupe(notes []string) []string {
	seen := map[string]bool{}
	out := notes[:0]
	for _, n := range notes {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

func referencedDatasets(m *Model, expr string) []string {
	var out []string
	seen := map[string]bool{}
	for _, match := range identRe.FindAllStringSubmatch(expr, -1) {
		if m.dataset(match[1]) != nil && !seen[match[1]] {
			seen[match[1]] = true
			out = append(out, match[1])
		}
	}
	return out
}

func mustExpr(metric *Metric) string {
	expr, _ := metric.Expression.ForBigQuery()
	return expr
}

func truncateByGrain(expr, grain string) (string, error) {
	switch strings.ToLower(grain) {
	case "day", "week", "month", "quarter", "year":
	default:
		return "", &Error{Reason: fmt.Sprintf("unsupported time grain %q (supported: day, week, month, quarter, year)", grain)}
	}
	return fmt.Sprintf("DATE_TRUNC(DATE(%s), %s)", expr, strings.ToUpper(grain)), nil
}

func renderFilter(expr string, f Filter) (string, error) {
	op := strings.ToLower(strings.TrimSpace(f.Op))
	switch op {
	case "=", "!=", ">", ">=", "<", "<=":
		lit, err := renderLiteral(f.Value)
		if err != nil {
			return "", err
		}
		if op == "!=" {
			op = "<>"
		}
		return fmt.Sprintf("%s %s %s", expr, op, lit), nil
	case "in", "not_in":
		vals, ok := f.Value.([]any)
		if !ok || len(vals) == 0 {
			return "", &Error{Reason: fmt.Sprintf("filter on %s: op %q requires a non-empty list value", f.Field, op)}
		}
		var lits []string
		for _, v := range vals {
			lit, err := renderLiteral(v)
			if err != nil {
				return "", err
			}
			lits = append(lits, lit)
		}
		kw := "IN"
		if op == "not_in" {
			kw = "NOT IN"
		}
		return fmt.Sprintf("%s %s (%s)", expr, kw, strings.Join(lits, ", ")), nil
	default:
		return "", &Error{Reason: fmt.Sprintf("unsupported filter op %q (supported: = != > >= < <= in not_in)", f.Op)}
	}
}

// renderLiteral emits a SQL literal. Strings are single-quoted with quote
// doubling; control characters are rejected rather than escaped.
func renderLiteral(v any) (string, error) {
	switch val := v.(type) {
	case string:
		for _, r := range val {
			if r < 0x20 || r == 0x7f || r == '\\' {
				return "", &Error{Reason: "string filter values must not contain control characters or backslashes"}
			}
		}
		return "'" + strings.ReplaceAll(val, "'", "''") + "'", nil
	case float64, int, int64:
		return fmt.Sprintf("%v", val), nil
	case bool:
		if val {
			return "TRUE", nil
		}
		return "FALSE", nil
	default:
		return "", &Error{Reason: fmt.Sprintf("unsupported filter value type %T", v)}
	}
}

// quoteSource backtick-quotes a physical source reference
// (e.g. project.dataset.table) the BigQuery way.
func quoteSource(source string) string {
	return "`" + source + "`"
}
