package domain

import (
	"path"
	"strings"
)

// PathFieldKeys are the top-level frontmatter keys whose values name a
// path (OKF SPEC §6.2): resource, sources[].resource, computation,
// executor.resource and attester.resource. A value there is an absolute
// URL, a bundle-relative path beginning with "/", or a path relative to
// the concept's own directory — the same three forms a body link takes,
// read by the same rules.
var PathFieldKeys = []string{"resource", "sources", "computation", "executor", "attester"}

// PathMove is one rename a move performs, as bundle paths: either one
// object (Old "metrics/revenue.md") or everything under a directory (Old
// "metrics/revenue/", with its trailing slash).
type PathMove struct {
	Old, New string
}

// ConceptMoves are the renames moving one concept performs: its own
// document, and the namespace directory whose files go with it (design
// doc 0075 §5).
func ConceptMoves(oldID, newID string) []PathMove {
	return []PathMove{
		{Old: ConceptPath(oldID), New: ConceptPath(newID)},
		{Old: oldID + "/", New: newID + "/"},
	}
}

// PrefixMoves is the rename moving a directory performs: everything
// under it (design doc 0132).
func PrefixMoves(oldPrefix, newPrefix string) []PathMove {
	return []PathMove{{Old: oldPrefix + "/", New: newPrefix + "/"}}
}

// MovePathValue rewrites one path-valued field of the concept at id so it
// follows moves, and reports whether it changed. A value that names
// something a move carried comes back bundle-absolute, the form a move
// cannot reinterpret — the rule body links follow (RewriteBodyLinks).
//
// absolutize is for the concept that moved itself: its relative values
// were written against the directory it used to sit in (id is that old
// id), so each one comes back absolute whether or not its target moved,
// or it would start meaning something else in the new directory
// (AbsolutizeBodyLinks says the same of its body).
//
// A URL, an empty value and a path that climbs out of the bundle are left
// alone, and so, in practice, is a scope descriptor in sources[].resource
// ("all queries in BigQuery project X"): read as a relative path it names
// nothing a move carries, and only a value a move carries is rewritten.
func MovePathValue(id, value string, moves []PathMove, absolutize bool) (string, bool) {
	target, frag := splitFragment(strings.TrimSpace(value))
	if target == "" || schemeRe.MatchString(target) {
		return value, false
	}
	var p string
	if abs, ok := strings.CutPrefix(target, "/"); ok {
		p = path.Clean(abs)
	} else {
		dir := path.Dir(id)
		if dir == "." {
			dir = ""
		}
		p = path.Join(dir, target)
	}
	if p == "." || p == "" || strings.HasPrefix(p, "..") {
		return value, false
	}
	for _, m := range moves {
		switch {
		case strings.HasSuffix(m.Old, "/") && strings.HasPrefix(p, m.Old):
			return "/" + m.New + strings.TrimPrefix(p, m.Old) + frag, true
		case p == m.Old:
			return "/" + m.New + frag, true
		}
	}
	if absolutize && looksLikeRelativePath(target) {
		return "/" + p + frag, true
	}
	return value, false
}

// looksLikeRelativePath tells a relative path from the other things a
// path-valued field may hold. Rewriting a value because it named
// something that moved needs no such test — only a path can name one —
// but absolutizing every value of the moved concept would turn a scope
// descriptor ("all queries in BigQuery project X") or a bare warehouse
// name ("project.dataset.table") into a path nobody wrote. So only a
// value that says it is a path — no whitespace, and "./", "../" or a
// directory in it — is absolutized; a bare "eq.py" is left as written.
func looksLikeRelativePath(target string) bool {
	if strings.HasPrefix(target, "/") || strings.ContainsAny(target, " \t\n") {
		return false
	}
	return strings.HasPrefix(target, "./") || strings.HasPrefix(target, "../") || strings.Contains(target, "/")
}
