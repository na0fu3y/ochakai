// Package store persists knowledge in PostgreSQL, the only runtime
// dependency of ochakai.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/na0fu3y/ochakai/internal/blob"
	"github.com/na0fu3y/ochakai/internal/domain"
)

var ErrNotFound = errors.New("knowledge not found")
var ErrAlreadyExists = errors.New("knowledge already exists")

// ErrConflict is returned by Update when an If-Match precondition does not
// match the stored revision: the entry changed since the caller read it
// (design doc 0025 §11). The entry exists — a missing entry is ErrNotFound.
var ErrConflict = errors.New("knowledge changed since it was read")

// ErrNotDeleted is returned by Purge for an entry that is still live.
// Purging is the second step of a two-step destruction: delete first, so
// no single call can erase history that was in use a moment ago.
var ErrNotDeleted = errors.New("knowledge is live; soft-delete it before purging")

// nowStored is the current UTC time truncated to the microsecond precision
// PostgreSQL timestamptz stores. Setting entity timestamps from it means an
// in-memory updated_at always equals the value that round-trips through the
// database — the invariant the ETag/If-Match optimistic lock depends on
// (design doc 0025 §11): time.Now()'s nanoseconds would otherwise make the
// value returned by a write differ from the stored one on nanosecond-
// resolution clocks (Linux), breaking a client's next conditional update.
func nowStored() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

type Store struct {
	pool *pgxpool.Pool
	// blobs holds attachment bytes (GCS, design doc 0013); metadata stays
	// in PostgreSQL. When nil, attachments are unsupported — markdown
	// entries only.
	blobs blob.Store
	// lastEventPrune throttles knowledge_event pruning (unix seconds).
	lastEventPrune atomic.Int64

	// Usage events buffer in memory and flush on a timer so recording
	// never touches the read path (design doc 0025 §11). usageBuf is
	// guarded by usageMu; the flush loop stops on flushStop and drains
	// once more before flushWG releases (see New/Close, usage.go).
	usageMu   sync.Mutex
	usageBuf  []usageEvent
	flushStop chan struct{}
	flushWG   sync.WaitGroup
}

// UseBlobStore routes attachment bytes to b (design doc 0013). Call
// before serving.
func (s *Store) UseBlobStore(b blob.Store) { s.blobs = b }

// HasBlobStore reports whether attachment bytes have somewhere to live —
// false means attachments are unsupported (design doc 0013).
func (s *Store) HasBlobStore() bool { return s.blobs != nil }

func New(ctx context.Context, databaseURL string, iamAuth bool) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	if iamAuth {
		// Cloud SQL IAM database authentication: the password of every
		// new connection is a fresh short-lived access token.
		tokens := &metadataTokenSource{}
		cfg.BeforeConnect = func(ctx context.Context, cc *pgx.ConnConfig) error {
			tok, err := tokens.password(ctx)
			if err != nil {
				return err
			}
			cc.Password = tok
			return nil
		}
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	s := &Store{pool: pool, flushStop: make(chan struct{})}
	s.flushWG.Add(1)
	go s.usageFlushLoop()
	return s, nil
}

// Close stops the usage flush loop (draining the buffer one last time) and
// releases the connection pool. Buffered events that have not yet flushed
// are lost only if the process dies before Close — usage is a best-effort
// statistic (design doc 0025 §10).
func (s *Store) Close() {
	if s.flushStop != nil {
		close(s.flushStop)
		s.flushWG.Wait()
	}
	s.pool.Close()
}

// Filter narrows search and list operations.
type Filter struct {
	Types    []domain.Type
	Statuses []domain.Status
	Tags     []string
}

const knowledgeCols = `type, id, title, description, resource, tags, status, status_note,
	created_by_kind, created_by_name, created_by_via,
	verified_by_kind, verified_by_name, verified_by_via, verified_at,
	rejected_by_kind, rejected_by_name, rejected_by_via, rejected_at,
	links, attrs, body, created_at, updated_at`

// knowledgeDest returns the scan destinations for knowledgeCols targeting
// k, plus a finish func that decodes the JSON columns and nullable actors
// after the scan. Row shapes with trailing columns (score, usage totals)
// append their own destinations — the column list lives here once.
func knowledgeDest(k *domain.Knowledge) (dests []any, finish func() error) {
	var verifiedKind, verifiedName, rejectedKind, rejectedName *string
	var verifiedVia, rejectedVia string
	var links, attrs []byte
	dests = []any{&k.Type, &k.ID, &k.Title, &k.Description, &k.Resource, &k.Tags, &k.Status, &k.StatusNote,
		&k.CreatedBy.Kind, &k.CreatedBy.Name, &k.CreatedBy.Via,
		&verifiedKind, &verifiedName, &verifiedVia, &k.VerifiedAt,
		&rejectedKind, &rejectedName, &rejectedVia, &k.RejectedAt,
		&links, &attrs, &k.Body, &k.CreatedAt, &k.UpdatedAt}
	finish = func() error {
		k.VerifiedBy = actorFrom(verifiedKind, verifiedName, verifiedVia)
		k.RejectedBy = actorFrom(rejectedKind, rejectedName, rejectedVia)
		if err := json.Unmarshal(links, &k.Links); err != nil {
			return err
		}
		return json.Unmarshal(attrs, &k.Attrs)
	}
	return dests, finish
}

func scanKnowledge(row pgx.CollectableRow) (domain.Knowledge, error) {
	var k domain.Knowledge
	dests, finish := knowledgeDest(&k)
	if err := row.Scan(dests...); err != nil {
		return k, err
	}
	return k, finish()
}

