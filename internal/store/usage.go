package store

import (
	"context"
	"time"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// eventRetention is how long raw knowledge_event rows are kept. Running
// totals in knowledge_usage survive pruning, so /usage stays accurate.
const eventRetention = 180 * 24 * time.Hour

// RecordEvents appends raw usage events and bumps the running totals in a
// single statement. ids name the target entries. Callers treat failures
// as non-fatal: usage recording must never fail a read.
func (s *Store) RecordEvents(ctx context.Context, event string, actor domain.Actor, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := s.pool.Exec(ctx, `
		WITH ev AS (
			INSERT INTO knowledge_event (knowledge_id, event, actor_kind, actor_name)
			SELECT i, $2, $3, $4 FROM unnest($1::text[]) AS u(i)
			RETURNING knowledge_id, event
		)
		INSERT INTO knowledge_usage (knowledge_id, event, count, last_at)
		SELECT knowledge_id, event, count(*), now() FROM ev GROUP BY 1, 2
		ON CONFLICT (knowledge_id, event)
		DO UPDATE SET count = knowledge_usage.count + EXCLUDED.count, last_at = EXCLUDED.last_at`,
		ids, event, actor.Kind, actor.Name)
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
