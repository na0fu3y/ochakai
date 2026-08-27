// Moving a directory: the same act as a move, one level up. A prefix is
// not an object — nothing is stored at a directory (design doc 0075 §2) —
// so what moves is every object addressed under it, and the whole of that
// is one transaction: a half-moved directory is worse than a refused one
// (design doc 0132, reading 0129 §2 at this grain).
package store

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/okf"
)

// MovePrefix moves every object addressed under oldPrefix to newPrefix and
// returns how many it moved. Concepts move with everything keyed by their
// ids — revisions, the verification and rejection ledgers, usage, events,
// embeddings — and the files under the directory move with them, whether
// or not they sit under a concept's own namespace.
//
// Three classes of object move that a client cannot see and therefore
// cannot move for itself (design doc 0132 §1): concepts a human rejected,
// concepts that were deleted and still own their addresses, and the files
// that no concept's namespace covers. Leaving any of them would mean the
// old directory never empties, so an import into it later collides with a
// row nobody can list.
//
// A concept whose id *equals* oldPrefix does not move. 0075 §2 lets a
// concept and a directory share a name, so `old` names two things; the
// caller said which by asking for a directory.
//
// within bounds where the rewrite may reach, exactly as Move's does
// (design doc 0129): a non-nil list of prefixes the caller may write, and
// ErrOutsideScope if any moved concept, any destination, or any referrer
// of any of them sits outside. nil is the administrator's move.
func (s *Store) MovePrefix(ctx context.Context, oldPrefix, newPrefix string, actor domain.Actor, within []string) (int, error) {
	moved := 0
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		// ORDER BY id, and locked here rather than as the rows are
		// written: concurrent moves take the same lock order and queue
		// instead of deadlocking, which is the reason rewriteReferences
		// orders its own read.
		rows, err := tx.Query(ctx,
			`SELECT id, deleted_at IS NULL FROM object
			 WHERE id IS NOT NULL AND starts_with(id, $1 || '/')
			 ORDER BY id FOR UPDATE`, oldPrefix)
		if err != nil {
			return err
		}
		type concept struct {
			old, new string
			live     bool
		}
		var concepts []concept
		for rows.Next() {
			var c concept
			if err := rows.Scan(&c.old, &c.live); err != nil {
				rows.Close()
				return err
			}
			c.new = newPrefix + strings.TrimPrefix(c.old, oldPrefix)
			concepts = append(concepts, c)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		var files int
		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM object WHERE id IS NULL AND starts_with(path, $1 || '/')`,
			oldPrefix).Scan(&files); err != nil {
			return err
		}
		// An empty directory is not a rename that succeeded quietly: the
		// caller named a place, and nothing is there.
		if len(concepts) == 0 && files == 0 {
			return ErrNotFound
		}
		// The destination has to be empty, which is the one-concept
		// move's "the destination must be a fresh id" over a whole
		// directory. A deleted row counts as occupied for its reason
		// there too: it owns the address and brings revisions of its own.
		var occupied bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM object
			   WHERE (id IS NOT NULL AND starts_with(id, $1 || '/'))
			      OR (id IS NULL AND starts_with(path, $1 || '/')))`,
			newPrefix).Scan(&occupied); err != nil {
			return err
		}
		if occupied {
			return fmt.Errorf("%w: %s already holds objects", ErrAlreadyExists, newPrefix)
		}
		now := NowStored()
		movedNew := make(map[string]bool, len(concepts))
		pairs := make([][2]string, 0, len(concepts))
		for _, c := range concepts {
			if within != nil && (!underAny(c.old, within) || !underAny(c.new, within)) {
				return ErrOutsideScope
			}
			movedNew[c.new] = true
			pairs = append(pairs, [2]string{c.old, c.new})
		}
		for _, c := range concepts {
			if err := rekeyConcept(ctx, tx, c.old, c.new, actor, now, c.live); err != nil {
				return err
			}
		}
		// The files, once for the whole directory rather than once per
		// concept: a file under a moved concept's namespace and a file
		// sitting loose in the directory are the same object at
		// different addresses, and only the directory covers both.
		//
		// starts_with, not LIKE, for the reason the one-concept move
		// gives: an id may contain "_", which LIKE reads as a wildcard.
		for _, q := range []string{
			`UPDATE object SET path = $2 || substr(path, length($1) + 1)
			 WHERE id IS NULL AND starts_with(path, $1 || '/')`,
			// A concept anywhere in the bundle may name a file that lives
			// in this directory, so the naming is repaired bundle-wide
			// rather than under the prefix.
			`UPDATE object SET files = (
			     SELECT COALESCE(jsonb_agg(
			         CASE WHEN starts_with(x, $1 || '/') THEN $2 || substr(x, length($1) + 1) ELSE x END), '[]'::jsonb)
			       FROM jsonb_array_elements_text(files) x)
			 WHERE id IS NOT NULL
			   AND EXISTS (SELECT 1 FROM jsonb_array_elements_text(files) x WHERE starts_with(x, $1 || '/'))`,
			`UPDATE knowledge_revision SET path = $2 || substr(path, length($1) + 1)
			 WHERE id IS NULL AND starts_with(path, $1 || '/')`,
		} {
			if _, err := tx.Exec(ctx, q, oldPrefix, newPrefix); err != nil {
				return err
			}
		}
		if err := execTolerateMissingTable(ctx, tx,
			`UPDATE attachment_embedding SET path = $2 || substr(path, length($1) + 1)
			 WHERE starts_with(path, $1 || '/')`, oldPrefix, newPrefix); err != nil {
			return err
		}
		final, err := s.rewritePrefixReferences(ctx, tx, pairs, movedNew, actor, now, within)
		if err != nil {
			return err
		}
		// The "move" revision per concept, written from the row as it
		// stands after the rewrite, so the revision and the row agree on
		// what the body says. A deleted concept gets none: it is a
		// tombstone, and its history moved with it above.
		for _, c := range concepts {
			if !c.live {
				continue
			}
			k := final[c.new]
			if k == nil {
				return fmt.Errorf("moving %s: %s vanished under the move", oldPrefix, c.new)
			}
			if err := s.addRevision(ctx, tx, k, "move", actor); err != nil {
				return err
			}
		}
		moved = len(concepts) + files
		return nil
	})
	if err != nil {
		return 0, err
	}
	return moved, nil
}