func actorFrom(kind, name *string, via string) *domain.Actor {
	if kind == nil || name == nil {
		return nil
	}
	return &domain.Actor{Kind: *kind, Name: *name, Via: via}
}

// actorPtrs splits an optional actor into its columns. Kind and name are
// nullable together (no actor at all); via is a plain column because an
// absent delegation and an empty one are the same thing.
func actorPtrs(a *domain.Actor) (kind, name *string, via string) {
	if a == nil {
		return nil, nil, ""
	}
	return &a.Kind, &a.Name, a.Via
}

func (s *Store) Get(ctx context.Context, id string) (*domain.Knowledge, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+knowledgeCols+` FROM knowledge WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return nil, err
	}
	k, err := pgx.CollectExactlyOneRow(rows, scanKnowledge)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &k, nil
}

// GetTombstone returns the soft-deleted entry holding id, or ErrNotFound
// when the id is free or live. Create revives such a row in place
// (ON CONFLICT ... WHERE deleted_at IS NOT NULL), overwriting its status
// and status_note, so a surface that must not overwrite a ruling has to
// be able to see the ruling a tombstone still carries.
func (s *Store) GetTombstone(ctx context.Context, id string) (*domain.Knowledge, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+knowledgeCols+` FROM knowledge WHERE id = $1 AND deleted_at IS NOT NULL`, id)
	if err != nil {
		return nil, err
	}
	k, err := pgx.CollectExactlyOneRow(rows, scanKnowledge)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &k, nil
}

// ListLinkingTo returns live entries whose links point at id, most
// recently updated first. This is the reverse edge Context needs: the
// insight that explains a metric links to the metric, not the other way
// round. Both bare and ochakai:// target forms match.
func (s *Store) ListLinkingTo(ctx context.Context, id string, limit int) ([]domain.Knowledge, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+knowledgeCols+` FROM knowledge
		 WHERE deleted_at IS NULL AND (links @> $1 OR links @> $2)
		 ORDER BY updated_at DESC LIMIT $3`,
		fmt.Sprintf(`[{"target": %q}]`, id),
		fmt.Sprintf(`[{"target": %q}]`, "ochakai://"+id),
		limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanKnowledge)
}

// ListModelsDefiningMetric returns live models entries whose spec defines
// the named metric (attrs.spec.metrics[].name), ordered by id. This is
// compile-time model resolution when no model id is passed (design doc
// 0019): the model is the source of truth for its metrics, and entries
// live wherever the user put them.
func (s *Store) ListModelsDefiningMetric(ctx context.Context, metric string) ([]domain.Knowledge, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+knowledgeCols+` FROM knowledge
		 WHERE deleted_at IS NULL AND type = $1
		   AND attrs->'spec'->'metrics' @> jsonb_build_array(jsonb_build_object('name', $2::text))
		 ORDER BY id`, domain.TypeModels, metric)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanKnowledge)
}

// ListMetricEntryIDs returns the ids of live metrics entries that name
// the given models entry via attrs.model — the entries compile usage is
// attributed to (design doc 0019).
func (s *Store) ListMetricEntryIDs(ctx context.Context, modelID string) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id FROM knowledge
		 WHERE deleted_at IS NULL AND type = $1 AND attrs->>'model' = $2
		 ORDER BY id`, domain.TypeMetrics, modelID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// Create inserts a new entry. A live entry with the same id is
// ErrAlreadyExists — including rejected ones, so the memory of no
// survives. A soft-deleted entry is revived instead: the ID would
// otherwise be dead forever (the row still owns the primary key while
// Update refuses deleted rows), and its history stays in the revisions
// either way.
func (s *Store) Create(ctx context.Context, k *domain.Knowledge) error {
	now := nowStored()
	k.CreatedAt, k.UpdatedAt = now, now
	return s.withTx(ctx, func(tx pgx.Tx) error {
		links, attrs, err := marshalJSONFields(k)
		if err != nil {
			return err
		}
		verifiedKind, verifiedName, verifiedVia := actorPtrs(k.VerifiedBy)
		rejectedKind, rejectedName, rejectedVia := actorPtrs(k.RejectedBy)
		tag, err := tx.Exec(ctx, `INSERT INTO knowledge (`+knowledgeCols+`)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24)
			ON CONFLICT (id) DO UPDATE SET
				type=EXCLUDED.type,
				title=EXCLUDED.title, description=EXCLUDED.description, resource=EXCLUDED.resource, tags=EXCLUDED.tags,
				status=EXCLUDED.status, status_note=EXCLUDED.status_note,
				created_by_kind=EXCLUDED.created_by_kind, created_by_name=EXCLUDED.created_by_name,
				created_by_via=EXCLUDED.created_by_via,
				verified_by_kind=EXCLUDED.verified_by_kind, verified_by_name=EXCLUDED.verified_by_name,
				verified_by_via=EXCLUDED.verified_by_via, verified_at=EXCLUDED.verified_at,
				rejected_by_kind=EXCLUDED.rejected_by_kind, rejected_by_name=EXCLUDED.rejected_by_name,
				rejected_by_via=EXCLUDED.rejected_by_via, rejected_at=EXCLUDED.rejected_at,
				links=EXCLUDED.links, attrs=EXCLUDED.attrs, body=EXCLUDED.body,
				created_at=EXCLUDED.created_at, updated_at=EXCLUDED.updated_at,
				deleted_at=NULL
			WHERE knowledge.deleted_at IS NOT NULL`,
			k.Type, k.ID, k.Title, k.Description, k.Resource, k.Tags, k.Status, k.StatusNote,
			k.CreatedBy.Kind, k.CreatedBy.Name, k.CreatedBy.Via,
			verifiedKind, verifiedName, verifiedVia, k.VerifiedAt,
			rejectedKind, rejectedName, rejectedVia, k.RejectedAt,
			links, attrs, k.Body, k.CreatedAt, k.UpdatedAt)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrAlreadyExists
		}
		return s.addRevision(ctx, tx, k, "create", k.CreatedBy)
	})
}

