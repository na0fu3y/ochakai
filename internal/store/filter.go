// Filter is what narrows a read — the same struct for a search, a listing
// and get_context, so a filter learned once means the same thing
// everywhere. Everything here builds the WHERE clause; nothing here
// decides an order.
package store

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// Filter narrows search and list operations.
type Filter struct {
	Types    []domain.Type
	Statuses []domain.Status
	Tags     []string
	// Source narrows to entries citing this exact resource — the reverse
	// of sources[].resource, for "what derives from this material"
	// (design doc 0037 §2.3). Single-valued on purpose: the question is
	// about one artifact that changed, and a disjunction of containment
	// tests would not use the GIN index.
	Source string
	// LinksTo narrows to entries whose body links at this id — the
	// reverse of the edge design doc 0024 derives from the markdown,
	// and the question the web UI asks as "linked from". Single-valued
	// like Source and for the same reason: it is about one entry, and
	// the answer is a set rather than a ranking.
	LinksTo string
	// Prefixes narrow to entries addressed under one of these paths —
	// the id is the address (design doc 0017), so this is how a caller
	// scopes a search to a team's subtree, the company-wide one, or both
	// (design doc 0041). Repeatable and OR-ed, unlike Source: the ordinary
	// question is "my scope and the shared one", and splitting that into
	// two calls would leave the caller merging two incomparable rankings.
	// Values arrive normalized by the service — NFC, no trailing slash.
	Prefixes []string

	// Trust and Rejected ask about the instance ledgers rather than the
	// document (design docs 0043 §§3.2-3.3, 0046 §3.10). Trust is empty
	// when the question is not asked, and OR-ed like Statuses when it is;
	// Rejected is tri-state, and nil still hides rejected entries — the
	// default is not "no constraint" but "not the ones we said no to".
	Trust    []domain.Trust
	Rejected *bool

	// Frontmatter narrows by a frontmatter key read out of the jsonb
	// index, which is what keeps the query surface additive: a key OKF
	// adds in a later version is askable with no column and no migration
	// (design doc 0046 §3.11). Which keys those are is the service's
	// question, not the store's — OKF's own, less the ones a named filter
	// answers (design doc 0047). Every pair must match (AND), matching a
	// scalar exactly or a list by membership.
	// Values are text, and a value that spells a number or a boolean
	// matches the typed frontmatter as well as the text: YAML types what
	// it parses, so `required: true` is indexed as a boolean and would
	// otherwise be unaskable (frontmatterContainment).
	//
	// Exact and membership are the only two, deliberately: ranges,
	// negation and boolean expressions are the doorway to a query
	// language, and ochakai is a knowledge store whose answers are
	// entries rather than a database whose answers are rows (0046 §5).
	Frontmatter map[string]string
}

