// Package testdb hands an integration test a namespace of its own in the
// shared test database, and takes it back when the test ends.
//
// The database is shared on purpose: CI starts one container for the
// whole run, and a developer keeps one up across sessions
// (CONTRIBUTING). What that costs is that every test writes into the
// same corpus, and a test that leaves its rows behind makes the corpus
// grow without bound. That is not a tidiness problem. Ranking is
// bounded — a search returns the top ten — so leftover entries push a
// test's own entry out of its assertions, and the failure reads as a
// search bug in whatever PR happens to cross the threshold (issue #278
// found this the hard way, twice).
//
// So a test takes its namespace from Unique, and Unique is the only
// place that knows how to give one back.
package testdb

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// URL returns the database the caller is about to talk to, and skips the
// test when the environment names none. It is the only place that reads
// OCHAKAI_TEST_DATABASE_URL for that purpose, so the skip has one
// wording and, more usefully, a count.
//
// Without a database, well over a hundred of the tree's tests skip, and
// a skip reason is invisible unless something asks for it — so what a
// first contributor sees is a green run that verified none of the store,
// and then a red CI, which does have one.
func URL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("OCHAKAI_TEST_DATABASE_URL")
	if u == "" {
		skipped.Add(1)
		t.Skip("OCHAKAI_TEST_DATABASE_URL not set: this test needs PostgreSQL and did not run")
	}
	return u
}

// skipped counts the tests URL sent away in this package's run.
var skipped atomic.Int64

// Report runs m and, when tests skipped for want of a database, prints
// one line saying so before the package's status. A package whose
// integration tests need it says
//
//	func TestMain(m *testing.M) { os.Exit(testdb.Report(m)) }
//
// Where that line is visible is worth knowing, because it is not
// everywhere: `go test ./...` throws away the output of a package that
// passed, so the line reaches `go test -v`, a bare `go test` inside the
// package's own directory, and any run where something in the package
// failed. It cannot reach a plain green `go test ./...` — no test binary
// can, and a cached package does not run at all. The summary that a
// whole-tree run gets is scripts/check's closing line, which names the
// skip because the script, unlike the binary, is the one being watched.
func Report(m *testing.M) int {
	code := m.Run()
	if n := skipped.Load(); n > 0 {
		fmt.Fprintf(os.Stderr,
			"testdb: %d test(s) skipped — OCHAKAI_TEST_DATABASE_URL is not set, so nothing here was verified against PostgreSQL (scripts/check --db starts one)\n", n)
	}
	return code
}

// Unique returns name with a run-unique number appended, for a test to
// build its ids and paths from, and registers the sweep that removes
// every row carrying it.
//
// Call it before the store, service or server the test writes through:
// cleanups run last-registered-first, so the sweep registered first runs
// last — after the pool the test wrote with has closed, which is why it
// opens a connection of its own.
//
// The name should say which test the rows belong to, so a sweep that
// somehow does not run leaves rows that name their owner.
func Unique(t *testing.T, name string) string {
	t.Helper()
	token := fmt.Sprintf("%s%d", name, time.Now().UnixNano())
	t.Cleanup(func() { Sweep(t, token) })
	return token
}

