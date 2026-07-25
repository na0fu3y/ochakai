package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// migrateLockKey is the advisory-lock key serializing Migrate across
// processes ("ocha" in ASCII; arbitrary but stable).
const migrateLockKey = 0x6f636861

// Migrate applies embedded migrations in filename order. Applied versions
// are tracked in schema_migrations. When embedDim > 0, the pgvector schema
// is also ensured (separate from versioned migrations because the vector
// column dimension is configuration-dependent).
//
// Concurrent callers (several server instances starting at once, or
// parallel test binaries sharing a database) are serialized by a session
// advisory lock: without it, two processes can both see a migration as
// unapplied and both run it — the second then fails (or worse, rewrites
// data twice) against the schema the first already changed.
func (s *Store) Migrate(ctx context.Context, embedDim int) error {
	lock, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration lock connection: %w", err)
	}
	defer lock.Release()
	if _, err := lock.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrateLockKey); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	// Best-effort unlock: if it fails the connection is broken, and the
	// pool discards broken connections on Release, which ends the session
	// and releases the lock server-side.
	defer func() { _, _ = lock.Exec(ctx, `SELECT pg_advisory_unlock($1)`, migrateLockKey) }()

	if _, err := s.pool.Exec(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		var applied bool
		if err := s.pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, name).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		sql, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, name); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}

	if embedDim > 0 {
		if err := s.migrateEmbedding(ctx, embedDim); err != nil {
			return err
		}
	}
	return nil
}

// migrateEmbedding sets up pgvector storage. Runs only when an embedding
// provider is configured, keeping plain PostgreSQL sufficient by default.
func (s *Store) migrateEmbedding(ctx context.Context, dim int) error {
	if _, err := s.pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS vector`); err != nil {
		return fmt.Errorf("pgvector extension is required for semantic search (create it as the admin user, or unset OCHAKAI_VERTEX_PROJECT): %w", err)
	}
	if _, err := s.pool.Exec(ctx, fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS knowledge_embedding (
			id         text NOT NULL PRIMARY KEY,
			model      text NOT NULL,
			embedding  vector(%d) NOT NULL,
			updated_at timestamptz NOT NULL DEFAULT now()
		)`, dim)); err != nil {
		return fmt.Errorf("create knowledge_embedding: %w", err)
	}
	// Attachment vectors (design doc 0020): one row per embedded
	// attachment, mapped back to the owning entry at search time.
	if _, err := s.pool.Exec(ctx, fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS attachment_embedding (
			knowledge_id text NOT NULL,
			name         text NOT NULL,
			model        text NOT NULL,
			embedding    vector(%d) NOT NULL,
			updated_at   timestamptz NOT NULL DEFAULT now(),
			PRIMARY KEY (knowledge_id, name)
		)`, dim)); err != nil {
		return fmt.Errorf("create attachment_embedding: %w", err)
	}
	if err := s.checkEmbeddingDim(ctx, dim); err != nil {
		return err
	}
	return s.migrateVectorIndexes(ctx, dim)
}

// checkEmbeddingDim refuses to serve when the configured dimension is not
// the one the vector columns were created with.
//
// CREATE TABLE IF NOT EXISTS keeps the old column, so changing
// OCHAKAI_EMBEDDING_DIM on a base that already has vectors used to start
// cleanly and break everything downstream: every embedding write fails
// with a warning nobody reads, and every search fails outright, because
// the query vector cannot be compared against a column of another width.
// `ochakai reembed`, which the deploy guide points at for a model change,
// fails on every entry for the same reason. There is no recovery this can
// perform on its own — the old vectors are in a space nothing queries any
// more, and dropping them is the operator's call — so it says exactly
// that and stops.
func (s *Store) checkEmbeddingDim(ctx context.Context, dim int) error {
	var stored int
	// to_regclass rather than a cast: the cast raises when the table is
	// absent, and "absent" is the one case with nothing to disagree about.
	err := s.pool.QueryRow(ctx,
		`SELECT atttypmod FROM pg_attribute
		 WHERE attrelid = to_regclass('knowledge_embedding') AND attname = 'embedding'`).
		Scan(&stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read the stored embedding dimension: %w", err)
	}
	if stored == dim {
		return nil
	}
	return fmt.Errorf("knowledge_embedding.embedding is vector(%d) but OCHAKAI_EMBEDDING_DIM is %d: "+
		"the stored vectors are in a space nothing would query, so writes and searches would both fail. "+
		"Set OCHAKAI_EMBEDDING_DIM back to %d, or drop the old vectors and rebuild them "+
		"(DROP TABLE knowledge_embedding, attachment_embedding; restart; ochakai reembed)",
		stored, dim, stored)
}

// hnswMaxDim is pgvector's indexing limit for the vector type. Above it
// no ANN index can be built (halfvec would raise it to 4000, but storing
// half precision is a different decision than indexing), so those
// deployments keep the exact scan.
const hnswMaxDim = 2000

// migrateVectorIndexes builds the HNSW indexes behind semantic search.
// 0001 left them out on the grounds that an exact scan is fine at
// knowledge-base scale — true at a few thousand rows, but a table
// specification plus a glossary plus the Golden Queries and Insights the
// write-back loop accumulates passes that within a year, and every search
// reads every vector until it does not.
//
// The build is not deferred to a background goroutine: it runs once, on
// the first startup after semantic search is enabled — when the table has
// just been created and is empty — and the advisory lock in Migrate
// already serializes instances starting together. A deployment that
// enables it against an existing corpus pays a one-time build well inside
// Cloud Run's startup allowance.
//
// vector_cosine_ops matches the `<=>` operator the searches order by; an
// index built for another distance would simply never be used.
func (s *Store) migrateVectorIndexes(ctx context.Context, dim int) error {
	if dim > hnswMaxDim {
		// Not an error: the exact scan still answers correctly, and the
		// operator chose the dimension deliberately.
		return nil
	}
	for _, stmt := range []string{
		`CREATE INDEX IF NOT EXISTS knowledge_embedding_hnsw
			ON knowledge_embedding USING hnsw (embedding vector_cosine_ops)`,
		`CREATE INDEX IF NOT EXISTS attachment_embedding_hnsw
			ON attachment_embedding USING hnsw (embedding vector_cosine_ops)`,
	} {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("create vector index: %w", err)
		}
	}
	return nil
}
