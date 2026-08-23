// Access: what this caller may read and write (design doc 0109).
//
// This is the one place ochakai looks at who is calling in order to
// decide what happens, rather than only to record it (0065 §1). It sits
// in the service package for the reason read-only does: REST and MCP
// both come through here, so a write path added later is covered
// without its author having to know a middleware exists (0066 §2).
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/httpauth"
	"github.com/na0fu3y/ochakai/internal/store"
)

// ErrForbidden is returned when the caller is inside the bundle but
// outside what this operation needs: a write under a directory they may
// only read, or a whole-bundle operation without being an administrator.
//
// A read of something outside the caller's scope does not come back
// this way — it comes back as store.ErrNotFound, because a 403 on a
// path is an answer to "does this exist", and the deployments that
// asked for this feature asked so that the answer would be no (0109 §4).
var ErrForbidden = errors.New("this caller may not do that here")

// Scope is what one caller may do, resolved once per operation.
type Scope struct {
	// Unscoped is a deployment with no policy at all — the default, and
	// every deployment that existed before 0109. Everything is allowed
	// and nothing is filtered: the feature is off until a first grant
	// turns it on.
	Unscoped bool
	// AdminPrefixes are the subtrees whose rules this caller may edit,
	// from the may_admin grants that name them (design doc 0124). Empty
	// for everyone else, including a full administrator — Admin above is
	// the wider fact and is checked first.
	AdminPrefixes []string
	// Admin is a principal named by OCHAKAI_ADMINS. Administrators may
	// read and write everything and are the only callers who may run the
	// operations that take the bundle as a whole (§3).
	Admin bool
	// Read and Write are the prefixes granted to this principal. Write
	// prefixes also appear in Read: writing implies reading.
	Read  []string
	Write []string
}

// Everything reports whether this scope has no boundary in it.
func (sc *Scope) Everything() bool { return sc.Unscoped || sc.Admin }

// MayRead reports whether id is inside the caller's read scope.
func (sc *Scope) MayRead(id string) bool { return sc.Everything() || under(id, sc.Read) }

// MayWrite reports whether id is inside the caller's write scope.
func (sc *Scope) MayWrite(id string) bool { return sc.Everything() || under(id, sc.Write) }

func under(id string, prefixes []string) bool {
	for _, p := range prefixes {
		if domain.Under(id, p) {
			return true
		}
	}
	return false
}

// scope resolves what the calling actor may do. It reads the policy on
// every operation that needs it, and deliberately does not cache: a
// grant that was withdrawn is withdrawn, and a cache would put a window
// on that in which the product's answer to "have you revoked Tanaka" is
// "almost".
func (s *Service) scope(ctx context.Context) (*Scope, error) {
	principal := domain.PrincipalOf(httpauth.Actor(ctx))
	if s.Config != nil && matches(principal, s.Config.Admins) {
		return &Scope{Admin: true}, nil
	}
	// A service with no store holds no policy and no knowledge either:
	// the same nil-tolerance readOnly has for Config, and the shape the
	// surface tests build when they are asking a question about tool
	// schemas rather than about data.
	if s.Store == nil {
		return &Scope{Unscoped: true}, nil
	}
	p, err := s.Store.AccessPolicy(ctx)
	if err != nil {
		// A policy that cannot be read is not an open door. Refusing is
		// the only safe direction: the alternative fails open on exactly
		// the deployment that asked not to.
		return nil, err
	}
	if !p.Active {
		return &Scope{Unscoped: true}, nil
	}
	sc := &Scope{}
	for _, r := range p.Rules {
		if r.Principal != domain.AnyPrincipal && r.Principal != principal {
			continue
		}
		sc.Read = append(sc.Read, r.Prefix)
		if r.MayWrite {
			sc.Write = append(sc.Write, r.Prefix)
		}
		if r.MayAdmin {
			sc.AdminPrefixes = append(sc.AdminPrefixes, r.Prefix)
		}
	}
	return sc, nil
}

func matches(principal string, allowed []string) bool {
	for _, a := range allowed {
		if a == domain.AnyPrincipal || a == principal {
			return true
		}
	}
	return false
}

