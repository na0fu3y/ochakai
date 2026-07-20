package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

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
	// The legacy {rel, target} shape, which is what 0010/0011 operate on —
	// links only lose rel in 0015 (design doc 0024), which runs later and
	// is not part of this chain.
	type legacyLink struct {
		Rel    string `json:"rel"`
		Target string `json:"target"`
	}
	var lk []legacyLink
	if err := json.Unmarshal(links, &lk); err != nil {
		t.Fatal(err)
	}
	wantLinks := []legacyLink{
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

// TestMigrationSemanticModelEntries verifies 0012 against rows shaped the
// way a pre-0012 server wrote them: a semantic_model table row, a metric
// entry whose attrs.model is the bare model name, and a table entry whose
// defined_in link targets 'model/<name>'. Same scratch-schema technique
// as TestMigrationLegacyData, on the post-0011 shape.
func TestMigrationSemanticModelEntries(t *testing.T) {
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

	for _, q := range []string{
		`CREATE SCHEMA mig12_scratch`,
		`SET LOCAL search_path TO mig12_scratch`,
		// The post-0011 shapes, reduced to the columns 0012 touches.
		`CREATE TABLE knowledge (
			type text NOT NULL, id text NOT NULL PRIMARY KEY, title text NOT NULL,
			description text NOT NULL DEFAULT '',
			status text NOT NULL DEFAULT 'draft', status_note text NOT NULL DEFAULT '',
			created_by_kind text NOT NULL, created_by_name text NOT NULL,
			links jsonb NOT NULL DEFAULT '[]', attrs jsonb NOT NULL DEFAULT '{}',
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now())`,
		`CREATE TABLE knowledge_revision (
			id text NOT NULL, rev integer NOT NULL, change text NOT NULL,
			changed_by_kind text NOT NULL, changed_by_name text NOT NULL,
			snapshot jsonb NOT NULL, changed_at timestamptz NOT NULL DEFAULT now(),
			PRIMARY KEY (id, rev))`,
		`CREATE TABLE semantic_model (
			name text NOT NULL PRIMARY KEY, spec jsonb NOT NULL,
			updated_at timestamptz NOT NULL DEFAULT now())`,
		`INSERT INTO semantic_model (name, spec) VALUES
			('sales', '{"name":"sales","description":"Sales star schema","datasets":[{"name":"orders"}]}')`,
		`INSERT INTO knowledge (type, id, title, created_by_kind, created_by_name, links, attrs) VALUES
			('metrics', 'metrics/revenue', '売上', 'human', 't', '[]',
			 '{"model":"sales","expression":"SUM(orders.amount)"}'),
			('tables', 'tables/orders', 'orders', 'human', 't',
			 '[{"rel":"defined_in","target":"model/sales"}]', '{"model":"sales"}')`,
		`INSERT INTO knowledge_revision (id, rev, change, changed_by_kind, changed_by_name, snapshot) VALUES
			('metrics/revenue', 1, 'create', 'human', 't',
			 '{"type":"metrics","id":"metrics/revenue","attrs":{"model":"sales"}}'),
			('tables/orders', 1, 'create', 'human', 't',
			 '{"type":"tables","id":"tables/orders","links":[{"rel":"defined_in","target":"model/sales"}]}')`,
	} {
		if _, err := tx.Exec(ctx, q); err != nil {
			t.Fatalf("scratch setup: %v\n%s", err, q)
		}
	}

	sql, err := migrationFS.ReadFile("migrations/0012_semantic_model_entries.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(sql)); err != nil {
		t.Fatalf("apply 0012: %v", err)
	}

	// The model row: a draft models entry with the spec verbatim in
	// attrs.spec and migration provenance.
	var typ, title, desc, status, createdBy string
	var attrs []byte
	if err := tx.QueryRow(ctx,
		`SELECT type, title, description, status, created_by_name, attrs
		   FROM knowledge WHERE id = 'models/sales'`).
		Scan(&typ, &title, &desc, &status, &createdBy, &attrs); err != nil {
		t.Fatalf("model row not at models/sales: %v", err)
	}
	if typ != "models" || title != "sales" || desc != "Sales star schema" ||
		status != "draft" || createdBy != "migration-0012" {
		t.Errorf("model row = %s %s %q %s %s", typ, title, desc, status, createdBy)
	}
	var am map[string]any
	if err := json.Unmarshal(attrs, &am); err != nil {
		t.Fatal(err)
	}
	spec, _ := am["spec"].(map[string]any)
	if spec == nil || spec["name"] != "sales" {
		t.Errorf("attrs.spec not verbatim: %s", attrs)
	}

	// Its create revision reads back as a consistent snapshot.
	var snapshot []byte
	if err := tx.QueryRow(ctx,
		`SELECT snapshot FROM knowledge_revision WHERE id = 'models/sales' AND rev = 1`).
		Scan(&snapshot); err != nil {
		t.Fatalf("model create revision missing: %v", err)
	}
	var snap domain.Knowledge
	if err := json.Unmarshal(snapshot, &snap); err != nil {
		t.Fatalf("model snapshot does not read as knowledge: %v\n%s", err, snapshot)
	}
	if snap.Type != "models" || snap.ID != "models/sales" || snap.CreatedBy.Name != "migration-0012" {
		t.Errorf("model snapshot = %s", snapshot)
	}

	// attrs.model became the models entry id, in the live row and the
	// revision snapshot.
	for _, q := range []string{
		`SELECT attrs->>'model' FROM knowledge WHERE id = 'metrics/revenue'`,
		`SELECT snapshot->'attrs'->>'model' FROM knowledge_revision WHERE id = 'metrics/revenue'`,
	} {
		var model string
		if err := tx.QueryRow(ctx, q).Scan(&model); err != nil {
			t.Fatal(err)
		}
		if model != "models/sales" {
			t.Errorf("%s = %q, want models/sales", q, model)
		}
	}

	// The defined_in link target gained its real entry, live and in history.
	for _, q := range []string{
		`SELECT links->0->>'target' FROM knowledge WHERE id = 'tables/orders'`,
		`SELECT snapshot->'links'->0->>'target' FROM knowledge_revision WHERE id = 'tables/orders'`,
	} {
		var target string
		if err := tx.QueryRow(ctx, q).Scan(&target); err != nil {
			t.Fatal(err)
		}
		if target != "models/sales" {
			t.Errorf("%s = %q, want models/sales", q, target)
		}
	}

	var gone *string
	if err := tx.QueryRow(ctx,
		`SELECT to_regclass('mig12_scratch.semantic_model')::text`).Scan(&gone); err != nil {
		t.Fatal(err)
	}
	if gone != nil {
		t.Errorf("semantic_model table still exists as %s", *gone)
	}
}

// TestMigrateConcurrent reproduces the boot race: several processes
// (server instances starting at once, or parallel test binaries sharing
// a database) calling Migrate against the same unmigrated database.
// Without the advisory lock, two of them can both see a migration as
// unapplied and both apply it — the second failing (or rewriting data
// twice) against the schema the first already changed.
//
// The scratch-transaction technique above cannot express this (the race
// needs separate sessions), so it uses a run-unique schema instead. Its
// search_path keeps public second: the pg_trgm operator class stays
// resolvable while every unqualified table reference binds to the
// scoped schema first.
func TestMigrateConcurrent(t *testing.T) {
	dbURL := os.Getenv("OCHAKAI_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("OCHAKAI_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()

	admin, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	// Ensure the extension lives in public before any scoped Migrate
	// runs 0001's CREATE EXTENSION — on a fresh database it would
	// otherwise install into the scoped schema and vanish with the
	// cleanup below, breaking later runs in public.
	if _, err := admin.pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pg_trgm`); err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("migrate_race_%d", time.Now().UnixNano())
	if _, err := admin.pool.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.pool.Exec(context.Background(), `DROP SCHEMA `+schema+` CASCADE`); err != nil {
			t.Errorf("drop schema %s: %v", schema, err)
		}
	}()

	sep := "?"
	if strings.Contains(dbURL, "?") {
		sep = "&"
	}
	scopedURL := dbURL + sep + "options=-csearch_path%3D" + schema + ",public"

	// knowledge_embedding is created outside versioned migrations (only
	// when semantic search is configured), and 0010/0011 probe for it
	// with an unqualified to_regclass that would otherwise leak through
	// search_path to public's already-migrated table. Pre-create it in
	// the scoped schema in its pre-0010 shape — which also makes those
	// migration branches run deterministically here.
	scoped, err := New(ctx, scopedURL, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(scoped.Close)
	if _, err := scoped.pool.Exec(ctx, `CREATE TABLE knowledge_embedding (
		type text NOT NULL, id text NOT NULL, model text, PRIMARY KEY (type, id))`); err != nil {
		t.Fatal(err)
	}

	const racers = 4
	var wg sync.WaitGroup
	errs := make([]error, racers)
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := New(ctx, scopedURL, false)
			if err != nil {
				errs[i] = fmt.Errorf("dial: %w", err)
				return
			}
			defer s.Close()
			errs[i] = s.Migrate(ctx, 0)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent Migrate %d: %v", i, err)
		}
	}

	// The winner applied every migration exactly once; the rest saw them
	// as applied and did nothing. Either way the scoped schema must end
	// migrated: spot-check a table the chain creates and reshapes.
	var n int
	if err := scoped.pool.QueryRow(ctx,
		`SELECT count(*) FROM information_schema.tables
		  WHERE table_schema = $1 AND table_name = 'knowledge'`, schema).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("knowledge table in %s: got %d, want 1", schema, n)
	}
}

// TestMigrationLinksIntoBody covers 0015 (design doc 0024): links used to
// be authored as a field, so they exist independently of the prose. The
// migration writes them back into the body — the old rel becoming the
// anchor text — before the column turns into something derived from the
// body, which is the only way those edges survive the next write.
func TestMigrationLinksIntoBody(t *testing.T) {
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

	for _, q := range []string{
		`CREATE SCHEMA links_scratch`,
		`SET LOCAL search_path TO links_scratch`,
		`CREATE TABLE knowledge (
			id text PRIMARY KEY, body text NOT NULL DEFAULT '',
			links jsonb NOT NULL DEFAULT '[]',
			updated_at timestamptz NOT NULL DEFAULT now(),
			deleted_at timestamptz)`,
		`INSERT INTO knowledge (id, body, links) VALUES
			('insights/a', 'Existing prose.',
			 '[{"rel":"about","target":"metrics/revenue"},{"rel":"cites","target":"ochakai://tables/orders"}]'),
			('insights/empty-body', '',
			 '[{"rel":"","target":"metrics/revenue"}]'),
			('insights/no-links', 'Just prose.', '[]'),
			('insights/deleted', 'Gone.', '[{"rel":"about","target":"metrics/revenue"}]')`,
		`UPDATE knowledge SET deleted_at = now() WHERE id = 'insights/deleted'`,
	} {
		if _, err := tx.Exec(ctx, q); err != nil {
			t.Fatalf("scratch setup: %v\n%s", err, q)
		}
	}

	sql, err := migrationFS.ReadFile("migrations/0015_links_from_body.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(sql)); err != nil {
		t.Fatalf("apply 0015: %v", err)
	}

	for _, tc := range []struct {
		id, wantBody string
		wantLinks    []domain.Link
	}{
		{
			id: "insights/a",
			wantBody: "Existing prose.\n\n# Links\n\n" +
				"- [about](/metrics/revenue.md)\n- [cites](/tables/orders.md)",
			wantLinks: []domain.Link{
				{Target: "metrics/revenue", Text: "about"},
				{Target: "tables/orders", Text: "cites"},
			},
		}, {
			// An empty rel falls back to the target's last segment, so no
			// link renders as "[]()".
			id:        "insights/empty-body",
			wantBody:  "# Links\n\n- [revenue](/metrics/revenue.md)",
			wantLinks: []domain.Link{{Target: "metrics/revenue", Text: "revenue"}},
		}, {
			id:        "insights/no-links",
			wantBody:  "Just prose.",
			wantLinks: []domain.Link{},
		}, {
			// Soft-deleted entries keep the shape they were deleted in.
			id:        "insights/deleted",
			wantBody:  "Gone.",
			wantLinks: []domain.Link{{Target: "metrics/revenue"}},
		},
	} {
		var body string
		var raw []byte
		if err := tx.QueryRow(ctx,
			`SELECT body, links FROM knowledge WHERE id = $1`, tc.id).Scan(&body, &raw); err != nil {
			t.Fatalf("%s: %v", tc.id, err)
		}
		if body != tc.wantBody {
			t.Errorf("%s body =\n%q\nwant\n%q", tc.id, body, tc.wantBody)
		}
		if tc.id == "insights/deleted" {
			continue // its links keep the legacy {rel, target} shape
		}
		got := []domain.Link{}
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		sortLinks(got)
		want := append([]domain.Link{}, tc.wantLinks...)
		sortLinks(want)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s links = %v, want %v", tc.id, got, want)
		}
		// The written-back body must derive exactly the links stored.
		derived := domain.LinksFromBody(tc.id, body)
		if derived == nil {
			derived = []domain.Link{}
		}
		sortLinks(derived)
		if !reflect.DeepEqual(derived, want) {
			t.Errorf("%s: body derives %v, but the column holds %v", tc.id, derived, want)
		}
	}
}

func sortLinks(l []domain.Link) {
	sort.Slice(l, func(i, j int) bool {
		if l[i].Target != l[j].Target {
			return l[i].Target < l[j].Target
		}
		return l[i].Text < l[j].Text
	})
}
