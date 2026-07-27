// Package store persists knowledge in PostgreSQL, the only runtime
// dependency of ochakai.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/na0fu3y/ochakai/internal/blob"
	"github.com/na0fu3y/ochakai/internal/domain"
)

// isUniqueViolation reports whether err is PostgreSQL's unique_violation
// (SQLSTATE 23505) — a race lost to another writer on the same key.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

var ErrNotFound = errors.New("knowledge not found")
var ErrAlreadyExists = errors.New("knowledge already exists")

// ErrConflict is returned by Update when an If-Match precondition does not
// match the stored revision: the entry changed since the caller read it
// (design doc 0030). The entry exists — a missing entry is ErrNotFound.
var ErrConflict = errors.New("knowledge changed since it was read")

// ErrCuratedTombstone is returned by Create when the id holds a
// soft-deleted entry a human ruled on and the caller asked for curated
// tombstones to be kept (design doc 0015 §3.1). Distinct from
// ErrAlreadyExists: the blocking row is invisible to a plain read.
var ErrCuratedTombstone = errors.New("knowledge id holds a curated tombstone")

// ErrNotDeleted is returned by Purge for an entry that is still live.
// Purging is the second step of a two-step destruction: delete first, so
// no single call can erase history that was in use a moment ago.
var ErrNotDeleted = errors.New("knowledge is live; soft-delete it before purging")

// NowStored is the current UTC time truncated to the microsecond precision
// PostgreSQL timestamptz stores. Setting entity timestamps from it means an
// in-memory updated_at always equals the value that round-trips through the
// database — the invariant the ETag/If-Match optimistic lock depends on
// (design doc 0030 §3.2): time.Now()'s nanoseconds would otherwise make the
// value returned by a write differ from the stored one on nanosecond-
// resolution clocks (Linux), breaking a client's next conditional update.
//
// Exported because the service layer stamps timestamps of its own —
// verified_at, rejected_at — and they answer to the same column.
func NowStored() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

type Store struct {
	pool *pgxpool.Pool
	// blobs holds attachment bytes (GCS, design doc 0013); metadata stays
	// in PostgreSQL. When nil, attachments are unsupported — markdown
	// entries only.
	blobs blob.Store
	// lastEventPrune throttles knowledge_event pruning (unix seconds).
	lastEventPrune atomic.Int64
	// log carries what only the store can see. The background flush loop
	// has no caller to return an error to, so without this a database
	// that rejects writes loses usage in silence.
	log *slog.Logger

	// Usage events buffer in memory and flush on a timer so recording
	// never touches the read path (design doc 0029). usageBuf is
	// guarded by usageMu; the flush loop stops on flushStop and drains
	// once more before flushWG releases (see New/Close, usage.go).
	usageMu   sync.Mutex
	usageBuf  []usageEvent
	flushStop chan struct{}
	flushWG   sync.WaitGroup
}

// UseLogger routes the store's own diagnostics to l — the flush loop's
// failures, which no request is waiting on. Call before serving; the
// default is slog.Default().
func (s *Store) UseLogger(l *slog.Logger) {
	if l != nil {
		s.log = l
	}
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
	s := &Store{pool: pool, flushStop: make(chan struct{}), log: slog.Default()}
	s.flushWG.Add(1)
	go s.usageFlushLoop()
	return s, nil
}

// Close stops the usage flush loop (draining the buffer one last time) and
// releases the connection pool. Buffered events that have not yet flushed
// are lost only if the process dies before Close — usage is a best-effort
// statistic (design doc 0029).
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
	// Source narrows to entries citing this exact resource — the reverse
	// of sources[].resource, for "what derives from this material"
	// (design doc 0037 §2.3). Single-valued on purpose: the question is
	// about one artifact that changed, and a disjunction of containment
	// tests would not use the GIN index.
	Source string
}

const knowledgeCols = `type, id, title, description, resource, tags, status, status_note, stale_after,
	sources, usage_window, runtime, parameters, computation, executor, attester,
	created_by_kind, created_by_name, created_by_via,
	updated_by_kind, updated_by_name, updated_by_via,
	verified_by_kind, verified_by_name, verified_by_via, verified_at,
	rejected_by_kind, rejected_by_name, rejected_by_via, rejected_at,
	links, attrs, body, created_at, updated_at`

