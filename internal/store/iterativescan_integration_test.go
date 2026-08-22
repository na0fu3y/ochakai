package store

import (
	"context"
	"fmt"
	"testing"
)

// A filtered vector search can answer short, and say nothing about it.
// The HNSW scan runs before the WHERE clause, so a search asking for ten
// rows under a filter matching one row in a hundred gets only what
// survives of its ef_search candidates — three, on the fixture below —
// with the response carrying no sign that the other seven were never
// looked for. `hnsw.iterative_scan` (pgvector 0.8.0) keeps walking the
// graph instead of stopping at those candidates.
//
// **What that is worth was measured, and it is not "the limit".** Over
// twelve freshly generated fixtures the pairs were 4/10, 3/3, 3/5, 5/5,
// 3/6, 3/3, 3/3, 4/6, 3/3, 3/6, 4/5, 3/3: never worse, better in half of
// them, complete in one. Raising hnsw.max_scan_tuples and
// hnsw.scan_mem_multiplier moved none of it, so what ends the scan is the
// graph running out of reachable matches rather than a budget — on
// uniform random vectors, which build a poor graph. That is the claim
// annSearch is allowed to rest on: the ceiling is lifted, not the answer
// guaranteed.
//
// So the assertions are the two things that held across every fixture:
// the truncation reproduces, and iterating never returns fewer rows than
// not iterating. On the fixed fixture below the pair is 1 and 10.
//
// A table of its own, not ochakai's: the subject is what pgvector does
// under a selective filter, and reaching it through the schema would need
// the planner to prefer the index over a sequential scan, which at any
// corpus a test can build in a second it does not.
func TestIterativeScanIsWhatMakesAFilteredSearchAnswerInFull(t *testing.T) {
	ctx := context.Background()
	s := newSearchStore(t, ctx)
	if _, err := s.pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS vector`); err != nil {
		t.Skipf("no pgvector here: %v", err)
	}
	if !s.hasIterativeScan(ctx) {
		t.Skip("pgvector older than 0.8.0: the setting under test does not exist")
	}

	// One connection for the whole test, because the fixture is a
	// temporary table and that is a property of a session: a pooled
	// connection is a different session each time, and the pool grows
	// under the other tests in this package.
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()

	// A fixed name is safe now that the connection is pinned: a temporary
	// table lives and dies with its session, so no other test — or run —
	// can see this one.
	const table = "iterscan"
	// Deterministic vectors rather than random ones. A fresh draw each
	// run makes the numbers below move — measured, they ranged over
	// 3..5 without the scan and 3..10 with it — and an assertion on a
	// number a draw decides is a test that fails on Tuesdays. These
	// vectors trace a curve, so the graph is the same graph every time.
	if _, err := conn.Exec(ctx, fmt.Sprintf(`
		CREATE TEMP TABLE %s(id int PRIMARY KEY, kind text, embedding vector(8));
		INSERT INTO %s
		SELECT g,
		       CASE WHEN g %% 100 = 0 THEN 'wanted' ELSE 'other' END,
		       ARRAY[sin(g), cos(g), sin(g * 2), cos(g * 2),
		             sin(g * 3), cos(g * 3), sin(g * 5), cos(g * 5)]::vector
		FROM generate_series(1, 3000) g;
		CREATE INDEX ON %s USING hnsw (embedding vector_cosine_ops);
		ANALYZE %s;`, table, table, table, table)); err != nil {
		t.Fatalf("building the fixture: %v", err)
	}

	const limit = 10
	rowsUnder := func(mode string) int {
		t.Helper()
		tx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		// The index has to be the plan for the question to arise at all;
		// on a table this size the planner would otherwise scan and sort,
		// which is exact and answers in full. Production reaches the same
		// plan by having enough rows for the index to win.
		for _, set := range []string{
			"SET LOCAL enable_seqscan = off",
			fmt.Sprintf("SET LOCAL hnsw.ef_search = %d", efSearch(limit)),
			fmt.Sprintf("SET LOCAL hnsw.iterative_scan = '%s'", mode),
		} {
			if _, err := tx.Exec(ctx, set); err != nil {
				t.Fatalf("%s: %v", set, err)
			}
		}
		var n int
		if err := tx.QueryRow(ctx, fmt.Sprintf(`
			SELECT count(*) FROM (
				SELECT id FROM %s WHERE kind = 'wanted'
				ORDER BY embedding <=> (SELECT embedding FROM %s WHERE id = 1)
				LIMIT %d) hits`, table, table, limit)).Scan(&n); err != nil {
			t.Fatalf("search with iterative_scan=%s: %v", mode, err)
		}
		return n
	}

	// There are three times as many matching rows as the search asks for,
	// so anything short of the limit is the scan stopping early rather
	// than the corpus running out.
	var matching int
	if err := conn.QueryRow(ctx, fmt.Sprintf(
		`SELECT count(*) FROM %s WHERE kind = 'wanted'`, table)).Scan(&matching); err != nil {
		t.Fatal(err)
	}
	if matching < limit {
		t.Fatalf("the fixture holds %d matching rows, fewer than the %d asked for", matching, limit)
	}

	// The failure this exists for. If pgvector ever stops truncating here
	// the setting has become unnecessary rather than wrong — but that is
	// a thing to find out deliberately, not to assume.
	plain := rowsUnder("off")
	if plain >= limit {
		t.Errorf("without an iterative scan a filtered search returned %d of %d rows: "+
			"the truncation this guards against did not reproduce, so the guard proves nothing here", plain, limit)
	}
	// And what annSearch buys by asking for the scan: more of the answer,
	// never less of it.
	iterating := rowsUnder("strict_order")
	if iterating < plain {
		t.Errorf("hnsw.iterative_scan = strict_order returned %d rows where not iterating returned %d: "+
			"asking for the scan cost rows, which is the one outcome that would make it the wrong default",
			iterating, plain)
	}
	t.Logf("filtered search for %d rows: %d without an iterative scan, %d with one", limit, plain, iterating)
}

// The wiring: a store that migrated against a pgvector new enough to have
// the setting is a store that will ask for it. Settled once at migration
// because it is a property of the installation, and read here the way
// annSearch reads it.
func TestTheStoreLearnsWhetherItCanScanIteratively(t *testing.T) {
	ctx := context.Background()
	s := newSearchStore(t, ctx)
	// newSearchStore migrates without embeddings (dim 0), which never
	// reaches the vector half — so the store has nothing to know yet.
	if s.iterativeScan {
		t.Error("the store claims an iterative scan before the vector schema was ever set up")
	}
	if err := s.Migrate(ctx, 8); err != nil {
		t.Skipf("no pgvector here: %v", err)
	}
	if want := s.hasIterativeScan(ctx); s.iterativeScan != want {
		t.Errorf("store.iterativeScan = %v after migrating, want %v (what this pgvector supports)", s.iterativeScan, want)
	}
}
