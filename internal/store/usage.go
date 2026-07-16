package store

import (
	"context"
	"time"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// EventTarget names one knowledge entry a usage event refers to.
type EventTarget struct {
	Type domain.Type
	ID   string
}

// eventRetention is how long raw knowledge_event rows are kept. Running
// totals in knowledge_usage survive pruning, so /usage stays accurate.
const eventRetention = 180 * 24 * time.Hour

// RecordEvents appends raw usage events and bumps the running totals in a
// single statement. Callers treat failures as non-fatal: usage recording
// must never fail a read.
func (s *Store) RecordEvents(ctx context.Context, event string, actor domain.Actor, targets []EventTarget) error {
	if len(targets) == 0 {
		return nil
	}
	types := make([]string, len(targets))
	ids := make([]string, len(targets))
	for i, t := range targets {
		types[i] = string(t.Type)
		ids[i] = t.ID
	}
	_, err := s.pool.Exec(ctx, `
		WITH ev AS (
			INSERT INTO knowledge_event (knowledge_type, knowledge_id, event, actor_kind, actor_name)
			SELECT t, i, $3, $4, $5 FROM unnest($1::text[], $2::text[]) AS u(t, i)
			RETURNING knowledge_type, knowledge_id, event
		)
		INSERT INTO knowledge_usage (knowledge_type, knowledge_id, event, count, last_at)
		SELECT knowledge_type, knowledge_id, event, count(*), now() FROM ev GROUP BY 1, 2, 3
		ON CONFLICT (knowledge_type, knowledge_id, event)
		DO UPDATE SET count = knowledge_usage.count + EXCLUDED.count, last_at = EXCLUDED.last_at`,
		types, ids, event, actor.Kind, actor.Name)
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
func (s *Store) Usage(ctx context.Context, typ domain.Type, id string) (*domain.Usage, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT event, count, last_at FROM knowledge_usage WHERE knowledge_type = $1 AND knowledge_id = $2`, typ, id)
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
		}
		if u.LastUsedAt == nil || lastAt.After(*u.LastUsedAt) {
			t := lastAt
			u.LastUsedAt = &t
		}
	}
	return u, rows.Err()
}
