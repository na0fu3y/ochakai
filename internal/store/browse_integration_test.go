package store

import (
	"context"
	"os"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// Browse (design docs 0014, 0016): one level of dirs and entries per
// call, rooted at the top-level segments; rejected entries invisible,
// and prefix matching by string (an ID with "_" must not act as a LIKE
// wildcard).
func TestIntegrationBrowse(t *testing.T) {
	dbURL := os.Getenv("OCHAKAI_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("OCHAKAI_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	for _, del := range []string{
		`DELETE FROM knowledge WHERE id LIKE 'it-br-%'`,
		`DELETE FROM knowledge_revision WHERE id LIKE 'it-br-%'`,
	} {
		if _, err := s.pool.Exec(ctx, del); err != nil {
			t.Fatal(err)
		}
	}

	actor := domain.Actor{Kind: "human", Name: "test"}
	mk := func(typ domain.Type, id string, status domain.Status) {
		k := &domain.Knowledge{Type: typ, ID: id, Title: "t:" + id, Status: status, CreatedBy: actor}
		if err := s.Create(ctx, k); err != nil {
			t.Fatal(err)
		}
	}
	mk(domain.TypeQuery, "it-br-sales/monthly", domain.StatusVerified)
	mk(domain.TypeQuery, "it-br-sales/regions/apac", domain.StatusDraft)
	mk(domain.TypeQuery, "it-br-top", domain.StatusDraft)
	mk(domain.TypeQuery, "it-br-rejected", domain.StatusRejected)
	// "_" in the prefix must match literally, not as a LIKE wildcard:
	// "it-br_x/deep" would match a LIKE pattern built from "it-br-…".
	mk(domain.TypeQuery, "it-br_x/deep", domain.StatusDraft)
	mk(domain.TypeMetric, "it-br-revenue", domain.StatusDraft)

	// The root is the top-level segments of the shared test DB; our
	// directory must be there with its subtree count.
	rootDirs, _, _, err := s.Browse(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	rootCounts := map[string]int{}
	for _, d := range rootDirs {
		rootCounts[d.Name] = d.Count
	}
	if rootCounts["it-br-sales"] != 2 || rootCounts["it-br_x"] != 1 {
		t.Errorf("root dir counts wrong: %v", rootCounts)
	}

	dirs, entries, truncated, err := s.Browse(ctx, "it-br-sales/")
	if err != nil {
		t.Fatal(err)
	}
	if truncated {
		t.Error("small listing must not truncate")
	}
	if len(dirs) != 1 || dirs[0].Name != "regions" || dirs[0].Count != 1 {
		t.Errorf("dirs = %+v, want regions(1)", dirs)
	}
	if len(entries) != 1 || entries[0].ID != "it-br-sales/monthly" || entries[0].Type != domain.TypeQuery ||
		entries[0].Title != "t:it-br-sales/monthly" || entries[0].Status != domain.StatusVerified {
		t.Errorf("entries = %+v", entries)
	}

	// The underscore ID lives in its own directory, not under it-br-sales.
	dirs, entries, _, err = s.Browse(ctx, "it-br_x/")
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 0 || len(entries) != 1 || entries[0].ID != "it-br_x/deep" {
		t.Errorf("underscore prefix: dirs=%+v entries=%+v", dirs, entries)
	}

	// Root level: the rejected entry is invisible.
	_, entries, _, err = s.Browse(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.ID == "it-br-rejected" {
			t.Error("rejected entry visible in browse")
		}
	}
}