// The SQL fragments that restate knowledgeCols are derived from it once,
// so an envelope column is added by editing the list and knowledgeDest —
// not by hunting down hand-written copies in each write path.
var (
	knowledgeColNames = func() []string {
		cols := strings.Split(knowledgeCols, ",")
		for i, c := range cols {
			cols[i] = strings.TrimSpace(c)
		}
		return cols
	}()

	// knowledgeParams is the "$1,...,$N" placeholder list matching
	// knowledgeCols; an argument past the columns is $len+1.
	knowledgeParams = func() string {
		params := make([]string, len(knowledgeColNames))
		for i := range params {
			params[i] = fmt.Sprintf("$%d", i+1)
		}
		return strings.Join(params, ",")
	}()

	// knowledgeExcluded assigns every column except the key from EXCLUDED,
	// for Create's revive-in-place upsert. A column missing here would
	// silently keep the tombstone's stale value on revival, with no error
	// anywhere — which is why the list is derived rather than written out.
	knowledgeExcluded = func() string {
		var parts []string
		for _, c := range knowledgeColNames {
			if c == "id" {
				continue
			}
			parts = append(parts, c+"=EXCLUDED."+c)
		}
		return strings.Join(parts, ", ")
	}()

	// knowledgeColsK is knowledgeCols with every column prefixed by "k",
	// the alias every query that joins another table gives the knowledge
	// table. Built once: the column list is fixed at compile time, so
	// re-splitting it per query bought nothing.
	knowledgeColsK = func() string {
		cols := make([]string, len(knowledgeColNames))
		for i, c := range knowledgeColNames {
			cols[i] = "k." + c
		}
		return strings.Join(cols, ", ")
	}()
)

// knowledgeDest returns the scan destinations for knowledgeCols targeting
// k, plus a finish func that decodes the JSON columns and nullable actors
// after the scan. Row shapes with trailing columns (score, usage totals)
// append their own destinations — the column list lives here once.
func knowledgeDest(k *domain.Knowledge) (dests []any, finish func() error) {
	var verifiedKind, verifiedName, rejectedKind, rejectedName *string
	var verifiedVia, rejectedVia string
	var staleAfter *time.Time
	var links, attrs []byte
	var sources, usageWindow, parameters, executor, attester []byte
	dests = []any{&k.Type, &k.ID, &k.Title, &k.Description, &k.Resource, &k.Tags, &k.Status, &k.StatusNote, &staleAfter,
		&sources, &usageWindow, &k.Runtime, &parameters, &k.Computation, &executor, &attester,
		&k.CreatedBy.Kind, &k.CreatedBy.Name, &k.CreatedBy.Via,
		&k.UpdatedBy.Kind, &k.UpdatedBy.Name, &k.UpdatedBy.Via,
		&verifiedKind, &verifiedName, &verifiedVia, &k.VerifiedAt,
		&rejectedKind, &rejectedName, &rejectedVia, &k.RejectedAt,
		&links, &attrs, &k.Body, &k.CreatedAt, &k.UpdatedAt}
	finish = func() error {
		k.VerifiedBy = actorFrom(verifiedKind, verifiedName, verifiedVia)
		k.RejectedBy = actorFrom(rejectedKind, rejectedName, rejectedVia)
		if staleAfter != nil {
			k.StaleAfter = staleAfter.UTC().Format(domain.StaleAfterLayout)
		}
		// The nullable OKF columns decode only when present: an absent
		// usage_window is a nil pointer, not a zero-valued one, because
		// "no window declared" and "an empty window" are different claims.
		for _, d := range []struct {
			raw []byte
			out any
		}{
			{sources, &k.Sources}, {parameters, &k.Parameters},
			{usageWindow, &k.UsageWindow}, {executor, &k.Executor}, {attester, &k.Attester},
			{links, &k.Links}, {attrs, &k.Attrs},
		} {
			if len(d.raw) == 0 {
				continue
			}
			if err := json.Unmarshal(d.raw, d.out); err != nil {
				return err
			}
		}
		return nil
	}
	return dests, finish
}

// staleAfterArg turns the entry's stale_after into a date argument: NULL
// when unset. The value is validated at the service boundary, so a
// malformed one here is a bug, not user input — it fails the write rather
// than being silently dropped.
func staleAfterArg(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	d, err := time.Parse(domain.StaleAfterLayout, s)
	if err != nil {
		return nil, fmt.Errorf("stale_after %q: %w", s, err)
	}
	return &d, nil
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

// queryKnowledge runs a query selecting knowledgeCols and collects the
// entries it returns.
func (s *Store) queryKnowledge(ctx context.Context, sql string, args ...any) ([]domain.Knowledge, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanKnowledge)
}

