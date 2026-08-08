package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/store"
)

// The context pack is the one call an agent is told to make before a
// data question, and it used to be the slowest thing in the system: one
// round trip per hit, one per link and one per primary for the
// backlinks, each waiting on the one before. Fixing that is only worth
// something if it stays fixed, and nothing about the result says how
// many times the database was asked to produce it.
//
// So it is read from outside (design doc 0035), off the pool's own
// counter: build a pack twice at sizes an order of magnitude apart and
// compare what each cost. A count that tracks the pack size is the shape
// of the defect, whatever the constant happens to be on the day — which
// is why the assertion is on the difference and not on a number.

// TestContextReadsDoNotGrowWithThePack builds a two-concept pack and a
// nineteen-concept one and compares their cost in statements.
func TestContextReadsDoNotGrowWithThePack(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t, ctx)
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "reads"}
	run := uid(t, "ctxreads")

	// Two clusters. Every leaf links at its hub and the hub links back at
	// every leaf, so a pack has both directions to follow — the hop that
	// used to cost a round trip per primary in each direction.
	build := func(cluster string, leaves int) {
		t.Helper()
		hub := fmt.Sprintf("%s/%s/hub", run, cluster)
		body := ""
		for i := range leaves {
			body += fmt.Sprintf("Links at [%d](/%s/%s/leaf-%02d.md).\n", i, run, cluster, i)
		}
		if _, err := svc.Create(ctx, &domain.Knowledge{
			Type: domain.TypeMetrics, ID: hub, Title: cluster + " hub",
			Status: domain.StatusStable, Body: body,
		}, actor); err != nil {
			t.Fatalf("create %s: %v", hub, err)
		}
		for i := range leaves {
			id := fmt.Sprintf("%s/%s/leaf-%02d", run, cluster, i)
			if _, err := svc.Create(ctx, &domain.Knowledge{
				Type: domain.TypeInsights, ID: id, Title: fmt.Sprintf("%s leaf %d", cluster, i),
				Status: domain.StatusStable,
				Body:   fmt.Sprintf("About the %s hub, see [hub](/%s.md).", cluster, hub),
			}, actor); err != nil {
				t.Fatalf("create %s: %v", id, err)
			}
		}
	}
	build("small", 1)
	build("large", 18)

	measure := func(cluster string, wantAtLeast int) int64 {
		t.Helper()
		filter := store.Filter{Prefixes: []string{run + "/" + cluster}}
		before := svc.Store.StatementCount()
		res, err := svc.Context(ctx, ContextRequest{Query: cluster + " hub", Filter: filter, Limit: 20})
		if err != nil {
			t.Fatalf("context %s: %v", cluster, err)
		}
		after := svc.Store.StatementCount()
		if got := len(res.Concepts) + len(res.Outline); got < wantAtLeast {
			t.Fatalf("the %s pack held %d concepts, want at least %d: the comparison "+
				"means nothing unless the two packs are different sizes", cluster, got, wantAtLeast)
		}
		return after - before
	}
	small := measure("small", 2)
	large := measure("large", 15)

	t.Logf("statements: %d for a 2-concept pack, %d for a 19-concept pack", small, large)
	// Slack of two, because a pack can take one extra batch of primaries
	// when a hit does not survive the read, and because the usage flush
	// that follows a pack is asynchronous and may or may not land inside
	// the window. What is asserted is that nine times the concepts is not
	// nine times the statements.
	if large > small+2 {
		t.Errorf("a 19-concept pack cost %d statements against %d for a 2-concept pack: "+
			"the pack is reading once per concept again", large, small)
	}
}
