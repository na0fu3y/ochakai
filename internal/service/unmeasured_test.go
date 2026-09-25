package service

import (
	"context"
	"testing"
)

// A replay's reads are not the loop's to count (design doc 0146): an
// unmeasured context stops before the store is reached at all, so a
// Service with no store survives what would otherwise dereference it.
func TestAnUnmeasuredReadRecordsNothing(t *testing.T) {
	s := &Service{}
	ctx := Unmeasured(context.Background())
	s.recordUsage(ctx, "fetched", []string{"metrics/revenue"})
	s.recordMiss(ctx, "誰も書いていない語")
	if unmeasured(context.Background()) {
		t.Error("a plain context reads as unmeasured")
	}
}
