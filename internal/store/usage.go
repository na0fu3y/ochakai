package store

import (
	"context"
	"time"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// eventRetention is how long raw knowledge_event rows are kept. Running
// totals in knowledge_usage survive pruning, so /usage stays accurate.
const eventRetention = 180 * 24 * time.Hour

// usageFlushInterval is how often buffered usage events are written, and
// usageBufferMax caps the buffer so a stalled database cannot grow it
// without bound — past the cap, new events are dropped (usage is a
// best-effort statistic, design doc 0025 §10).
const (
	usageFlushInterval = 5 * time.Second
	usageBufferMax     = 20000
)

// usageEvent is one buffered occurrence: who used which entry, when.
type usageEvent struct {
	id        string
	event     string
	actorKind string
	actorName string
	at        time.Time
}

// RecordEvents buffers usage events in memory and returns immediately,
// keeping recording off the read path (design doc 0025 §11): the events
// are written by the background flush loop, not by the caller. ids name
// the target entries. Callers treat the error as non-fatal — it signals
// only that the buffer was full and events were dropped, never a failed
// read.
func (s *Store) RecordEvents(ctx context.Context, event string, actor domain.Actor, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now()
	s.usageMu.Lock()
	defer s.usageMu.Unlock()
	if len(s.usageBuf)+len(ids) > usageBufferMax {
		return errUsageBufferFull
	}
	for _, id := range ids {
		s.usageBuf = append(s.usageBuf, usageEvent{id, event, actor.Kind, actor.Name, now})
	}
	return nil
}

// errUsageBufferFull is returned by RecordEvents when the buffer is at
// capacity; the events are dropped. Surfaced so the caller's log records
// that usage was undercounted (service.recordUsage), never fatal.
var errUsageBufferFull = errUsageBuffer("usage buffer full; events dropped")

type errUsageBuffer string

func (e errUsageBuffer) Error() string { return string(e) }

// usageFlushLoop drains the usage buffer on a timer until Close, then once
// more so a clean shutdown loses nothing already buffered.
func (s *Store) usageFlushLoop() {
	defer s.flushWG.Done()
	t := time.NewTicker(usageFlushInterval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			s.FlushUsage(context.Background())
		case <-s.flushStop:
			s.FlushUsage(context.Background())
			return
		}
	}
}

// FlushUsage writes all buffered usage events: one knowledge_event row per
// occurrence, and the running totals in knowledge_usage bumped in the same
// statement. Exported so shutdown and tests can force a flush. On a
// database error the drained batch is lost (best-effort); the error is
// returned for visibility.
func (s *Store) FlushUsage(ctx context.Context) error {
	s.usageMu.Lock()
	if len(s.usageBuf) == 0 {
		s.usageMu.Unlock()
		return nil
	}
	batch := s.usageBuf
	s.usageBuf = nil
	s.usageMu.Unlock()

	ids := make([]string, len(batch))
	events := make([]string, len(batch))
	kinds := make([]string, len(batch))
	names := make([]string, len(batch))
	ats := make([]time.Time, len(batch))
	for i, e := range batch {
		ids[i], events[i], kinds[i], names[i], ats[i] = e.id, e.event, e.actorKind, e.actorName, e.at
	}
	// last_at takes the greatest event time on either side so a flush whose
	// batch happens to carry an older occurrence never rewinds the total.
	_, err := s.pool.Exec(ctx, `
		WITH ev AS (
			INSERT INTO knowledge_event (knowledge_id, event, actor_kind, actor_name, at)
			SELECT * FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::timestamptz[])
			RETURNING knowledge_id, event, at
		)
		INSERT INTO knowledge_usage (knowledge_id, event, count, last_at)
		SELECT knowledge_id, event, count(*), max(at) FROM ev GROUP BY 1, 2
		ON CONFLICT (knowledge_id, event)
		DO UPDATE SET count = knowledge_usage.count + EXCLUDED.count,
			last_at = GREATEST(knowledge_usage.last_at, EXCLUDED.last_at)`,
		ids, events, kinds, names, ats)
	if err != nil {
		return err
	}
	s.maybePruneEvents(ctx)
	return nil
}

// RecordOutcome appends one outcome event (worked/failed) with its note
// and bumps the running totals. Unlike RecordEvents, failures matter to
// the caller — an outcome report is a deliberate write, not a side
// effect of a read — so the error is returned for the transport to
// surface.
func (s *Store) RecordOutcome(ctx context.Context, event string, actor domain.Actor, id, note string) error {
	_, err := s.pool.Exec(ctx, `
		WITH ev AS (
			INSERT INTO knowledge_event (knowledge_id, event, actor_kind, actor_name, note)
			VALUES ($1, $2, $3, $4, $5)
		)
		INSERT INTO knowledge_usage (knowledge_id, event, count, last_at)
		VALUES ($1, $2, 1, now())
		ON CONFLICT (knowledge_id, event)
		DO UPDATE SET count = knowledge_usage.count + 1, last_at = EXCLUDED.last_at`,
		id, event, actor.Kind, actor.Name, note)
	if err != nil {
		return err
	}
	s.maybePruneEvents(ctx)
	return nil
}

// maybePruneEvents drops raw events older than the retention window, at
// most once per day per process (totals live on in knowledge_usage).
func (s *Store) maybePruneEvents(ctx context.Context) {
	now := time.Now().Unix()
	last := s.lastEventPrune.Load()
	if now-last < int64((24 * time.Hour).Seconds()) {
		return
	}
	if !s.lastEventPrune.CompareAndSwap(last, now) {
		return
	}
	_, _ = s.pool.Exec(ctx, `DELETE FROM knowledge_event WHERE at < now() - $1::interval`,
		eventRetention.String())
}

// Usage returns the running usage totals for one knowledge entry.
func (s *Store) Usage(ctx context.Context, id string) (*domain.Usage, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT event, count, last_at FROM knowledge_usage WHERE knowledge_id = $1`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	u := &domain.Usage{}
	for rows.Next() {
		var event string
		var count int64
		var lastAt time.Time
		if err := rows.Scan(&event, &count, &lastAt); err != nil {
			return nil, err
		}
		switch event {
		case domain.EventSearchHit:
			u.SearchHits = count
		case domain.EventFetched:
			u.Fetches = count
		case domain.EventCompiled:
			u.Compiles = count
		case domain.EventWorked:
			u.Worked = count
		case domain.EventFailed:
			u.Failed = count
		}
		if u.LastUsedAt == nil || lastAt.After(*u.LastUsedAt) {
			t := lastAt
			u.LastUsedAt = &t
		}
	}
	return u, rows.Err()
}
