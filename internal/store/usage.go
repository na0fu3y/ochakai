package store

import (
	"context"
	"fmt"
	"time"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// eventRetention is how long raw knowledge_event rows are kept. Running
// totals in knowledge_usage survive pruning, so /usage stays accurate.
const eventRetention = 180 * 24 * time.Hour

// usageFlushInterval is how often buffered usage events are written, and
// usageBufferMax caps the buffer so a stalled database cannot grow it
// without bound — past the cap, new events are dropped (usage is a
// best-effort statistic, design doc 0029).
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

// missEvent is one buffered search that found nothing: what was asked, by
// whom, when (design doc 0051 §3.1). It rides the same buffer and the
// same flush as the usage events because it is the same kind of fact —
// the server's own observation of a read, worth losing before a read is
// (0029 §3.1).
type missEvent struct {
	query string
	// key groups this asking with the others that meant the same thing
	// (domain.MissKey, migration 0037). The query stays as typed.
	key       string
	actorKind string
	actorName string
	at        time.Time
}

// RecordEvents buffers usage events in memory and returns immediately,
// keeping recording off the read path (design doc 0029): the events
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
		s.droppedEvents += int64(len(ids))
		return errUsageBufferFull
	}
	for _, id := range ids {
		s.usageBuf = append(s.usageBuf, usageEvent{id, event, actor.Kind, actor.Name, now})
	}
	return nil
}

// RecordMiss buffers one search that returned nothing, and returns
// immediately (design doc 0051 §3.2). Same contract as RecordEvents: the
// row is written by the background flush loop, and the error says only
// that the buffer was full, never that the search failed.
func (s *Store) RecordMiss(ctx context.Context, query, key string, actor domain.Actor) error {
	if query == "" {
		return nil
	}
	now := time.Now()
	s.usageMu.Lock()
	defer s.usageMu.Unlock()
	if len(s.missBuf) >= usageBufferMax {
		s.droppedMisses++
		return errUsageBufferFull
	}
	s.missBuf = append(s.missBuf, missEvent{query, key, actor.Kind, actor.Name, now})
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
//
// Nobody is waiting on these writes, so the loop is the last place the
// error can be seen. It logs: design doc 0029 §3.1 accepts losing a
// failed batch, on the condition that the loss is visible rather than
// silent, and the loop is the only caller in a position to make it so.
func (s *Store) usageFlushLoop() {
	defer s.flushWG.Done()
	t := time.NewTicker(usageFlushInterval)
	defer t.Stop()
	flush := func() {
		if err := s.FlushUsage(context.Background()); err != nil {
			s.log.Warn("usage flush failed", "error", err)
		}
	}
	for {
		select {
		case <-t.C:
			flush()
		case <-s.flushStop:
			flush()
			return
		}
	}
}

// FlushUsage writes all buffered usage events: one knowledge_event row per
// occurrence, and the running totals in knowledge_usage bumped in the same
// statement. Exported so shutdown and tests can force a flush.
//
// On a database error the drained batch is lost and there is no retry
// queue: usage is best-effort, and design doc 0029 §3.1 chose that
// deliberately over spending memory on a stalled database. What the
// decision also asked for is that the loss be visible — the error says
// how many events went with it, and the flush loop logs it. How much was
// lost altogether is counted too, and written to usage_drop by the next
// flush that reaches the database (migration 0038), so it reaches the
// stats beside the numbers it damaged rather than only a log line.
func (s *Store) FlushUsage(ctx context.Context) error {
	s.usageMu.Lock()
	batch, misses := s.usageBuf, s.missBuf
	s.usageBuf, s.missBuf = nil, nil
	s.usageMu.Unlock()

	// The misses go first and independently: they are a different table
	// with no aggregate to maintain, and a batch of events failing is no
	// reason to drop the questions that came with it.
	missErr := s.flushMisses(ctx, misses)
	if missErr != nil {
		s.countDropped(0, int64(len(misses)))
	}
	if len(batch) == 0 {
		s.recordDrops(ctx)
		return missErr
	}

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
		s.countDropped(int64(len(batch)), 0)
		s.recordDrops(ctx)
		return fmt.Errorf("writing %d buffered usage events: %w", len(batch), err)
	}
	s.recordDrops(ctx)
	s.maybePruneEvents(ctx)
	return missErr
}

