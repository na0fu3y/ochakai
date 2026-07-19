package importer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/service"
	"github.com/na0fu3y/ochakai/internal/store"
)

// TestImportOssieIntegration exercises the whole ossie import against a
// real PostgreSQL (skipped unless OCHAKAI_TEST_DATABASE_URL is set; see
// the store integration test for the docker one-liner): the model is
// stored for compile, metrics/table entries are derived, re-import
// refreshes definitions without clobbering human curation, and an
// unchanged re-import reports unchanged instead of writing revisions.
func TestImportOssieIntegration(t *testing.T) {
	dbURL := os.Getenv("OCHAKAI_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("OCHAKAI_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	s, err := store.New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	svc := &service.Service{Store: s, Log: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	actor := domain.Actor{Kind: "agent", Name: "importer-test"}

	// Run-unique names keep reruns against a shared test DB conflict-free.
	uid := fmt.Sprintf("ossit%d", time.Now().UnixNano())
	modelName, metricName, dsName := uid+"_model", uid+"_revenue", uid+"_orders"
	yamlSrc := []byte(fmt.Sprintf(`
name: %s
datasets:
  - name: %s
    source: myproject.shop.orders
    description: One row per order.
    fields:
      - name: amount
        expression: total_price
metrics:
  - name: %s
    description: Total revenue.
    expression: SUM(%s.amount)
`, modelName, dsName, metricName, dsName))

	report, err := ImportOssie(ctx, svc, yamlSrc, actor)
	if err != nil {
		t.Fatalf("ImportOssie: %v", err)
	}
	if len(report.Models) != 1 || report.Models[0] != modelName {
		t.Errorf("models = %v, want [%s]", report.Models, modelName)
	}
	if len(report.Created) != 2 {
		t.Errorf("created = %v, want the metric and table entries", report.Created)
	}

	metric, err := svc.Get(ctx, domain.TypeMetrics, metricName)
	if err != nil {
		t.Fatalf("derived metric entry missing: %v", err)
	}
	if metric.Attrs["model"] != modelName {
		t.Errorf("metric attrs.model = %v, want %s", metric.Attrs["model"], modelName)
	}
	table, err := svc.Get(ctx, domain.TypeTables, dsName)
	if err != nil {
		t.Fatalf("derived table entry missing: %v", err)
	}
	if table.Resource != "https://bigquery.googleapis.com/v2/projects/myproject/datasets/shop/tables/orders" {
		t.Errorf("table resource = %q, want the canonical BigQuery URL", table.Resource)
	}

	// A byte-identical re-import writes nothing.
	again, err := ImportOssie(ctx, svc, yamlSrc, actor)
	if err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if len(again.Created) != 0 || len(again.Updated) != 0 || len(again.Unchanged) != 2 {
		t.Errorf("identical re-import: created=%v updated=%v unchanged=%v",
			again.Created, again.Updated, again.Unchanged)
	}

	// Human curation survives a definition refresh: verify the metric,
	// tag it, give it a body — then re-import a changed description.
	metric.Status = domain.StatusVerified
	metric.Tags = []string{"curated"}
	metric.Body = "human-written caveats"
	if _, _, err := svc.Update(ctx, metric, domain.Actor{Kind: "human", Name: "curator"}); err != nil {
		t.Fatal(err)
	}
	refreshed, err := ImportOssie(ctx, svc,
		[]byte(strings.Replace(string(yamlSrc), "Total revenue.", "Total revenue, refreshed.", 1)), actor)
	if err != nil {
		t.Fatalf("refresh import: %v", err)
	}
	if len(refreshed.Updated) != 1 || !strings.Contains(refreshed.Updated[0], metricName) {
		t.Errorf("refresh: updated = %v, want the metric entry", refreshed.Updated)
	}
	metric, err = svc.Get(ctx, domain.TypeMetrics, metricName)
	if err != nil {
		t.Fatal(err)
	}
	if metric.Description != "Total revenue, refreshed." {
		t.Errorf("description not refreshed: %q", metric.Description)
	}
	if metric.Status != domain.StatusVerified || len(metric.Tags) != 1 || metric.Body != "human-written caveats" {
		t.Errorf("human curation clobbered: status=%s tags=%v body=%q",
			metric.Status, metric.Tags, metric.Body)
	}

	// Garbage YAML is the client's mistake, not a crash.
	var invalid *service.InvalidInputError
	if _, err := ImportOssie(ctx, svc, []byte(": not yaml"), actor); !errors.As(err, &invalid) {
		t.Errorf("garbage YAML: want InvalidInputError, got %T %v", err, err)
	}
}
