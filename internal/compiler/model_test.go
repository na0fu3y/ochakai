package compiler

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// specMap mimics the real path a spec takes into ModelFromSpec /
// ModelsFromSpec: YAML → map[string]any (importer), stored as JSONB,
// decoded back to map[string]any (store).
func specMap(t *testing.T, src string) map[string]any {
	t.Helper()
	var spec map[string]any
	if err := yaml.Unmarshal([]byte(src), &spec); err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	return spec
}

func TestModelFromSpecSingleObject(t *testing.T) {
	m, err := ModelFromSpec(specMap(t, testModelYAML), "")
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
	expr, ok := m.Metrics[0].Expression.ForDialect("ANSI_SQL")
	if !ok || expr != "SUM(orders.amount)" {
		t.Errorf("metric expression = %q, %v", expr, ok)
	}
}

// The top-level document shape wraps models in a semantic_model list;
// name selects one, and an empty name means the first.
func TestModelFromSpecDocumentShape(t *testing.T) {
	doc := "semantic_model:\n" +
		"  - name: alpha\n    metrics:\n      - name: m1\n        expression: SUM(x)\n" +
		"  - name: beta\n    metrics:\n      - name: m2\n        expression: SUM(y)\n"
	spec := specMap(t, doc)

	m, err := ModelFromSpec(spec, "beta")
	if err != nil {
		t.Fatalf("ModelFromSpec(beta): %v", err)
	}
	if m.Name != "beta" {
		t.Errorf("name = %q, want beta", m.Name)
	}

	first, err := ModelFromSpec(spec, "")
	if err != nil {
		t.Fatalf("ModelFromSpec(empty name): %v", err)
	}
	if first.Name != "alpha" {
		t.Errorf("empty name should select the first model, got %q", first.Name)
	}

	if _, err := ModelFromSpec(spec, "missing"); err == nil ||
		!strings.Contains(err.Error(), "not found") {
		t.Errorf("unknown name should fail with not found, got %v", err)
	}
}

func TestModelsFromSpecShapes(t *testing.T) {
	single, err := ModelsFromSpec(specMap(t, testModelYAML))
	if err != nil || len(single) != 1 || single[0].Name != "sales_analytics" {
		t.Errorf("single object: models = %+v, err = %v", single, err)
	}

	doc := "semantic_model:\n  - name: alpha\n  - name: beta\n"
	many, err := ModelsFromSpec(specMap(t, doc))
	if err != nil || len(many) != 2 || many[0].Name != "alpha" || many[1].Name != "beta" {
		t.Errorf("document shape: models = %+v, err = %v", many, err)
	}

	if _, err := ModelsFromSpec(specMap(t, "just: garbage\n")); err == nil {
		t.Error("a nameless non-model document must be rejected")
	}
}

// A plain-string expression (the shortest Ossie shape) must parse as an
// ANSI_SQL expression through the YAML → JSON round-trip.
func TestModelFromSpecPlainStringExpression(t *testing.T) {
	m, err := ModelFromSpec(specMap(t, "name: mini\nmetrics:\n  - name: n\n    expression: COUNT(*)\n"), "")
	if err != nil {
		t.Fatalf("ModelFromSpec: %v", err)
	}
	expr, ok := m.Metrics[0].Expression.ForDialect("ANSI_SQL")
	if !ok || expr != "COUNT(*)" {
		t.Errorf("plain string expression = %q, %v", expr, ok)
	}
}