// getOne returns the single entry holding id on the live or the deleted
// side of the row's lifetime, or ErrNotFound when that side is empty.
func (s *Store) getOne(ctx context.Context, id string, deleted bool) (*domain.Knowledge, error) {
	cond := "deleted_at IS NULL"
	if deleted {
		cond = "deleted_at IS NOT NULL"
	}
	rows, err := s.pool.Query(ctx,
		`SELECT `+knowledgeCols+` FROM knowledge WHERE id = $1 AND `+cond, id)
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

func (s *Store) Get(ctx context.Context, id string) (*domain.Knowledge, error) {
	return s.getOne(ctx, id, false)
}

// GetTombstone returns the soft-deleted entry holding id, or ErrNotFound
// when the id is free or live. Create revives such a row in place
// (ON CONFLICT ... WHERE deleted_at IS NOT NULL), overwriting its status
// and status_note, so a surface that must not overwrite a ruling has to
// be able to see the ruling a tombstone still carries.
func (s *Store) GetTombstone(ctx context.Context, id string) (*domain.Knowledge, error) {
	return s.getOne(ctx, id, true)
}

// ListLinkingTo returns live entries whose links point at id, most
// recently updated first. This is the reverse edge Context needs: the
// insight that explains a metric links to the metric, not the other way
// round. Both bare and ochakai:// target forms match.
func (s *Store) ListLinkingTo(ctx context.Context, id string, limit int) ([]domain.Knowledge, error) {
	return s.queryKnowledge(ctx,
		`SELECT `+knowledgeCols+` FROM knowledge
		 WHERE deleted_at IS NULL AND (links @> $1 OR links @> $2)
		 ORDER BY updated_at DESC LIMIT $3`,
		fmt.Sprintf(`[{"target": %q}]`, id),
		fmt.Sprintf(`[{"target": %q}]`, "ochakai://"+id),
		limit)
}

// Create inserts a new entry. A live entry with the same id is
// ErrAlreadyExists — including rejected ones, so the memory of no
// survives. A soft-deleted entry is revived instead: the ID would
// otherwise be dead forever (the row still owns the primary key while
// Update refuses deleted rows), and its history stays in the revisions
// either way.
func (s *Store) Create(ctx context.Context, k *domain.Knowledge, keepCuratedTombstones bool) error {
	now := NowStored()
	k.CreatedAt, k.UpdatedAt = now, now
	// Reviving a tombstone overwrites its status in place. When the caller
	// must not replace a human ruling (design doc 0015 §3.1), the revival
	// carries the same restriction as the UPDATE itself, so a curation
	// landing between a service-level check and this write loses the race
	// instead of being erased by it.
	revive := ""
	if keepCuratedTombstones {
		revive = fmt.Sprintf(` AND knowledge.status <> ALL($%d::text[])`, len(knowledgeColNames)+1)
	}
	return s.withTx(ctx, func(tx pgx.Tx) error {
		j, err := marshalJSONFields(k)
		if err != nil {
			return err
		}
		staleAfter, err := staleAfterArg(k.StaleAfter)
		if err != nil {
			return err
		}
		verifiedKind, verifiedName, verifiedVia := actorPtrs(k.VerifiedBy)
		rejectedKind, rejectedName, rejectedVia := actorPtrs(k.RejectedBy)
		args := []any{
			k.Type, k.ID, k.Title, k.Description, k.Resource, k.Tags, k.Status, k.StatusNote, staleAfter,
			j.sources, j.usageWindow, k.Runtime, j.parameters, k.Computation, j.executor, j.attester,
			k.CreatedBy.Kind, k.CreatedBy.Name, k.CreatedBy.Via,
			k.UpdatedBy.Kind, k.UpdatedBy.Name, k.UpdatedBy.Via,
			verifiedKind, verifiedName, verifiedVia, k.VerifiedAt,
			rejectedKind, rejectedName, rejectedVia, k.RejectedAt,
			j.links, j.attrs, k.Body, k.CreatedAt, k.UpdatedAt,
		}
		if keepCuratedTombstones {
			curated := make([]string, len(domain.CuratedStatuses))
			for i, st := range domain.CuratedStatuses {
				curated[i] = string(st)
			}
			args = append(args, curated)
		}
		tag, err := tx.Exec(ctx, `INSERT INTO knowledge (`+knowledgeCols+`)
			VALUES (`+knowledgeParams+`)
			ON CONFLICT (id) DO UPDATE SET `+knowledgeExcluded+`, deleted_at=NULL
			WHERE knowledge.deleted_at IS NOT NULL`+revive,
			args...)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			if keepCuratedTombstones {
				// Either a live entry or a curated tombstone blocked the
				// write; only the second is this guard's business, and the
				// caller needs to tell them apart to explain itself.
				if t, err := s.GetTombstone(ctx, k.ID); err == nil && t.Status.Curated() {
					return ErrCuratedTombstone
				} else if err != nil && !errors.Is(err, ErrNotFound) {
					return err
				}
			}
			return ErrAlreadyExists
		}
		return s.addRevision(ctx, tx, k, "create", k.CreatedBy)
	})
}

