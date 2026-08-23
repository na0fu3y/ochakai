// Access is the policy that says which principals may read and write
// under which directories (design doc 0109). It is the one place in
// ochakai where the caller's identity decides what the caller may do:
// everywhere else identity is recorded as provenance and the judgement
// is left to whoever reads the concept (0065 §1).
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// AccessRule is one grant: principal may read under Prefix, and may
// write there when MayWrite. There is no deny rule — see migration
// 0041 for why the table holds grants only.
type AccessRule struct {
	// Prefix is a bundle path with no trailing slash, or "" for the
	// root. It is matched on segment boundaries, like the search filter
	// (0075 §6): "metrics" covers "metrics/revenue" and does not cover
	// "metrics-legacy/churn".
	Prefix string `json:"prefix"`
	// Principal is the actor spelling the ledger uses — "human:name" or
	// "process:name" (0065 §2) — or "*" for every authenticated caller,
	// the same wildcard OCHAKAI_DELEGATING_CALLERS already spells that
	// way.
	Principal string `json:"principal"`
	// MayWrite grants writing as well as reading. Writing implies
	// reading: a principal that could change a concept it cannot read
	// would be writing blind, and the read it would need to do the job
	// safely (If-Match) would be the thing refused.
	MayWrite bool `json:"may_write"`

	GrantedAt time.Time `json:"granted_at,omitzero"`
	GrantedBy string    `json:"granted_by,omitempty"`
}

// AnyPrincipal is the wildcard that matches every authenticated caller.
const AnyPrincipal = "*"

// MaxAccessRules bounds a policy. A boundary somebody can hold in their
// head is the product being sold here; past this many rows the answer
// to "who can read this" stops being readable, and the deployment that
// needs thousands of grants needs a second deployment (0065 §1) rather
// than a longer table.
const MaxAccessRules = 1000

// ValidPrincipal reports whether s can name a principal in a grant.
// The wildcard and the two actor kinds are all that can appear; a bare
// email is refused rather than guessed at, because "tanaka@example.com"
// and "human:tanaka@example.com" would otherwise be two spellings of a
// grant, and only one of them would ever match.
func ValidPrincipal(s string) bool {
	if s == AnyPrincipal {
		return true
	}
	kind, name, ok := strings.Cut(s, ":")
	return ok && ValidActorKind(kind) && name != "" && len(s) <= 320 &&
		!strings.ContainsAny(s, " \t\r\n")
}

// PrincipalOf spells an actor the way a grant names it. The via and
// producer that Actor.String carries are deliberately absent: a
// delegated write is scoped as the end user it names, which is what
// forwarding the identity was for (0065 §3).
func PrincipalOf(a Actor) string { return a.Kind + ":" + a.Name }

// Under reports whether the bundle path id sits at or beneath prefix,
// matched on segment boundaries. The empty prefix is the root and
// covers everything.
//
// The segment boundary is the whole point and not a nicety: a plain
// string prefix would let a directory created next door — "metrics" and
// "metrics-legacy" — fall inside somebody else's scope, and in a scope
// that decides who may read, that is the most expensive shape of error
// there is (0075 §6).
func Under(id, prefix string) bool {
	if prefix == "" {
		return true
	}
	return id == prefix || strings.HasPrefix(id, prefix+"/")
}

// ValidateAccessRules checks a whole policy and returns it sorted, so
// that the document a caller reads back is the document any other
// caller reads. Duplicate (prefix, principal) pairs are an error rather
// than a last-one-wins merge: the pair is the table's primary key, and
// a policy that silently dropped half of what was sent would be a
// policy nobody could verify by reading it back.
func ValidateAccessRules(rules []AccessRule) ([]AccessRule, error) {
	if len(rules) > MaxAccessRules {
		return nil, fmt.Errorf("policy has %d rules, the maximum is %d", len(rules), MaxAccessRules)
	}
	seen := make(map[string]bool, len(rules))
	out := make([]AccessRule, 0, len(rules))
	for _, r := range rules {
		r.Prefix = Normalize(strings.Trim(strings.TrimSpace(r.Prefix), "/"))
		r.Principal = strings.TrimSpace(r.Principal)
		if r.Prefix != "" && !ValidIDPrefix(r.Prefix) {
			return nil, fmt.Errorf("invalid prefix %q", r.Prefix)
		}
		if !ValidPrincipal(r.Principal) {
			return nil, fmt.Errorf("invalid principal %q: spell it human:<name>, process:<name>, or %q",
				r.Principal, AnyPrincipal)
		}
		key := r.Prefix + "\x00" + r.Principal
		if seen[key] {
			return nil, fmt.Errorf("duplicate rule for principal %q under %q", r.Principal, r.Prefix)
		}
		seen[key] = true
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Prefix != out[j].Prefix {
			return out[i].Prefix < out[j].Prefix
		}
		return out[i].Principal < out[j].Principal
	})
	return out, nil
}

// AccessPolicyVersion is the version of a whole policy: the hash of its
// grants, in a canonical order, hex-encoded. A caller echoes it in
// If-Match to replace the policy only if it still says what they read
// (design docs 0030, 0109).
//
// What is hashed is the grants and nothing else. granted_at and
// granted_by move every time the policy is written, including a write
// that changes no grant, and a version that moved then would invalidate
// a precondition somebody is holding against a policy that still means
// exactly what they read — the shape etagOf already refused for
// concepts, where verifying or attaching a file leaves a held
// precondition valid because only content moves the hash.
//
// The order is imposed here rather than taken from the read. The rules
// arrive sorted from both sides — ValidateAccessRules sorts in Go, the
// store's query in SQL — and those two orders are not the same
// function: a database collation decides the second, so two deployments
// could hash one policy two ways and neither would be wrong. A version
// is a promise about a set, so the set is put in one order first.
//
// An empty policy has a version like any other, and that is the point:
// the first grant on a deployment that has none is exactly the write
// two automated callers can race, and a precondition needs something to
// name for the state they both read.
func AccessPolicyVersion(rules []AccessRule) string {
	sorted := make([]AccessRule, len(rules))
	copy(sorted, rules)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Prefix != sorted[j].Prefix {
			return sorted[i].Prefix < sorted[j].Prefix
		}
		return sorted[i].Principal < sorted[j].Principal
	})
	h := sha256.New()
	for _, r := range sorted {
		write := "r"
		if r.MayWrite {
			write = "rw"
		}
		fmt.Fprintf(h, "%s\x00%s\x00%s\n", r.Prefix, r.Principal, write)
	}
	return hex.EncodeToString(h.Sum(nil))
}
