package apiclient

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/na0fu3y/ochakai/internal/compiler"
	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/service"
)

// The apiclient compile types deliberately mirror api/openapi.yaml
// instead of importing the server's types (which drag in the store and
// embedding dependency trees). This test pins the two shapes together so
// they cannot drift apart silently. Importing service here is fine:
// test files don't ship in the binary.

func TestCompileRequestMatchesServerWire(t *testing.T) {
	req := CompileRequest{
		Model:      "sales_analytics",
		Metrics:    []string{"revenue"},
		Dimensions: []string{"orders.region"},
		Filters: []Filter{
			{Field: "orders.status", Op: "=", Value: "shipped"},
			{Field: "orders.region", Op: "in", Value: []any{"tokyo", "osaka"}},
		},
		TimeGrain: &TimeGrain{Field: "orders.created_at", Grain: "month"},
		Dialect:   "bigquery",
		Limit:     100,
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var got service.CompileRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("server cannot decode the client request: %v", err)
	}
	want := service.CompileRequest{Model: "sales_analytics", Request: compiler.Request{
		Metrics:    []string{"revenue"},
		Dimensions: []string{"orders.region"},
		Filters: []compiler.Filter{
			{Field: "orders.status", Op: "=", Value: "shipped"},
			{Field: "orders.region", Op: "in", Value: []any{"tokyo", "osaka"}},
		},
		TimeGrain: &compiler.TimeGrain{Field: "orders.created_at", Grain: "month"},
		Dialect:   "bigquery",
		Limit:     100,
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("server decoded:\n%+v\nwant:\n%+v", got, want)
	}
}

func TestCompileResultMatchesServerWire(t *testing.T) {
	server := service.CompileResult{
		Result: compiler.Result{
			SQL:          "SELECT 1",
			Dialect:      compiler.DialectBigQuery,
			DatasetsUsed: []string{"orders"},
			Notes:        []string{"a note"},
		},
		VerifiedQueries: []domain.SearchHit{
			{Knowledge: domain.Knowledge{Type: domain.TypeQuery, ID: "q1", Title: "Q"}, Score: 0.7},
		},
	}
	data, err := json.Marshal(server)
	if err != nil {
		t.Fatal(err)
	}
	var got CompileResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("client cannot decode the server response: %v", err)
	}
	if got.SQL != "SELECT 1" || got.Dialect != string(compiler.DialectBigQuery) ||
		!reflect.DeepEqual(got.DatasetsUsed, []string{"orders"}) ||
		!reflect.DeepEqual(got.Notes, []string{"a note"}) ||
		len(got.VerifiedQueries) != 1 || got.VerifiedQueries[0].ID != "q1" {
		t.Errorf("client decoded: %+v", got)
	}
}

func TestContextResultMatchesServerWire(t *testing.T) {
	server := service.ContextResult{
		Hits: []domain.SearchHit{
			{Knowledge: domain.Knowledge{Type: domain.TypeMetric, ID: "revenue", Title: "Revenue"}, Score: 0.9},
		},
		Entries: []domain.Knowledge{
			{Type: domain.TypeInsight, ID: "revenue-seasonality", Title: "Seasonality", Body: "Q4 peaks."},
		},
	}
	data, err := json.Marshal(server)
	if err != nil {
		t.Fatal(err)
	}
	var got ContextResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("client cannot decode the server response: %v", err)
	}
	if len(got.Hits) != 1 || got.Hits[0].ID != "revenue" ||
		len(got.Entries) != 1 || got.Entries[0].Body != "Q4 peaks." {
		t.Errorf("client decoded: %+v", got)
	}
}