// mayRead refuses a read outside the caller's scope as if the address
// held nothing.
func (s *Service) mayRead(ctx context.Context, id string) error {
	sc, err := s.scope(ctx)
	if err != nil {
		return err
	}
	if !sc.MayRead(id) {
		return store.ErrNotFound
	}
	return nil
}

// mayWrite refuses a write outside the caller's write scope. Outside
// the read scope too, the refusal is still the 404 — saying "you may
// not write here" would confirm what is stored at an address the caller
// cannot see.
func (s *Service) mayWrite(ctx context.Context, id string) error {
	sc, err := s.scope(ctx)
	if err != nil {
		return err
	}
	return mayWriteIn(sc, id)
}

// mayWriteIn is mayWrite against a scope already in hand, for the caller
// that needs more from the scope than one id's answer (Move, 0129).
func mayWriteIn(sc *Scope, id string) error {
	switch {
	case sc.MayWrite(id):
		return nil
	case sc.MayRead(id):
		return fmt.Errorf("%w: %s is readable but not writable by this caller", ErrForbidden, id)
	default:
		return store.ErrNotFound
	}
}

// RequireAdmin gates what is left of the operations that take the
// bundle as a whole: the archive of the root, re-embedding, and the
// policy itself (§3).
//
// These are refused rather than narrowed, because narrowing them costs
// more than it buys: an export that quietly omitted what the caller
// cannot see would break the one invariant the bundle has — what goes
// in comes out (0075 §1). Whoever needs the whole bundle can be given
// the whole bundle.
//
// The list has shrunk three times, each by a record that found a way to
// return a part without it being mistaken for the whole: the statistics
// declare what they counted (0123), a subtree's archive names itself
// (0127), and a move runs when its rewrite fits inside the caller's
// scope (0129). What remains is what nobody has found that answer for.
// On a deployment with no rules every scope is Everything, so passing
// here does not mean the caller is an administrator. The two callers
// that need that narrower fact — writing the policy (0122) and editing
// one subtree of it (0124) — read the scope themselves rather than
// through this, as does the one that needs a wider fact than an id,
// move (0129).
func (s *Service) RequireAdmin(ctx context.Context, op string) error {
	sc, err := s.scope(ctx)
	if err != nil {
		return err
	}
	if sc.Everything() {
		return nil
	}
	return fmt.Errorf("%w: %s takes the whole bundle, which is an administrator's operation", ErrForbidden, op)
}

// MayArchive decides who may take the OKF archive at a bundle path
// (design doc 0127).
//
// **The whole bundle is the administrator's**, and asking for the root
// is how it is asked for: an archive of everything a scoped caller can
// see would be the shape 0109 §3 refused, a part of a base with nothing
// naming the whole it came from.
//
// **A subtree belongs to whoever may read it.** The archive carries the
// concepts and files that caller could already fetch one at a time, so
// refusing it withheld convenience rather than knowledge — and now that
// the archive says which subtree it is, in its root index.md and in its
// own filename, taking one no longer produces something that can be
// mistaken for a backup of the base.
func (s *Service) MayArchive(ctx context.Context, prefix string) error {
	sc, err := s.scope(ctx)
	if err != nil {
		return err
	}
	if sc.Everything() {
		return nil
	}
	if prefix == "" {
		return fmt.Errorf("%w: the archive of the whole bundle is an administrator's operation; ask for a "+
			"directory this caller may read and the archive of it says which one it is (design doc 0127)",
			ErrForbidden)
	}
	if !sc.MayRead(prefix) {
		// The same answer a read outside the scope gets: whether
		// something is at this address is the boundary itself (0109 §4).
		return store.ErrNotFound
	}
	return nil
}

// narrow forces the caller's read scope onto a filter, intersecting it
// with whatever prefixes the caller asked for. The bool is false when
// nothing is left — the caller asked about a subtree they cannot see,
// and the honest answer is an empty listing rather than an error, for
// the same reason a read is a 404: an error would say the subtree is
// there.
func (s *Service) narrow(ctx context.Context, f store.Filter) (store.Filter, bool, error) {
	sc, err := s.scope(ctx)
	if err != nil {
		return f, false, err
	}
	if sc.Everything() {
		return f, true, nil
	}
	if len(sc.Read) == 0 {
		return f, false, nil
	}
	if len(f.Prefixes) == 0 {
		f.Prefixes = sc.Read
		return f, true, nil
	}
	f.Prefixes = intersect(f.Prefixes, sc.Read)
	return f, len(f.Prefixes) > 0, nil
}

