// Package compiler deterministically compiles metric requests into SQL from
// Apache Ossie semantic models. Phase 1 scope (design doc §4): single fact
// table + star joins, BigQuery output only (design doc 0016). Requests outside the
// supported subset fail with a clear error — never a guess.
package compiler

import (
	"encoding/json"
	"fmt"
)

// Model is the subset of the Ossie core-spec (0.2.x) that ochakai compiles.
// The stored spec is kept verbatim; this struct only reads the parts we use.
type Model struct {
	Name          string         `json:"name" yaml:"name"`
	Description   string         `json:"description" yaml:"description"`
	Datasets      []Dataset      `json:"datasets" yaml:"datasets"`
	Relationships []Relationship `json:"relationships" yaml:"relationships"`
	Metrics       []Metric       `json:"metrics" yaml:"metrics"`
}

type Dataset struct {
	Name        string   `json:"name" yaml:"name"`
	Source      string   `json:"source" yaml:"source"`
	PrimaryKey  []string `json:"primary_key" yaml:"primary_key"`
	Description string   `json:"description" yaml:"description"`
	Fields      []Field  `json:"fields" yaml:"fields"`
}

type Field struct {
	Name        string      `json:"name" yaml:"name"`
	Expression  Expressions `json:"expression" yaml:"expression"`
	Description string      `json:"description" yaml:"description"`
	Dimension   *struct {
		IsTime bool `json:"is_time" yaml:"is_time"`
	} `json:"dimension" yaml:"dimension"`
}

type Relationship struct {
	Name        string   `json:"name" yaml:"name"`
	From        string   `json:"from" yaml:"from"`
	To          string   `json:"to" yaml:"to"`
	FromColumns []string `json:"from_columns" yaml:"from_columns"`
	ToColumns   []string `json:"to_columns" yaml:"to_columns"`
}

type Metric struct {
	Name        string      `json:"name" yaml:"name"`
	Expression  Expressions `json:"expression" yaml:"expression"`
	Description string      `json:"description" yaml:"description"`
}

// DialectExpression is one dialect's rendering of an expression.
type DialectExpression struct {
	Dialect    string `json:"dialect" yaml:"dialect"`
	Expression string `json:"expression" yaml:"expression"`
}

// Expressions tolerates the shapes that appear in Ossie's draft spec and
// examples: a plain string, a list of {dialect, expression}, or an object
// with a "dialects" list.
type Expressions []DialectExpression

func (e *Expressions) UnmarshalJSON(data []byte) error {
	var plain string
	if err := json.Unmarshal(data, &plain); err == nil {
		*e = Expressions{{Dialect: "ANSI_SQL", Expression: plain}}
		return nil
	}
	var list []DialectExpression
	if err := json.Unmarshal(data, &list); err == nil {
		*e = list
		return nil
	}
	var wrapped struct {
		Dialects []DialectExpression `json:"dialects"`
	}
	if err := json.Unmarshal(data, &wrapped); err == nil && wrapped.Dialects != nil {
		*e = wrapped.Dialects
		return nil
	}
	return fmt.Errorf("unsupported expression shape: %s", truncate(string(data), 120))
}

func (e *Expressions) UnmarshalYAML(unmarshal func(any) error) error {
	var plain string
	if err := unmarshal(&plain); err == nil {
		*e = Expressions{{Dialect: "ANSI_SQL", Expression: plain}}
		return nil
	}
	var list []DialectExpression
	if err := unmarshal(&list); err == nil {
		*e = list
		return nil
	}
	var wrapped struct {
		Dialects []DialectExpression `yaml:"dialects"`
	}
	if err := unmarshal(&wrapped); err == nil && wrapped.Dialects != nil {
		*e = wrapped.Dialects
		return nil
	}
	return fmt.Errorf("unsupported expression shape")
}

// ForBigQuery returns the expression under the Ossie BIGQUERY dialect
// key, falling back to ANSI_SQL. Compilation targets BigQuery only
// (design doc 0016) — the fallback is about accepting semantic models
// whose expressions are declared portably, not about other outputs.
func (e Expressions) ForBigQuery() (string, bool) {
	for _, d := range e {
		if d.Dialect == "BIGQUERY" {
			return d.Expression, true
		}
	}
	for _, d := range e {
		if d.Dialect == "ANSI_SQL" || d.Dialect == "" {
			return d.Expression, true
		}
	}
	return "", false
}

func (m *Model) dataset(name string) *Dataset {
	for i := range m.Datasets {
		if m.Datasets[i].Name == name {
			return &m.Datasets[i]
		}
	}
	return nil
}

func (m *Model) metric(name string) *Metric {
	for i := range m.Metrics {
		if m.Metrics[i].Name == name {
			return &m.Metrics[i]
		}
	}
	return nil
}

func (d *Dataset) field(name string) *Field {
	for i := range d.Fields {
		if d.Fields[i].Name == name {
			return &d.Fields[i]
		}
	}
	return nil
}

// ModelFromSpec reads a stored semantic model spec (the JSON-safe map held
// in a models entry's attrs.spec, design doc 0018). One entry is one model:
// the multi-model {semantic_model: [...]} document shape is a source-file
// layout, split by the client before entries are created.
func ModelFromSpec(spec map[string]any) (*Model, error) {
	raw, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	var m Model
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parse semantic model: %w", err)
	}
	return &m, nil
}

// Validate checks a model's structural integrity — the write-time guard
// behind type=models entries (design doc 0018 §4.2): names present and
// unique, relationships joining datasets that exist with matched column
// counts. Join and primary-key columns are physical columns, not modeled
// fields, so their existence cannot be checked here; request-dependent
// concerns (dialect choice, the supported compile subset) stay
// compile-time errors.
func (m *Model) Validate() error {
	if m.Name == "" {
		return fmt.Errorf("model name is required")
	}
	if len(m.Datasets) == 0 {
		return fmt.Errorf("model %q has no datasets", m.Name)
	}
	seenDS := map[string]bool{}
	for _, ds := range m.Datasets {
		if ds.Name == "" {
			return fmt.Errorf("dataset without a name")
		}
		if seenDS[ds.Name] {
			return fmt.Errorf("duplicate dataset %q", ds.Name)
		}
		seenDS[ds.Name] = true
		seenF := map[string]bool{}
		for _, f := range ds.Fields {
			if f.Name == "" {
				return fmt.Errorf("dataset %q has a field without a name", ds.Name)
			}
			if seenF[f.Name] {
				return fmt.Errorf("dataset %q has duplicate field %q", ds.Name, f.Name)
			}
			seenF[f.Name] = true
		}
	}
	seenM := map[string]bool{}
	for _, mt := range m.Metrics {
		if mt.Name == "" {
			return fmt.Errorf("metric without a name")
		}
		if seenM[mt.Name] {
			return fmt.Errorf("duplicate metric %q", mt.Name)
		}
		seenM[mt.Name] = true
	}
	for _, r := range m.Relationships {
		if m.dataset(r.From) == nil {
			return fmt.Errorf("relationship %q joins unknown dataset %q", r.Name, r.From)
		}
		if m.dataset(r.To) == nil {
			return fmt.Errorf("relationship %q joins unknown dataset %q", r.Name, r.To)
		}
		if len(r.FromColumns) == 0 || len(r.FromColumns) != len(r.ToColumns) {
			return fmt.Errorf("relationship %q needs matching from_columns and to_columns", r.Name)
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
