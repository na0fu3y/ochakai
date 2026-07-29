package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/store"
)

// A listing walked one page at a time must hand back exactly the listing
// read in one go — same entries, same order, no gap at a page boundary
// and no entry seen twice (design doc 0050 §2.1). That is the whole
// promise of a keyset cursor, and it is the one that breaks quietly: an
// off-by-one in the "strictly after" predicate loses or repeats the row
// on the seam, which no single-page test can see.
//
// Every mode is walked, because each has its own ORDER BY and therefore
// its own predicate: a descending count beside an ascending timestamp
// (usage), two counts and a nullable verification time (failed), a date
// (stale_after), the nullable time alone (verified_at), and the id by
// itself (the source lookup).
func TestListingsWalkToTheEndIntegration(t *testing.T) {
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
	svc := &Service{Store: s, Log: slog.New(slog.DiscardHandler)}
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "test"}

	// The test database is shared with the other packages' integration
	// tests, which leave entries behind: a tag only this run's entries
	// carry is what keeps the walk over a set this test controls.
	run := fmt.Sprintf("svcit-cursor-%d", time.Now().UnixNano())
	resource := "https://example.test/" + run
	ids := make([]string, 6)
	for i := range ids {
		ids[i] = fmt.Sprintf("%s/e%d", run, i)
		k := &domain.Knowledge{
			Type: domain.TypeInsights, ID: ids[i], Title: fmt.Sprintf("草案 %d", i),
			Tags:   []string{run},
			Status: domain.StatusDraft,
			// Distinct dates, all past, so the stale feed lists every one
			// of them in an order the cursor has to reproduce.
			StaleAfter: fmt.Sprintf("2020-01-%02d", i+1),
			Sources:    []domain.Source{{Resource: resource}},
			CreatedBy:  actor,
		}
		if err := s.Create(ctx, k, false); err != nil {
			t.Fatal(err)
		}
	}

	// Verified before the failures below, so the entries that fail after
	// being verified stay in the re-verification feed. Three are left
	// unverified: the verification-age order ends in a NULL tail, which is
	// the position a cursor cannot express as a value.
	for _, id := range ids[:3] {
		if _, err := svc.Verify(ctx, id, actor); err != nil {
			t.Fatal(err)
		}
	}
	// Demand, with ties at 5 and at 0 so the usage feed's second key
	// (oldest-created first) decides, not the id alone.
	for i, hits := range []int{5, 5, 3, 0, 0, 1} {
		for range hits {
			if err := s.RecordEvents(ctx, domain.EventSearchHit, actor, []string{ids[i]}); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Failure reports, with a tie at 3 broken by the fewer corroborating
	// "worked" reports.
	for i, failed := range map[int]int{2: 3, 3: 3, 4: 2, 5: 1} {
		for range failed {
			if err := s.RecordOutcome(ctx, domain.EventFailed, actor, ids[i], ""); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := s.RecordOutcome(ctx, domain.EventWorked, actor, ids[2], ""); err != nil {
		t.Fatal(err)
	}
	if err := s.FlushUsage(ctx); err != nil {
		t.Fatal(err)
	}

	filter := func() store.Filter { return store.Filter{Tags: []string{run}} }
	for _, mode := range []struct {
		name   string
		sort   string
		source string
	}{
		{name: "verified_at", sort: "verified_at"},
		{name: "usage", sort: "usage"},
		{name: "failed", sort: "failed"},
		{name: "stale_after", sort: "stale_after"},
		{name: "source lookup", source: resource},
	} {
		t.Run(mode.name, func(t *testing.T) {
			f := filter()
			f.Source = mode.source
			whole, err := svc.SearchOrList(ctx, "", mode.sort, "", f, 100)
			if err != nil {
				t.Fatal(err)
			}
			if len(whole.Hits) == 0 {
				t.Fatal("the listing is empty; the test set never reached the store")
			}
			if whole.Cursor != "" {
				t.Errorf("a listing that ended handed back a cursor: %q", whole.Cursor)
			}

			// Pages of one: every boundary in the order is a boundary the
			// predicate has to get right.
			var walked []string
			cursor := ""
			for page := 0; ; page++ {
				if page > len(whole.Hits)+2 {
					t.Fatal("the walk did not end; the cursor is not advancing")
				}
				f := filter()
				f.Source = mode.source
				got, err := svc.SearchOrList(ctx, "", mode.sort, cursor, f, 1)
				if err != nil {
					t.Fatal(err)
				}
				for _, h := range got.Hits {
					walked = append(walked, h.ID)
				}
				if got.Cursor == "" {
					break
				}
				if len(got.Hits) != 1 {
					t.Fatalf("a page with a cursor behind it returned %d hits, want 1", len(got.Hits))
				}
				cursor = got.Cursor
			}

			want := make([]string, len(whole.Hits))
			for i, h := range whole.Hits {
				want[i] = h.ID
			}
			if len(walked) != len(want) {
				t.Fatalf("walking one page at a time saw %d entries, reading it whole saw %d:\n walked %v\n whole  %v",
					len(walked), len(want), walked, want)
			}
			for i := range want {
				if walked[i] != want[i] {
					t.Fatalf("page %d of the walk is %s, the whole listing has %s there:\n walked %v\n whole  %v",
						i, walked[i], want[i], walked, want)
				}
			}
		})
	}

	// A cursor names the listing it came from: the keys are that
	// listing's ordering columns, so one from another feed is not a
	// position here, and answering with something would be worse than
	// refusing (design doc 0050 §2.1).
	usage, err := svc.SearchOrList(ctx, "", "usage", "", filter(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if usage.Cursor == "" {
		t.Fatal("the usage feed ended after one entry; the test set never reached the store")
	}
	var inputErr *InvalidInputError
	if _, err := svc.SearchOrList(ctx, "", "failed", usage.Cursor, filter(), 1); !errors.As(err, &inputErr) {
		t.Errorf("a usage cursor resumed the failed feed: %v", err)
	}
}