// Update writes k over the live entry with the same id. When ifMatch is
// non-nil, the write is conditional on the stored updated_at equalling it
// (optimistic concurrency, design doc 0030): a mismatch means the entry
// changed since the caller read it and returns ErrConflict, closing the
// read-modify-write race that silently lost updates. A nil ifMatch keeps
// the prior last-write-wins behavior for callers that do not opt in.
func (s *Store) Update(ctx context.Context, k *domain.Knowledge, actor domain.Actor, ifMatch *time.Time) error {
	k.UpdatedAt = NowStored()
	return s.withTx(ctx, func(tx pgx.Tx) error {
		j, err := marshalJSONFields(k)
		if err != nil {
			return err
		}
		staleAfter, err := staleAfterArg(k.StaleAfter)
		if err != nil {
			return err
		}
		verifiedKind, verifiedName, verifiedVia := actorPtrs(k.VerifiedBy)
		rejectedKind, rejectedName, rejectedVia := actorPtrs(k.RejectedBy)
		cond := ""
		args := []any{k.ID, k.Type, k.Title, k.Description, k.Resource, k.Tags, k.Status, k.StatusNote, staleAfter,
			j.sources, j.usageWindow, k.Runtime, j.parameters, k.Computation, j.executor, j.attester,
			k.UpdatedBy.Kind, k.UpdatedBy.Name, k.UpdatedBy.Via,
			verifiedKind, verifiedName, verifiedVia, k.VerifiedAt,
			rejectedKind, rejectedName, rejectedVia, k.RejectedAt,
			j.links, j.attrs, k.Body, k.UpdatedAt}
		if ifMatch != nil {
			args = append(args, ifMatch.UTC())
			cond = fmt.Sprintf(" AND updated_at=$%d", len(args))
		}
		tag, err := tx.Exec(ctx, `UPDATE knowledge SET
			type=$2, title=$3, description=$4, resource=$5, tags=$6, status=$7, status_note=$8, stale_after=$9,
			sources=$10, usage_window=$11, runtime=$12, parameters=$13, computation=$14, executor=$15, attester=$16,
			updated_by_kind=$17, updated_by_name=$18, updated_by_via=$19,
			verified_by_kind=$20, verified_by_name=$21, verified_by_via=$22, verified_at=$23,
			rejected_by_kind=$24, rejected_by_name=$25, rejected_by_via=$26, rejected_at=$27,
			links=$28, attrs=$29, body=$30, updated_at=$31
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

// Verify records that a reviewer vouched for the entry as it stands:
// status becomes verified and verified_by/verified_at are stamped now,
// even when the entry was already verified.
//
// Update cannot express this. It carries verified_at over from the stored
// entry whenever the status was already verified, and writes nothing at
// all when the content is unchanged (SameContent) — so "I looked at this
// again and it is still right" had nowhere to land, and both review feeds
// had no exit: the verification-age feed reads verified_at, and the
// re-verification feed lists entries whose last failure report is newer
// than it (design doc 0025 §6).
//
// A rejection is cleared, mirroring the status transitions Update makes:
// an entry cannot be both vouched for and turned down.
//
// The timestamp comes from the database, not from NowStored: the feed
// compares it against the last failure report, which RecordOutcome stamps
// with the database's now(). Two clocks a millisecond apart are enough to
// hide a report that arrived after the verification, or to keep one that
// arrived before it — so both sides read the same clock, and RETURNING
// hands back exactly what was stored (the ETag invariant NowStored exists
// for).
func (s *Store) Verify(ctx context.Context, id string, actor domain.Actor) (*domain.Knowledge, error) {
	var k *domain.Knowledge
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		var err error
		if k, err = s.Get(ctx, id); err != nil {
			return err
		}
		var verifiedAt, updatedAt time.Time
		// deleted_at IS NULL guards the race with a concurrent delete, as
		// in SoftDelete: the Get above ran outside this transaction.
		err = tx.QueryRow(ctx, `UPDATE knowledge SET
			status='verified',
			verified_by_kind=$2, verified_by_name=$3, verified_by_via=$4, verified_at=now(),
			rejected_by_kind=NULL, rejected_by_name=NULL, rejected_by_via='', rejected_at=NULL,
			updated_at=now()
			WHERE id=$1 AND deleted_at IS NULL
			RETURNING verified_at, updated_at`,
			id, actor.Kind, actor.Name, actor.Via).Scan(&verifiedAt, &updatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		verifiedAt = verifiedAt.UTC()
		k.Status = domain.StatusVerified
		k.VerifiedBy, k.VerifiedAt = &actor, &verifiedAt
		k.RejectedBy, k.RejectedAt = nil, nil
		k.UpdatedAt = updatedAt.UTC()
		return s.addRevision(ctx, tx, k, "verify", actor)
	})
	if err != nil {
		return nil, err
	}
	return k, nil
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
		// FOR UPDATE, because the check and the delete must see the same
		// row: Create revives a tombstone in place, so without the lock a
		// revival committing in between would leave this transaction
		// deleting a live entry — the one thing two-step destruction
		// exists to prevent. The lock also serializes two purges of the
		// same id, which would otherwise both log an audit row.
		var deleted bool
		err := tx.QueryRow(ctx,
			`SELECT deleted_at IS NOT NULL FROM knowledge WHERE id=$1 FOR UPDATE`, id).Scan(&deleted)
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
		} {
			if _, err := tx.Exec(ctx, q, id); err != nil {
				return err
			}
		}
		// The tombstone condition is repeated here so the destructive
		// statement carries the precondition itself, not just the lock
		// taken above: a purge that would hit a live row aborts the
		// transaction instead of erasing it.
		tag, err := tx.Exec(ctx, `DELETE FROM knowledge WHERE id=$1 AND deleted_at IS NOT NULL`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotDeleted
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
	k.UpdatedAt = NowStored()
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
			`UPDATE knowledge SET id=$2, updated_at=$3,
			 updated_by_kind=$4, updated_by_name=$5, updated_by_via=$6
			 WHERE id=$1 AND deleted_at IS NULL`,
			oldID, newID, k.UpdatedAt, actor.Kind, actor.Name, actor.Via)
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
		`SELECT `+knowledgeCols+` FROM knowledge
		 WHERE deleted_at IS NULL AND (links @> $1 OR links @> $2 OR attrs->>'model' = $3 OR id = $4)
		 ORDER BY id FOR UPDATE`,
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
		// deleted_at IS NULL restates what the locked read already
		// established, so the statement does not depend on the reader
		// having filtered: rewriting a tombstone would plant a repaired
		// body and a revision on an entry nobody can see, to resurface
		// whenever someone revives it.
		tag, err := tx.Exec(ctx,
			`UPDATE knowledge SET links=$2, attrs=$3, body=$4, updated_at=$5,
			 updated_by_kind=$6, updated_by_name=$7, updated_by_via=$8
			 WHERE id=$1 AND deleted_at IS NULL`,
			r.ID, j.links, j.attrs, r.Body, r.UpdatedAt, actor.Kind, actor.Name, actor.Via)
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
			continue
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

// maxQueryFragments bounds how many pieces a query is split into: every
// fragment is another ILIKE term on the same scan, and a pasted paragraph
// would otherwise put a hundred of them there.
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
// Every term of that is a substring test against one column, so the
// candidate predicate and the score read the same expressions, and the
// GIN trigram index serves the ones it can — patterns of three characters
// or more (migration 0016; two-character Japanese windows are answered by
// a scan, see queryFragments). Fragment matching
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
		SELECT `+knowledgeColsK+`, scored.score
		FROM knowledge k JOIN scored ON scored.id = k.id
		ORDER BY scored.score DESC`,
		strings.Join(weighted, " + "), strings.Join(weights, " + "),
		strings.Join(weight, ", "), strings.Join(hit, ", "), wholeParam,
		strings.Join(tests, " OR "), where, limit)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanHit)
}

// SearchVector ranks by cosine distance against stored embeddings.
func (s *Store) SearchVector(ctx context.Context, vec []float32, model string, f Filter, limit int) ([]domain.SearchHit, error) {
	// Columns must be qualified: knowledge_embedding shares id/updated_at.
	where, args := f.buildWhere("k.")
	args = append(args, encodeVector(vec))
	vecParam := len(args)
	// Only this model's vectors: a vector written by another model lives
	// in a different space, so comparing it to this query's vector yields
	// a number that means nothing. Reembed refills the gap (design doc
	// 0020); until it runs the entry is lexical-only, which is a visible
	// absence rather than a plausible wrong ranking.
	args = append(args, model)
	q := fmt.Sprintf(`
		SELECT `+knowledgeColsK+`, 1 - (e.embedding <=> $%d::vector) AS score
		FROM knowledge k JOIN knowledge_embedding e ON k.id = e.id AND e.model = $%d
		WHERE %s
		ORDER BY e.embedding <=> $%d::vector LIMIT %d`, vecParam, len(args), where, vecParam, limit)
	return s.annSearch(ctx, limit, q, args)
}

// SearchVectorAttachments ranks entries by their closest attachment
// embedding (design doc 0020). Each entry appears once, carrying its
// best attachment's score — attachments never stand alone, so the hit
// is the owning entry.
func (s *Store) SearchVectorAttachments(ctx context.Context, vec []float32, model string, f Filter, limit int) ([]domain.SearchHit, error) {
	where, args := f.buildWhere("k.")
	args = append(args, encodeVector(vec))
	vecParam := len(args)
	args = append(args, model) // this model's vectors only, as in SearchVector
	q := fmt.Sprintf(`
		SELECT `+knowledgeCols+`, score FROM (
			SELECT DISTINCT ON (k.id) k.*, 1 - (e.embedding <=> $%d::vector) AS score
			FROM knowledge k JOIN attachment_embedding e ON k.id = e.knowledge_id AND e.model = $%d
			WHERE %s
			ORDER BY k.id, e.embedding <=> $%d::vector
		) best
		ORDER BY score DESC LIMIT %d`, vecParam, len(args), where, vecParam, limit)
	return s.annSearch(ctx, limit, q, args)
}

// efSearch is how many candidates the HNSW index is asked to consider for
// a search wanting limit rows.
//
// The index scan runs first and the WHERE clause filters what it returns,
// so ef_search is a ceiling on the answer, not a starting point: at
// pgvector's default of 40, a search for 100 rows could never return more
// than 40 even before a type or tag filter took its cut. Four times the
// limit is the headroom for that filtering, floored at the default so a
// small search is no worse than before and capped at pgvector's maximum.
//
// Set per query rather than on the connection: pooled connections are
// shared, and a lingering session GUC would silently retune searches that
// never asked.
func efSearch(limit int) int {
	const (
		defaultEF = 40   // pgvector's own default
		maxEF     = 1000 // pgvector's ceiling
	)
	ef := 4 * limit
	if ef < defaultEF {
		ef = defaultEF
	}
	if ef > maxEF {
		ef = maxEF
	}
	return ef
}

// annSearch runs a vector query with ef_search sized for the limit. The
// transaction exists only to scope SET LOCAL to this one statement; it
// reads, so it ends in a rollback.
func (s *Store) annSearch(ctx context.Context, limit int, q string, args []any) ([]domain.SearchHit, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL hnsw.ef_search = %d", efSearch(limit))); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanHit)
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
	if f.Source != "" {
		// Containment, so the GIN index on sources answers it directly.
		// Matching is on resource alone: sources[].id is a key for the
		// body's footnotes within one document, and means nothing across
		// entries (design doc 0037 §2.3).
		args = append(args, sourceContainment(f.Source))
		conds = append(conds, fmt.Sprintf("%ssources @> $%d", prefix, len(args)))
	}
	return strings.Join(conds, " AND "), args
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

// ListBySource lists the entries a filter matches, by id. It exists for
// the source lookup: "what cites this resource" is a question with no
// text to rank by, so it lists rather than searches (design doc 0037
// §2.3). Ordered by id because the answer is a set — a stable, readable
// order beats a relevance score nothing computed.
func (s *Store) ListBySource(ctx context.Context, f Filter, limit int) ([]domain.Knowledge, error) {
	where, args := f.buildWhere("")
	return s.queryKnowledge(ctx, fmt.Sprintf(`SELECT `+knowledgeCols+` FROM knowledge WHERE %s
		ORDER BY id LIMIT %d`, where, limit), args...)
}

// ListByStaleAfter returns the entries whose declared expiry has passed,
// most overdue first — the feed for "what did we say needed re-checking
// by now" (design doc 0037 §2.1).
//
// Only entries that are stale today. A declaration that has not come due
// is not work, and 0025 asked feeds to be queues a reviewer can finish;
// the same reason sort=failed lists only entries with failures.
//
// Unlike the other two feeds, verifying does not empty this one: the date
// is the writer's declaration, not something the server observed, so
// clearing it means editing the entry to re-declare an expiry
// (design doc 0037 §2.2).
func (s *Store) ListByStaleAfter(ctx context.Context, f Filter, limit int) ([]domain.Knowledge, error) {
	where, args := f.buildWhere("")
	// "Today" is UTC, spelled out rather than left to current_date. The
	// column is a bare date so that staleness needs no timezone (design
	// doc 0036 §3.9), and current_date would hand that decision to
	// whatever TimeZone the database session happens to carry — the same
	// entry would be stale on one deployment and not on another.
	return s.queryKnowledge(ctx, fmt.Sprintf(`SELECT `+knowledgeCols+` FROM knowledge WHERE %s
		AND stale_after IS NOT NULL AND stale_after <= (now() AT TIME ZONE 'UTC')::date
		ORDER BY stale_after ASC, id LIMIT %d`, where, limit), args...)
}

// ListByVerifiedAt returns filtered entries ordered by verification age,
// oldest first (never-verified entries last). This is the feed for golden
// query canary runs: "which verified queries have gone longest unchecked".
func (s *Store) ListByVerifiedAt(ctx context.Context, f Filter, limit int) ([]domain.Knowledge, error) {
	where, args := f.buildWhere("")
	return s.queryKnowledge(ctx, fmt.Sprintf(`SELECT `+knowledgeCols+` FROM knowledge WHERE %s
		ORDER BY verified_at ASC NULLS LAST, id LIMIT %d`, where, limit), args...)
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
			COALESCE(sum(count) FILTER (WHERE event = 'worked'), 0)    AS worked,
			COALESCE(sum(count) FILTER (WHERE event = 'failed'), 0)    AS failed,
			max(last_at) FILTER (WHERE event = 'failed')                AS last_failed_at,
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
		SELECT `+knowledgeColsK+`,
			u.search_hits, u.fetches, u.worked, u.failed, u.last_used_at
		FROM knowledge k`+usageLateral+`
		WHERE %s
		ORDER BY u.search_hits DESC, k.created_at ASC, k.id LIMIT %d`, where, limit)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanUsageHit)
}

// ListByFailed returns filtered entries whose failure reports are still
// unanswered, worst first — the re-verification feed (design doc 0025).
// It is the evidence-based counterpart to the verified_at feed's
// time-based staleness, so a healthy base yields an empty feed.
//
// "Still unanswered" is the difference between a queue and a ledger. The
// totals in knowledge_usage only ever grow, so ranking by them alone left
// an entry in the feed forever: a reviewer who checked it, found it right
// and re-verified it saw it sitting at the top the next morning, and the
// queue could only lengthen. An entry drops out once it is verified after
// its last failure report (Verify, or a promotion from draft) — or once
// it is rejected, which the default status filter already hides.
// Never-verified entries stay: a failing draft has had no ruling at all.
//
// Ordering stays on the lifetime failure count: presence answers "is this
// unresolved", ranking answers "how bad has this been". Ties break on
// fewest corroborating "worked" reports, then verification age (oldest
// first, never-verified last), then id. Each hit carries its usage totals
// so the reviewer sees the worked/failed evidence inline; score is 0.
func (s *Store) ListByFailed(ctx context.Context, f Filter, limit int) ([]domain.SearchHit, error) {
	where, args := f.buildWhere("k.")
	q := fmt.Sprintf(`
		SELECT `+knowledgeColsK+`,
			u.search_hits, u.fetches, u.worked, u.failed, u.last_used_at
		FROM knowledge k`+usageLateral+`
		WHERE %s AND u.failed > 0
			AND (k.verified_at IS NULL OR u.last_failed_at > k.verified_at)
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
		&u.SearchHits, &u.Fetches, &u.Worked, &u.Failed, &u.LastUsedAt)...); err != nil {
		return h, err
	}
	if err := finish(); err != nil {
		return h, err
	}
	h.Usage = &u
	return h, nil
}

