// Embeddings: which concepts and attachments have no vector for the
// model in use, and where a computed vector is put. Nothing here calls
// Vertex AI — the service does that and hands the numbers down.
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/na0fu3y/ochakai/internal/domain"
)

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
		`SELECT k.id FROM object k
		 LEFT JOIN knowledge_embedding e ON e.id = k.id AND e.model = $1
		 WHERE k.deleted_at IS NULL AND k.id IS NOT NULL AND e.id IS NULL AND ($2 = '' OR k.id > $2)
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
		`SELECT k.id, split_part(f.path, '/', array_length(string_to_array(f.path, '/'), 1))
		   FROM object k JOIN object f ON `+attributedTo+`
		   LEFT JOIN attachment_embedding e
		     ON e.knowledge_id = k.id
		    AND e.name = split_part(f.path, '/', array_length(string_to_array(f.path, '/'), 1))
		    AND e.model = $1
		  WHERE k.id IS NOT NULL AND k.deleted_at IS NULL AND e.knowledge_id IS NULL
		    AND ($2 = '' OR (k.id, f.path) > ($2, $3))
		  ORDER BY k.id, f.path LIMIT $4`, model, afterID, afterName, limit)
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
		`SELECT count(*)
		   FROM object k JOIN object f ON `+attributedTo+`
		   LEFT JOIN attachment_embedding e
		     ON e.knowledge_id = k.id
		    AND e.name = split_part(f.path, '/', array_length(string_to_array(f.path, '/'), 1))
		    AND e.model = $1
		  WHERE k.id IS NOT NULL AND k.deleted_at IS NULL AND e.knowledge_id IS NULL`, model).Scan(&n)
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
		`SELECT count(*) FROM object k
		 LEFT JOIN knowledge_embedding e ON e.id = k.id AND e.model = $1
		 WHERE k.deleted_at IS NULL AND k.id IS NOT NULL AND e.id IS NULL`, model).Scan(&n)
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