// Update writes k over the live entry with the same id. When ifMatch is
// non-nil, the write is conditional on the stored updated_at equalling it
// (optimistic concurrency, design doc 0025 §11): a mismatch means the entry
// changed since the caller read it and returns ErrConflict, closing the
// read-modify-write race that silently lost updates. A nil ifMatch keeps
// the prior last-write-wins behavior for callers that do not opt in.
func (s *Store) Update(ctx context.Context, k *domain.Knowledge, actor domain.Actor, ifMatch *time.Time) error {
	k.UpdatedAt = nowStored()
	return s.withTx(ctx, func(tx pgx.Tx) error {
		links, attrs, err := marshalJSONFields(k)
		if err != nil {
			return err
		}
		verifiedKind, verifiedName, verifiedVia := actorPtrs(k.VerifiedBy)
		rejectedKind, rejectedName, rejectedVia := actorPtrs(k.RejectedBy)
		cond := ""
		args := []any{k.ID, k.Type, k.Title, k.Description, k.Resource, k.Tags, k.Status, k.StatusNote,
			verifiedKind, verifiedName, verifiedVia, k.VerifiedAt,
			rejectedKind, rejectedName, rejectedVia, k.RejectedAt,
			links, attrs, k.Body, k.UpdatedAt}
		if ifMatch != nil {
			args = append(args, ifMatch.UTC())
			cond = fmt.Sprintf(" AND updated_at=$%d", len(args))
		}
		tag, err := tx.Exec(ctx, `UPDATE knowledge SET
			type=$2, title=$3, description=$4, resource=$5, tags=$6, status=$7, status_note=$8,
			verified_by_kind=$9, verified_by_name=$10, verified_by_via=$11, verified_at=$12,
			rejected_by_kind=$13, rejected_by_name=$14, rejected_by_via=$15, rejected_at=$16,
			links=$17, attrs=$18, body=$19, updated_at=$20
			WHERE id=$1 AND deleted_at IS NULL`+cond, args...)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			// No row matched. With a precondition, tell a live-but-changed
			// entry (ErrConflict) apart from a missing one (ErrNotFound).
			if ifMatch != nil {
				var live bool
				if err := tx.QueryRow(ctx,
					`SELECT EXISTS (SELECT 1 FROM knowledge WHERE id=$1 AND deleted_at IS NULL)`,
					k.ID).Scan(&live); err != nil {
					return err
				}
				if live {
					return ErrConflict
				}
			}
			return ErrNotFound
		}
		return s.addRevision(ctx, tx, k, "update", actor)
	})
}

// SoftDelete hides an entry from reads while keeping full history.
// Create on the same id revives it.
func (s *Store) SoftDelete(ctx context.Context, id string, actor domain.Actor) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		k, err := s.Get(ctx, id)
		if err != nil {
			return err
		}
		// deleted_at IS NULL guards the race with a concurrent delete: the
		// Get above ran outside this transaction, and a double delete must
		// not record a second "delete" revision.
		tag, err := tx.Exec(ctx,
			`UPDATE knowledge SET deleted_at = now(), updated_at = now()
			 WHERE id=$1 AND deleted_at IS NULL`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if err := execTolerateMissingTable(ctx, tx, `DELETE FROM knowledge_embedding WHERE id=$1`, id); err != nil {
			return err
		}
		return s.addRevision(ctx, tx, k, "delete", actor)
	})
}