// buildWhere renders filter conditions; prefix qualifies columns (e.g. "k.")
// for joined queries and may be empty. Rejected entries are excluded unless
// asked for: knowledge that was never accepted must not resurface in
// answers, but remains queryable on request so agents can check whether a
// proposal was already rejected.
func (f Filter) buildWhere(prefix string) (string, []any) {
	// A file is an object in the same table now (design doc 0046 §3.13),
	// and it is not an entry: no type, no title, nothing to rank. The id
	// is what a concept has and a file has not, so every scan that
	// returns entries says so here — once, where every filtered read goes
	// through, rather than in each of them.
	conds := []string{prefix + "deleted_at IS NULL", prefix + "id IS NOT NULL"}
	var args []any
	if len(f.Types) > 0 {
		// Types match case-insensitively (design doc 0023 §3.3): the stored
		// spelling is whatever the writer used, so both sides fold. There is
		// no index on type, so lower() costs nothing here.
		folded := make([]string, len(f.Types))
		for i, t := range f.Types {
			folded[i] = domain.FoldType(t)
		}
		args = append(args, folded)
		conds = append(conds, fmt.Sprintf("lower(%stype) = ANY($%d)", prefix, len(args)))
	}
	if len(f.Statuses) > 0 {
		args = append(args, f.Statuses)
		conds = append(conds, fmt.Sprintf("%sstatus = ANY($%d)", prefix, len(args)))
	}
	// Rejection and verification are ledgers now, so both are asked about
	// independently of the lifecycle value (design doc 0043 §§3.2-3.3).
	// The rejected default is "hide", which is where it has always been —
	// but it used to fall out of the status filter, and a filter that
	// named any status turned the exclusion off with it. It no longer
	// does: "show me the drafts" never meant "including the ones we
	// turned down".
	//
	// The outer id must be qualified even when nothing else here is: the
	// ledger tables have an id column of their own, so a bare "id" inside
	// the subquery binds to the ledger's, making the condition r.id = r.id
	// — true for every row, which silently empties every unaliased query.
	outer := prefix
	if outer == "" {
		outer = "object."
	}
	rejectedExists := fmt.Sprintf(
		"EXISTS (SELECT 1 FROM knowledge_rejection r WHERE r.id = %sid)", outer)
	if f.Rejected != nil && *f.Rejected {
		conds = append(conds, rejectedExists)
	} else {
		conds = append(conds, "NOT "+rejectedExists)
	}
	if len(f.Trust) > 0 {
		// SPEC §5.3's tiers as predicates over the ledger: a person makes
		// an entry human-reviewed, any other confirmation
		// machine-confirmed, none unverified. Asking for several ORs them,
		// so "anything somebody confirmed" is two values rather than a
		// second kind of filter.
		anyVerification := fmt.Sprintf(
			"EXISTS (SELECT 1 FROM knowledge_verification v WHERE v.id = %sid)", outer)
		byHuman := fmt.Sprintf(
			"EXISTS (SELECT 1 FROM knowledge_verification v WHERE v.id = %sid AND v.by_kind = 'human')", outer)
		var tiers []string
		for _, t := range f.Trust {
			switch t {
			case domain.TrustUnverified:
				tiers = append(tiers, "NOT "+anyVerification)
			case domain.TrustMachine:
				tiers = append(tiers, anyVerification+" AND NOT "+byHuman)
			case domain.TrustHuman:
				tiers = append(tiers, byHuman)
			default:
				// A tier nobody defines matches nothing. Dropping it
				// instead would answer a question the caller did not ask
				// — and answer it with everything.
				tiers = append(tiers, "false")
			}
		}
		if len(tiers) > 0 {
			conds = append(conds, "("+strings.Join(tiers, ") OR (")+")")
		}
	}
	if len(f.Tags) > 0 {
		args = append(args, f.Tags)
		conds = append(conds, fmt.Sprintf("%stags && $%d", prefix, len(args)))
	}
	// Containment against the frontmatter index, once per shape the value
	// could have been written in (frontmatterContainment). Every shape is
	// answered by the same GIN index, so the alternatives cost a wider
	// index probe rather than a scan.
	for _, key := range sortedKeys(f.Frontmatter) {
		forms := frontmatterContainment(key, f.Frontmatter[key])
		if len(forms) == 0 {
			continue
		}
		alts := make([]string, 0, len(forms))
		for _, form := range forms {
			args = append(args, form)
			alts = append(alts, fmt.Sprintf("%sfrontmatter @> $%d", prefix, len(args)))
		}
		conds = append(conds, "("+strings.Join(alts, " OR ")+")")
	}
	if f.Source != "" {
		// Containment, so the GIN index on sources answers it directly.
		// Matching is on resource alone: sources[].id is a key for the
		// body's footnotes within one document, and means nothing across
		// entries (design doc 0037 §2.3).
		args = append(args, sourceContainment(f.Source))
		conds = append(conds, fmt.Sprintf("%ssources @> $%d", prefix, len(args)))
	}
	if f.LinksTo != "" {
		// The same containment the dedicated backlinks query used before
		// this became a filter. There is no index on links, and there was
		// none then either: the predicate is unchanged, and what changed
		// is that it now composes with the rest of the filter instead of
		// living in a query of its own.
		args = append(args, linkContainment(f.LinksTo))
		conds = append(conds, fmt.Sprintf("%slinks @> $%d", prefix, len(args)))
	}
	if cond, prefixArgs := prefixScope(prefix+"id", f.Prefixes, len(args)+1); cond != "" {
		args = append(args, prefixArgs...)
		conds = append(conds, cond)
	}
	return strings.Join(conds, " AND "), args
}