// intersect keeps the narrower of each overlapping pair. Two prefixes
// overlap only when one contains the other — segment matching leaves no
// third case — so the intersection of the sets is the set of the
// narrower ones, and a requested prefix outside every granted one
// simply drops.
func intersect(requested, granted []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, want := range requested {
		for _, have := range granted {
			var keep string
			switch {
			case domain.Under(want, have):
				keep = want
			case domain.Under(have, want):
				keep = have
			default:
				continue
			}
			if !seen[keep] {
				seen[keep] = true
				out = append(out, keep)
			}
		}
	}
	return out
}

// scopeRows drops the rows of a derived listing that the caller may not
// read. It is the last line of defence for the answers assembled from
// more than one query — the backlinks a read carries, the level a
// browse returns — where the filter that narrows a search does not
// reach.
func scopeRows[T any](sc *Scope, rows []T, id func(T) string) []T {
	if sc.Everything() {
		return rows
	}
	out := rows[:0]
	for _, r := range rows {
		if sc.MayRead(id(r)) {
			out = append(out, r)
		}
	}
	return out
}

// Policy returns the whole access policy and its version. An
// administrator's read: the rules name people and the directories they
// may see, which is a map of the boundaries a scoped caller is on one
// side of. The version is what a caller echoes in If-Match to replace
// the policy it just read (design doc 0030).
func (s *Service) Policy(ctx context.Context) ([]domain.AccessRule, string, error) {
	sc, err := s.scope(ctx)
	if err != nil {
		return nil, "", err
	}
	if !sc.Everything() && len(sc.AdminPrefixes) == 0 {
		if err := s.RequireAdmin(ctx, "reading the access policy"); err != nil {
			return nil, "", err
		}
	}
	p, err := s.Store.AccessPolicy(ctx)
	if err != nil {
		return nil, "", err
	}
	if sc.Everything() {
		return p.Rules, p.Version, nil
	}
	// A prefix administrator reads the rules they may edit and no
	// others: the rest name people and directories they cannot see, which
	// is the same answer 0109 §4 gives a scoped read. The version covers
	// what was read, so it is the precondition for replacing it (0124).
	mine, _ := partitionRules(p.Rules, sc.AdminPrefixes)
	return mine, domain.AccessPolicyVersion(mine), nil
}