// Purge hard-deletes a soft-deleted entry and everything keyed by its id:
// revisions, usage totals and events, embeddings, attachment metadata.
// This is how an id comes back into circulation for a move — Create
// already revives a soft-deleted entry in place, but Move cannot, because
// the tombstone still owns the primary key and its revisions still own
// (id, rev).
//
// Only a soft-deleted entry can be purged. Delete first, purge second: no
// single call erases history, and the entry stays recoverable (Create
// revives it) right up until someone deliberately asks for it to be gone.
//
// Attachment bytes are left in the blob store, as DeleteAttachment leaves
// them: blobs are content-addressed and shared between entries, so
// reclaiming them cannot be a side effect of one purge. No sweep that
// reclaims them exists yet — GCS grows monotonically, and an operator who
// cares has to write the sweep themselves.
//
// The purge itself is recorded in knowledge_purge (migration 0018), which
// is the one table it does not touch: the entry is gone, but the fact
// that someone destroyed it is not.
func (s *Store) Purge(ctx context.Context, id string, actor domain.Actor) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		var deleted bool
		err := tx.QueryRow(ctx,
			`SELECT deleted_at IS NOT NULL FROM knowledge WHERE id=$1`, id).Scan(&deleted)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if !deleted {
			return ErrNotDeleted
		}
		if _, err := tx.Exec(ctx, `INSERT INTO knowledge_purge
			(id, type, title, revisions, purged_by_kind, purged_by_name, purged_by_via)
			SELECT k.id, k.type, k.title,
			       (SELECT count(*) FROM knowledge_revision r WHERE r.id = k.id),
			       $2, $3, $4
			  FROM knowledge k WHERE k.id = $1`,
			id, actor.Kind, actor.Name, actor.Via); err != nil {
			return err
		}
		for _, q := range []string{
			`DELETE FROM attachment WHERE knowledge_id=$1`,
			`DELETE FROM knowledge_usage WHERE knowledge_id=$1`,
			`DELETE FROM knowledge_event WHERE knowledge_id=$1`,
			`DELETE FROM knowledge_revision WHERE id=$1`,
			`DELETE FROM knowledge WHERE id=$1`,
		} {
			if _, err := tx.Exec(ctx, q, id); err != nil {
				return err
			}
		}
		// Embedding tables exist only once semantic search has been enabled.
		for _, q := range []string{
			`DELETE FROM knowledge_embedding WHERE id=$1`,
			`DELETE FROM attachment_embedding WHERE knowledge_id=$1`,
		} {
			if err := execTolerateMissingTable(ctx, tx, q, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// Move renames an entry to a new id. The id is the address (design doc
// 0017), so everything keyed by it follows in one transaction — the row,
// its revisions, attachments, usage, events, and embeddings — and live
// entries that reference the old id (link targets in both bare and
// ochakai:// forms, and attrs.model) are rewritten so no reference
// breaks. Attachment bytes never move: blobs are content-addressed
// (design doc 0011). The destination must be a fresh id — a row there,
// even soft-deleted, already owns that address and its revision history.
func (s *Store) Move(ctx context.Context, oldID, newID string, actor domain.Actor) (*domain.Knowledge, error) {
	k, err := s.Get(ctx, oldID)
	if err != nil {
		return nil, err
	}
	k.UpdatedAt = nowStored()
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
			`SELECT true, deleted_at IS NOT NULL FROM knowledge WHERE id=$1`, newID).
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
			`UPDATE knowledge SET id=$2, updated_at=$3 WHERE id=$1 AND deleted_at IS NULL`,
			oldID, newID, k.UpdatedAt)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		for _, q := range []string{
			`UPDATE knowledge_revision SET id=$2 WHERE id=$1`,
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
			`UPDATE attachment_embedding SET knowledge_id=$2 WHERE knowledge_id=$1`,
		} {
			if err := execTolerateMissingTable(ctx, tx, q, oldID, newID); err != nil {
				return err
			}
		}
		if err := s.rewriteReferences(ctx, tx, oldID, newID, actor); err != nil {
			return err
		}
		k.ID = newID
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
// so a self-referencing entry (already at newID) is covered too.
//
// The body is what gets rewritten: links are derived from it (design doc
// 0024), so repairing the links column alone would leave the author's
// prose pointing at an id that no longer exists. The links column is then
// re-derived from the repaired body.
//
// Candidates come from the links column, which still holds the pre-move
// derivation and so is an accurate index of who refers to oldID.
func (s *Store) rewriteReferences(ctx context.Context, tx pgx.Tx, oldID, newID string, actor domain.Actor) error {
	rows, err := tx.Query(ctx,
		`SELECT `+knowledgeCols+` FROM knowledge
		 WHERE deleted_at IS NULL AND (links @> $1 OR links @> $2 OR attrs->>'model' = $3 OR id = $4)`,
		fmt.Sprintf(`[{"target": %q}]`, oldID),
		fmt.Sprintf(`[{"target": %q}]`, "ochakai://"+oldID),
		oldID, newID)
	if err != nil {
		return err
	}
	referrers, err := pgx.CollectRows(rows, scanKnowledge)
	if err != nil {
		return err
	}
	now := nowStored()
	for i := range referrers {
		r := &referrers[i]
		// The moved entry resolves its own relative links against its old
		// directory, so it is rewritten as if it still lived at oldID.
		from := r.ID
		if r.ID == newID {
			from = oldID
		}
		body := domain.RewriteBodyLinks(from, r.Body, oldID, newID)
		modelMoved := false
		if m, ok := r.Attrs["model"].(string); ok && m == oldID {
			r.Attrs["model"] = newID
			modelMoved = true
		}
		if body == r.Body && !modelMoved {
			continue // matched the links index but nothing to repair
		}
		r.Body = body
		r.Links = domain.LinksFromBody(r.ID, r.Body)
		links, attrs, err := marshalJSONFields(r)
		if err != nil {
			return err
		}
		r.UpdatedAt = now
		if _, err := tx.Exec(ctx,
			`UPDATE knowledge SET links=$2, attrs=$3, body=$4, updated_at=$5 WHERE id=$1`,
			r.ID, links, attrs, r.Body, r.UpdatedAt); err != nil {
			return err
		}
		if err := s.addRevision(ctx, tx, r, "update", actor); err != nil {
			return err
		}
	}
	return nil
}

// ListRevisions returns an entry's change history, newest first. It
// reads history, so it works for soft-deleted entries too — the audit
// trail is most interesting exactly when the entry is gone.
func (s *Store) ListRevisions(ctx context.Context, id string, limit int) ([]domain.Revision, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT rev, change, changed_by_kind, changed_by_name, changed_by_via, changed_at, snapshot
		 FROM knowledge_revision WHERE id=$1 ORDER BY rev DESC LIMIT $2`,
		id, limit)
	if err != nil {
		return nil, err
	}
	revs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Revision, error) {
		var r domain.Revision
		var snapshot []byte
		if err := row.Scan(&r.Rev, &r.Change, &r.ChangedBy.Kind, &r.ChangedBy.Name, &r.ChangedBy.Via, &r.ChangedAt, &snapshot); err != nil {
			return r, err
		}
		return r, json.Unmarshal(snapshot, &r.Snapshot)
	})
	if err != nil {
		return nil, err
	}
	if len(revs) == 0 {
		// Distinguish "no such entry" from "entry with empty history"
		// (the latter cannot happen: create always writes rev 1).
		return nil, ErrNotFound
	}
	return revs, nil
}

func (s *Store) addRevision(ctx context.Context, tx pgx.Tx, k *domain.Knowledge, change string, actor domain.Actor) error {
	snapshot, err := json.Marshal(k)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO knowledge_revision (id, rev, change, changed_by_kind, changed_by_name, changed_by_via, snapshot)
		VALUES ($1, (SELECT COALESCE(MAX(rev), 0) + 1 FROM knowledge_revision WHERE id=$1), $2, $3, $4, $5, $6)`,
		k.ID, change, actor.Kind, actor.Name, actor.Via, snapshot)
	return err
}

