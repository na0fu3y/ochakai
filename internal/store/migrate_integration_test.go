package store

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/testdb"
)

// TestMigrationLegacyData verifies the 0010 → 0011 migration chain
// (knowledge-catalog alignment, then path addressing) against rows shaped
// the way a pre-0010 server wrote them: singular type slugs, composite
// (type, id) keys, attrs.resource / attrs.source, and "<type>/<id>" link
// targets. The shared test database is already migrated, so the legacy
// schema is rebuilt in a scratch Postgres schema inside one rolled-back
// transaction — the migration files run there verbatim, in order.
func TestMigrationLegacyData(t *testing.T) {
	dbURL := testdb.URL(t)
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
	dbURL := testdb.URL(t)
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
	dbURL := testdb.URL(t)
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
	schema := testdb.Unique(t, "migrate_race_")
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
		  WHERE table_schema = $1 AND table_name = 'object'`, schema).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("object table in %s: got %d, want 1", schema, n)
	}
}

// TestMigrationLinksIntoBody covers 0015 (design doc 0024): links used to
// be authored as a field, so they exist independently of the prose. The
// migration writes them back into the body — the old rel becoming the
// anchor text — before the column turns into something derived from the
// body, which is the only way those edges survive the next write.
func TestMigrationLinksIntoBody(t *testing.T) {
	dbURL := testdb.URL(t)
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

// The HNSW indexes exist once semantic search is configured, and the
// searches can use them: an index built for the wrong operator class is
// simply never chosen, which looks exactly like having no index at all.
// Above pgvector's 2000-dimension indexing limit the migration must skip
// them rather than fail — the exact scan still answers correctly.
func TestIntegrationVectorIndexes(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 4); err != nil {
		t.Fatal(err)
	}

	for _, index := range []string{"knowledge_embedding_hnsw", "attachment_embedding_hnsw"} {
		var exists bool
		if err := s.pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM pg_class WHERE relname = $1)`, index).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Errorf("%s was not created", index)
		}
	}

	// The planner must be able to reach it through the `<=>` ordering the
	// searches use. enable_seqscan=off is a cost penalty, not a
	// prohibition, so a wrong operator class would still show a scan.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET LOCAL enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.Query(ctx, `EXPLAIN SELECT id FROM knowledge_embedding
		ORDER BY embedding <=> $1::vector LIMIT 5`, encodeVector([]float32{1, 0, 0, 0}))
	if err != nil {
		t.Fatalf("EXPLAIN: %v", err)
	}
	plan := ""
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan += line + "\n"
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan, "knowledge_embedding_hnsw") {
		t.Errorf("vector search cannot use the HNSW index; plan was:\n%s", plan)
	}

	// A dimension above pgvector's indexing limit skips the index instead
	// of failing the migration.
	if err := s.migrateVectorIndexes(ctx, hnswMaxDim+1); err != nil {
		t.Errorf("migrateVectorIndexes above the dimension limit: %v", err)
	}
}

// Changing the embedding dimension on a base that already holds vectors
// is not something CREATE TABLE IF NOT EXISTS can absorb: it keeps the
// old column, every write then fails on the width, and every search fails
// outright because the query vector cannot be compared against it.
// Startup used to refuse and hand the operator a DROP TABLE; it performs
// the rebuild itself now, because a vector is derived and `reembed`
// refills it (design doc 0053 §3).
//
// Scoped to its own schema: this one is destructive by design, and the
// other store tests share the database.
func TestIntegrationEmbeddingDimChangeRebuilds(t *testing.T) {
	ctx := context.Background()
	s := scopedStore(ctx, t, testdb.Unique(t, "embed_dim_"))
	// The vector tables are created outside the versioned migrations, and
	// 0010/0011/0013 probe for them with an *unqualified* `to_regclass`,
	// which walks the whole search path: with none in this schema they
	// would find public's already-migrated table and try to rename a
	// `type` column that has not existed there since 0011. Pre-create the
	// pre-0010 shape here, as TestMigrateConcurrent does for the same
	// reason — with the vector column those migrations do not touch, so
	// what comes out the far end is exactly what migrateEmbedding builds
	// at dim 4 and this test can then resize.
	if _, err := s.pool.Exec(ctx, `CREATE TABLE knowledge_embedding (
		type       text NOT NULL,
		id         text NOT NULL,
		model      text NOT NULL,
		embedding  vector(4) NOT NULL,
		updated_at timestamptz NOT NULL DEFAULT now(),
		PRIMARY KEY (type, id))`); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx, 4); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO knowledge_embedding (id, model, embedding) VALUES ($1, $2, $3::vector)`,
		"queries/x", "test-model", encodeVector([]float32{1, 0, 0, 0})); err != nil {
		t.Fatal(err)
	}

	// What public holds before the resize. The rebuild drops the vector
	// tables, and it must drop *this* schema's — the store's path is
	// "<schema>,public", so a bare table name in that DROP reaches past
	// the scope this test declares (there is no attachment_embedding
	// here, and there is one in public). It took public's, and the tests
	// that share public failed afterwards, somewhere else and not every
	// time.
	publicHas := func(table string) bool {
		t.Helper()
		var exists bool
		if err := s.pool.QueryRow(ctx,
			`SELECT to_regclass('public.'||$1) IS NOT NULL`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		return exists
	}
	before := map[string]bool{
		"knowledge_embedding":  publicHas("knowledge_embedding"),
		"attachment_embedding": publicHas("attachment_embedding"),
	}

	if err := s.Migrate(ctx, 8); err != nil {
		t.Fatalf("a changed embedding dimension must rebuild rather than refuse: %v", err)
	}
	for table, had := range before {
		if had && !publicHas(table) {
			t.Errorf("the resize dropped public.%s: a scoped store rebuilt its own vector space "+
				"and took another schema's table with it", table)
		}
	}
	// The space is the new one, and empty: the old vectors were in a
	// space nothing would query, and nothing was carried into this one
	// pretending otherwise.
	var dim, rows int
	if err := s.pool.QueryRow(ctx,
		`SELECT atttypmod FROM pg_attribute
		 WHERE attrelid = to_regclass('knowledge_embedding') AND attname = 'embedding'`).Scan(&dim); err != nil {
		t.Fatal(err)
	}
	if dim != 8 {
		t.Errorf("knowledge_embedding.embedding is vector(%d), want vector(8)", dim)
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM knowledge_embedding`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Errorf("knowledge_embedding holds %d rows after the resize, want 0", rows)
	}
	// A write at the new width is what the old refusal was protecting
	// against, and it now works without an operator touching the schema.
	if err := s.UpsertEmbedding(ctx, "queries/x", "test-model",
		[]float32{1, 0, 0, 0, 0, 0, 0, 0}, false); err != nil {
		t.Errorf("writing a vector at the new dimension: %v", err)
	}
	if err := s.Migrate(ctx, 8); err != nil {
		t.Errorf("re-running at the same dimension must change nothing: %v", err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM knowledge_embedding`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Errorf("a restart at the same dimension dropped the vectors (%d rows, want 1)", rows)
	}
}