// partitionRules is the service's copy of the split the store makes
// inside its transaction: a rule belongs to a prefix administrator when
// its own prefix sits at or under one of theirs. The shallower rule that
// governs their subtree is deliberately not theirs — editing the rule
// that contains you is how a subtree grows into the bundle.
func partitionRules(rules []domain.AccessRule, prefixes []string) (inside, outside []domain.AccessRule) {
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

// SetPolicy replaces the whole access policy.
//
// Refused on a read-only deployment like any other write. The policy is
// not knowledge, but a deployment that says it does not change is a
// deployment whose boundaries do not move either — and a read-only
// instance whose access rules could still be edited would be one where
// the frozen half is the half that matters least.
// A non-nil ifMatch replaces the policy only while it still has that
// version, and fails with a conflict when it does not. The policy is
// one document that is read, edited and put back whole, so two callers
// placing a grant each would otherwise silently drop one of them —
// which is the write a deployment that provisions boundaries
// automatically makes most often (design doc 0120).
//
// Writing rules is an administrator's own operation, not merely one an
// administrator's check happens to pass: the first rule hands the
// policy to OCHAKAI_ADMINS, so a caller outside that list is refused
// before anything is written rather than after (design doc 0122).
func (s *Service) SetPolicy(ctx context.Context, rules []domain.AccessRule, actor domain.Actor,
	ifMatch *string,
) ([]domain.AccessRule, string, error) {
	if err := s.readOnly(); err != nil {
		return nil, "", err
	}
	sc, err := s.scope(ctx)
	if err != nil {
		return nil, "", err
	}
	// Two ways to be allowed here, and the narrower one arrived second.
	// A prefix administrator edits the rules under the subtrees a full
	// administrator granted them (design doc 0124); everyone else needs
	// the whole bundle, which is what adminScope's refusal says.
	if !sc.Everything() && len(sc.AdminPrefixes) == 0 {
		if err := s.RequireAdmin(ctx, "editing the access policy"); err != nil {
			return nil, "", err
		}
	}
	checked, err := domain.ValidateAccessRules(rules)
	if err != nil {
		return nil, "", Invalidf("%v", err)
	}
	// The floor under a policy: administrators come from the deployment's
	// configuration, so a policy can never be written that nobody can
	// undo. Writing the first rule on a deployment that named no
	// administrator is refused here rather than at startup, where the
	// same condition is also checked — this is the moment it is about to
	// become true, and refusing it is cheaper than telling an operator
	// their next deploy will not come up.
	if len(checked) > 0 && (s.Config == nil || len(s.Config.Admins) == 0) {
		return nil, "", Invalidf("this deployment names no administrators, so a policy written now could not be " +
			"edited or undone by anyone: set OCHAKAI_ADMINS first (design doc 0109 §3)")
	}
	// The same floor, read from the caller's side. On a deployment with
	// no rules every caller's scope is Everything — that is 0109 §2's
	// promise that no grants means no boundary — so the check above
	// cannot yet tell an administrator from anybody else. The first rule
	// ends that: from the moment it lands the policy belongs to
	// OCHAKAI_ADMINS, and a caller outside that list has just written a
	// door they are on the wrong side of (design doc 0122).
	//
	// Refused here, before the write, and not by the read that follows
	// it. Deciding on the way out is how this arrived: the rows landed,
	// the log said the policy was replaced, and the caller was told 403
	// — an answer that says nothing was written.
	if len(checked) > 0 && !sc.Admin && len(sc.AdminPrefixes) == 0 {
		return nil, "", fmt.Errorf("%w: a policy is edited by the principals OCHAKAI_ADMINS names, and %s is "+
			"not one of them; writing the first rule here would lock this caller out of undoing it "+
			"(design doc 0109 §3)", ErrForbidden, domain.PrincipalOf(httpauth.Actor(ctx)))
	}
	// A prefix administrator's document describes their own subtrees and
	// can say nothing about the rest, so every rule in it has to sit
	// under one of them. Refused rather than dropped: a rule silently
	// discarded is a boundary the caller believes they placed
	// (design doc 0124).
	var within []string
	if !sc.Everything() {
		within = sc.AdminPrefixes
		if _, outside := partitionRules(checked, within); len(outside) > 0 {
			return nil, "", fmt.Errorf(
				"%w: %q is outside the directories this caller administers (%s); a rule there is placed by an "+
					"administrator of it, or by one OCHAKAI_ADMINS names (design doc 0124)",
				ErrForbidden, displayPrefix(outside[0].Prefix), strings.Join(within, ", "))
		}
	}
	if err := s.Store.ReplaceAccessPolicy(ctx, checked, actor, ifMatch, within); err != nil {
		return nil, "", err
	}
	s.Log.Info("access policy replaced", "rules", len(checked), "actor", actor.String(),
		"within", strings.Join(within, ","))
	// Read back rather than call Policy: the write has landed, and a
	// second admin check on the way out could only turn a write that
	// happened into a refusal that says it did not (0122). What the
	// caller gets is what is stored — the version included, since
	// granted_at and granted_by are the store's to fill (0120 §3).
	p, err := s.Store.AccessPolicy(ctx)
	if err != nil {
		return nil, "", err
	}
	if within == nil {
		return p.Rules, p.Version, nil
	}
	mine, _ := partitionRules(p.Rules, within)
	return mine, domain.AccessPolicyVersion(mine), nil
}

// displayPrefix names the root the way the rest of this surface does,
// so a refusal about it does not read as a refusal about an empty
// string.
func displayPrefix(p string) string {
	if p == "" {
		return "/"
	}
	return p
}
