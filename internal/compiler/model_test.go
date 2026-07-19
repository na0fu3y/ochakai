package compiler

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// specMap mimics the real path a spec takes into ModelFromSpec: YAML →
// map[string]any (the client filling attrs.spec), stored as JSONB,
// decoded back to map[string]any (store).
func specMap(t *testing.T, src string) map[string]any {
	t.Helper()
	var spec map[string]any
	if err := yaml.Unmarshal([]byte(src), &spec); err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	return spec
}

func TestModelFromSpec(t *testing.T) {
	m, err := ModelFromSpec(specMap(t, testModelYAML))
	if err != nil {
		t.Fatalf("ModelFromSpec: %v", err)
	}
	if m.Name != "sales_analytics" {
		t.Errorf("name = %q, want sales_analytics", m.Name)
	}
	if len(m.Datasets) != 2 || len(m.Metrics) != 2 || len(m.Relationships) != 1 {
		t.Errorf("model shape wrong: %d datasets, %d metrics, %d relationships",
			len(m.Datasets), len(m.Metrics), len(m.Relationships))
	}
	expr, ok := m.Metrics[0].Expression.ForBigQuery()
	if !ok || expr != "SUM(orders.amount)" {
		t.Errorf("metric expression = %q, %v", expr, ok)
	}
}

// A plain-string expression (the shortest Ossie shape) must parse as an
// ANSI_SQL expression through the YAML → JSON round-trip.
func TestModelFromSpecPlainStringExpression(t *testing.T) {
	m, err := ModelFromSpec(specMap(t, "name: mini\nmetrics:\n  - name: n\n    expression: COUNT(*)\n"))
	if err != nil {
		t.Fatalf("ModelFromSpec: %v", err)
	}
	expr, ok := m.Metrics[0].Expression.ForBigQuery()
	if !ok || expr != "COUNT(*)" {
		t.Errorf("plain string expression = %q, %v", expr, ok)
	}
}

// Validate is the write-time guard behind models entries (design doc
// 0018 §4.2): the example model passes, and each structural defect is
// named in the error. Join and primary-key columns are physical columns,
// so a join on an undeclared column is fine.
func TestModelValidate(t *testing.T) {
	m, err := ModelFromSpec(specMap(t, testModelYAML))
	if err != nil {
		t.Fatalf("ModelFromSpec: %v", err)
	}
	if err := m.Validate(); err != nil {
		t.Errorf("example model must validate, got %v", err)
	}

	cases := []struct {
		name string
		yaml string
		want string
	}{
		{"missing name", "datasets:\n  - name: d\n", "name is required"},
		{"no datasets", "name: m\nmetrics:\n  - name: n\n    expression: COUNT(*)\n", "no datasets"},
		{"duplicate dataset", "name: m\ndatasets:\n  - name: d\n  - name: d\n", "duplicate dataset"},
		{"nameless field", "name: m\ndatasets:\n  - name: d\n    fields:\n      - expression: x\n", "field without a name"},
		{"duplicate field", "name: m\ndatasets:\n  - name: d\n    fields:\n      - name: f\n        expression: x\n      - name: f\n        expression: y\n", "duplicate field"},
		{"duplicate metric", "name: m\ndatasets:\n  - name: d\nmetrics:\n  - name: n\n    expression: COUNT(*)\n  - name: n\n    expression: COUNT(*)\n", "duplicate metric"},
		{"unknown join dataset", "name: m\ndatasets:\n  - name: d\nrelationships:\n  - name: r\n    from: d\n    to: nowhere\n    from_columns: [a]\n    to_columns: [b]\n", `unknown dataset "nowhere"`},
		{"mismatched join columns", "name: m\ndatasets:\n  - name: d\n  - name: e\nrelationships:\n  - name: r\n    from: d\n    to: e\n    from_columns: [a, b]\n    to_columns: [c]\n", "matching from_columns and to_columns"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, err := ModelFromSpec(specMap(t, c.yaml))
			if err != nil {
				t.Fatalf("ModelFromSpec: %v", err)
			}
			if err := m.Validate(); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("Validate = %v, want error mentioning %q", err, c.want)
			}
		})
	}
}