// maxQueryFragments bounds how many pieces a query is split into, so a
// pasted paragraph cannot turn one search into a hundred index probes.
const maxQueryFragments = 24

// queryFragments splits a search query into the pieces a document might
// actually contain.
//
// Matching the query as a whole is useless for the input get_context is
// built for. Its documented argument is a data question, and the recall
// hook feeds it a raw user prompt — "why is revenue down?" appears
// verbatim in no body, and trigram similarity between a short query and a
// multi-kilobyte document is 0.01-0.1 at best (for Japanese, where the
// three-character windows of a question and a body almost never align, it
// is routinely exactly 0). Neither a substring test nor a similarity
// threshold can find anything from that. The signal is not the question;
// it is the terms inside it.
//
// Latin-script tokens stay whole: "revenue" is a word, and its pieces are
// not. Runs of Han/Hiragana/Katakana have no spaces to split on, so they
// become sliding two-character windows — the standard n-gram treatment
// for Japanese, and the smallest unit that still carries meaning (売上,
// 受注, 原価).
//
// Two characters is below what a trigram index can serve, and pg_trgm
// does not narrow the scan for such a pattern: LIKE '%売上%' has no word
// boundary to pad against, so no whole trigram can be extracted and the
// planner reads the table. Measured on 5000 entries, '%revenue%' and
// '%売上の%' are index scans at 0.2ms while '%売上%' is a sequential scan
// at 16ms. Three-character windows would restore the index and lose 売上,
// 原価, 客数 — the terms the search is for — so the scan is the price of
// finding them. SearchLexical keeps that scan cheap by scoring over ids
// alone.
func queryFragments(query string) []string {
	// Fragments are collected per run and then interleaved, so the cap
	// falls evenly across the query instead of truncating its tail. A
	// question often names its subject last ("〜の件で、直近の原価率は?"),
	// and cutting in reading order would drop exactly that.
	var runs [][]string
	for _, token := range strings.FieldsFunc(query, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		// A token can still mix scripts, because FieldsFunc treats latin
		// and kanji alike as letters: "PL科目", "売上KPI", "EC売上" all
		// arrive whole. Dispatching on the first character alone would
		// send "PL科目" through the latin path (matching only the exact
		// string "PL科目") and cut "売上KPI" into windows that break KPI
		// apart. Domain vocabulary is full of these, so split at the
		// script boundary first and treat each run on its own terms.
		for _, run := range scriptRuns(token) {
			runes := []rune(run)
			if !scriptWithoutSpaces(runes[0]) || len(runes) <= 2 {
				runs = append(runs, []string{run})
				continue
			}
			runs = append(runs, contentWindows(runes))
		}
	}

	var out []string
	seen := map[string]bool{}
	for round := 0; len(out) < maxQueryFragments; round++ {
		progressed := false
		for _, run := range runs {
			if round >= len(run) {
				continue
			}
			progressed = true
			if frag := run[round]; !seen[frag] {
				seen[frag] = true
				if out = append(out, frag); len(out) >= maxQueryFragments {
					break
				}
			}
		}
		if !progressed {
			break
		}
	}
	if len(out) == 0 {
		// A single character, or punctuation only: match it as written
		// rather than matching nothing.
		if q := strings.TrimSpace(query); q != "" {
			out = append(out, q)
		}
	}
	return out
}

// contentWindows cuts a space-less run into two-character windows and
// keeps the ones that begin with a content character.
//
// Japanese grammar is written in hiragana and content words in kanji or
// katakana, so a window's first character says which it is: 売上 and 下が
// (the stem of 下がる) begin a word, while ぜ売 and が下 straddle a
// boundary and mean nothing. Keeping only the former drops both the
// function words, which match nearly every entry, and the accidental
// fragments, which match almost none — and the latter matter more than
// they look, because scoring weights rare fragments highest and a
// meaningless one is rare by construction.
//
// A run with no content character at all — an all-kana query like
// ください — keeps every window, or it would match nothing.
func contentWindows(runes []rune) []string {
	all := make([]string, 0, len(runes))
	var content []string
	for i := 0; i+2 <= len(runes); i++ {
		w := string(runes[i : i+2])
		all = append(all, w)
		if contentChar(runes[i]) {
			content = append(content, w)
		}
	}
	if len(content) > 0 {
		return content
	}
	return all
}

// scriptRuns splits a token wherever it crosses between a space-less
// script (Han, kana) and anything else, so "PL科目" becomes "PL" and
// "科目".
func scriptRuns(token string) []string {
	var runs []string
	runes := []rune(token)
	start := 0
	for i := 1; i < len(runes); i++ {
		if scriptWithoutSpaces(runes[i]) != scriptWithoutSpaces(runes[start]) {
			runs = append(runs, string(runes[start:i]))
			start = i
		}
	}
	return append(runs, string(runes[start:]))
}

