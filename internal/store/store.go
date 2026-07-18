// Package store persists knowledge in PostgreSQL, the only runtime
// dependency of ochakai.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/na0fu3y/ochakai/internal/blob"
	"github.com/na0fu3y/ochakai/internal/domain"
)

var ErrNotFound = errors.New("knowledge not found")
var ErrAlreadyExists = errors.New("knowledge already exists")

type Store struct {
	pool *pgxpool.Pool
	// blobs holds attachment bytes (GCS, design doc 0013); metadata stays
	// in PostgreSQL. When nil, attachments are unsupported — markdown
	// entries only.
	blobs blob.Store
	// lastEventPrune throttles knowledge_event pruning (unix seconds).
	lastEventPrune atomic.Int64
}

// UseBlobStore routes attachment bytes to b (design doc 0013). Call
// before serving; legacy inline rows are moved by MigrateBlobsOut.
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
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

// Filter narrows search and list operations.
type Filter struct {
	Types    []domain.Type
	Statuses []domain.Status
	Tags     []string
}

const knowledgeCols = `type, id, title, description, tags, status, status_note,
	created_by_kind, created_by_name, verified_by_kind, verified_by_name, verified_at,
	rejected_by_kind, rejected_by_name, rejected_at,
	links, attrs, body, created_at, updated_at`

func scanKnowledge(row pgx.CollectableRow) (domain.Knowledge, error) {
	var k domain.Knowledge
	var verifiedKind, verifiedName, rejectedKind, rejectedName *string
	var links, attrs []byte
	err := row.Scan(&k.Type, &k.ID, &k.Title, &k.Description, &k.Tags, &k.Status, &k.StatusNote,
		&k.CreatedBy.Kind, &k.CreatedBy.Name, &verifiedKind, &verifiedName, &k.VerifiedAt,
		&rejectedKind, &rejectedName, &k.RejectedAt,
		&links, &attrs, &k.Body, &k.CreatedAt, &k.UpdatedAt)
	if err != nil {
		return k, err
	}
	k.VerifiedBy = actorFrom(verifiedKind, verifiedName)
	k.RejectedBy = actorFrom(rejectedKind, rejectedName)
	if err := json.Unmarshal(links, &k.Links); err != nil {
		return k, err
	}
	if err := json.Unmarshal(attrs, &k.Attrs); err != nil {
		return k, err
	}
	return k, nil
}

func actorFrom(kind, name *string) *domain.Actor {
	if kind == nil || name == nil {
		return nil
	}
	return &domain.Actor{Kind: *kind, Name: *name}
}

// actorPtrs splits an optional actor into nullable columns.
func actorPtrs(a *domain.Actor) (kind, name *string) {
	if a == nil {
		return nil, nil
	}
	return &a.Kind, &a.Name
}

func (s *Store) Get(ctx context.Context, typ domain.Type, id string) (*domain.Knowledge, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+knowledgeCols+` FROM knowledge WHERE type = $1 AND id = $2 AND deleted_at IS NULL`, typ, id)
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

// ListLinkingTo returns live entries whose links point at "<type>/<id>",
// most recently updated first. This is the reverse edge Context needs:
// the insight that explains a metric links to the metric, not the other
// way round. Both bare and ochakai:// target forms match.
func (s *Store) ListLinkingTo(ctx context.Context, typ domain.Type, id string, limit int) ([]domain.Knowledge, error) {
	target := string(typ) + "/" + id
	rows, err := s.pool.Query(ctx,
		`SELECT `+knowledgeCols+` FROM knowledge
		 WHERE deleted_at IS NULL AND (links @> $1 OR links @> $2)
		 ORDER BY updated_at DESC LIMIT $3`,
		fmt.Sprintf(`[{"target": %q}]`, target),
		fmt.Sprintf(`[{"target": %q}]`, "ochakai://"+target),
		limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanKnowledge)
}

