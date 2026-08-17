package store

import (
	"context"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/testdb"
)

// Browse (design docs 0014, 0016): one level of dirs and entries per
// call, rooted at the top-level segments; rejected entries invisible,
// and prefix matching by string (an ID with "_" must not act as a LIKE
// wildcard).
func TestIntegrationBrowse(t *testing.T) {
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
	// starts_with rather than LIKE 'it-br-%', which is the very trap
	// this test is about: the "it-br_x" id below has "_" where the
	// pattern has "-", so LIKE left it behind and the next run of this
	// test failed on an id it had created itself.
	for _, del := range []string{
		`DELETE FROM object WHERE starts_with(id, 'it-br')`,
		`DELETE FROM knowledge_revision WHERE starts_with(id, 'it-br')`,
		`DELETE FROM knowledge_rejection WHERE starts_with(id, 'it-br')`,
		`DELETE FROM knowledge_verification WHERE starts_with(id, 'it-br')`,
	} {
		if _, err := s.pool.Exec(ctx, del); err != nil {
			t.Fatal(err)
		}
	}

	actor := domain.Actor{Kind: "human", Name: "test"}
	mk := func(typ domain.Type, id string, status domain.Status) {
		k := &domain.Knowledge{Type: typ, ID: id, Title: "t:" + id, Description: "d:" + id, Status: status, CreatedBy: actor}
		if err := s.Create(ctx, k, false); err != nil {
			t.Fatal(err)
		}
	}
	mk(domain.TypeComputations, "it-br-sales/monthly", domain.StatusStable)
	mk(domain.TypeComputations, "it-br-sales/regions/apac", domain.StatusDraft)
	mk(domain.TypeComputations, "it-br-top", domain.StatusDraft)
	mk(domain.TypeComputations, "it-br-rejected", domain.StatusDraft)
	// A rejection is a ledger row, not a status (design doc 0043 §3.3),
	// so browse has to consult the ledger to keep hiding it.
	if _, err := s.Reject(ctx, "it-br-rejected", actor, "duplicate"); err != nil {
		t.Fatal(err)
	}
	// "_" in the prefix must match literally, not as a LIKE wildcard:
	// "it-br_x/deep" would match a LIKE pattern built from "it-br-…".
	mk(domain.TypeComputations, "it-br_x/deep", domain.StatusDraft)
	mk(domain.TypeMetrics, "it-br-revenue", domain.StatusDraft)

	// The root is the top-level segments of the shared test DB; our
	// directory must be there with its subtree count.
	root, err := s.Browse(ctx, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	rootCounts := map[string]int{}
	for _, d := range root.Dirs {
		rootCounts[d.Name] = d.Count
	}
	if rootCounts["it-br-sales"] != 2 || rootCounts["it-br_x"] != 1 {
		t.Errorf("root dir counts wrong: %v", rootCounts)
	}

	lvl, err := s.Browse(ctx, "it-br-sales/", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if lvl.Truncated {
		t.Error("small listing must not truncate")
	}
	if len(lvl.Dirs) != 1 || lvl.Dirs[0].Name != "regions" || lvl.Dirs[0].Count != 1 {
		t.Errorf("dirs = %+v, want regions(1)", lvl.Dirs)
	}
	if e := lvl.Concepts; len(e) != 1 || e[0].ID != "it-br-sales/monthly" || e[0].Type != domain.TypeComputations ||
		e[0].Title != "t:it-br-sales/monthly" || e[0].Description != "d:it-br-sales/monthly" ||
		e[0].Status != domain.StatusStable {
		t.Errorf("concepts = %+v", lvl.Concepts)
	}

	// The underscore ID lives in its own directory, not under it-br-sales.
	lvl, err = s.Browse(ctx, "it-br_x/", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(lvl.Dirs) != 0 || len(lvl.Concepts) != 1 || lvl.Concepts[0].ID != "it-br_x/deep" {
		t.Errorf("underscore prefix: dirs=%+v concepts=%+v", lvl.Dirs, lvl.Concepts)
	}

	// Root level: the rejected entry is invisible.
	lvl, err = s.Browse(ctx, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range lvl.Concepts {
		if e.ID == "it-br-rejected" {
			t.Error("rejected entry visible in browse")
		}
	}
}

// A level pages: a directory wider than one page hands back a position,
// and resuming from it reads the rest (design docs 0068 §2.1, 0101).
//
// The rows go in with one INSERT rather than 1001 writes through the
// service: what is under test is the keyset predicate and where a page
// stops, and neither reads anything the write path would have filled in.
func TestBrowsePagesPastTheCap(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)

	dir := testdb.Unique(t, "it-brpage")
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.WithoutCancel(ctx),
			`DELETE FROM object WHERE starts_with(path, $1)`, dir+"/")
	})
	// One past the page, so the last page is a short one rather than an
	// empty one — the case that tells "full page" from "done".
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO object (path, id, type, title, description, body, status,
		                    created_by_kind, created_by_name, updated_by_kind, updated_by_name)
		SELECT $1 || '/' || to_char(n, 'FM0000') || '.md', $1 || '/' || to_char(n, 'FM0000'),
		       'Metric', '', '', '', 'stable', 'human', 't', 'human', 't'
		  FROM generate_series(1, $2::int) n`, dir, MaxBrowseEntries+1); err != nil {
		t.Fatal(err)
	}

	first, err := s.Browse(ctx, dir+"/", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Concepts) != MaxBrowseEntries || !first.MoreConcepts || !first.Truncated {
		t.Fatalf("first page = %d concepts, more=%v truncated=%v",
			len(first.Concepts), first.MoreConcepts, first.Truncated)
	}
	if first.MoreFiles {
		t.Error("no files were written, so no file page can be behind this one")
	}

	last := first.Concepts[len(first.Concepts)-1].ID
	next, err := s.Browse(ctx, dir+"/", &BrowseAfter{Concepts: &last}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Concepts) != 1 || next.MoreConcepts {
		t.Fatalf("second page = %d concepts, more=%v, want the one row left", len(next.Concepts), next.MoreConcepts)
	}
	if next.Concepts[0].ID <= last {
		t.Errorf("second page starts at %q, which is not after %q", next.Concepts[0].ID, last)
	}
	// The subdirectories ride on the first page only: they are a
	// grouping, so repeating them would report the same directory twice
	// to anyone walking the level.
	if next.Dirs != nil {
		t.Errorf("a resumed page carries %d subdirectories, want none", len(next.Dirs))
	}
	// A kind the cursor marked finished is not asked again — the files
	// query does not run, which is what a nil position means.
	files, err := s.Browse(ctx, dir+"/", &BrowseAfter{Concepts: &last, Files: nil}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if files.Files != nil {
		t.Errorf("a finished kind came back with %d rows", len(files.Files))
	}
}
