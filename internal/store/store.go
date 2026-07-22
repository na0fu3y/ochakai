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
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

// Filter narrows search and list operations.
type Filter struct {
	Types    []domain.Type
	Statuses []domain.Status
	Tags     []string
}

const knowledgeCols = `type, id, title, description, resource, tags, status, status_note,
	created_by_kind, created_by_name, verified_by_kind, verified_by_name, verified_at,
	rejected_by_kind, rejected_by_name, rejected_at,
	links, attrs, body, created_at, updated_at`

// knowledgeDest returns the scan destinations for knowledgeCols targeting
// k, plus a finish func that decodes the JSON columns and nullable actors
// after the scan. Row shapes with trailing columns (score, usage totals)
// append their own destinations — the column list lives here once.
func knowledgeDest(k *domain.Knowledge) (dests []any, finish func() error) {
	var verifiedKind, verifiedName, rejectedKind, rejectedName *string
	var links, attrs []byte
	dests = []any{&k.Type, &k.ID, &k.Title, &k.Description, &k.Resource, &k.Tags, &k.Status, &k.StatusNote,
		&k.CreatedBy.Kind, &k.CreatedBy.Name, &verifiedKind, &verifiedName, &k.VerifiedAt,
		&rejectedKind, &rejectedName, &k.RejectedAt,
		&links, &attrs, &k.Body, &k.CreatedAt, &k.UpdatedAt}
	finish = func() error {
		k.VerifiedBy = actorFrom(verifiedKind, verifiedName)
		k.RejectedBy = actorFrom(rejectedKind, rejectedName)
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
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
			ON CONFLICT (id) DO UPDATE SET
				type=EXCLUDED.type,
				title=EXCLUDED.title, description=EXCLUDED.description, resource=EXCLUDED.resource, tags=EXCLUDED.tags,
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
			k.Type, k.ID, k.Title, k.Description, k.Resource, k.Tags, k.Status, k.StatusNote,
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
			type=$2, title=$3, description=$4, resource=$5, tags=$6, status=$7, status_note=$8,
			verified_by_kind=$9, verified_by_name=$10, verified_at=$11,
			rejected_by_kind=$12, rejected_by_name=$13, rejected_at=$14,
			links=$15, attrs=$16, body=$17, updated_at=$18
			WHERE id=$1 AND deleted_at IS NULL`,
			k.ID, k.Type, k.Title, k.Description, k.Resource, k.Tags, k.Status, k.StatusNote,
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
	k.UpdatedAt = time.Now().UTC()
	err = s.withTx(ctx, func(tx pgx.Tx) error {
		var taken bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM knowledge WHERE id=$1)`, newID).Scan(&taken); err != nil {
			return err
		}
		if taken {
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
	now := time.Now().UTC()
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
		`SELECT rev, change, changed_by_kind, changed_by_name, changed_at, snapshot
		 FROM knowledge_revision WHERE id=$1 ORDER BY rev DESC LIMIT $2`,
		id, limit)
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
	_, err = tx.Exec(ctx, `INSERT INTO knowledge_revision (id, rev, change, changed_by_kind, changed_by_name, snapshot)
		VALUES ($1, (SELECT COALESCE(MAX(rev), 0) + 1 FROM knowledge_revision WHERE id=$1), $2, $3, $4, $5)`,
		k.ID, change, actor.Kind, actor.Name, snapshot)
	return err
}

// SearchLexical ranks by trigram similarity with a substring-match floor
// (trigram alone misses short Japanese terms), verified entries boosted.
// Attachment filenames join the haystack (design doc 0020): "seeds" finds
// the entry carrying seeds.txt, embedder or not. So does the id (design
// doc 0022): with title optional, the filename may be the entry's only
// name.
func (s *Store) SearchLexical(ctx context.Context, query string, f Filter, limit int) ([]domain.SearchHit, error) {
	where, args := f.buildWhere("k.")
	args = append(args, query)
	q := fmt.Sprintf(`
		SELECT `+knowledgeCols+`, score FROM (
			SELECT k.*, similarity(k.id || ' ' || k.title || ' ' || k.description || ' ' || array_to_string(k.tags, ' ') || ' ' || k.body || ' ' || att.names, $%d)
				+ CASE WHEN k.id || ' ' || k.title || ' ' || k.description || ' ' || k.body || ' ' || att.names ILIKE '%%' || $%d || '%%' THEN 0.3 ELSE 0 END
				+ CASE WHEN k.status = 'verified' THEN 0.05 ELSE 0 END AS score
			FROM knowledge k
			LEFT JOIN LATERAL (
				SELECT COALESCE(string_agg(a.name, ' '), '') AS names
				FROM attachment a WHERE a.knowledge_id = k.id
			) att ON true
			WHERE %s
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