// scopedStore opens a store whose search_path is a schema of its own, so
// a test that changes the schema does not disturb the ones sharing this
// database. The schema is dropped when the test ends.
func scopedStore(ctx context.Context, t *testing.T, schema string) *Store {
	t.Helper()
	dbURL := testdb.URL(t)
	admin, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	// The extensions belong in public: a scoped CREATE EXTENSION would
	// install into the scoped schema and vanish with it, breaking later
	// runs (see TestMigrateConcurrent).
	for _, ext := range []string{"pg_trgm", "vector"} {
		if _, err := admin.pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS `+ext); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := admin.pool.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.pool.Exec(context.Background(), `DROP SCHEMA `+schema+` CASCADE`); err != nil {
			t.Errorf("drop schema %s: %v", schema, err)
		}
	})
	sep := "?"
	if strings.Contains(dbURL, "?") {
		sep = "&"
	}
	s, err := New(ctx, dbURL+sep+"options=-csearch_path%3D"+schema+",public", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// TestMigrationUpdatedByBackfill checks 0019's backfill: updated_by is the
// actor of the newest revision that changed content, and created_by only
// for entries whose revisions cannot answer. A verify revision must not
// win — confirming an entry is not producing its content, which is the
// whole distinction v0.2 draws between verified and generated (design doc
// 0036 §3.3).
func TestMigrationUpdatedByBackfill(t *testing.T) {
	dbURL := testdb.URL(t)
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
		`CREATE SCHEMA okf2_scratch`,
		`SET LOCAL search_path TO okf2_scratch`,
		`CREATE TABLE knowledge (
			id text PRIMARY KEY,
			created_by_kind text NOT NULL, created_by_name text NOT NULL,
			created_by_via text NOT NULL DEFAULT '')`,
		`CREATE TABLE knowledge_revision (
			id text NOT NULL, rev int NOT NULL, change text NOT NULL,
			changed_by_kind text NOT NULL, changed_by_name text NOT NULL,
			changed_by_via text NOT NULL DEFAULT '')`,
		`INSERT INTO knowledge (id, created_by_kind, created_by_name) VALUES
			('insights/edited', 'agent', 'claude-code'),
			('insights/verified-later', 'agent', 'claude-code'),
			('insights/delegated', 'agent', 'claude-code'),
			('insights/no-revisions', 'human', 'na0')`,
		`INSERT INTO knowledge_revision (id, rev, change, changed_by_kind, changed_by_name, changed_by_via) VALUES
			('insights/edited', 1, 'create', 'agent', 'claude-code', ''),
			('insights/edited', 2, 'update', 'human', 'tanaka', ''),
			('insights/verified-later', 1, 'create', 'agent', 'claude-code', ''),
			('insights/verified-later', 2, 'verify', 'human', 'na0', ''),
			('insights/delegated', 1, 'create', 'agent', 'claude-code', ''),
			('insights/delegated', 2, 'move', 'human', 'tanaka', 'agent:insightflow')`,
	} {
		if _, err := tx.Exec(ctx, q); err != nil {
			t.Fatalf("scratch setup: %v\n%s", err, q)
		}
	}

	sql, err := migrationFS.ReadFile("migrations/0019_okf_v0_2.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(sql)); err != nil {
		t.Fatalf("apply 0019: %v", err)
	}

	for _, tc := range []struct{ id, want string }{
		{"insights/edited", "human:tanaka"},                          // the last content change
		{"insights/verified-later", "agent:claude-code"},             // a verify is not a content change
		{"insights/delegated", "human:tanaka via agent:insightflow"}, // delegation carries too
		{"insights/no-revisions", "human:na0"},                       // falls back to created_by
	} {
		var a domain.Actor
		if err := tx.QueryRow(ctx,
			`SELECT updated_by_kind, updated_by_name, updated_by_via FROM knowledge WHERE id = $1`,
			tc.id).Scan(&a.Kind, &a.Name, &a.Via); err != nil {
			t.Fatalf("%s: %v", tc.id, err)
		}
		if a.String() != tc.want {
			t.Errorf("%s updated_by = %q, want %q", tc.id, a.String(), tc.want)
		}
	}

	// The column exists and takes a date; NULL means "nothing dates this".
	var staleAfter *time.Time
	if err := tx.QueryRow(ctx,
		`SELECT stale_after FROM knowledge WHERE id = 'insights/edited'`).Scan(&staleAfter); err != nil {
		t.Fatalf("stale_after: %v", err)
	}
	if staleAfter != nil {
		t.Errorf("stale_after = %v, want NULL for an entry that never set one", staleAfter)
	}
}

// Migration 0020 lifts OKF v0.2's provenance and Attested Computation
// families out of attrs and into their own columns (design doc 0036).
//
// Three properties matter and none of them is obvious from the SQL. The
// attr must be removed as the column is filled, or the value shows up
// twice on the wire and the next full-replacement PUT makes the duplicate
// permanent. A value the migration cannot read must be left entirely
// alone, attr included, rather than half-converted. And updated_at must
// not move: it is the optimistic-locking version and OKF's generated.at,
// so bumping it would invalidate every held ETag and misdate every entry.
func TestMigrationOKFFamiliesOutOfAttrs(t *testing.T) {
	dbURL := testdb.URL(t)
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

	// A date that went through attrs before this migration came back as a
	// full timestamp; the column keeps the day the producer wrote.
	for _, q := range []string{
		`CREATE SCHEMA okf35_scratch`,
		`SET LOCAL search_path TO okf35_scratch`,
		`CREATE TABLE knowledge (
			id text PRIMARY KEY,
			attrs jsonb NOT NULL DEFAULT '{}',
			updated_at timestamptz NOT NULL DEFAULT now())`,
		`INSERT INTO knowledge (id, attrs, updated_at) VALUES
			('finance/full', '{
				"dialect": "standard-sql",
				"sources": [
					{"id": "rev", "resource": "https://wiki.example/rev", "title": "Policy",
					 "author": "human:jsmith", "usage_count": 0,
					 "last_modified": "2026-06-15T00:00:00Z",
					 "usage_window": {"from": "2026-05-01T00:00:00Z", "to": "2026-05-31"}},
					{"id": "nameless", "title": "no resource"}
				],
				"usage_window": {"from": "2026-06-01", "to": "2026-06-30"},
				"runtime": "bigquery",
				"computation": "queries/rev.sql",
				"parameters": [{"name": "year", "type": "integer", "required": true},
				               {"type": "orphan"}],
				"executor": {"resource": "run.md", "receipt": ["job_id"]},
				"attester": {"resource": "check.py"}
			}'::jsonb, '2026-07-01T00:00:00Z'),
			('finance/malformed', '{"sources": ["just a string"], "executor": {"resource": "run.md"}}'::jsonb,
			 '2026-07-01T00:00:00Z'),
			('finance/none', '{"dialect": "standard-sql"}'::jsonb, '2026-07-01T00:00:00Z')`,
	} {
		if _, err := tx.Exec(ctx, q); err != nil {
			t.Fatalf("scratch setup: %v\n%s", err, q)
		}
	}

	sql, err := migrationFS.ReadFile("migrations/0020_okf_schema_first.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(sql)); err != nil {
		t.Fatalf("apply 0020: %v", err)
	}

	read := func(id string) (k domain.Knowledge, attrs map[string]any, updatedAt time.Time) {
		var sources, window, params, executor, attester, rawAttrs []byte
		if err := tx.QueryRow(ctx, `SELECT sources, usage_window, runtime, parameters, computation,
			executor, attester, attrs, updated_at FROM knowledge WHERE id = $1`, id).
			Scan(&sources, &window, &k.Runtime, &params, &k.Computation,
				&executor, &attester, &rawAttrs, &updatedAt); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		for _, d := range []struct {
			raw []byte
			out any
		}{{sources, &k.Sources}, {window, &k.UsageWindow}, {params, &k.Parameters},
			{executor, &k.Executor}, {attester, &k.Attester}, {rawAttrs, &attrs}} {
			if len(d.raw) == 0 {
				continue
			}
			if err := json.Unmarshal(d.raw, d.out); err != nil {
				t.Fatalf("%s: decoding %s: %v", id, d.raw, err)
			}
		}
		return k, attrs, updatedAt
	}

	k, attrs, updatedAt := read("finance/full")
	// The source that named no resource is dropped: SPEC §5.1 requires one.
	if len(k.Sources) != 1 {
		t.Fatalf("sources = %+v, want only the one naming a resource", k.Sources)
	}
	got := k.Sources[0]
	if got.LastModified != "2026-06-15" || got.UsageWindow == nil || got.UsageWindow.From != "2026-05-01" {
		t.Errorf("dates kept their clock component: %+v (%+v)", got, got.UsageWindow)
	}
	if got.UsageCount == nil || *got.UsageCount != 0 {
		t.Errorf("usage_count = %v, want a pointer to 0 — zero uses is a count", got.UsageCount)
	}
	if k.UsageWindow == nil || k.UsageWindow.To != "2026-06-30" {
		t.Errorf("usage_window = %+v", k.UsageWindow)
	}
	if k.Runtime != "bigquery" || k.Computation != "queries/rev.sql" {
		t.Errorf("runtime = %q, computation = %q", k.Runtime, k.Computation)
	}
	// The parameter missing a name is dropped rather than stored half-formed.
	want := []domain.Parameter{{Name: "year", Type: "integer", Required: true}}
	if !reflect.DeepEqual(k.Parameters, want) {
		t.Errorf("parameters = %+v, want %+v", k.Parameters, want)
	}
	if k.Executor == nil || k.Executor.Resource != "run.md" || k.Attester == nil {
		t.Errorf("executor = %+v, attester = %+v", k.Executor, k.Attester)
	}
	// Only the producer's own key is left; every promoted key is gone, or
	// the value would render and round-trip twice.
	if !reflect.DeepEqual(attrs, map[string]any{"dialect": "standard-sql"}) {
		t.Errorf("attrs = %v, want only the producer extension key", attrs)
	}
	if !updatedAt.Equal(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("updated_at moved to %v: it is the ETag version and generated.at", updatedAt)
	}

	// A shape the migration cannot read is left completely alone — attr
	// intact — so a human can still find it.
	k, attrs, _ = read("finance/malformed")
	if len(k.Sources) != 0 || k.Executor != nil {
		t.Errorf("a malformed family must not be half-converted: %+v %+v", k.Sources, k.Executor)
	}
	if _, ok := attrs["sources"]; !ok {
		t.Error("an unconverted sources attr must stay in attrs, not vanish")
	}
	if _, ok := attrs["executor"]; !ok {
		t.Error("an executor missing its receipt must stay in attrs")
	}

	// An entry with none of these families gets the empty defaults.
	k, attrs, _ = read("finance/none")
	if len(k.Sources) != 0 || k.UsageWindow != nil || k.Runtime != "" || k.Executor != nil {
		t.Errorf("an entry with no OKF families got values: %+v", k)
	}
	if len(attrs) != 1 {
		t.Errorf("attrs = %v, want the producer key untouched", attrs)
	}
}

// TestMigrationActorProcess covers 0022: every stored actor kind of
// "agent" becomes "process", the spelling OKF SPEC §7 defines (design doc
// 0043 §3.8). The scratch schema carries one row per table that stores an
// actor, including a delegation — a "via" holds the caller's whole
// kind:name string, so a half-migration would leave "process:tanaka via
// agent:app" on exactly the rows delegation exists to describe.
func TestMigrationActorProcess(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback is the cleanup

	for _, q := range []string{
		`CREATE SCHEMA actor_scratch`,
		`SET LOCAL search_path TO actor_scratch`,
		`CREATE TABLE knowledge (
			id text PRIMARY KEY,
			created_by_kind text NOT NULL, created_by_name text NOT NULL, created_by_via text NOT NULL DEFAULT '',
			updated_by_kind text NOT NULL, updated_by_name text NOT NULL, updated_by_via text NOT NULL DEFAULT '',
			verified_by_kind text, verified_by_name text, verified_by_via text NOT NULL DEFAULT '',
			rejected_by_kind text, rejected_by_name text, rejected_by_via text NOT NULL DEFAULT '',
			updated_at timestamptz NOT NULL)`,
		`CREATE TABLE knowledge_revision (
			id text NOT NULL, rev int NOT NULL, change text NOT NULL,
			changed_by_kind text NOT NULL, changed_by_name text NOT NULL,
			changed_by_via text NOT NULL DEFAULT '', snapshot jsonb NOT NULL)`,
		`CREATE TABLE knowledge_event (id text NOT NULL, actor_kind text NOT NULL, actor_name text NOT NULL)`,
		`CREATE TABLE attachment (knowledge_id text NOT NULL, name text NOT NULL,
			created_by_kind text NOT NULL, created_by_name text NOT NULL)`,
		`CREATE TABLE knowledge_purge (id text NOT NULL,
			purged_by_kind text NOT NULL, purged_by_name text NOT NULL, purged_by_via text NOT NULL DEFAULT '')`,
		`INSERT INTO knowledge (id,
			created_by_kind, created_by_name, created_by_via,
			updated_by_kind, updated_by_name, updated_by_via,
			verified_by_kind, verified_by_name, rejected_by_kind, rejected_by_name, updated_at) VALUES
			('metrics/revenue', 'agent', 'claude-code', '',
			 'human', 'tanaka', 'agent:app@x.iam.gserviceaccount.com',
			 'human', 'na0', NULL, NULL, '2026-07-01T00:00:00Z'),
			('metrics/human-only', 'human', 'na0', '', 'human', 'na0', '',
			 NULL, NULL, 'agent', 'bot', '2026-07-01T00:00:00Z')`,
		`INSERT INTO knowledge_revision (id, rev, change, changed_by_kind, changed_by_name, changed_by_via, snapshot) VALUES
			('metrics/revenue', 1, 'create', 'agent', 'claude-code', '', '{
				"created_by": {"kind": "agent", "name": "claude-code"},
				"updated_by": {"kind": "human", "name": "tanaka", "via": "agent:app@x.iam.gserviceaccount.com"},
				"body": "the literal {\"kind\": \"agent\"} in prose stays"
			}'::jsonb)`,
		`INSERT INTO knowledge_event (id, actor_kind, actor_name) VALUES ('metrics/revenue', 'agent', 'claude-code')`,
		`INSERT INTO attachment (knowledge_id, name, created_by_kind, created_by_name)
			VALUES ('metrics/revenue', 'chart.png', 'agent', 'claude-code')`,
		`INSERT INTO knowledge_purge (id, purged_by_kind, purged_by_name, purged_by_via)
			VALUES ('metrics/gone', 'agent', 'cleanup', 'agent:ops@x.iam.gserviceaccount.com')`,
	} {
		if _, err := tx.Exec(ctx, q); err != nil {
			t.Fatalf("scratch setup: %v\n%s", err, q)
		}
	}

	sql, err := migrationFS.ReadFile("migrations/0022_actor_process.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(sql)); err != nil {
		t.Fatalf("apply 0022: %v", err)
	}

	var created, updated, verified domain.Actor
	var updatedAt time.Time
	if err := tx.QueryRow(ctx, `SELECT created_by_kind, created_by_name, created_by_via,
		updated_by_kind, updated_by_name, updated_by_via,
		verified_by_kind, verified_by_name, updated_at
		FROM knowledge WHERE id = 'metrics/revenue'`).
		Scan(&created.Kind, &created.Name, &created.Via,
			&updated.Kind, &updated.Name, &updated.Via,
			&verified.Kind, &verified.Name, &updatedAt); err != nil {
		t.Fatal(err)
	}
	if created.String() != "process:claude-code" {
		t.Errorf("created_by = %q, want process:claude-code", created)
	}
	if updated.String() != "human:tanaka via process:app@x.iam.gserviceaccount.com" {
		t.Errorf("updated_by = %q: the via half must migrate too", updated)
	}
	if verified.String() != "human:na0" {
		t.Errorf("verified_by = %q: a human actor must be left alone", verified)
	}
	if !updatedAt.Equal(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("updated_at moved to %v: it is the ETag version and generated.at", updatedAt)
	}

	var rejectedKind string
	if err := tx.QueryRow(ctx,
		`SELECT rejected_by_kind FROM knowledge WHERE id = 'metrics/human-only'`).Scan(&rejectedKind); err != nil {
		t.Fatal(err)
	}
	if rejectedKind != "process" {
		t.Errorf("rejected_by_kind = %q, want process", rejectedKind)
	}

	var revKind, revBody, snapCreated, snapVia string
	if err := tx.QueryRow(ctx, `SELECT changed_by_kind,
		snapshot ->> 'body', snapshot -> 'created_by' ->> 'kind', snapshot -> 'updated_by' ->> 'via'
		FROM knowledge_revision WHERE id = 'metrics/revenue' AND rev = 1`).
		Scan(&revKind, &revBody, &snapCreated, &snapVia); err != nil {
		t.Fatal(err)
	}
	if revKind != "process" || snapCreated != "process" {
		t.Errorf("revision actor = %q, snapshot created_by.kind = %q", revKind, snapCreated)
	}
	if snapVia != "process:app@x.iam.gserviceaccount.com" {
		t.Errorf("snapshot updated_by.via = %q", snapVia)
	}
	// The rewrite is keyed, not textual: prose that happens to quote the
	// old spelling is content and must survive untouched.
	if !strings.Contains(revBody, `{"kind": "agent"}`) {
		t.Errorf("snapshot body was rewritten: %q", revBody)
	}

	for _, tc := range []struct{ query, want, what string }{
		{`SELECT actor_kind FROM knowledge_event`, "process", "knowledge_event"},
		{`SELECT created_by_kind FROM attachment`, "process", "attachment"},
		{`SELECT purged_by_kind FROM knowledge_purge`, "process", "knowledge_purge"},
		{`SELECT purged_by_via FROM knowledge_purge`, "process:ops@x.iam.gserviceaccount.com", "purge via"},
	} {
		var got string
		if err := tx.QueryRow(ctx, tc.query).Scan(&got); err != nil {
			t.Fatalf("%s: %v", tc.what, err)
		}
		if got != tc.want {
			t.Errorf("%s = %q, want %q", tc.what, got, tc.want)
		}
	}
}