// rekeyConcept re-addresses one concept and everything keyed by its id.
// It is the one-concept move's own list of statements, minus the file
// sweep (MovePrefix does that once for the directory) and minus the
// reference rewrite (once, at the end, so a link between two concepts
// that both moved is repaired one time rather than once per sibling).
//
// live says whether the row is a tombstone. A tombstone is re-addressed
// and nothing else: stamping updated_by on a deleted concept would record
// the mover as having changed something nobody can read.
func rekeyConcept(ctx context.Context, tx pgx.Tx, oldID, newID string, actor domain.Actor, now time.Time, live bool) error {
	q := `UPDATE object SET id=$2, path=$3 WHERE id=$1`
	args := []any{oldID, newID, domain.ConceptPath(newID)}
	if live {
		q = `UPDATE object SET id=$2, path=$3, updated_at=$4, content_changed_at=$4,
		     updated_by_kind=$5, updated_by_name=$6, updated_by_via=$7, updated_by_producer=$8
		     WHERE id=$1`
		args = append(args, now, actor.Kind, actor.Name, actor.Via, actor.Producer)
	}
	tag, err := tx.Exec(ctx, q, args...)
	if isUniqueViolation(err) {
		return fmt.Errorf("%w: %s", ErrAlreadyExists, newID)
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	for _, q := range []string{
		`UPDATE knowledge_revision SET id=$2, path=$2 || '.md' WHERE id=$1`,
		`UPDATE knowledge_verification SET id=$2 WHERE id=$1`,
		`UPDATE knowledge_rejection SET id=$2 WHERE id=$1`,
		`UPDATE attachment SET knowledge_id=$2 WHERE knowledge_id=$1`,
		`UPDATE knowledge_usage SET knowledge_id=$2 WHERE knowledge_id=$1`,
		`UPDATE knowledge_event SET knowledge_id=$2 WHERE knowledge_id=$1`,
	} {
		if _, err := tx.Exec(ctx, q, oldID, newID); err != nil {
			return err
		}
	}
	return execTolerateMissingTable(ctx, tx,
		`UPDATE knowledge_embedding SET id=$2 WHERE id=$1`, oldID, newID)
}

// rewritePrefixReferences repairs every body that points into the moved
// directory, once, against the whole mapping.
//
// Who gets an "update" revision is the decision design doc 0132 §5
// records. A concept that moved has its own links repaired as part of the
// move — the rule the one-concept move already applies to the moved
// entry's own body, extended to the siblings that moved beside it — so it
// takes no "update"; its "move" revision carries the final text. A
// concept outside the directory that pointed in did not move and its body
// changed, so it takes one, exactly as before.
//
// Without that split, moving a directory of mutually-linking concepts
// would stack a revision per internal link and leave a history nobody can
// read.
func (s *Store) rewritePrefixReferences(ctx context.Context, tx pgx.Tx, pairs [][2]string,
	movedNew map[string]bool, actor domain.Actor, now time.Time, within []string,
) (map[string]*domain.Knowledge, error) {
	// A directory of nothing but files has no reference to repair, and no
	// "move" revision to write from this read either.
	if len(pairs) == 0 {
		return nil, nil
	}
	olds := make([]string, len(pairs))
	news := make([]string, len(pairs))
	for i, p := range pairs {
		olds[i], news[i] = p[0], p[1]
	}
	// The links column still holds the pre-move derivation, so it is an
	// accurate index of who pointed into the directory. Asked as a
	// membership test rather than with the containment operator the
	// single-id lookup uses: one statement over the whole mapping beats
	// one indexed statement per moved concept once a directory is more
	// than a couple of concepts deep.
	rows, err := tx.Query(ctx,
		`SELECT `+knowledgeSelectDoc+` FROM object
		 WHERE deleted_at IS NULL
		   AND (EXISTS (SELECT 1 FROM jsonb_array_elements(links) l WHERE l->>'target' = ANY($1))
		        OR attrs->>'model' = ANY($1)
		        OR id = ANY($2))
		 ORDER BY id FOR UPDATE`, olds, news)
	if err != nil {
		return nil, err
	}
	referrers, err := pgx.CollectRows(rows, scanKnowledgeDoc)
	if err != nil {
		return nil, err
	}
	// The blast radius, decided before a row is written (design doc 0129
	// §2): a referrer the caller may not write means the whole move is
	// refused, never narrowed. The moved concepts are already checked.
	if within != nil {
		for i := range referrers {
			if movedNew[referrers[i].ID] {
				continue
			}
			if !underAny(referrers[i].ID, within) {
				return nil, ErrOutsideScope
			}
		}
	}
	// Every live moved concept is in this read — the id clause selects it
	// whether or not it points at anything — so the final text of each is
	// here, and the caller writes its "move" revision from it rather than
	// reading the row a second time.
	final := make(map[string]*domain.Knowledge, len(pairs))
	for i := range referrers {
		if movedNew[referrers[i].ID] {
			final[referrers[i].ID] = &referrers[i]
		}
	}
	oldOf := make(map[string]string, len(pairs))
	for _, p := range pairs {
		oldOf[p[1]] = p[0]
	}
	for i := range referrers {
		r := &referrers[i]
		self := movedNew[r.ID]
		body := r.Body
		// A moved concept resolves its own relative links against the
		// directory it used to sit in, so it is rewritten as if it were
		// still there, and relative links are absolutized first when the
		// directory it sits in changed. Absolute is the form a move
		// cannot reinterpret.
		from := r.ID
		if self {
			from = oldOf[r.ID]
			if path.Dir(from) != path.Dir(r.ID) {
				body = domain.AbsolutizeBodyLinks(from, body)
			}
		}
		for _, p := range pairs {
			body = domain.RewriteBodyLinks(from, body, p[0], p[1])
		}
		modelMoved := false
		if m, ok := r.Attrs["model"].(string); ok {
			for _, p := range pairs {
				if m == p[0] {
					r.Attrs["model"] = p[1]
					modelMoved = true
					break
				}
			}
		}
		if body == r.Body && !modelMoved {
			continue // matched the index but nothing to repair
		}
		r.Body = body
		r.Doc = string(okf.ReplaceBody([]byte(r.Doc), body))
		r.Links = domain.LinksFromBody(r.ID, r.Body)
		r.UpdatedBy = actor
		// A moved concept's row already carries the move's instant; only
		// a referrer that stayed put is being changed now.
		r.UpdatedAt = now
		r.ContentChangedAt = now
		j, err := marshalJSONFields(r)
		if err != nil {
			return nil, err
		}
		doc, hash, err := storedDoc(r)
		if err != nil {
			return nil, err
		}
		fm, err := storedFrontmatter(doc)
		if err != nil {
			return nil, err
		}
		r.ContentHash = hash
		tag, err := tx.Exec(ctx,
			`UPDATE object SET links=$2, attrs=$3, body=$4, updated_at=$5,
			 updated_by_kind=$6, updated_by_name=$7, updated_by_via=$8, updated_by_producer=$9,
			 doc=$10, content_hash=$11, content_changed_at=$12, frontmatter=$13
			 WHERE id=$1 AND deleted_at IS NULL`,
			r.ID, j.links, j.attrs, r.Body, r.UpdatedAt, actor.Kind, actor.Name, actor.Via, actor.Producer,
			doc, hash, r.ContentChangedAt, fm)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() == 0 {
			return nil, fmt.Errorf("repairing references into %s: %s changed under the move", olds[0], r.ID)
		}
		if self {
			continue // its "move" revision carries this text
		}
		if err := s.addRevision(ctx, tx, r, "update", actor); err != nil {
			return nil, err
		}
	}
	return final, nil
}
