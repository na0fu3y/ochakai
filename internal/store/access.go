// Access: the grants that say who may read and write under which
// directory (design doc 0109, migration 0041). The store keeps rows;
// what they mean for one request is the service's question.
package store

import (
	"context"
	"fmt"
	"strings"

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
	// Version is the hash of the grants (domain.AccessPolicyVersion),
	// the token a caller echoes in If-Match to replace the policy only
	// if it still says what they read.
	Version string
}

// accessPolicyLockID keys the advisory lock policy writes take, for the
// span of the transaction that reads the current version and replaces
// the rules. Row locks cannot serialize this: the state two callers
// most often race for is the *empty* policy — the first grant on a
// deployment that has none — and there are no rows there to lock. The
// value is arbitrary and only has to be one thing in one place.
const accessPolicyLockID int64 = 0xacce55

// AccessPolicy reads the whole policy. It is small by construction
// (domain.MaxAccessRules) and read once per request that needs it.
func (s *Store) AccessPolicy(ctx context.Context) (*AccessPolicy, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT prefix, principal, may_write, may_admin, granted_at, granted_by
		  FROM access_rule
		 ORDER BY prefix, principal`)
	if err != nil {
		return nil, fmt.Errorf("read access policy: %w", err)
	}
	defer rows.Close()
	p := &AccessPolicy{}
	for rows.Next() {
		var r domain.AccessRule
		if err := rows.Scan(&r.Prefix, &r.Principal, &r.MayWrite, &r.MayAdmin, &r.GrantedAt, &r.GrantedBy); err != nil {
			return nil, fmt.Errorf("scan access rule: %w", err)
		}
		p.Rules = append(p.Rules, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read access policy: %w", err)
	}
	p.Active = len(p.Rules) > 0
	p.Version = domain.AccessPolicyVersion(p.Rules)
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
// A non-nil ifMatch is a precondition: the write lands only while the
// stored policy still hashes to it, and fails with ErrConflict when it
// does not — the same sentinel and the same 412 a concept's stale
// If-Match already answers with (design doc 0030). It is read inside
// this transaction, under the lock, because a version compared before
// the transaction opened would be a precondition with a window under
// it, and the window is the whole thing being closed.
//
// granted_at and granted_by are the server's, not the caller's: they
// are an observation of who changed the policy, and a caller that could
// write them could write somebody else's name into the record of a
// grant they made themselves.
func (s *Store) ReplaceAccessPolicy(ctx context.Context, rules []domain.AccessRule, by domain.Actor,
	ifMatch *string, within []string,
) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin access policy write: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, accessPolicyLockID); err != nil {
		return fmt.Errorf("lock access policy: %w", err)
	}
	current, err := accessRulesTx(ctx, tx)
	if err != nil {
		return err
	}
	// What the caller was looking at, which is what a precondition of
	// theirs is about. A prefix administrator reads and replaces the
	// rules under their own subtrees and never sees the rest, so a
	// version over the whole policy would fail their write whenever a
	// rule they cannot see moved (design doc 0124).
	visible, outside := current, []domain.AccessRule(nil)
	if within != nil {
		visible, outside = partitionByPrefixes(current, within)
	}
	if ifMatch != nil {
		if got := domain.AccessPolicyVersion(visible); got != strings.Trim(*ifMatch, `"`) {
			return fmt.Errorf("%w: access policy is at version %s", ErrConflict, got)
		}
	}
	// The rules outside the caller's reach are carried over, not deleted.
	// They cannot be in the document — the caller never read them — so a
	// document that omits them says nothing about them.
	rules = append(append([]domain.AccessRule{}, rules...), outside...)
	if _, err := tx.Exec(ctx, `DELETE FROM access_rule`); err != nil {
		return fmt.Errorf("clear access policy: %w", err)
	}
	if len(rules) > 0 {
		batch := &pgx.Batch{}
		for _, r := range rules {
			batch.Queue(`
				INSERT INTO access_rule (prefix, principal, may_write, may_admin, granted_by)
				VALUES ($1, $2, $3, $4, $5)`, r.Prefix, r.Principal, r.MayWrite, r.MayAdmin, by.String())
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

// accessRulesTx reads the rules inside a transaction, for the version a
// precondition is compared against. It reads what AccessPolicy reads;
// the order does not matter here, because the version imposes its own.
func accessRulesTx(ctx context.Context, tx pgx.Tx) ([]domain.AccessRule, error) {
	rows, err := tx.Query(ctx, `SELECT prefix, principal, may_write, may_admin FROM access_rule`)
	if err != nil {
		return nil, fmt.Errorf("read access policy: %w", err)
	}
	defer rows.Close()
	var out []domain.AccessRule
	for rows.Next() {
		var r domain.AccessRule
		if err := rows.Scan(&r.Prefix, &r.Principal, &r.MayWrite, &r.MayAdmin); err != nil {
			return nil, fmt.Errorf("scan access rule: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read access policy: %w", err)
	}
	return out, nil
}

// partitionByPrefixes splits rules into the ones a caller administering
// prefixes may see and replace, and the ones that must survive their
// write untouched. A rule is theirs when its own prefix sits at or under
// one of theirs, matched on segment boundaries like every other prefix
// question here (0075 §6) — so an administrator of "teams/growth" holds
// the rules about "teams/growth/metrics" and none about "teams".
//
// The shallower rule is deliberately not theirs even though it governs
// their subtree: they could otherwise widen their own reach by editing
// the rule that contains them.
func partitionByPrefixes(rules []domain.AccessRule, prefixes []string) (inside, outside []domain.AccessRule) {
	for _, r := range rules {
		mine := false
		for _, p := range prefixes {
			if domain.Under(r.Prefix, p) {
				mine = true
				break
			}
		}
		if mine {
			inside = append(inside, r)
		} else {
			outside = append(outside, r)
		}
	}
	return inside, outside
}
