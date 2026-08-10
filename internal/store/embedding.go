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

// unembeddedFiles is the queue of files awaiting a vector, shared by the
// listing and its count so the number an operator is told to work down is
// the number the passes actually work down.
//
// It is a queue of *files*, not of (concept, file) pairs: since design
// doc 0091 the vector is keyed by the file's path and attribution is
// resolved at search time, so a file two concepts link is one row of work
// and a file nobody links yet is still work — it ranks the day a body
// names it, without a second pass.
//
// mediaTypes is what this deployment's model can be handed. It is the
// caller's because it is a property of the model, not of the corpus: on a
// text-only embedder a PNG is not pending, it is out of scope, and a
// queue that held it would never empty.
const unembeddedFiles = `FROM object f
	LEFT JOIN attachment_embedding e ON e.path = f.path AND e.model = $1
	WHERE f.id IS NULL AND f.deleted_at IS NULL AND e.path IS NULL
	  AND lower(split_part(f.media_type, ';', 1)) = ANY($2)`

// ListUnembeddedFiles is ListUnembedded for file vectors (design doc
// 0020). Writes are what produce them, so a bundle imported before
// semantic search — or one whose embedding tables were rebuilt to change
// dimensions — has none, and its files are findable by name only. after
// is a keyset cursor on the path, which is the key.
func (s *Store) ListUnembeddedFiles(ctx context.Context, model string, mediaTypes []string, after string, limit int) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT f.path `+unembeddedFiles+`
		  AND ($3 = '' OR f.path > $3)
		  ORDER BY f.path LIMIT $4`, model, mediaTypes, after, limit)
	if err != nil {
		if isUndefinedTable(err) {
			return nil, nil // embedding tables appear with semantic search
		}
		return nil, err
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil && isUndefinedTable(err) {
		return nil, nil
	}
	return out, err
}

// CountUnembeddedFiles counts what ListUnembeddedFiles would eventually
// return.
func (s *Store) CountUnembeddedFiles(ctx context.Context, model string, mediaTypes []string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) `+unembeddedFiles, model, mediaTypes).Scan(&n)
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
//
// truncated says the vector was computed over the front of the concept
// rather than all of it: the text did not fit the model's input window,
// so the tail is not in semantic search. It is recorded rather than
// logged because it is a property of the stored vector — the same
// concept is truncated under one model and whole under another, and
// nothing else on the row remembers which window it met.
func (s *Store) UpsertEmbedding(ctx context.Context, id, model string, vec []float32, truncated bool) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO knowledge_embedding (id, model, embedding, truncated, updated_at)
		VALUES ($1, $2, $3::vector, $4, now())
		ON CONFLICT (id) DO UPDATE SET model = $2, embedding = $3::vector, truncated = $4, updated_at = now()`,
		id, model, encodeVector(vec), truncated)
	return err
}

// EmbeddingCoverage counts the live concepts holding a vector for the
// model in use, and how many of those vectors saw only part of their
// concept.
//
// Truncation is the failure this exists to make visible. A concept over
// the window still embeds, still ranks, and still looks like every other
// concept in the results — it is simply missing its second half from the
// vector half of search, and until this counted them nothing said so on
// any surface. The longer and more valuable the document, the more of it
// disappears (issue #532).
//
// Scoped by prefix like the concept tallies, because it is a fact about
// concepts. Absent tables mean semantic search was never configured here,
// which is zero of both rather than an error.
func (s *Store) EmbeddingCoverage(ctx context.Context, model string, prefixes []string) (vectors, truncated int64, err error) {
	cond, args := prefixScope("k.id", prefixes, 2)
	if cond != "" {
		cond = " AND " + cond
	}
	err = s.pool.QueryRow(ctx,
		`SELECT count(*), count(*) FILTER (WHERE e.truncated)
		   FROM object k JOIN knowledge_embedding e ON e.id = k.id AND e.model = $1
		  WHERE k.deleted_at IS NULL AND k.id IS NOT NULL`+cond,
		append([]any{model}, args...)...).Scan(&vectors, &truncated)
	if err != nil && isUndefinedTable(err) {
		return 0, 0, nil
	}
	return vectors, truncated, err
}

// UpsertFileEmbedding stores the document embedding for one file,
// by the path that is its address (design docs 0020, 0075).
func (s *Store) UpsertFileEmbedding(ctx context.Context, path, model string, vec []float32) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO attachment_embedding (path, model, embedding, updated_at)
		VALUES ($1, $2, $3::vector, now())
		ON CONFLICT (path) DO UPDATE SET model = $2, embedding = $3::vector, updated_at = now()`,
		path, model, encodeVector(vec))
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

// CountFileEmbedding reports whether one attachment has a vector
// for the named model (0 or 1). Reembed uses it to tell an embedding
// that landed from one the provider declined — the attach path logs its
// own failures rather than returning them.
func (s *Store) CountFileEmbedding(ctx context.Context, path, model string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM attachment_embedding
		 WHERE path = $1 AND model = $2`, path, model).Scan(&n)
	if err != nil && isUndefinedTable(err) {
		return 0, nil
	}
	return n, err
}