// ListUnembedded returns the ids of live entries with no vector for the
// named model, oldest first, up to limit. That covers both halves of the
// same gap: entries written before semantic search was configured (there
// is no row), and entries whose vector belongs to a model that has since
// been changed (design doc 0020 — the old vectors sit in a space nobody
// queries any more).
//
// after is a keyset cursor: pass the last id of the previous page to get
// the next one. Without it a pass whose entries all fail keeps handing
// back the same window, and the caller — which stops when a pass embeds
// nothing — never reaches the rest of the corpus.
func (s *Store) ListUnembedded(ctx context.Context, model, after string, limit int) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT k.id FROM knowledge k
		 LEFT JOIN knowledge_embedding e ON e.id = k.id AND e.model = $1
		 WHERE k.deleted_at IS NULL AND e.id IS NULL AND ($2 = '' OR k.id > $2)
		 ORDER BY k.id LIMIT $3`, model, after, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// UnembeddedAttachment names one attachment awaiting a vector.
type UnembeddedAttachment struct {
	KnowledgeID string
	Name        string
}

// ListUnembeddedAttachments is ListUnembedded for attachment vectors
// (design doc 0020). Attach writes them, so a corpus loaded before
// semantic search — or one whose embedding tables were dropped to change
// dimensions — has none, and the files are findable by name only. The
// cursor is the (knowledge_id, name) pair, for the same reason.
func (s *Store) ListUnembeddedAttachments(ctx context.Context, model, afterID, afterName string, limit int) ([]UnembeddedAttachment, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT a.knowledge_id, a.name FROM attachment a
		 JOIN knowledge k ON k.id = a.knowledge_id AND k.deleted_at IS NULL
		 LEFT JOIN attachment_embedding e
		   ON e.knowledge_id = a.knowledge_id AND e.name = a.name AND e.model = $1
		 WHERE e.knowledge_id IS NULL
		   AND ($2 = '' OR (a.knowledge_id, a.name) > ($2, $3))
		 ORDER BY a.knowledge_id, a.name LIMIT $4`, model, afterID, afterName, limit)
	if err != nil {
		if isUndefinedTable(err) {
			return nil, nil // embedding tables appear with semantic search
		}
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (UnembeddedAttachment, error) {
		var a UnembeddedAttachment
		return a, row.Scan(&a.KnowledgeID, &a.Name)
	})
	if err != nil && isUndefinedTable(err) {
		return nil, nil
	}
	return out, err
}