// countDropped remembers observations that never reached the database.
func (s *Store) countDropped(events, misses int64) {
	if events == 0 && misses == 0 {
		return
	}
	s.usageMu.Lock()
	defer s.usageMu.Unlock()
	s.droppedEvents += events
	s.droppedMisses += misses
}

// recordDrops writes what this process has lost so far and clears it,
// leaving the count where every instance's stats can read it.
//
// Taken and put back rather than read and cleared: if this insert is the
// statement that fails — the same stalled database that caused the drop
// — the count has to survive for the next attempt, or the fix would lose
// exactly the numbers it exists to keep. What is dropped between here
// and the process dying goes with it, which is the retry queue design
// doc 0029 refused, and the honest edge of this.
func (s *Store) recordDrops(ctx context.Context) {
	s.usageMu.Lock()
	events, misses := s.droppedEvents, s.droppedMisses
	s.droppedEvents, s.droppedMisses = 0, 0
	s.usageMu.Unlock()
	if events == 0 && misses == 0 {
		return
	}
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO usage_drop (at, events, misses) VALUES (now(), $1, $2)`, events, misses); err != nil {
		s.countDropped(events, misses)
	}
}

// flushMisses writes one search_miss row per buffered miss. Same bargain
// as the events above: the batch is lost on error, and the error says how
// much went with it.
func (s *Store) flushMisses(ctx context.Context, batch []missEvent) error {
	if len(batch) == 0 {
		return nil
	}
	queries := make([]string, len(batch))
	keys := make([]string, len(batch))
	kinds := make([]string, len(batch))
	names := make([]string, len(batch))
	ats := make([]time.Time, len(batch))
	for i, m := range batch {
		queries[i], keys[i] = m.query, m.key
		kinds[i], names[i], ats[i] = m.actorKind, m.actorName, m.at
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO search_miss (query, normalized, actor_kind, actor_name, at)
		SELECT * FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::timestamptz[])`,
		queries, keys, kinds, names, ats)
	if err != nil {
		return fmt.Errorf("writing %d buffered search misses: %w", len(batch), err)
	}
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
//
// Search misses are pruned on the same schedule and to the same day, and
// nothing survives them — unlike a usage event, a miss has no running
// total, so the window is the whole memory (design doc 0051 §3.3). That
// is what /api/v1/stats caps its own window at.
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
	_, _ = s.pool.Exec(ctx, `DELETE FROM search_miss WHERE at < now() - $1::interval`,
		eventRetention.String())
	_, _ = s.pool.Exec(ctx, `DELETE FROM usage_drop WHERE at < now() - $1::interval`,
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// The same window the feeds rank in, on the same shape, so `recent`
	// never means one thing in a listing and another here. A concept read
	// through this endpoint is usually one a curator is looking at
	// *because* a feed put it in front of them.
	u.Recent.Days = domain.RecentUsageDays
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE event = 'fetched'),
		       count(*) FILTER (WHERE event = 'failed')
		  FROM knowledge_event
		 WHERE knowledge_id = $1 AND at >= now() - $2::interval`,
		id, recentWindow()).Scan(&u.Recent.Fetches, &u.Recent.Failed); err != nil {
		return nil, err
	}
	reports, err := s.outcomeReports(ctx, id)
	if err != nil {
		return nil, err
	}
	u.Reports = reports
	return u, nil
}

// outcomeReports reads back the notes callers wrote with their outcome
// reports, newest first and bounded (design doc 0137).
//
// The rows this walks are the same ones the recent counts are taken over
// and the same ones the daily prune drops at 180 days, so the list
// answers only for the retention window — which the surfaces say rather
// than leave to be discovered. Only noted reports are selected: a report
// with nothing to say is already counted in the totals and would only
// pad a list a person is meant to read.
func (s *Store) outcomeReports(ctx context.Context, id string) ([]domain.OutcomeReport, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT event, at, actor_kind, actor_name, note
		  FROM knowledge_event
		 WHERE knowledge_id = $1 AND event IN ($2, $3) AND note <> ''
		 ORDER BY at DESC
		 LIMIT $4`,
		id, domain.EventWorked, domain.EventFailed, domain.OutcomeReportLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.OutcomeReport
	for rows.Next() {
		var r domain.OutcomeReport
		if err := rows.Scan(&r.Outcome, &r.At, &r.By.Kind, &r.By.Name, &r.Note); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