// Create inserts a new entry. A live entry with the same type/id is
// ErrAlreadyExists — including rejected ones, so the memory of no
// survives. A soft-deleted entry is revived instead: the ID would
// otherwise be dead forever (the row still owns the primary key while
// Update refuses deleted rows), and its history stays in the revisions
// either way.
func (s *Store) Create(ctx context.Context, k *domain.Knowledge) error {
	now := time.Now().UTC()
	k.CreatedAt, k.UpdatedAt = now, now
	return s.withTx(ctx, func(tx pgx.Tx) error {
		links, attrs, err := marshalJSONFields(k)
		if err != nil {
			return err
		}
		verifiedKind, verifiedName := actorPtrs(k.VerifiedBy)
		rejectedKind, rejectedName := actorPtrs(k.RejectedBy)
		tag, err := tx.Exec(ctx, `INSERT INTO knowledge (`+knowledgeCols+`)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
			ON CONFLICT (type, id) DO UPDATE SET
				title=EXCLUDED.title, description=EXCLUDED.description, tags=EXCLUDED.tags,
				status=EXCLUDED.status, status_note=EXCLUDED.status_note,
				created_by_kind=EXCLUDED.created_by_kind, created_by_name=EXCLUDED.created_by_name,
				verified_by_kind=EXCLUDED.verified_by_kind, verified_by_name=EXCLUDED.verified_by_name,
				verified_at=EXCLUDED.verified_at,
				rejected_by_kind=EXCLUDED.rejected_by_kind, rejected_by_name=EXCLUDED.rejected_by_name,
				rejected_at=EXCLUDED.rejected_at,
				links=EXCLUDED.links, attrs=EXCLUDED.attrs, body=EXCLUDED.body,
				created_at=EXCLUDED.created_at, updated_at=EXCLUDED.updated_at,
				deleted_at=NULL
			WHERE knowledge.deleted_at IS NOT NULL`,
			k.Type, k.ID, k.Title, k.Description, k.Tags, k.Status, k.StatusNote,
			k.CreatedBy.Kind, k.CreatedBy.Name, verifiedKind, verifiedName, k.VerifiedAt,
			rejectedKind, rejectedName, k.RejectedAt,
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

func (s *Store) Update(ctx context.Context, k *domain.Knowledge, actor domain.Actor) error {
	k.UpdatedAt = time.Now().UTC()
	return s.withTx(ctx, func(tx pgx.Tx) error {
		links, attrs, err := marshalJSONFields(k)
		if err != nil {
			return err
		}
		verifiedKind, verifiedName := actorPtrs(k.VerifiedBy)
		rejectedKind, rejectedName := actorPtrs(k.RejectedBy)
		tag, err := tx.Exec(ctx, `UPDATE knowledge SET
			title=$3, description=$4, tags=$5, status=$6, status_note=$7,
			verified_by_kind=$8, verified_by_name=$9, verified_at=$10,
			rejected_by_kind=$11, rejected_by_name=$12, rejected_at=$13,
			links=$14, attrs=$15, body=$16, updated_at=$17
			WHERE type=$1 AND id=$2 AND deleted_at IS NULL`,
			k.Type, k.ID, k.Title, k.Description, k.Tags, k.Status, k.StatusNote,
			verifiedKind, verifiedName, k.VerifiedAt,
			rejectedKind, rejectedName, k.RejectedAt,
			links, attrs, k.Body, k.UpdatedAt)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return s.addRevision(ctx, tx, k, "update", actor)
	})
}

// SoftDelete hides an entry from reads while keeping full history.
// Create on the same type/id revives it.
func (s *Store) SoftDelete(ctx context.Context, typ domain.Type, id string, actor domain.Actor) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		k, err := s.Get(ctx, typ, id)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE knowledge SET deleted_at = now(), updated_at = now() WHERE type=$1 AND id=$2`, typ, id); err != nil {
			return err
		}
		// knowledge_embedding only exists once semantic search has been
		// enabled; a failed statement aborts the whole Postgres transaction,
		// so tolerate a missing table via a savepoint.
		sp, err := tx.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := sp.Exec(ctx, `DELETE FROM knowledge_embedding WHERE type=$1 AND id=$2`, typ, id); err != nil {
			_ = sp.Rollback(ctx)
			if !isUndefinedTable(err) {
				return err
			}
		} else if err := sp.Commit(ctx); err != nil {
			return err
		}
		return s.addRevision(ctx, tx, k, "delete", actor)
	})
}

// ListRevisions returns an entry's change history, newest first. It
// reads history, so it works for soft-deleted entries too — the audit
// trail is most interesting exactly when the entry is gone.
func (s *Store) ListRevisions(ctx context.Context, typ domain.Type, id string, limit int) ([]domain.Revision, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT rev, change, changed_by_kind, changed_by_name, changed_at, snapshot
		 FROM knowledge_revision WHERE type=$1 AND id=$2 ORDER BY rev DESC LIMIT $3`,
		typ, id, limit)
	if err != nil {
		return nil, err
	}
	revs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Revision, error) {
		var r domain.Revision
		var snapshot []byte
		if err := row.Scan(&r.Rev, &r.Change, &r.ChangedBy.Kind, &r.ChangedBy.Name, &r.ChangedAt, &snapshot); err != nil {
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
	_, err = tx.Exec(ctx, `INSERT INTO knowledge_revision (type, id, rev, change, changed_by_kind, changed_by_name, snapshot)
		VALUES ($1, $2, (SELECT COALESCE(MAX(rev), 0) + 1 FROM knowledge_revision WHERE type=$1 AND id=$2), $3, $4, $5, $6)`,
		k.Type, k.ID, change, actor.Kind, actor.Name, snapshot)
	return err
}

// SearchLexical ranks by trigram similarity with a substring-match floor
// (trigram alone misses short Japanese terms), verified entries boosted.
func (s *Store) SearchLexical(ctx context.Context, query string, f Filter, limit int) ([]domain.SearchHit, error) {
	where, args := f.buildWhere("")
	args = append(args, query)
	q := fmt.Sprintf(`
		SELECT `+knowledgeCols+`, score FROM (
			SELECT *, similarity(title || ' ' || description || ' ' || array_to_string(tags, ' ') || ' ' || body, $%d)
				+ CASE WHEN title || ' ' || description || ' ' || body ILIKE '%%' || $%d || '%%' THEN 0.3 ELSE 0 END
				+ CASE WHEN status = 'verified' THEN 0.05 ELSE 0 END AS score
			FROM knowledge WHERE %s
		) ranked
		WHERE score > 0.05
		ORDER BY score DESC LIMIT %d`, len(args), len(args), where, limit)
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
	// Columns must be qualified: knowledge_embedding shares type/id/updated_at.
	where, args := f.buildWhere("k.")
	args = append(args, encodeVector(vec))
	q := fmt.Sprintf(`
		SELECT `+qualifyCols("k")+`, 1 - (e.embedding <=> $%d::vector) AS score
		FROM knowledge k JOIN knowledge_embedding e ON k.type = e.type AND k.id = e.id
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

func scanHit(row pgx.CollectableRow) (domain.SearchHit, error) {
	var h domain.SearchHit
	var verifiedKind, verifiedName, rejectedKind, rejectedName *string
	var links, attrs []byte
	err := row.Scan(&h.Type, &h.ID, &h.Title, &h.Description, &h.Tags, &h.Status, &h.StatusNote,
		&h.CreatedBy.Kind, &h.CreatedBy.Name, &verifiedKind, &verifiedName, &h.VerifiedAt,
		&rejectedKind, &rejectedName, &h.RejectedAt,
		&links, &attrs, &h.Body, &h.CreatedAt, &h.UpdatedAt, &h.Score)
	if err != nil {
		return h, err
	}
	h.VerifiedBy = actorFrom(verifiedKind, verifiedName)
	h.RejectedBy = actorFrom(rejectedKind, rejectedName)
	if err := json.Unmarshal(links, &h.Links); err != nil {
		return h, err
	}
	if err := json.Unmarshal(attrs, &h.Attrs); err != nil {
		return h, err
	}
	return h, nil
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
		args = append(args, f.Types)
		conds = append(conds, fmt.Sprintf("%stype = ANY($%d)", prefix, len(args)))
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
		ORDER BY verified_at ASC NULLS LAST, type, id LIMIT %d`, where, limit)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanKnowledge)
}

// ListByUsage returns filtered entries ordered by demand, most-searched
// first: search_hits descending, then oldest-created (created_at ascending)
// as the tiebreak. This is the draft review feed — the promotion queue at
// the top, and never-used drafts (search_hits 0) sinking oldest-first to
// the bottom for inventory. Each hit carries its usage totals so the
// caller renders the signal without a per-entry round trip. Score is 0.
func (s *Store) ListByUsage(ctx context.Context, f Filter, limit int) ([]domain.SearchHit, error) {
	where, args := f.buildWhere("k.")
	// Conditional aggregation over knowledge_usage in one lateral pass:
	// running totals per event live in that table (see usage.go).
	q := fmt.Sprintf(`
		SELECT `+qualifyCols("k")+`,
			u.search_hits, u.fetches, u.compiles, u.worked, u.failed, u.last_used_at
		FROM knowledge k
		LEFT JOIN LATERAL (
			SELECT
				COALESCE(sum(count) FILTER (WHERE event = 'search_hit'), 0) AS search_hits,
				COALESCE(sum(count) FILTER (WHERE event = 'fetched'), 0)   AS fetches,
				COALESCE(sum(count) FILTER (WHERE event = 'compiled'), 0)  AS compiles,
				COALESCE(sum(count) FILTER (WHERE event = 'worked'), 0)    AS worked,
				COALESCE(sum(count) FILTER (WHERE event = 'failed'), 0)    AS failed,
				max(last_at) AS last_used_at
			FROM knowledge_usage
			WHERE knowledge_type = k.type AND knowledge_id = k.id
		) u ON true
		WHERE %s
		ORDER BY u.search_hits DESC, k.created_at ASC, k.type, k.id LIMIT %d`, where, limit)
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
	var verifiedKind, verifiedName, rejectedKind, rejectedName *string
	var links, attrs []byte
	var u domain.Usage
	err := row.Scan(&h.Type, &h.ID, &h.Title, &h.Description, &h.Tags, &h.Status, &h.StatusNote,
		&h.CreatedBy.Kind, &h.CreatedBy.Name, &verifiedKind, &verifiedName, &h.VerifiedAt,
		&rejectedKind, &rejectedName, &h.RejectedAt,
		&links, &attrs, &h.Body, &h.CreatedAt, &h.UpdatedAt,
		&u.SearchHits, &u.Fetches, &u.Compiles, &u.Worked, &u.Failed, &u.LastUsedAt)
	if err != nil {
		return h, err
	}
	h.VerifiedBy = actorFrom(verifiedKind, verifiedName)
	h.RejectedBy = actorFrom(rejectedKind, rejectedName)
	if err := json.Unmarshal(links, &h.Links); err != nil {
		return h, err
	}
	if err := json.Unmarshal(attrs, &h.Attrs); err != nil {
		return h, err
	}
	h.Usage = &u
	return h, nil
}

// ListAll returns every non-deleted entry, ordered by type then id.
// Used by the OKF exporter.
func (s *Store) ListAll(ctx context.Context) ([]domain.Knowledge, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+knowledgeCols+` FROM knowledge WHERE deleted_at IS NULL ORDER BY type, id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanKnowledge)
}

// UpsertEmbedding stores the document embedding for a knowledge entry.
func (s *Store) UpsertEmbedding(ctx context.Context, typ domain.Type, id, model string, vec []float32) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO knowledge_embedding (type, id, model, embedding, updated_at)
		VALUES ($1, $2, $3, $4::vector, now())
		ON CONFLICT (type, id) DO UPDATE SET model = $3, embedding = $4::vector, updated_at = now()`,
		typ, id, model, encodeVector(vec))
	return err
}

func (s *Store) UpsertSemanticModel(ctx context.Context, name string, spec map[string]any) error {
	specJSON, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO semantic_model (name, spec, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (name) DO UPDATE SET spec = $2, updated_at = now()`, name, specJSON)
	return err
}

func (s *Store) GetSemanticModel(ctx context.Context, name string) (map[string]any, error) {
	var spec []byte
	err := s.pool.QueryRow(ctx, `SELECT spec FROM semantic_model WHERE name = $1`, name).Scan(&spec)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(spec, &m); err != nil {
		return nil, err
	}
	return m, nil
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
