// Moving a concept: the id is its address (design doc 0017), so a rename
// is a write to every document that pointed at the old one. Kept beside
// the lifecycle rather than in it, because it is the one operation that
// touches rows it was not asked about.
package store

import (
	"context"
	"errors"
	"fmt"
	"path"

	"github.com/jackc/pgx/v5"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/okf"
)

// Move renames an entry to a new id. The id is the address (design doc
// 0017), so everything keyed by it follows in one transaction — the row,
// its revisions, attachments, usage, events, and embeddings — and live
// entries that reference the old id (link targets in both bare and
// both of SPEC §6's forms, and attrs.model) are rewritten so no reference
// breaks. Attachment bytes never move: blobs are content-addressed
// (design doc 0011). The destination must be a fresh id — a row there,
// even soft-deleted, already owns that address and its revision history.
func (s *Store) Move(ctx context.Context, oldID, newID string, actor domain.Actor) (*domain.Knowledge, error) {
	k, err := s.Get(ctx, oldID)
	if err != nil {
		return nil, err
	}
	k.UpdatedAt = NowStored()
	k.ContentChangedAt = k.UpdatedAt
	// A move rewrites the entry's own relative links and every referrer's
	// body, so the mover is who the content now stands by: generated.by
	// in an export (design doc 0036 §3.3).
	k.UpdatedBy = actor
	err = s.withTx(ctx, func(tx pgx.Tx) error {
		// A soft-deleted entry still owns the id — the row holds the primary
		// key and its revisions hold (id, rev) — so the destination is taken
		// either way. Create can revive such an entry in place; a move
		// cannot, because the arriving entry brings revisions of its own.
		// Say which case it is: "already exists" sends someone looking for
		// an entry they cannot see, and without purge the id would be
		// blocked forever.
		var occupied, occupantDeleted bool
		if err := tx.QueryRow(ctx,
			`SELECT true, deleted_at IS NOT NULL FROM object WHERE id=$1`, newID).
			Scan(&occupied, &occupantDeleted); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if occupied {
			if occupantDeleted {
				return fmt.Errorf("%w: %s holds a deleted entry; purge it to free the id", ErrAlreadyExists, newID)
			}
			return ErrAlreadyExists
		}
		// deleted_at IS NULL guards the race with a concurrent delete,
		// exactly as in SoftDelete: the Get above ran outside this
		// transaction.
		tag, err := tx.Exec(ctx,
			`UPDATE object SET id=$2, path=$8, updated_at=$3, content_changed_at=$3,
			 updated_by_kind=$4, updated_by_name=$5, updated_by_via=$6, updated_by_producer=$7
			 WHERE id=$1 AND deleted_at IS NULL`,
			oldID, newID, k.UpdatedAt, actor.Kind, actor.Name, actor.Via, actor.Producer, domain.ConceptPath(newID))
		if isUniqueViolation(err) {
			// The probe above found the destination free, but it took no
			// lock — a create can land on newID in the window. The primary
			// key stops it either way; this is so the caller reads the
			// same answer as when the id was already taken, rather than a
			// 500 with a constraint name in it.
			return fmt.Errorf("%w: %s", ErrAlreadyExists, newID)
		}
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		for _, q := range []string{
			// The entry's namespace moves with it (design doc 0046 §3.3):
			// the address is part of what a file is, and leaving the
			// files behind would leave them under a directory whose
			// concept is gone. Only the files — a concept under there has
			// an id, an address and a history of its own, and moves when
			// somebody moves it.
			//
			// starts_with, not LIKE, for the reason browsing gives: an id
			// may contain "_", which LIKE reads as a wildcard. Moving
			// "sales_2024" would have dragged every file under
			// "salesX2024/" along with it.
			`UPDATE object SET path = $2 || substr(path, length($1) + 1)
			 WHERE id IS NULL AND starts_with(path, $1 || '/')`,
			// The naming follows the files it names.
			`UPDATE object SET files = (
			     SELECT COALESCE(jsonb_agg(
			         CASE WHEN starts_with(x, $1 || '/') THEN $2 || substr(x, length($1) + 1) ELSE x END), '[]'::jsonb)
			       FROM jsonb_array_elements_text(files) x)
			 WHERE id IS NOT NULL
			   AND EXISTS (SELECT 1 FROM jsonb_array_elements_text(files) x WHERE starts_with(x, $1 || '/'))`,
			`UPDATE knowledge_revision SET id=$2, path=$2 || '.md' WHERE id=$1`,
			// A file's history is keyed by its path, so it moves when the
			// file does — otherwise the log of the directory it arrived
			// in would be silent about how it got there, and the one it
			// left would go on reporting a file that is not there.
			`UPDATE knowledge_revision SET path = $2 || substr(path, length($1) + 1)
			 WHERE id IS NULL AND starts_with(path, $1 || '/')`,
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
		// Embedding tables exist only once semantic search has been
		// enabled; the embedding text does not include the id, so
		// re-keying is enough — no re-embed.
		for _, q := range []string{
			`UPDATE knowledge_embedding SET id=$2 WHERE id=$1`,
			// A file's vector is keyed by the file's path, so it follows
			// the file — the same prefix rewrite the file objects and
			// their revisions get above. Files the body names elsewhere
			// in the bundle do not move and neither do their vectors.
			`UPDATE attachment_embedding SET path = $2 || substr(path, length($1) + 1)
			 WHERE starts_with(path, $1 || '/')`,
		} {
			if err := execTolerateMissingTable(ctx, tx, q, oldID, newID); err != nil {
				return err
			}
		}
		k.ID = newID
		if err := s.rewriteReferences(ctx, tx, oldID, k, actor); err != nil {
			return err
		}
		return s.addRevision(ctx, tx, k, "move", actor)
	})
	if err != nil {
		return nil, err
	}
	return k, nil
}

// rewriteReferences updates live entries that reference oldID — the
// markdown links in their body, and attrs.model (design doc 0019) — each
// rewrite recorded as an "update" revision. Runs after the rename itself,
// so the moved entry (already renamed, passed as moved with the move's
// timestamp stamped) is covered too — but its own rewrite is part of the
// move, not a separate change: the row keeps the move's updated_at, no
// "update" revision is added, and the rewritten body, links, and attrs
// are folded back into moved so the caller's "move" revision and return
// value carry the final state.
//
// The body is what gets rewritten: links are derived from it (design doc
// 0024), so repairing the links column alone would leave the author's
// prose pointing at an id that no longer exists. The links column is then
// re-derived from the repaired body.
//
// Candidates come from the links column, which still holds the pre-move
// derivation and so is an accurate index of who refers to oldID.
func (s *Store) rewriteReferences(ctx context.Context, tx pgx.Tx, oldID string, moved *domain.Knowledge, actor domain.Actor) error {
	newID := moved.ID
	// FOR UPDATE, because each referrer is read here and written whole
	// below: the rewrite carries body, links and attrs from this read, so
	// an update committing in the window would be silently reverted — and
	// recorded as an ordinary "update" revision, which hides that anything
	// was lost. ORDER BY id gives concurrent moves one lock order, so they
	// queue instead of deadlocking.
	rows, err := tx.Query(ctx,
		`SELECT `+knowledgeSelectDoc+` FROM object
		 WHERE deleted_at IS NULL AND (links @> $1 OR attrs->>'model' = $2 OR id = $3)
		 ORDER BY id FOR UPDATE`,
		fmt.Sprintf(`[{"target": %q}]`, oldID),
		oldID, newID)
	if err != nil {
		return err
	}
	// With the document: the rewrite below replaces the body inside the
	// stored bytes, leaving the frontmatter of every entry that merely
	// cited the moved one exactly as its writer left it.
	referrers, err := pgx.CollectRows(rows, scanKnowledgeDoc)
	if err != nil {
		return err
	}
	now := NowStored()
	movedDirs := path.Dir(oldID) != path.Dir(newID)
	for i := range referrers {
		r := &referrers[i]
		self := r.ID == newID
		// The moved entry resolves its own relative links against its old
		// directory, so it is rewritten as if it still lived at oldID.
		from := r.ID
		body := r.Body
		if self {
			from = oldID
			// Its own outbound relative links point at the old directory,
			// and links are derived from the body against the entry's
			// current id: left alone, "./gross.md" silently starts meaning
			// a different entry (or none) the next time this row is
			// written. Absolute is the form a move cannot reinterpret.
			if movedDirs {
				body = domain.AbsolutizeBodyLinks(oldID, body)
			}
		}
		body = domain.RewriteBodyLinks(from, body, oldID, newID)
		modelMoved := false
		if m, ok := r.Attrs["model"].(string); ok && m == oldID {
			r.Attrs["model"] = newID
			modelMoved = true
		}
		if body == r.Body && !modelMoved {
			continue // matched the links index but nothing to repair
		}
		r.Body = body
		r.Doc = string(okf.ReplaceBody([]byte(r.Doc), body))
		r.Links = domain.LinksFromBody(r.ID, r.Body)
		// The rewrite is a content change by the mover, and it is recorded
		// as one ("update" revision below), so the entry's generated.by
		// follows it (design doc 0036 §3.3).
		r.UpdatedBy = actor
		// Only the columns a link rewrite touches are written: a move
		// repairs references, and an entry's citations and computation
		// contract are not references it makes.
		j, err := marshalJSONFields(r)
		if err != nil {
			return err
		}
		// The moved entry's own rewrite is the move, not a change on top
		// of it: keep the move's timestamp so the row, the "move"
		// revision, and the returned entry agree on one instant.
		if self {
			r.UpdatedAt = moved.UpdatedAt
		} else {
			r.UpdatedAt = now
		}
		// A repaired link is a change to what the entry says, so
		// generated moves with it — both halves of it, the actor above
		// and the timestamp here (design doc 0046 §3.4).
		r.ContentChangedAt = r.UpdatedAt
		// deleted_at IS NULL restates what the locked read already
		// established, so the statement does not depend on the reader
		// having filtered: rewriting a tombstone would plant a repaired
		// body and a revision on an entry nobody can see, to resurface
		// whenever someone revives it.
		// The stored document and its hash move with the body: the body
		// is swapped inside the bytes the entry is stored as, and the
		// index columns beside them are derived from the same value
		// (design docs 0043 §3.1, 0046 §2.2).
		doc, hash, err := storedDoc(r)
		if err != nil {
			return err
		}
		fm, err := storedFrontmatter(doc)
		if err != nil {
			return err
		}
		r.ContentHash = hash
		tag, err := tx.Exec(ctx,
			`UPDATE object SET links=$2, attrs=$3, body=$4, updated_at=$5,
			 updated_by_kind=$6, updated_by_name=$7, updated_by_via=$8, updated_by_producer=$9,
			 doc=$10, content_hash=$11, content_changed_at=$12, frontmatter=$13
			 WHERE id=$1 AND deleted_at IS NULL`,
			r.ID, j.links, j.attrs, r.Body, r.UpdatedAt, actor.Kind, actor.Name, actor.Via, actor.Producer, doc, hash,
			r.ContentChangedAt, fm)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("rewriting references to %s: %s changed under the move", oldID, r.ID)
		}
		if self {
			// Fold the result back into the caller's entry; the caller
			// records the single "move" revision with this final state.
			moved.Body = r.Body
			moved.Links = r.Links
			moved.Attrs = r.Attrs
			// And the document the body was rewritten inside, with the
			// version that goes with it: the "move" revision the caller
			// records is written from this entry (design doc 0046 §2.2).
			moved.Doc = r.Doc
			moved.ContentHash = r.ContentHash
			continue
		}
		if err := s.addRevision(ctx, tx, r, "update", actor); err != nil {
			return err
		}
	}
	return nil
}