// CountUnembeddedAttachments counts what ListUnembeddedAttachments would
// eventually return.
func (s *Store) CountUnembeddedAttachments(ctx context.Context, model string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM attachment a
		 JOIN knowledge k ON k.id = a.knowledge_id AND k.deleted_at IS NULL
		 LEFT JOIN attachment_embedding e
		   ON e.knowledge_id = a.knowledge_id AND e.name = a.name AND e.model = $1
		 WHERE e.knowledge_id IS NULL`, model).Scan(&n)
	if err != nil && isUndefinedTable(err) {
		return 0, nil
	}
	return n, err
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

// knowledgeJSON carries the entry's jsonb columns, encoded once per write.
// The three nullable ones stay nil when the entry declares nothing, so an
// undeclared usage_window or contract is SQL NULL rather than the JSON
// literal "null" — the read side tells those apart to keep "no window" and
// "an empty window" distinct (design doc 0036 §3.1).
type knowledgeJSON struct {
	links, attrs                    []byte
	sources, parameters             []byte
	usageWindow, executor, attester []byte
}

func marshalJSONFields(k *domain.Knowledge) (knowledgeJSON, error) {
	if k.Tags == nil {
		k.Tags = []string{}
	}
	if k.Links == nil {
		k.Links = []domain.Link{}
	}
	if k.Attrs == nil {
		k.Attrs = map[string]any{}
	}
	if k.Sources == nil {
		k.Sources = []domain.Source{}
	}
	if k.Parameters == nil {
		k.Parameters = []domain.Parameter{}
	}
	var j knowledgeJSON
	for _, f := range []struct {
		out *[]byte
		in  any
		// nullable columns encode only when the entry has the value.
		skip bool
	}{
		{&j.links, k.Links, false},
		{&j.attrs, k.Attrs, false},
		{&j.sources, k.Sources, false},
		{&j.parameters, k.Parameters, false},
		{&j.usageWindow, k.UsageWindow, k.UsageWindow == nil},
		{&j.executor, k.Executor, k.Executor == nil},
		{&j.attester, k.Attester, k.Attester == nil},
	} {
		if f.skip {
			continue
		}
		b, err := json.Marshal(f.in)
		if err != nil {
			return knowledgeJSON{}, err
		}
		*f.out = b
	}
	return j, nil
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

// CountAttachmentEmbedding reports whether one attachment has a vector
// for the named model (0 or 1). Reembed uses it to tell an embedding
// that landed from one the provider declined — the attach path logs its
// own failures rather than returning them.
func (s *Store) CountAttachmentEmbedding(ctx context.Context, id, name, model string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM attachment_embedding
		 WHERE knowledge_id = $1 AND name = $2 AND model = $3`, id, name, model).Scan(&n)
	if err != nil && isUndefinedTable(err) {
		return 0, nil
	}
	return n, err
}