// The 0024 backfill composes the stored document for rows written before
// the document was the stored form, and turns each revision snapshot into
// the document that revision held (design doc 0043 §§3.1, 3.9). It runs
// in Go rather than in SQL because composing a document means writing
// YAML in the spelling the spec fixes.
//
// It runs against the real shared database rather than a scratch schema:
// what is being tested is Migrate's own backfill pass, which reads
// through the store's connection and cannot be pointed at a search_path
// set for one transaction.
func TestBackfillComposesStoredDocuments(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "test"}
	id := testdb.Unique(t, "it-backfill-")
	k := &domain.Knowledge{Type: domain.TypeInsights, ID: id, Title: "季節性",
		Status: domain.StatusStable, Body: "12月は+40%。", CreatedBy: actor}
	if err := s.Create(ctx, k, false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(ctx, `DELETE FROM object WHERE id = $1`, id)
		_, _ = s.pool.Exec(ctx, `DELETE FROM knowledge_revision WHERE id = $1`, id)
	})

	// Put the row back into the pre-0024 shape: no document, no hash, and
	// a revision carrying a JSON snapshot instead of a document.
	snapshot, err := json.Marshal(k)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`UPDATE object SET doc = '', content_hash = '' WHERE id = $1`, []any{id}},
		{`UPDATE knowledge_revision SET doc = '', snapshot = $2 WHERE id = $1`, []any{id, snapshot}},
	} {
		if _, err := s.pool.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.backfillDocuments(ctx); err != nil {
		t.Fatal(err)
	}

	var doc, hash string
	var updatedAt time.Time
	if err := s.pool.QueryRow(ctx,
		`SELECT doc, content_hash, updated_at FROM object WHERE id = $1`, id).
		Scan(&doc, &hash, &updatedAt); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "title: 季節性") || !strings.Contains(doc, "12月は+40%。") {
		t.Errorf("composed document does not carry the entry:\n%s", doc)
	}
	// The stored form carries no key this instance owns — that is what
	// makes the hash a content hash (design doc 0043 §3.4).
	for _, owned := range []string{"generated:", "created_by:"} {
		if strings.Contains(doc, owned) {
			t.Errorf("composed document carries the server-owned key %q:\n%s", owned, doc)
		}
	}
	if hash == "" || !updatedAt.Equal(k.UpdatedAt) {
		t.Errorf("hash = %q, updated_at = %v (want it unmoved from %v)", hash, updatedAt, k.UpdatedAt)
	}

	revs, err := s.ListRevisions(ctx, id, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) == 0 || !strings.Contains(revs[0].Document, "title: 季節性") {
		t.Errorf("revision was not composed into a document: %+v", revs)
	}
	// A revision keeps the keys this instance owns: it records what the
	// entry was, and who had confirmed it then is part of that.
	if !strings.Contains(revs[0].Document, "created_by:") {
		t.Errorf("revision document dropped the provenance it is there to record:\n%s", revs[0].Document)
	}

	// Idempotent: a second pass over an already-composed base changes
	// nothing, so a restart is free and a half-finished run resumes.
	if err := s.backfillDocuments(ctx); err != nil {
		t.Fatal(err)
	}
	var again string
	if err := s.pool.QueryRow(ctx, `SELECT doc FROM object WHERE id = $1`, id).Scan(&again); err != nil {
		t.Fatal(err)
	}
	if again != doc {
		t.Error("a second backfill pass rewrote an already-composed document")
	}
}

