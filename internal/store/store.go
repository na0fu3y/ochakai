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
// reclaiming them is a separate sweep, not a side effect of one purge.
func (s *Store) Purge(ctx context.Context, id string) error {
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
// 受注, 原価). Two characters is below what a trigram index can resolve
// exactly, but pg_trgm still narrows the scan with partial trigrams.
func queryFragments(query string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(frag string) {
		if frag == "" || seen[frag] || len(out) >= maxQueryFragments {
			return
		}
		seen[frag] = true
		out = append(out, frag)
	}
	for _, token := range strings.FieldsFunc(query, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		runes := []rune(token)
		if !scriptWithoutSpaces(runes[0]) || len(runes) <= 2 {
			add(token)
			continue
		}
		// Windows that carry a content character first. Japanese grammar
		// is written in hiragana, so an all-hiragana window (ってい, のか)
		// is a function word: it matches nearly every entry and would
		// drown the one window that names the subject. Han and katakana
		// carry the meaning. A run with no content character at all —
		// an all-kana query — keeps its windows, or it would match
		// nothing.
		windows := make([]string, 0, len(runes))
		var content []string
		for i := 0; i+2 <= len(runes); i++ {
			w := string(runes[i : i+2])
			windows = append(windows, w)
			if strings.ContainsFunc(w, contentChar) {
				content = append(content, w)
			}
		}
		if len(content) > 0 {
			windows = content
		}
		for _, w := range windows {
			add(w)
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
	var hit, weight, weighted, weights []string
	for i, frag := range frags {
		args = append(args, "%"+escapeLike(frag)+"%")
		hit = append(hit, fmt.Sprintf("k.search_text ILIKE $%d AS h%d", len(args), i))
		// Document frequency over the candidates, not the whole table: one
		// window pass, no extra scans.
		weight = append(weight, fmt.Sprintf(
			"ln(1 + total::float / GREATEST(count(*) FILTER (WHERE h%d) OVER (), 1)) AS w%d", i, i))
		weighted = append(weighted, fmt.Sprintf("CASE WHEN h%d THEN w%d ELSE 0 END", i, i))
		weights = append(weights, fmt.Sprintf("w%d", i))
	}
	tests := make([]string, len(frags))
	for i := range frags {
		tests[i] = fmt.Sprintf("k.search_text ILIKE $%d", wholeParam+1+i)
	}
	q := fmt.Sprintf(`
		SELECT `+knowledgeCols+`, score FROM (
			SELECT w.*, (%[1]s) / NULLIF(%[2]s, 0)
				+ CASE WHEN w.search_text ILIKE $%[3]d THEN 0.3 ELSE 0 END
				+ CASE WHEN w.status = 'verified' THEN 0.05 ELSE 0 END AS score
			FROM (
				SELECT c.*, %[4]s FROM (
					SELECT k.*, %[5]s, count(*) OVER () AS total
					FROM knowledge k
					WHERE (%[6]s) AND %[7]s
				) c
			) w
		) ranked
		ORDER BY score DESC LIMIT %[8]d`,
		strings.Join(weighted, " + "), strings.Join(weights, " + "), wholeParam,
		strings.Join(weight, ", "), strings.Join(hit, ", "),
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