// Private hands the test a database of its own on the same server and
// drops it when the test ends, for state that Unique cannot isolate.
//
// Unique is what almost everything here wants: rows carry a token, the
// sweep takes them back, and one shared database is what keeps the suite
// quick. It works because every row has an address to put the token in.
//
// The access policy has none. It is one document for the whole
// deployment (design doc 0109 §5), and a single rule anywhere in a
// database puts *every* caller of that database inside a boundary —
// reads outside the granted prefixes become ErrNotFound and the
// whole-bundle operations become ErrForbidden. `go test ./...` runs the
// packages in parallel against one database, so while a test holds a
// policy, the integration tests in restapi, mcpserver and store are
// callers of a scoped deployment without knowing it. That is a race
// nobody can see in the failing test: it surfaces as a 403 on an export
// three packages away.
//
// So a test that writes a policy takes a database with it. Call this
// before the store the test writes through — cleanups run
// last-registered-first, so the drop registered here runs after that
// pool has closed.
func Private(t *testing.T, name string) string {
	t.Helper()
	shared := URL(t) // skips the test when there is no database
	// net/url rather than pgx.ParseConfig, because the config's
	// ConnString reports the string it was parsed from: changing the
	// database on the struct would hand back a URL still naming the
	// shared one, and the test would pass while isolating nothing.
	u, err := url.Parse(shared)
	if err != nil || u.Scheme == "" {
		t.Fatalf("testdb: OCHAKAI_TEST_DATABASE_URL is not a URL, so a private database cannot be named from it: %q", shared)
	}
	// Lowercase and digits only: an unquoted identifier is folded, and a
	// name that needed quoting everywhere it appears is a trap for the
	// next person to write one of these by hand.
	db := fmt.Sprintf("%s_%d", strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, strings.ToLower(name)), time.Now().UnixNano())

	// Outlives t.Context(), which is cancelled before cleanups run.
	ctx := context.Background()
	exec := func(sql string) error {
		conn, err := pgx.Connect(ctx, shared)
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close(ctx) }()
		_, err = conn.Exec(ctx, sql)
		return err
	}
	quoted := pgx.Identifier{db}.Sanitize()
	if err := exec("CREATE DATABASE " + quoted); err != nil {
		t.Fatalf("testdb: creating %s: %v", db, err)
	}
	t.Cleanup(func() {
		// FORCE because a pool that failed to close would otherwise keep
		// the database alive and the next run would find it: a leaked
		// database is worse than a leaked row, since nothing sweeps it.
		if err := exec("DROP DATABASE IF EXISTS " + quoted + " WITH (FORCE)"); err != nil {
			t.Errorf("testdb: dropping %s: %v", db, err)
		}
	})

	private := *u
	private.Path = "/" + db
	return private.String()
}

// Sweep removes every row whose object, ledger or measurement key
// carries token anywhere in it. Exported for the few tests that write
// under a name they did not get from Unique.
//
// Anywhere rather than at the front, because a test's token is as often
// in the middle of an id as at the start: one run writes
// "metrics/<token>-revenue" beside "insights/<token>-reading", and those
// share no prefix. The token carries a nanosecond timestamp, so it
// names one run of one test and nothing else.
//
// It runs outside the test's own transaction and connection on purpose:
// by the time it runs the test's pool is usually closed, and a sweep
// that needed the subject's machinery to work would not run for a test
// that failed halfway through leaving rows behind.
func Sweep(t *testing.T, token string) {
	t.Helper()
	dbURL := os.Getenv("OCHAKAI_TEST_DATABASE_URL")
	if dbURL == "" {
		return // the test skipped; there is nothing to sweep
	}
	// Outlives t.Context(), which is cancelled before cleanups run.
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Errorf("testdb: sweeping %s: connect: %v", token, err)
		return
	}
	defer func() { _ = conn.Close(ctx) }()
	// Every table an entry or a file can leave a row in, keyed by
	// whichever column holds the address. strpos rather than LIKE: an
	// id may contain "_" and "%", which LIKE would read as wildcards.
	for _, q := range []string{
		`DELETE FROM object WHERE strpos(id, $1) > 0 OR strpos(path, $1) > 0`,
		`DELETE FROM knowledge_revision WHERE strpos(id, $1) > 0 OR strpos(path, $1) > 0`,
		`DELETE FROM knowledge_verification WHERE strpos(id, $1) > 0`,
		`DELETE FROM knowledge_rejection WHERE strpos(id, $1) > 0`,
		`DELETE FROM knowledge_purge WHERE strpos(id, $1) > 0`,
		`DELETE FROM knowledge_usage WHERE strpos(knowledge_id, $1) > 0`,
		`DELETE FROM knowledge_event WHERE strpos(knowledge_id, $1) > 0`,
		`DELETE FROM attachment WHERE strpos(knowledge_id, $1) > 0`,
	} {
		if _, err := conn.Exec(ctx, q, token); err != nil {
			t.Errorf("testdb: sweeping %s: %v", token, err)
			return
		}
	}
	// The embedding tables exist only where semantic search has been
	// configured against this database, so a failure to find them is
	// not a failure to clean up.
	for _, q := range []string{
		`DELETE FROM knowledge_embedding WHERE strpos(id, $1) > 0`,
		`DELETE FROM attachment_embedding WHERE strpos(path, $1) > 0`,
	} {
		_, _ = conn.Exec(ctx, q, token)
	}
}
