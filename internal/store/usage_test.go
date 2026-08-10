package store

import (
	"context"
	"fmt"
	"github.com/na0fu3y/ochakai/internal/testdb"
	"strings"
	"testing"
	"time"
)

func usageEvents(prefix string, n int) []usageEvent {
	out := make([]usageEvent, n)
	now := time.Now()
	for i := range out {
		out[i] = usageEvent{
			id: fmt.Sprintf("%s-%d", prefix, i), event: "fetched",
			actorKind: "human", actorName: "test", at: now,
		}
	}
	return out
}

// A flush that fails must say how much it lost. Design doc 0029 §3.1
// accepts losing the batch — there is no retry queue, deliberately —
// on the condition that the loss is visible rather than silent, and
// nobody but the flush loop is in a position to see it.
func TestIntegrationFlushErrorNamesTheEventsItLost(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Take the database away from a store that still has events buffered:
	// the shape of a flush arriving while Cloud SQL is unreachable.
	s.pool.Close()
	s.usageBuf = usageEvents("it-flush-lost", 3)

	err = s.FlushUsage(ctx)
	if err == nil {
		t.Fatal("flush against a closed pool: want an error")
	}
	if !strings.Contains(err.Error(), "3 buffered usage events") {
		t.Errorf("error does not say what was lost: %v", err)
	}
	if len(s.usageBuf) != 0 {
		t.Errorf("buffer holds %d events; a drained batch is not retried (0029 §3.1)", len(s.usageBuf))
	}
}