// prefixScope is the subtree condition on its own, over whichever column
// holds the id. buildWhere is the whole of a listing's filter and carries
// defaults a tally must not inherit — it hides rejected entries, and
// stats counts them — so the one predicate the two do share is written
// once here rather than twice, differently. Two faces that disagree about
// what "teams/growth" covers is the failure this exists to prevent.
//
// Matching is on segment boundaries, not raw string prefixes: a path is a
// sequence of segments, so "metrics" covers "metrics" itself and
// everything under "metrics/" while leaving "metrics-legacy/churn" alone.
// A raw prefix would pull a neighbouring directory into a scope the
// caller drew around one (design doc 0041 §2.2). LIKE stays out for the
// same reason browsing avoids it — ids may contain "_", which LIKE reads
// as a wildcard.
//
// argN is the placeholder number the caller's next argument takes; the
// empty prefix list is no condition at all.
func prefixScope(column string, prefixes []string, argN int) (string, []any) {
	if len(prefixes) == 0 {
		return "", nil
	}
	return fmt.Sprintf(
		`EXISTS (SELECT 1 FROM unnest($%d::text[]) AS p
			WHERE %s = p OR left(%s, length(p) + 1) = p || '/')`,
		argN, column, column), []any{prefixes}
}

// frontmatterContainment builds the jsonb documents that answer one
// `fm.<key>=<value>` pair; the entry matches when it contains any of
// them.
//
// A value the document wrote as a scalar and one it wrote inside a list
// are both "the entry says k is v", and a caller asking `fm.tags=finance`
// should not have to know which shape the writer chose.
//
// Both shapes are tried a second time when the value spells a JSON number
// or boolean, because YAML types what it parses: `required: true` reaches
// the index as the boolean true and `usage_count: 5` as the number 5,
// while a filter value is always text. Text alone made every key whose
// value is not a string unaskable — and unaskable silently, since a
// containment test that matches nothing is zero rows rather than an
// error, which is the wrong answer that looks like a valid one. Trying
// both keeps §3.11's claim true for those keys: the day the spec adds
// one, it can be asked for.
//
// It is not new syntax. `fm.n=5` is still one key and one value, it still
// finds a document that wrote 5 as the string "5", and the comparison is
// still exact match or list membership — the operators 0046 §5 keeps out
// stay out.
func frontmatterContainment(key, value string) [][]byte {
	forms := []any{value, []string{value}}
	if typed, ok := jsonScalar(value); ok {
		forms = append(forms, typed, []json.RawMessage{typed})
	}
	out := make([][]byte, 0, len(forms))
	for _, form := range forms {
		b, err := json.Marshal(map[string]any{key: form})
		if err != nil { // a string, a number and a boolean all marshal
			continue
		}
		out = append(out, b)
	}
	return out
}

// jsonScalar returns value's typed JSON spelling and reports whether it
// has one. A number and the two booleans do, and nothing else does:
// those are the YAML scalars that reach the frontmatter index as
// something other than text (okf.yamlScalar renders a date back as a
// string, so a date needs no second form).
//
// An object and an array are deliberately not read. A filter value is a
// value, and one that could carry a structure would be the query language
// 0046 §5 closed the door on, arriving through the value instead of
// through an operator.
//
// The text is passed through verbatim rather than decoded and re-encoded,
// so an integer longer than a float64 can hold keeps its digits.
func jsonScalar(value string) (json.RawMessage, bool) {
	switch value {
	case "true", "false":
		return json.RawMessage(value), true
	}
	// A leading digit or minus is what separates a number from the rest of
	// the grammar: json.Valid also accepts `"5"`, `[5]` and `null`, none of
	// which is a scalar a producer wrote unquoted.
	if value == "" || !json.Valid([]byte(value)) {
		return nil, false
	}
	if c := value[0]; c != '-' && (c < '0' || c > '9') {
		return nil, false
	}
	return json.RawMessage(value), true
}

// sourceContainment builds the jsonb value that matches an entry citing
// resource. Marshalled rather than formatted, so a resource containing a
// quote or a backslash cannot break out of the literal.
func sourceContainment(resource string) []byte {
	b, err := json.Marshal([]map[string]string{{"resource": resource}})
	if err != nil { // a string always marshals
		panic(fmt.Sprintf("marshalling source filter %q: %v", resource, err))
	}
	return b
}

// linkContainment builds the jsonb document that matches an entry whose
// links name the given target — the containment the reverse lookup asks
// with.
func linkContainment(target string) []byte {
	b, err := json.Marshal([]map[string]string{{"target": target}})
	if err != nil { // a string always marshals
		panic(fmt.Sprintf("marshalling links filter %q: %v", target, err))
	}
	return b
}