// scriptWithoutSpaces reports whether r belongs to a script that does not
// separate words with spaces, so a token of it needs windowing rather than
// taking whole.
func scriptWithoutSpaces(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r)
}

// contentChar reports whether r is the kind of character a Japanese
// content word is written in, as opposed to the hiragana that spells the
// grammar around it.
func contentChar(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Katakana, r)
}

// SearchLexical ranks entries by how much of the query they contain,
// verified entries boosted. The haystack is the search_text column
// (migration 0016): the id (design doc 0022 — with title optional, the
// filename may be the entry's only name), the envelope fields, the body,
// and the attachment filenames (design doc 0020 — "seeds" finds the entry
// carrying seeds.txt, embedder or not), maintained by trigger.
//
// The score is the fraction of the query's fragments the entry contains,
// plus a bonus when the whole query appears verbatim (a keyword search
// should outrank an entry that merely shares terms with a question), plus
// the verified boost. An entry containing none of the fragments is not a
// result — the floor is "matched something", not a magic number.
//
// Every term of that is a substring test against one column, which is
// what the GIN trigram index serves, so the candidate predicate and the
// score read the same expressions (migration 0016). Fragment matching
// replaced a similarity()/`%` pair that could not work: `%` is true only
// above pg_trgm.similarity_threshold (0.3) and a real query never scores
// that against a real document, so the candidate set was whatever the
// whole-query substring test found — nothing, for a question.
func (s *Store) SearchLexical(ctx context.Context, query string, f Filter, limit int) ([]domain.SearchHit, error) {
	where, args := f.buildWhere("k.")
	// Each pattern is escaped: unescaped, a query containing '%' matches
	// every entry and flattens the ranking ('_' matches any one character).
	args = append(args, "%"+escapeLike(query)+"%")
	wholeParam := len(args)
	frags := queryFragments(query)
	var hit, weight, weighted, weights, tests []string
	for i, frag := range frags {
		args = append(args, "%"+escapeLike(frag)+"%")
		tests = append(tests, fmt.Sprintf("k.search_text ILIKE $%d", len(args)))
		hit = append(hit, fmt.Sprintf("k.search_text ILIKE $%d AS h%d", len(args), i))
		// Document frequency over the candidates, not the whole table: one
		// window pass, no extra scans.
		weight = append(weight, fmt.Sprintf(
			"ln(1 + total::float / GREATEST(count(*) FILTER (WHERE h%d) OVER (), 1)) AS w%d", i, i))
		weighted = append(weighted, fmt.Sprintf("CASE WHEN h%d THEN w%d ELSE 0 END", i, i))
		weights = append(weights, fmt.Sprintf("w%d", i))
	}
	// Scoring carries ids and booleans, never rows. The window functions
	// force every candidate to be materialized before LIMIT can apply, and
	// the candidate set is an OR over fragments — one common two-character
	// window and it is most of the table. Selecting k.* through those
	// layers would push every body, and search_text (a near-copy of the
	// body) alongside it, through a sort node: tens of megabytes on a
	// corpus of a few thousand, spilling work_mem on the instance size
	// this is meant to run on. The entries are fetched once the top N is
	// known.
	q := fmt.Sprintf(`
		WITH scored AS (
			SELECT w.id, (%[1]s) / NULLIF(%[2]s, 0) + w.whole + w.verified AS score
			FROM (
				SELECT c.*, %[3]s FROM (
					SELECT k.id, %[4]s, count(*) OVER () AS total,
						CASE WHEN k.search_text ILIKE $%[5]d THEN 0.3 ELSE 0 END AS whole,
						CASE WHEN k.status = 'verified' THEN 0.05 ELSE 0 END AS verified
					FROM knowledge k
					WHERE (%[6]s) AND %[7]s
				) c
			) w
			ORDER BY score DESC LIMIT %[8]d
		)
		SELECT `+qualifyCols("k")+`, scored.score
		FROM knowledge k JOIN scored ON scored.id = k.id
		ORDER BY scored.score DESC`,
		strings.Join(weighted, " + "), strings.Join(weights, " + "),
		strings.Join(weight, ", "), strings.Join(hit, ", "), wholeParam,
		strings.Join(tests, " OR "), where, limit)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.SearchHit, error) {
		return scanHit(row)
	})
}

// SearchVector ranks by cosine distance against stored embeddings.
func (s *Store) SearchVector(ctx context.Context, vec []float32, f Filter, limit int) ([]domain.SearchHit, error) {
	// Columns must be qualified: knowledge_embedding shares id/updated_at.
	where, args := f.buildWhere("k.")
	args = append(args, encodeVector(vec))
	q := fmt.Sprintf(`
		SELECT `+qualifyCols("k")+`, 1 - (e.embedding <=> $%d::vector) AS score
		FROM knowledge k JOIN knowledge_embedding e ON k.id = e.id
		WHERE %s
		ORDER BY e.embedding <=> $%d::vector LIMIT %d`, len(args), where, len(args), limit)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.SearchHit, error) {
		return scanHit(row)
	})
}

// SearchVectorAttachments ranks entries by their closest attachment
// embedding (design doc 0020). Each entry appears once, carrying its
// best attachment's score — attachments never stand alone, so the hit
// is the owning entry.
func (s *Store) SearchVectorAttachments(ctx context.Context, vec []float32, f Filter, limit int) ([]domain.SearchHit, error) {
	where, args := f.buildWhere("k.")
	args = append(args, encodeVector(vec))
	q := fmt.Sprintf(`
		SELECT `+knowledgeCols+`, score FROM (
			SELECT DISTINCT ON (k.id) k.*, 1 - (e.embedding <=> $%d::vector) AS score
			FROM knowledge k JOIN attachment_embedding e ON k.id = e.knowledge_id
			WHERE %s
			ORDER BY k.id, e.embedding <=> $%d::vector
		) best
		ORDER BY score DESC LIMIT %d`, len(args), where, len(args), limit)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.SearchHit, error) {
		return scanHit(row)
	})
}

