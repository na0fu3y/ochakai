// Moving a concept: the id is its address (design doc 0017), so a rename
// is a write to every document that pointed at the old one. Kept beside
// the lifecycle rather than in it, because it is the one operation that
// touches rows it was not asked about.
package store

import (
	"context"
	"encoding/json"
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
//
// within bounds where the rewrite may reach: a non-nil list of prefixes
// the caller may write, and ErrOutsideScope if a referrer sits outside
// them (design doc 0129). nil is the administrator's move and the move
// on a deployment with no policy, both unbounded. The check belongs
// here rather than in the service for the reason the policy's own
// partition does (design doc 0124): the referrers are read inside this
// transaction, under FOR UPDATE, and a check outside it would be
// answering about a set another writer can still add to.
func (s *Store) Move(ctx context.Context, oldID, newID string, actor domain.Actor, within []string) (*domain.Knowledge, error) {
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
		// The verification rows are still keyed by oldID here; they
		// follow the entry below.
		err := tx.QueryRow(ctx,
			`UPDATE object SET id=$2, path=$8, updated_at=$3, content_changed_at=`+changedAfterVerified("$3")+`,
			 updated_by_kind=$4, updated_by_name=$5, updated_by_via=$6, updated_by_producer=$7
			 WHERE id=$1 AND deleted_at IS NULL
			 RETURNING content_changed_at`,
			oldID, newID, k.UpdatedAt, actor.Kind, actor.Name, actor.Via, actor.Producer, domain.ConceptPath(newID)).
			Scan(&k.ContentChangedAt)
		if isUniqueViolation(err) {
			// The probe above found the destination free, but it took no
			// lock — a create can land on newID in the window. The primary
			// key stops it either way; this is so the caller reads the
			// same answer as when the id was already taken, rather than a
			// 500 with a constraint name in it.
			return fmt.Errorf("%w: %s", ErrAlreadyExists, newID)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
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
			`UPDATE attachment SET knowledge_id=$2 WHERE knowledge_id=$1`,
			`UPDATE knowledge_usage SET knowledge_id=$2 WHERE knowledge_id=$1`,
			`UPDATE knowledge_event SET knowledge_id=$2 WHERE knowledge_id=$1`,
		} {
			if _, err := tx.Exec(ctx, q, oldID, newID); err != nil {
				return err
			}
		}
		// Embedding tables exist only once semantic search has been
		// enabled. Re-keyed here so the row follows the concept; the
		// text it was made from opens with the old id, so the service
		// makes the vector again once the move commits.
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
		if err := s.rewriteReferences(ctx, tx, oldID, k, actor, within); err != nil {
			return err
		}
		return s.addRevision(ctx, tx, k, "move", actor, "")
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
func (s *Store) rewriteReferences(ctx context.Context, tx pgx.Tx, oldID string, moved *domain.Knowledge,
	actor domain.Actor, within []string,
) error {
	newID := moved.ID
	// FOR UPDATE, because each referrer is read here and written whole
	// below: the rewrite carries body, links and attrs from this read, so
	// an update committing in the window would be silently reverted — and
	// recorded as an ordinary "update" revision, which hides that anything
	// was lost. ORDER BY id gives concurrent moves one lock order, so they
	// queue instead of deadlocking.
	rows, err := tx.Query(ctx,
		`SELECT `+knowledgeSelectDoc+` FROM object
		 WHERE deleted_at IS NULL AND (links @> $1 OR attrs->>'model' = $2 OR id = $3
		       OR (frontmatter ?| $4 AND strpos(doc, $5) > 0))
		 ORDER BY id FOR UPDATE`,
		// The same containment the reverse lookup asks with, marshalled
		// rather than formatted: %q spells a Go literal, and the two
		// disagree for a rune Go writes as \U0001d173 — which no id is
		// likely to carry, and which JSON has no such escape for.
		linkContainment(oldID),
		oldID, newID,
		// A path in the frontmatter (OKF SPEC §6.2) is not in the links
		// index, so the candidates are the documents that carry such a
		// key and spell the moved name somewhere; which of them really
		// point at the move is decided below, per value.
		domain.PathFieldKeys, path.Base(oldID))
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
	moves := domain.ConceptMoves(oldID, newID)
	// Each referrer's repair is worked out before any is written, because
	// only a referrer that really changes decides the blast radius: a
	// candidate that merely spelled the name matches the query and needs
	// nothing.
	var repaired []*domain.Knowledge
	for i := range referrers {
		r := &referrers[i]
		self := r.ID == newID
		// The moved entry resolves its own relative links against its old
		// directory, so it is rewritten as if it still lived at oldID.
		from := r.ID
		if self {
			from = oldID
		}
		ok, err := repairReferences(r, from, self && movedDirs, moves,
			func(body string) string { return domain.RewriteBodyLinks(from, body, oldID, newID) },
			map[string]string{oldID: newID})
		if err != nil {
			return err
		}
		if ok {
			repaired = append(repaired, r)
		}
	}
	// The blast radius, decided before a single row is written: every
	// referrer this loop would rewrite has to be one the caller may
	// write. Refused whole rather than narrowed — a rewrite that skipped
	// what it may not touch would leave those links pointing at an id
	// that is gone, which is the one thing a move exists to prevent
	// (design doc 0129 §2).
	if within != nil {
		for _, r := range repaired {
			// The moved entry rewrites its own relative links as part of
			// the move; the caller's right to write it is the right to
			// write the destination, checked before the transaction.
			if r.ID == newID {
				continue
			}
			if !underAny(r.ID, within) {
				return ErrOutsideScope
			}
		}
	}
	for _, r := range repaired {
		self := r.ID == newID
		// The rewrite is a content change by the mover, and it is recorded
		// as one ("update" revision below), so the entry's generated.by
		// follows it (design doc 0036 §3.3).
		r.UpdatedBy = actor
		// The columns a repair can touch are written: the body and what
		// is derived from it, and the path-valued fields (OKF SPEC §6.2)
		// that name what the move carried.
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
		// and the timestamp here (design doc 0046 §3.4). The moved entry
		// keeps the move's own stamp, already past its verifications.
		if self {
			r.ContentChangedAt = moved.ContentChangedAt
		} else {
			r.ContentChangedAt = r.UpdatedAt
		}
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
		err = tx.QueryRow(ctx,
			`UPDATE object SET links=$2, attrs=$3, body=$4, updated_at=$5,
			 updated_by_kind=$6, updated_by_name=$7, updated_by_via=$8, updated_by_producer=$9,
			 doc=$10, content_hash=$11, content_changed_at=`+changedAfterVerified("$12")+`, frontmatter=$13,
			 resource=$14, sources=$15, computation=$16, executor=$17, attester=$18
			 WHERE id=$1 AND deleted_at IS NULL
			 RETURNING content_changed_at`,
			r.ID, j.links, j.attrs, r.Body, r.UpdatedAt, actor.Kind, actor.Name, actor.Via, actor.Producer, doc, hash,
			r.ContentChangedAt, fm, r.Resource, j.sources, r.Computation, j.executor, j.attester).Scan(&r.ContentChangedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("rewriting references to %s: %s changed under the move", oldID, r.ID)
		}
		if err != nil {
			return err
		}
		if self {
			// Fold the result back into the caller's entry; the caller
			// records the single "move" revision with this final state.
			moved.Body = r.Body
			moved.Links = r.Links
			moved.Attrs = r.Attrs
			moved.Resource, moved.Sources, moved.Computation = r.Resource, r.Sources, r.Computation
			moved.Executor, moved.Attester = r.Executor, r.Attester
			// And the document the body was rewritten inside, with the
			// version that goes with it: the "move" revision the caller
			// records is written from this entry (design doc 0046 §2.2).
			moved.Doc = r.Doc
			moved.ContentHash = r.ContentHash
			continue
		}
		if err := s.addRevision(ctx, tx, r, "update", actor, ""); err != nil {
			return err
		}
	}
	return nil
}

// repairReferences applies a move to one entry that points at what moved,
// and reports whether anything changed: the body's links (rewriteBody),
// the paths its frontmatter names (OKF SPEC §6.2: resource,
// sources[].resource, computation, executor.resource, attester.resource),
// and a `model` key naming a moved id (models, old id to new).
//
// from is the id relative values are resolved against — the old id, for
// the entry that moved itself — and absolutize says that entry changed
// directory, so its own relative links and paths come back absolute
// rather than starting to mean something else where it now sits.
//
// The document is edited in place, never re-rendered: the body is
// swapped inside the stored bytes, and each frontmatter key that changed
// is rewritten as its own block with every other line left as written.
func repairReferences(r *domain.Knowledge, from string, absolutize bool, moves []domain.PathMove,
	rewriteBody func(string) string, models map[string]string,
) (bool, error) {
	changed := false
	doc := []byte(r.Doc)
	body := r.Body
	if absolutize {
		body = domain.AbsolutizeBodyLinks(from, body)
	}
	if body = rewriteBody(body); body != r.Body {
		r.Body = body
		r.Links = domain.LinksFromBody(r.ID, body)
		doc = okf.ReplaceBody(doc, body)
		changed = true
	}
	out, pathsMoved, err := okf.RewritePathFields(doc, func(v string) (string, bool) {
		return domain.MovePathValue(from, v, moves, absolutize)
	})
	if err != nil {
		return false, fmt.Errorf("%s: %w", r.ID, err)
	}
	if pathsMoved {
		d, _, err := okf.Parse(out)
		if err != nil {
			return false, fmt.Errorf("%s: %w", r.ID, err)
		}
		doc = out
		r.Resource, r.Sources, r.Computation = d.Resource, d.Sources, d.Computation
		r.Executor, r.Attester = d.Executor, d.Attester
		changed = true
	}
	if m, ok := r.Attrs["model"].(string); ok {
		if to, moved := models[m]; moved {
			r.Attrs["model"] = to
			// In place when the block allows it; otherwise the stored
			// document no longer says what the row does, and storedDoc
			// renders the canonical form, as it always has for this key.
			if v, err := json.Marshal(to); err == nil {
				if out, err := okf.SetFrontmatterKeys(doc, map[string]json.RawMessage{"model": v}, nil); err == nil {
					doc = out
				}
			}
			changed = true
		}
	}
	r.Doc = string(doc)
	return changed, nil
}

// underAny reports whether id sits at or beneath any of prefixes, on
// segment boundaries (domain.Under). The empty list matches nothing:
// a caller holding no write grant may write nowhere, which is not the
// same as the nil that means "unbounded".
func underAny(id string, prefixes []string) bool {
	for _, p := range prefixes {
		if domain.Under(id, p) {
			return true
		}
	}
	return false
}
