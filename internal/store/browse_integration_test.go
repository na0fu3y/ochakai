package store

import (
	"context"
	"os"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// Browse (design doc 0014): types with counts at the root, then one
// level of dirs and entries per call; rejected entries invisible, and
// prefix matching by string (an ID with "_" must not act as a LIKE
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

	types, err := s.ListTypes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[domain.Type]int{}
	for _, tc := range types {
		counts[tc.Type] = tc.Count
	}
	// Counts cover the whole shared test DB; ours set the minimum, and
	// the rejected entry must not be part of it.
	if counts[domain.TypeQuery] < 4 || counts[domain.TypeMetric] < 1 {
		t.Errorf("type counts too small: %v", counts)
	}

	dirs, entries, truncated, err := s.Browse(ctx, domain.TypeQuery, "it-br-sales/")
	if err != nil {
		t.Fatal(err)
	}
	if truncated {
		t.Error("small listing must not truncate")
	}
	if len(dirs) != 1 || dirs[0].Name != "regions" || dirs[0].Count != 1 {
		t.Errorf("dirs = %+v, want regions(1)", dirs)
	}
	if len(entries) != 1 || entries[0].ID != "it-br-sales/monthly" ||
		entries[0].Title != "t:it-br-sales/monthly" || entries[0].Status != domain.StatusVerified {
		t.Errorf("entries = %+v", entries)
	}

	// The underscore ID lives in its own directory, not under it-br-sales.
	dirs, entries, _, err = s.Browse(ctx, domain.TypeQuery, "it-br_x/")
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 0 || len(entries) != 1 || entries[0].ID != "it-br_x/deep" {
		t.Errorf("underscore prefix: dirs=%+v entries=%+v", dirs, entries)
	}

	// Root of the type: the rejected entry is invisible.
	_, entries, _, err = s.Browse(ctx, domain.TypeQuery, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.ID == "it-br-rejected" {
			t.Error("rejected entry visible in browse")
		}
	}
}