// Migration 0028 (design doc 0046 §§2.1, 3.1): the table of knowledge
// entries becomes the table of bundle objects, keyed by the path each
// one lives at. The concept id survives as the address a concept is
// called by — every ledger joins on it — and the revision log starts
// counting by path, so that a file's history will land beside the
// concept's rather than in a second place.
func TestMigrationObjectPathKey(t *testing.T) {
	dbURL := testdb.URL(t)
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
		`CREATE SCHEMA path_scratch`,
		`SET LOCAL search_path TO path_scratch`,
		`CREATE TABLE knowledge (
			id text NOT NULL, title text NOT NULL DEFAULT '',
			CONSTRAINT knowledge_pkey PRIMARY KEY (id))`,
		`CREATE TABLE knowledge_revision (
			id text NOT NULL, rev int NOT NULL, change text NOT NULL,
			CONSTRAINT knowledge_revision_pkey PRIMARY KEY (id, rev))`,
		`INSERT INTO knowledge (id, title) VALUES
			('metrics/revenue', '売上'), ('glossary/mau', 'MAU')`,
		`INSERT INTO knowledge_revision (id, rev, change) VALUES
			('metrics/revenue', 1, 'create'), ('metrics/revenue', 2, 'update'),
			('glossary/mau', 1, 'create')`,
	} {
		if _, err := tx.Exec(ctx, q); err != nil {
			t.Fatalf("scratch setup: %v\n%s", err, q)
		}
	}

	sql, err := migrationFS.ReadFile("migrations/0028_object_path_key.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(sql)); err != nil {
		t.Fatalf("apply 0028: %v", err)
	}

	// Every concept is at its id plus ".md" (SPEC §2), and the path is
	// what the row is keyed by.
	var mismatched int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM object WHERE path <> id || '.md'`).Scan(&mismatched); err != nil {
		t.Fatal(err)
	}
	if mismatched != 0 {
		t.Errorf("%d rows are not at their concept's path", mismatched)
	}
	for _, table := range []string{"object", "knowledge_revision"} {
		var keyed bool
		if err := tx.QueryRow(ctx,
			`SELECT bool_or(a.attname = 'path') FROM pg_index i
			   JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
			  WHERE i.indrelid = ('path_scratch.' || $1)::regclass AND i.indisprimary`,
			table).Scan(&keyed); err != nil {
			t.Fatalf("primary key of %s: %v", table, err)
		}
		if !keyed {
			t.Errorf("%s is not keyed by its path", table)
		}
	}
	// The id is still a key of its own: it is what verify, reject, usage
	// and move are called with (design doc 0046 §3.1).
	var unique bool
	if err := tx.QueryRow(ctx,
		`SELECT bool_or(i.indisunique) FROM pg_index i
		  WHERE i.indrelid = 'path_scratch.object'::regclass
		    AND NOT i.indisprimary`).Scan(&unique); err != nil {
		t.Fatal(err)
	}
	if !unique {
		t.Error("the concept id lost its unique index; every ledger joins on it")
	}
	// History is not rewritten, only re-keyed: both revisions of the
	// entry are still there, under its path.
	var revs int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM knowledge_revision WHERE path = 'metrics/revenue.md'`).Scan(&revs); err != nil {
		t.Fatal(err)
	}
	if revs != 2 {
		t.Errorf("revisions under the path: got %d, want 2", revs)
	}
}

