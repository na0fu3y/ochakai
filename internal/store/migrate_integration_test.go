package store

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// TestMigration0010LegacyData verifies the knowledge-catalog alignment
// migration (design doc 0016) against pre-0016 shaped rows: singular type
// slugs, attrs.resource / attrs.source, and singular link targets. The
// schema is already migrated by the time the test runs, so it seeds
// legacy-shaped rows and re-executes the 0010 SQL — every statement in it
// is a no-op on already-migrated data, which this double-application also
// proves.
func TestMigration0010LegacyData(t *testing.T) {
	dbURL := os.Getenv("OCHAKAI_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("OCHAKAI_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	// Registered before clear below, so LIFO cleanup deletes the fixtures
	// while the pool is still open.
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	// The database is shared with the other integration tests: clear
	// fixtures both before (leftovers from an aborted run) and after
	// (so mig-% rows don't leak into their search results).
	clear := func() {
		for _, table := range []string{"knowledge", "knowledge_revision"} {
			if _, err := s.pool.Exec(ctx, `DELETE FROM `+table+` WHERE id LIKE 'mig-%'`); err != nil {
				t.Fatal(err)
			}
		}
		for _, table := range []string{"knowledge_event", "knowledge_usage"} {
			if _, err := s.pool.Exec(ctx, `DELETE FROM `+table+` WHERE knowledge_id LIKE 'mig-%'`); err != nil {
				t.Fatal(err)
			}
		}
	}
	clear()
	t.Cleanup(clear)

	// Seed rows the way a pre-0016 server wrote them.
	seed := func(query string, args ...any) {
		t.Helper()
		if _, err := s.pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	seed(`INSERT INTO knowledge (type, id, title, created_by_kind, created_by_name, links, attrs)
	      VALUES ('metric', 'mig-revenue', '売上', 'human', 't',
	              '[{"rel":"about","target":"query/mig-q"},{"rel":"cites","target":"ochakai://table/mig-orders"}]',
	              '{"resource":"https://example.com/rev","keep":1}')`)
	seed(`INSERT INTO knowledge (type, id, title, created_by_kind, created_by_name, links, attrs)
	      VALUES ('table', 'mig-orders', 'orders', 'human', 't', '[]',
	              '{"source":"proj.shop.orders","model":"m"}')`)
	seed(`INSERT INTO knowledge_revision (type, id, rev, change, changed_by_kind, changed_by_name, snapshot)
	      VALUES ('metric', 'mig-revenue', 1, 'create', 'human', 't',
	              '{"type":"metric","id":"mig-revenue","links":[{"rel":"about","target":"query/mig-q"}]}')`)
	seed(`INSERT INTO knowledge_event (knowledge_type, knowledge_id, event, actor_kind, actor_name)
	      VALUES ('query', 'mig-q', 'fetched', 'human', 't')`)
	seed(`INSERT INTO knowledge_usage (knowledge_type, knowledge_id, event, count, last_at)
	      VALUES ('query', 'mig-q', 'fetched', 3, now())`)

	sql, err := migrationFS.ReadFile("migrations/0010_catalog_alignment.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, string(sql[jumpPastAlter(sql):])); err != nil {
		t.Fatal(err)
	}

	k, err := s.Get(ctx, domain.TypeMetrics, "mig-revenue")
	if err != nil {
		t.Fatalf("metric row not renamed to metrics: %v", err)
	}
	if k.Resource != "https://example.com/rev" {
		t.Errorf("resource = %q, want moved from attrs", k.Resource)
	}
	if _, ok := k.Attrs["resource"]; ok || k.Attrs["keep"] != float64(1) {
		t.Errorf("attrs = %v, want resource removed and keep intact", k.Attrs)
	}
	wantLinks := []domain.Link{
		{Rel: "about", Target: "queries/mig-q"},
		{Rel: "cites", Target: "ochakai://tables/mig-orders"},
	}
	if len(k.Links) != 2 || k.Links[0] != wantLinks[0] || k.Links[1] != wantLinks[1] {
		t.Errorf("links = %v, want %v", k.Links, wantLinks)
	}

	tbl, err := s.Get(ctx, domain.TypeTables, "mig-orders")
	if err != nil {
		t.Fatalf("table row not renamed to tables: %v", err)
	}
	if tbl.Resource != "https://bigquery.googleapis.com/v2/projects/proj/datasets/shop/tables/orders" ||
		tbl.Attrs["model"] != "m" {
		t.Errorf("table resource = %q attrs = %v, want source moved and normalized to the BigQuery URL", tbl.Resource, tbl.Attrs)
	}
	if _, ok := tbl.Attrs["source"]; ok {
		t.Errorf("attrs.source not removed: %v", tbl.Attrs)
	}

	var snapshot []byte
	if err := s.pool.QueryRow(ctx,
		`SELECT snapshot FROM knowledge_revision WHERE type='metrics' AND id='mig-revenue'`).Scan(&snapshot); err != nil {
		t.Fatalf("revision row not renamed: %v", err)
	}
	var snap struct {
		Type  string        `json:"type"`
		Links []domain.Link `json:"links"`
	}
	if err := json.Unmarshal(snapshot, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Type != "metrics" || len(snap.Links) != 1 || snap.Links[0].Target != "queries/mig-q" {
		t.Errorf("snapshot not rewritten: %s", snapshot)
	}

	var usageType string
	if err := s.pool.QueryRow(ctx,
		`SELECT knowledge_type FROM knowledge_usage WHERE knowledge_id='mig-q'`).Scan(&usageType); err != nil {
		t.Fatal(err)
	}
	var eventType string
	if err := s.pool.QueryRow(ctx,
		`SELECT knowledge_type FROM knowledge_event WHERE knowledge_id='mig-q'`).Scan(&eventType); err != nil {
		t.Fatal(err)
	}
	if usageType != "queries" || eventType != "queries" {
		t.Errorf("usage/event type = %q/%q, want queries", usageType, eventType)
	}
}

// jumpPastAlter returns the offset just past the ALTER TABLE ... resource
// statement, which cannot re-run once the column exists. Everything after
// it is re-executable.
func jumpPastAlter(sql []byte) int {
	const marker = "ALTER TABLE knowledge ADD COLUMN resource text NOT NULL DEFAULT '';"
	i := bytes.Index(sql, []byte(marker))
	if i < 0 {
		return 0
	}
	return i + len(marker)
}
