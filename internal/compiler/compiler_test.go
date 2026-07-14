package compiler

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const testModelYAML = `
name: sales_analytics
description: Sales star schema
datasets:
  - name: orders
    source: shop.core.orders
    primary_key: [order_id]
    fields:
      - name: amount
        expression:
          - dialect: ANSI_SQL
            expression: total_price
      - name: ordered_at
        expression:
          - dialect: ANSI_SQL
            expression: created_at
        dimension:
          is_time: true
  - name: customers
    source: shop.core.customers
    primary_key: [id]
    fields:
      - name: region
        expression:
          - dialect: ANSI_SQL
            expression: region_code
relationships:
  - name: orders_to_customers
    from: orders
    to: customers
    from_columns: [customer_id]
    to_columns: [id]
metrics:
  - name: revenue
    description: Total revenue
    expression:
      - dialect: ANSI_SQL
        expression: SUM(orders.amount)
  - name: avg_order_value
    expression:
      - dialect: ANSI_SQL
        expression: SUM(orders.amount) / COUNT(DISTINCT orders.order_id)
`

func testModel(t *testing.T) *Model {
	t.Helper()
	var m Model
	if err := yaml.Unmarshal([]byte(testModelYAML), &m); err != nil {
		t.Fatalf("parse test model: %v", err)
	}
	return &m
}

func TestCompileSimpleMetric(t *testing.T) {
	res, err := Compile(testModel(t), Request{Metrics: []string{"revenue"}, Dialect: "ansi"})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	want := "SELECT\n  SUM(orders.total_price) AS revenue\nFROM shop.core.orders AS orders"
	if res.SQL != want {
		t.Errorf("got:\n%s\nwant:\n%s", res.SQL, want)
	}
	if len(res.DatasetsUsed) != 1 || res.DatasetsUsed[0] != "orders" {
		t.Errorf("datasets used = %v", res.DatasetsUsed)
	}
}

func TestCompileStarJoinWithDimensionsFiltersGrain(t *testing.T) {
	res, err := Compile(testModel(t), Request{
		Metrics:    []string{"revenue", "avg_order_value"},
		Dimensions: []string{"customers.region"},
		Filters: []Filter{
			{Field: "customers.region", Op: "in", Value: []any{"JP", "US"}},
			{Field: "orders.amount", Op: ">", Value: float64(0)},
		},
		TimeGrain: &TimeGrain{Field: "orders.ordered_at", Grain: "month"},
		Dialect:   "bigquery",
		Limit:     100,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	sql := res.SQL
	checks := []string{
		"DATE_TRUNC(DATE(orders.created_at), MONTH) AS orders_ordered_at_month",
		"customers.region_code AS customers_region",
		"SUM(orders.total_price) AS revenue",
		"SUM(orders.total_price) / COUNT(DISTINCT orders.order_id) AS avg_order_value",
		"FROM `shop.core.orders` AS orders",
		"LEFT JOIN `shop.core.customers` AS customers ON orders.customer_id = customers.id",
		"customers.region_code IN ('JP', 'US')",
		"orders.total_price > 0",
		"GROUP BY 1, 2",
		"LIMIT 100",
	}
	for _, want := range checks {
		if !strings.Contains(sql, want) {
			t.Errorf("SQL missing %q:\n%s", want, sql)
		}
	}
}

func TestCompileUnknownMetricFails(t *testing.T) {
	_, err := Compile(testModel(t), Request{Metrics: []string{"nonexistent"}})
	if err == nil || !strings.Contains(err.Error(), "nonexistent") {
		t.Fatalf("want unknown-metric error, got %v", err)
	}
}

func TestCompileUnreachableDatasetFails(t *testing.T) {
	m := testModel(t)
	m.Relationships = nil
	_, err := Compile(m, Request{Metrics: []string{"revenue"}, Dimensions: []string{"customers.region"}})
	if err == nil || !strings.Contains(err.Error(), "no relationship path") {
		t.Fatalf("want unreachable error, got %v", err)
	}
}

func TestFilterLiteralEscaping(t *testing.T) {
	res, err := Compile(testModel(t), Request{
		Metrics: []string{"revenue"},
		Filters: []Filter{{Field: "customers.region", Op: "=", Value: "J'P"}},
		Dialect: "ansi",
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(res.SQL, "'J''P'") {
		t.Errorf("quote not doubled:\n%s", res.SQL)
	}

	_, err = Compile(testModel(t), Request{
		Metrics: []string{"revenue"},
		Filters: []Filter{{Field: "customers.region", Op: "=", Value: "bad\nvalue"}},
	})
	if err == nil {
		t.Error("control characters in literals must be rejected")
	}
}

func TestCompileUnsupportedDialectFails(t *testing.T) {
	_, err := Compile(testModel(t), Request{Metrics: []string{"revenue"}, Dialect: "oracle"})
	if err == nil || !strings.Contains(err.Error(), "unsupported dialect") {
		t.Fatalf("want dialect error, got %v", err)
	}
}

func TestExpressionShapes(t *testing.T) {
	cases := []string{
		`expression: SUM(orders.amount)`,
		"expression:\n  - dialect: ANSI_SQL\n    expression: SUM(orders.amount)",
		"expression:\n  dialects:\n    - dialect: ANSI_SQL\n      expression: SUM(orders.amount)",
	}
	for _, src := range cases {
		var m struct {
			Expression Expressions `yaml:"expression"`
		}
		if err := yaml.Unmarshal([]byte(src), &m); err != nil {
			t.Errorf("parse %q: %v", src, err)
			continue
		}
		expr, ok := m.Expression.ForDialect("ANSI_SQL")
		if !ok || expr != "SUM(orders.amount)" {
			t.Errorf("parse %q: got %q ok=%v", src, expr, ok)
		}
	}
}