// TestMigrationFilesBecomeObjects covers 0029 on the case that has files
// in it (design doc 0046 §§3.3, 3.13). A base with no attachment rows
// moves nothing, so the statement order inside the migration is only
// exercised by one that has them: the rows arrive with a NULL id, and
// 0016's trigger — which is still the one on the table when they land —
// computes the haystack from that id and hands back NULL. The column is
// NOT NULL, so the migration fails, and it fails after 0022-0028 have
// applied: the old binary is then looking for a table that no longer
// exists under that name. That is a failed upgrade taking the service
// down, which is why this test seeds a file rather than trusting an
// empty base.
func TestMigrationFilesBecomeObjects(t *testing.T) {
	dbURL := testdb.URL(t)
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
		`CREATE SCHEMA files_scratch`,
		`SET LOCAL search_path TO files_scratch`,
		// The post-0028 shape, reduced to what 0029 reads and writes.
		`CREATE TABLE object (
			path text NOT NULL PRIMARY KEY, id text, type text NOT NULL DEFAULT '',
			title text NOT NULL DEFAULT '', description text NOT NULL DEFAULT '',
			tags text[] NOT NULL DEFAULT '{}', body text NOT NULL DEFAULT '',
			search_text text NOT NULL DEFAULT '',
			created_by_kind text NOT NULL DEFAULT '', created_by_name text NOT NULL DEFAULT '',
			updated_by_kind text NOT NULL DEFAULT '', updated_by_name text NOT NULL DEFAULT '',
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now(),
			content_changed_at timestamptz NOT NULL DEFAULT now(),
			deleted_at timestamptz)`,
		`CREATE UNIQUE INDEX object_id ON object (id)`,
		`CREATE TABLE attachment (
			knowledge_id text NOT NULL, name text NOT NULL, sha256 text NOT NULL,
			okf_path text NOT NULL DEFAULT '',
			created_by_kind text NOT NULL, created_by_name text NOT NULL,
			created_at timestamptz NOT NULL DEFAULT now(),
			PRIMARY KEY (knowledge_id, name))`,
		`CREATE TABLE blob (sha256 text PRIMARY KEY, media_type text NOT NULL, size bigint NOT NULL)`,
		// 0016's haystack, which is what is on the table when the files
		// land — the whole point of the ordering this test pins.
		`CREATE OR REPLACE FUNCTION ochakai_search_text(
			p_id text, p_title text, p_description text, p_tags text[], p_body text
		 ) RETURNS text LANGUAGE sql STABLE AS $fn$
			SELECT p_id || ' ' || p_title || ' ' || p_description || ' '
				|| array_to_string(p_tags, ' ') || ' ' || p_body || ' '
				|| COALESCE((SELECT string_agg(a.name, ' ')
							   FROM attachment a WHERE a.knowledge_id = p_id), '')
		 $fn$`,
		`CREATE OR REPLACE FUNCTION ochakai_knowledge_search_text() RETURNS trigger
		 LANGUAGE plpgsql AS $fn$
		 BEGIN
			NEW.search_text := ochakai_search_text(NEW.id, NEW.title, NEW.description, NEW.tags, NEW.body);
			RETURN NEW;
		 END $fn$`,
		`CREATE TRIGGER knowledge_search_text BEFORE INSERT OR UPDATE ON object
			FOR EACH ROW EXECUTE FUNCTION ochakai_knowledge_search_text()`,
		`INSERT INTO object (path, id, title) VALUES
			('metrics/revenue.md', 'metrics/revenue', '売上'),
			('insights/q2.md', 'insights/q2', 'Q2')`,
		`INSERT INTO blob (sha256, media_type, size) VALUES
			('aaa', 'image/png', 11), ('bbb', 'application/pdf', 22)`,
		// One file at the canonical <id>/<name>, one an import placed
		// elsewhere in the bundle — the second is the attribution the
		// files column has to carry, since its path does not say it.
		`INSERT INTO attachment (knowledge_id, name, sha256, okf_path, created_by_kind, created_by_name) VALUES
			('metrics/revenue', 'chart.png', 'aaa', '', 'human', 'na0'),
			('insights/q2', 'deck.pdf', 'bbb', 'shared/deck.pdf', 'human', 'na0')`,
	} {
		if _, err := tx.Exec(ctx, q); err != nil {
			t.Fatalf("scratch setup: %v\n%s", err, q)
		}
	}

	sql, err := migrationFS.ReadFile("migrations/0029_files_become_objects.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(sql)); err != nil {
		t.Fatalf("apply 0029 to a base that has files: %v", err)
	}

	// Both files are objects now, at the path each lives at: the canonical
	// one under its entry's namespace, the imported one where it arrived.
	for _, tc := range []struct {
		path, mediaType string
		size            int64
	}{
		{"metrics/revenue/chart.png", "image/png", 11},
		{"shared/deck.pdf", "application/pdf", 22},
	} {
		var id *string
		var mediaType, searchText string
		var size int64
		if err := tx.QueryRow(ctx,
			`SELECT id, media_type, size, search_text FROM object WHERE path = $1`,
			tc.path).Scan(&id, &mediaType, &size, &searchText); err != nil {
			t.Fatalf("%s: %v", tc.path, err)
		}
		if id != nil {
			t.Errorf("%s has concept id %q: an id is the address of a concept", tc.path, *id)
		}
		if mediaType != tc.mediaType || size != tc.size {
			t.Errorf("%s = %s/%d, want %s/%d", tc.path, mediaType, size, tc.mediaType, tc.size)
		}
		// A file is found by its path, not by a haystack.
		if searchText != "" {
			t.Errorf("%s search_text = %q, want empty", tc.path, searchText)
		}
	}

	// Attribution: the canonical path says it, so nothing is written down;
	// the imported one cannot, so the owning entry names it.
	for _, tc := range []struct{ id, wantFiles string }{
		{"metrics/revenue", `[]`},
		{"insights/q2", `["shared/deck.pdf"]`},
	} {
		var files string
		if err := tx.QueryRow(ctx,
			`SELECT files::text FROM object WHERE id = $1`, tc.id).Scan(&files); err != nil {
			t.Fatal(err)
		}
		if files != tc.wantFiles {
			t.Errorf("%s files = %s, want %s", tc.id, files, tc.wantFiles)
		}
	}

	// And the entries can still be found by their filenames (design doc
	// 0020), now derived from the objects rather than the attachment rows.
	for _, tc := range []struct{ id, want string }{
		{"metrics/revenue", "chart.png"},
		{"insights/q2", "deck.pdf"},
	} {
		var searchText string
		if err := tx.QueryRow(ctx,
			`SELECT search_text FROM object WHERE id = $1`, tc.id).Scan(&searchText); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(searchText, tc.want) {
			t.Errorf("%s search_text = %q, want it to carry %q", tc.id, searchText, tc.want)
		}
	}
}
