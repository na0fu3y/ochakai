// Package compiler deterministically compiles metric requests into SQL from
// Apache Ossie semantic models. Phase 1 scope (design doc §4): single fact
// table + star joins, BigQuery and ANSI dialects. Requests outside the
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

// ForDialect returns the expression for the requested Ossie dialect,
// falling back to ANSI_SQL.
func (e Expressions) ForDialect(dialect string) (string, bool) {
	for _, d := range e {
		if d.Dialect == dialect {
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

// ModelFromSpec reads a stored semantic model spec (JSON round-trip of the
// imported YAML). It accepts either a single model object or the top-level
// {semantic_model: [...]} document shape, in which case name selects the
// model (empty name means the only model).
func ModelFromSpec(spec map[string]any, name string) (*Model, error) {
	raw, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	var doc struct {
		SemanticModel []json.RawMessage `json:"semantic_model"`
	}
	if err := json.Unmarshal(raw, &doc); err == nil && len(doc.SemanticModel) > 0 {
		for _, rm := range doc.SemanticModel {
			var m Model
			if err := json.Unmarshal(rm, &m); err != nil {
				return nil, fmt.Errorf("parse semantic model: %w", err)
			}
			if name == "" || m.Name == name {
				return &m, nil
			}
		}
		return nil, fmt.Errorf("semantic model %q not found in document", name)
	}
	var m Model
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parse semantic model: %w", err)
	}
	return &m, nil
}

// ModelsFromSpec reads every model in a spec document (either a single
// model object or {semantic_model: [...]}).
func ModelsFromSpec(spec map[string]any) ([]Model, error) {
	raw, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	var doc struct {
		SemanticModel []Model `json:"semantic_model"`
	}
	if err := json.Unmarshal(raw, &doc); err == nil && len(doc.SemanticModel) > 0 {
		return doc.SemanticModel, nil
	}
	var m Model
	if err := json.Unmarshal(raw, &m); err != nil || m.Name == "" {
		return nil, fmt.Errorf("document is neither a semantic model nor {semantic_model: [...]}")
	}
	return []Model{m}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
