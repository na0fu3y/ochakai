package store

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// TestMigrationLegacyData verifies the 0010 → 0011 migration chain
// (knowledge-catalog alignment, then path addressing) against rows shaped
// the way a pre-0010 server wrote them: singular type slugs, composite
// (type, id) keys, attrs.resource / attrs.source, and "<type>/<id>" link
// targets. The shared test database is already migrated, so the legacy
// schema is rebuilt in a scratch Postgres schema inside one rolled-back
// transaction — the migration files run there verbatim, in order.
func TestMigrationLegacyData(t *testing.T) {
	dbURL := os.Getenv("OCHAKAI_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("OCHAKAI_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback is the cleanup

	// A scratch schema, alone on the search path, catches every
	// unqualified table reference in the migration files.
	for _, q := range []string{
		`CREATE SCHEMA mig_scratch`,
		`SET LOCAL search_path TO mig_scratch`,
		// The legacy shapes, reduced to the columns the migrations touch.
		// Constraint names match the originals (knowledge_pkey etc.):
		// they are per-table, so the scratch copies don't collide with
		// the migrated tables in public.
		`CREATE TABLE knowledge (
			type text NOT NULL, id text NOT NULL, title text NOT NULL,
			created_by_kind text NOT NULL, created_by_name text NOT NULL,
			links jsonb NOT NULL DEFAULT '[]', attrs jsonb NOT NULL DEFAULT '{}',
			deleted_at timestamptz,
			PRIMARY KEY (type, id))`,
		`CREATE TABLE knowledge_revision (
			type text NOT NULL, id text NOT NULL, rev integer NOT NULL,
			change text NOT NULL, changed_by_kind text NOT NULL, changed_by_name text NOT NULL,
			snapshot jsonb NOT NULL,
			PRIMARY KEY (type, id, rev))`,
		`CREATE TABLE knowledge_event (
			knowledge_type text NOT NULL, knowledge_id text NOT NULL, event text NOT NULL,
			actor_kind text NOT NULL, actor_name text NOT NULL,
			at timestamptz NOT NULL DEFAULT now())`,
		`CREATE INDEX knowledge_event_target ON knowledge_event (knowledge_type, knowledge_id, at)`,
		`CREATE TABLE knowledge_usage (
			knowledge_type text NOT NULL, knowledge_id text NOT NULL, event text NOT NULL,
			count bigint NOT NULL DEFAULT 0, last_at timestamptz NOT NULL,
			PRIMARY KEY (knowledge_type, knowledge_id, event))`,
		`CREATE TABLE attachment (
			knowledge_type text NOT NULL, knowledge_id text NOT NULL, name text NOT NULL,
			sha256 text NOT NULL, okf_path text NOT NULL DEFAULT '',
			created_by_kind text NOT NULL, created_by_name text NOT NULL,
			created_at timestamptz NOT NULL DEFAULT now(),
			PRIMARY KEY (knowledge_type, knowledge_id, name))`,
		// Seed rows the way a pre-0010 server wrote them.
		`INSERT INTO knowledge (type, id, title, created_by_kind, created_by_name, links, attrs) VALUES
			('metric', 'mig-revenue', '売上', 'human', 't',
			 '[{"rel":"about","target":"query/mig-q"},{"rel":"cites","target":"ochakai://table/mig-orders"}]',
			 '{"resource":"https://example.com/rev","keep":1}'),
			('table', 'mig-orders', 'orders', 'human', 't', '[]',
			 '{"source":"proj.shop.orders","model":"m"}'),
			('table', 'mig-revenue', '同名の別タイプ', 'human', 't', '[]', '{}')`,
		`INSERT INTO knowledge_revision (type, id, rev, change, changed_by_kind, changed_by_name, snapshot) VALUES
			('metric', 'mig-revenue', 1, 'create', 'human', 't',
			 '{"type":"metric","id":"mig-revenue","links":[{"rel":"about","target":"query/mig-q"}]}')`,
		`INSERT INTO knowledge_event (knowledge_type, knowledge_id, event, actor_kind, actor_name)
			VALUES ('query', 'mig-q', 'fetched', 'human', 't')`,
		`INSERT INTO knowledge_usage (knowledge_type, knowledge_id, event, count, last_at)
			VALUES ('query', 'mig-q', 'fetched', 3, now())`,
		`INSERT INTO attachment (knowledge_type, knowledge_id, name, sha256, created_by_kind, created_by_name)
			VALUES ('metric', 'mig-revenue', 'chart.png', 'abc', 'human', 't')`,
	} {
		if _, err := tx.Exec(ctx, q); err != nil {
			t.Fatalf("scratch setup: %v\n%s", err, q)
		}
	}

	for _, name := range []string{"0010_catalog_alignment.sql", "0011_path_addressing.sql"} {
		sql, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}

	// The metric row: plural slug, path id, resource moved out of attrs,
	// link targets rewritten to the new ids.
	var typ, resource string
	var links, attrs []byte
	if err := tx.QueryRow(ctx,
		`SELECT type, resource, links, attrs FROM knowledge WHERE id = 'metrics/mig-revenue'`).
		Scan(&typ, &resource, &links, &attrs); err != nil {
		t.Fatalf("metric row not at metrics/mig-revenue: %v", err)
	}
	if typ != "metrics" || resource != "https://example.com/rev" {
		t.Errorf("type = %q resource = %q", typ, resource)
	}
	var lk []domain.Link
	if err := json.Unmarshal(links, &lk); err != nil {
		t.Fatal(err)
	}
	wantLinks := []domain.Link{
		{Rel: "about", Target: "queries/mig-q"},
		{Rel: "cites", Target: "ochakai://tables/mig-orders"},
	}
	if len(lk) != 2 || lk[0] != wantLinks[0] || lk[1] != wantLinks[1] {
		t.Errorf("links = %v, want %v", lk, wantLinks)
	}
	var am map[string]any
	if err := json.Unmarshal(attrs, &am); err != nil {
		t.Fatal(err)
	}
	if _, ok := am["resource"]; ok || am["keep"] != float64(1) {
		t.Errorf("attrs = %v, want resource removed and keep intact", am)
	}

	// The table row: source normalized to the canonical BigQuery URL —
	// and the id that collided with the metric across types survives
	// under its own path.
	if err := tx.QueryRow(ctx,
		`SELECT resource FROM knowledge WHERE id = 'tables/mig-orders'`).Scan(&resource); err != nil {
		t.Fatalf("table row not at tables/mig-orders: %v", err)
	}
	if resource != "https://bigquery.googleapis.com/v2/projects/proj/datasets/shop/tables/orders" {
		t.Errorf("table resource = %q, want the canonical BigQuery URL", resource)
	}
	var n int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM knowledge WHERE id IN ('metrics/mig-revenue', 'tables/mig-revenue')`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("same-id-different-type rows = %d, want both to survive under distinct paths", n)
	}

	// Revision snapshot: type renamed, id rewritten to the path, links
	// rewritten — restoring or auditing works in the post-rename world.
	var snapshot []byte
	if err := tx.QueryRow(ctx,
		`SELECT snapshot FROM knowledge_revision WHERE id = 'metrics/mig-revenue'`).Scan(&snapshot); err != nil {
		t.Fatalf("revision row not rekeyed: %v", err)
	}
	var snap struct {
		Type  string        `json:"type"`
		ID    string        `json:"id"`
		Links []domain.Link `json:"links"`
	}
	if err := json.Unmarshal(snapshot, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Type != "metrics" || snap.ID != "metrics/mig-revenue" ||
		len(snap.Links) != 1 || snap.Links[0].Target != "queries/mig-q" {
		t.Errorf("snapshot not rewritten: %s", snapshot)
	}

	// Satellite tables: rekeyed by the path id, type columns gone.
	for _, q := range []string{
		`SELECT count(*) FROM knowledge_event WHERE knowledge_id = 'queries/mig-q'`,
		`SELECT count(*) FROM knowledge_usage WHERE knowledge_id = 'queries/mig-q'`,
		`SELECT count(*) FROM attachment WHERE knowledge_id = 'metrics/mig-revenue' AND name = 'chart.png'`,
	} {
		if err := tx.QueryRow(ctx, q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("%s = %d, want 1", q, n)
		}
	}
}