func scanHit(row pgx.CollectableRow) (domain.SearchHit, error) {
	var h domain.SearchHit
	dests, finish := knowledgeDest(&h.Knowledge)
	if err := row.Scan(append(dests, &h.Score)...); err != nil {
		return h, err
	}
	return h, finish()
}

// buildWhere renders filter conditions; prefix qualifies columns (e.g. "k.")
// for joined queries and may be empty. Without an explicit status filter,
// rejected entries are excluded: knowledge that was never accepted must not
// resurface in answers, but remains queryable on request so agents can check
// whether a proposal was already rejected.
func (f Filter) buildWhere(prefix string) (string, []any) {
	conds := []string{prefix + "deleted_at IS NULL"}
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
	} else {
		conds = append(conds, fmt.Sprintf("%sstatus <> 'rejected'", prefix))
	}
	if len(f.Tags) > 0 {
		args = append(args, f.Tags)
		conds = append(conds, fmt.Sprintf("%stags && $%d", prefix, len(args)))
	}
	return strings.Join(conds, " AND "), args
}

// ListByVerifiedAt returns filtered entries ordered by verification age,
// oldest first (never-verified entries last). This is the feed for golden
// query canary runs: "which verified queries have gone longest unchecked".
func (s *Store) ListByVerifiedAt(ctx context.Context, f Filter, limit int) ([]domain.Knowledge, error) {
	where, args := f.buildWhere("")
	q := fmt.Sprintf(`SELECT `+knowledgeCols+` FROM knowledge WHERE %s
		ORDER BY verified_at ASC NULLS LAST, id LIMIT %d`, where, limit)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanKnowledge)
}

// usageLateral aggregates a knowledge entry's per-event running totals
// (see usage.go) in one pass, exposing them as u.search_hits … u.failed and
// u.last_used_at. Shared by the usage-carrying list feeds (ListByUsage,
// ListByFailed), which select the same projection but order it differently.
const usageLateral = `
	LEFT JOIN LATERAL (
		SELECT
			COALESCE(sum(count) FILTER (WHERE event = 'search_hit'), 0) AS search_hits,
			COALESCE(sum(count) FILTER (WHERE event = 'fetched'), 0)   AS fetches,
			COALESCE(sum(count) FILTER (WHERE event = 'compiled'), 0)  AS compiles,
			COALESCE(sum(count) FILTER (WHERE event = 'worked'), 0)    AS worked,
			COALESCE(sum(count) FILTER (WHERE event = 'failed'), 0)    AS failed,
			max(last_at) AS last_used_at
		FROM knowledge_usage
		WHERE knowledge_id = k.id
	) u ON true`

// ListByUsage returns filtered entries ordered by demand, most-searched
// first: search_hits descending, then oldest-created (created_at ascending)
// as the tiebreak. This is the draft review feed — the promotion queue at
// the top, and never-used drafts (search_hits 0) sinking oldest-first to
// the bottom for inventory. Each hit carries its usage totals so the
// caller renders the signal without a per-entry round trip. Score is 0.
func (s *Store) ListByUsage(ctx context.Context, f Filter, limit int) ([]domain.SearchHit, error) {
	where, args := f.buildWhere("k.")
	q := fmt.Sprintf(`
		SELECT `+qualifyCols("k")+`,
			u.search_hits, u.fetches, u.compiles, u.worked, u.failed, u.last_used_at
		FROM knowledge k`+usageLateral+`
		WHERE %s
		ORDER BY u.search_hits DESC, k.created_at ASC, k.id LIMIT %d`, where, limit)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanUsageHit)
}

// ListByFailed returns filtered entries that callers have reported wrong,
// worst first — the re-verification feed (design doc 0025). It is the
// evidence-based counterpart to the verified_at feed's time-based
// staleness: only entries with a failed outcome report appear (u.failed > 0),
// so a healthy base yields an empty feed. Ordering: most failures first,
// ties broken by fewest corroborating "worked" reports, then verification
// age (oldest first, never-verified drafts last), then id — verified
// knowledge being reported wrong outranks a failing draft. Each hit carries
// its usage totals so the reviewer sees the worked/failed evidence inline;
// score is 0.
func (s *Store) ListByFailed(ctx context.Context, f Filter, limit int) ([]domain.SearchHit, error) {
	where, args := f.buildWhere("k.")
	q := fmt.Sprintf(`
		SELECT `+qualifyCols("k")+`,
			u.search_hits, u.fetches, u.compiles, u.worked, u.failed, u.last_used_at
		FROM knowledge k`+usageLateral+`
		WHERE %s AND u.failed > 0
		ORDER BY u.failed DESC, u.worked ASC, k.verified_at ASC NULLS LAST, k.id LIMIT %d`, where, limit)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanUsageHit)
}

// scanUsageHit reads a knowledge row followed by its usage totals (the
// ListByUsage projection). Score stays 0 — this is a listing, not a search.
func scanUsageHit(row pgx.CollectableRow) (domain.SearchHit, error) {
	var h domain.SearchHit
	var u domain.Usage
	dests, finish := knowledgeDest(&h.Knowledge)
	if err := row.Scan(append(dests,
		&u.SearchHits, &u.Fetches, &u.Compiles, &u.Worked, &u.Failed, &u.LastUsedAt)...); err != nil {
		return h, err
	}
	if err := finish(); err != nil {
		return h, err
	}
	h.Usage = &u
	return h, nil
}

