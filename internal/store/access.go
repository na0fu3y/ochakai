// Access: the grants that say who may read and write under which
// directory (design doc 0109, migration 0041). The store keeps rows;
// what they mean for one request is the service's question.
package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// AccessPolicy is the whole policy as one value: the rules, and whether
// any rule exists at all.
//
// The two travel together because the second is what decides how the
// first is read. An empty policy is not "nobody may do anything" — it
// is a deployment that never asked for a boundary, and every such
// deployment must keep behaving exactly as it did before this table
// existed (0109 §2). One query answers both, so a request cannot see a
// policy that arrived between two of them.
type AccessPolicy struct {
	Rules  []domain.AccessRule
	Active bool
}

// AccessPolicy reads the whole policy. It is small by construction
// (domain.MaxAccessRules) and read once per request that needs it.
func (s *Store) AccessPolicy(ctx context.Context) (*AccessPolicy, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT prefix, principal, may_write, granted_at, granted_by
		  FROM access_rule
		 ORDER BY prefix, principal`)
	if err != nil {
		return nil, fmt.Errorf("read access policy: %w", err)
	}
	defer rows.Close()
	p := &AccessPolicy{}
	for rows.Next() {
		var r domain.AccessRule
		if err := rows.Scan(&r.Prefix, &r.Principal, &r.MayWrite, &r.GrantedAt, &r.GrantedBy); err != nil {
			return nil, fmt.Errorf("scan access rule: %w", err)
		}
		p.Rules = append(p.Rules, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read access policy: %w", err)
	}
	p.Active = len(p.Rules) > 0
	return p, nil
}

// ReplaceAccessPolicy writes the policy as a whole, in one transaction.
//
// Whole-document replacement rather than row operations: a policy is
// read as a set — "who can see personnel/" is answered by looking at
// all of it — and an operator who edits it edits what they just read.
// Per-row endpoints would make the surface three operations instead of
// one and would still need the whole document to be reviewable, and the
// concurrent-edit problem they appear to avoid is the one If-Match
// already solves for concepts (0030).
//
// granted_at and granted_by are the server's, not the caller's: they
// are an observation of who changed the policy, and a caller that could
// write them could write somebody else's name into the record of a
// grant they made themselves.
func (s *Store) ReplaceAccessPolicy(ctx context.Context, rules []domain.AccessRule, by domain.Actor) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin access policy write: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `DELETE FROM access_rule`); err != nil {
		return fmt.Errorf("clear access policy: %w", err)
	}
	if len(rules) > 0 {
		batch := &pgx.Batch{}
		for _, r := range rules {
			batch.Queue(`
				INSERT INTO access_rule (prefix, principal, may_write, granted_by)
				VALUES ($1, $2, $3, $4)`, r.Prefix, r.Principal, r.MayWrite, by.String())
		}
		if err := tx.SendBatch(ctx, batch).Close(); err != nil {
			return fmt.Errorf("write access policy: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit access policy: %w", err)
	}
	return nil
}