// ListAll returns every non-deleted entry, ordered by id. Used by the
// OKF exporter.
func (s *Store) ListAll(ctx context.Context) ([]domain.Knowledge, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+knowledgeCols+` FROM knowledge WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanKnowledge)
}

// ListAllIndexRows returns every live entry with only the fields the
// bundle's index.md files are rendered from — id, type, title,
// description. The bodies are the bulk of a knowledge base, and the
// indexes need none of them, so the exporter reads this first and pulls
// the documents themselves in batches (ListByIDs).
func (s *Store) ListAllIndexRows(ctx context.Context) ([]domain.Knowledge, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT type, id, title, description FROM knowledge WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Knowledge, error) {
		var k domain.Knowledge
		err := row.Scan(&k.Type, &k.ID, &k.Title, &k.Description)
		return k, err
	})
}

// ListByIDs returns the named live entries in id order. The exporter walks
// the id list in batches so that neither the whole knowledge base nor a
// pool connection is held for the length of a download.
func (s *Store) ListByIDs(ctx context.Context, ids []string) ([]domain.Knowledge, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+knowledgeCols+` FROM knowledge
		 WHERE deleted_at IS NULL AND id = ANY($1) ORDER BY id`, ids)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanKnowledge)
}

// ListUnembedded returns the ids of live entries with no vector for the
// named model, oldest first, up to limit. That covers both halves of the
// same gap: entries written before semantic search was configured (there
// is no row), and entries whose vector belongs to a model that has since
// been changed (design doc 0020 — the old vectors sit in a space nobody
// queries any more).
func (s *Store) ListUnembedded(ctx context.Context, model string, limit int) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT k.id FROM knowledge k
		 LEFT JOIN knowledge_embedding e ON e.id = k.id AND e.model = $1
		 WHERE k.deleted_at IS NULL AND e.id IS NULL
		 ORDER BY k.created_at LIMIT $2`, model, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// CountUnembedded counts the live entries with no vector for the named
// model. Reembed reports it so a bounded pass cannot be mistaken for a
// finished one.
func (s *Store) CountUnembedded(ctx context.Context, model string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM knowledge k
		 LEFT JOIN knowledge_embedding e ON e.id = k.id AND e.model = $1
		 WHERE k.deleted_at IS NULL AND e.id IS NULL`, model).Scan(&n)
	return n, err
}

// UpsertEmbedding stores the document embedding for a knowledge entry.
func (s *Store) UpsertEmbedding(ctx context.Context, id, model string, vec []float32) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO knowledge_embedding (id, model, embedding, updated_at)
		VALUES ($1, $2, $3::vector, now())
		ON CONFLICT (id) DO UPDATE SET model = $2, embedding = $3::vector, updated_at = now()`,
		id, model, encodeVector(vec))
	return err
}

// UpsertAttachmentEmbedding stores the document embedding for one
// attachment (design doc 0020).
func (s *Store) UpsertAttachmentEmbedding(ctx context.Context, id, name, model string, vec []float32) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO attachment_embedding (knowledge_id, name, model, embedding, updated_at)
		VALUES ($1, $2, $3, $4::vector, now())
		ON CONFLICT (knowledge_id, name) DO UPDATE SET model = $3, embedding = $4::vector, updated_at = now()`,
		id, name, model, encodeVector(vec))
	return err
}

// execTolerateMissingTable runs sql inside tx, treating an undefined
// table as a no-op. Embedding tables only exist once semantic search has
// been enabled, and a failed statement aborts the whole Postgres
// transaction, so the statement runs under a savepoint.
func execTolerateMissingTable(ctx context.Context, tx pgx.Tx, sql string, args ...any) error {
	sp, err := tx.Begin(ctx)
	if err != nil {
		return err
	}
	if _, err := sp.Exec(ctx, sql, args...); err != nil {
		_ = sp.Rollback(ctx)
		if !isUndefinedTable(err) {
			return err
		}
		return nil
	}
	return sp.Commit(ctx)
}

func (s *Store) withTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

func marshalJSONFields(k *domain.Knowledge) (links, attrs []byte, err error) {
	if k.Tags == nil {
		k.Tags = []string{}
	}
	if k.Links == nil {
		k.Links = []domain.Link{}
	}
	if k.Attrs == nil {
		k.Attrs = map[string]any{}
	}
	if links, err = json.Marshal(k.Links); err != nil {
		return nil, nil, err
	}
	if attrs, err = json.Marshal(k.Attrs); err != nil {
		return nil, nil, err
	}
	return links, attrs, nil
}

// qualifyCols prefixes every column in knowledgeCols with a table alias.
func qualifyCols(alias string) string {
	cols := strings.Split(knowledgeCols, ",")
	for i, c := range cols {
		cols[i] = alias + "." + strings.TrimSpace(c)
	}
	return strings.Join(cols, ", ")
}

// escapeLike neutralizes the LIKE/ILIKE pattern metacharacters '%' and '_'
// in a user query so it matches literally. PostgreSQL's default LIKE escape
// character is the backslash, so the backslash itself is escaped first.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// encodeVector renders a pgvector literal like "[0.1,0.2]".
func encodeVector(vec []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, v := range vec {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%g", v)
	}
	b.WriteByte(']')
	return b.String()
}

func isUndefinedTable(err error) bool {
	return err != nil && strings.Contains(err.Error(), "42P01")
}
