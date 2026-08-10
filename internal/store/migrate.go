package store

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"

	"github.com/jackc/pgx/v5"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/okf"
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

	// Runs inside the advisory lock, like the SQL migrations, so several
	// instances starting at once do not compose the same documents twice.
	if err := s.backfillDocuments(ctx); err != nil {
		return fmt.Errorf("backfill documents: %w", err)
	}
	if err := s.backfillLinkForms(ctx); err != nil {
		return fmt.Errorf("backfill link forms: %w", err)
	}
	if err := s.backfillFrontmatter(ctx); err != nil {
		return fmt.Errorf("backfill frontmatter: %w", err)
	}

	if embedDim > 0 {
		if err := s.migrateEmbedding(ctx, embedDim); err != nil {
			return err
		}
	}
	return nil
}

// backfillDocuments composes the canonical document (and its hash) for
// every entry and revision written before migration 0024 made the
// document the stored form (design doc 0043 §§3.1, 3.9).
//
// It lives here rather than in the migration file because composing a
// document means writing YAML in the spelling and key order the spec
// fixes — the same renderer an export uses, which is Go. An empty doc is
// the marker for "not yet composed", so this is idempotent: a run that
// dies half-way resumes, and a database that has been through it does a
// single counting query and returns.
//
// updated_at is deliberately not touched. Composing the stored form is a
// change of representation, not of content.
func (s *Store) backfillDocuments(ctx context.Context) error {
	if err := s.backfillEntryDocuments(ctx); err != nil {
		return err
	}
	return s.backfillRevisionDocuments(ctx)
}

func (s *Store) backfillEntryDocuments(ctx context.Context) error {
	for {
		rows, err := s.pool.Query(ctx,
			// id IS NOT NULL: a file object has no document to compose
			// and no id to scan into (design doc 0046 §3.13).
			`SELECT `+knowledgeSelect+` FROM object
			 WHERE doc = '' AND id IS NOT NULL ORDER BY id LIMIT $1`,
			backfillBatch)
		if err != nil {
			return err
		}
		batch, err := pgx.CollectRows(rows, scanKnowledge)
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		for i := range batch {
			// The row has no received bytes — it predates the document
			// being what is stored — so storedDoc composes the canonical
			// form, which is the document this entry has.
			doc, hash, err := storedDoc(&batch[i])
			if err != nil {
				// A row the renderer cannot read is left with an empty
				// doc rather than a wrong one, and named so somebody can
				// look. Returning here would wedge every later start.
				s.log.Warn("cannot compose the stored document", "id", batch[i].ID, "err", err)
				continue
			}
			if _, err := s.pool.Exec(ctx,
				`UPDATE object SET doc = $2, content_hash = $3 WHERE id = $1`,
				batch[i].ID, doc, hash); err != nil {
				return err
			}
		}
		// Rows the renderer refused keep doc = '' and would be selected
		// again forever; stop once a batch converted nothing.
		if !anyConverted(ctx, s, batch) {
			return nil
		}
	}
}

// backfillLinkForms rewrites the ochakai:// links migration 0026 marked,
// into the bundle-absolute form SPEC §6 defines (design doc 0046 §3.6).
//
// It is Go rather than SQL because three things have to move together
// and only one of them is a column: the body inside the stored document
// (swapped in place, so the frontmatter the writer wrote is not
// reformatted), the links index derived from that body, and the content
// hash over the result. A regexp_replace on the body alone would leave
// the document contradicting its own index.
//
// The work list is a table, so a run that dies half-way resumes and one
// that finishes leaves nothing to do. content_changed_at is not touched:
// spelling a link the way the spec spells it is not a change to what the
// entry says, any more than composing the stored document was.
func (s *Store) backfillLinkForms(ctx context.Context) error {
	for {
		rows, err := s.pool.Query(ctx,
			`SELECT `+knowledgeSelectDoc+` FROM object
			 WHERE id IN (SELECT id FROM link_form_backfill) ORDER BY id LIMIT $1`,
			backfillBatch)
		if err != nil {
			if isUndefinedTable(err) {
				return nil // migration 0026 has not run yet on this database
			}
			return err
		}
		batch, err := pgx.CollectRows(rows, scanKnowledgeDoc)
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		for i := range batch {
			k := &batch[i]
			body := rewriteOchakaiURIs(k.Body)
			k.Doc = string(okf.ReplaceBody([]byte(k.Doc), body))
			k.Body = body
			k.Links = domain.LinksFromBody(k.ID, body)
			doc, hash, err := storedDoc(k)
			if err != nil {
				s.log.Warn("cannot rewrite an entry's link forms", "id", k.ID, "err", err)
				continue
			}
			j, err := marshalJSONFields(k)
			if err != nil {
				return err
			}
			if _, err := s.pool.Exec(ctx,
				`UPDATE object SET body=$2, links=$3, doc=$4, content_hash=$5 WHERE id=$1`,
				k.ID, k.Body, j.links, doc, hash); err != nil {
				return err
			}
		}
		ids := make([]string, len(batch))
		for i := range batch {
			ids[i] = batch[i].ID
		}
		if _, err := s.pool.Exec(ctx, `DELETE FROM link_form_backfill WHERE id = ANY($1)`, ids); err != nil {
			return err
		}
	}
}

// backfillFrontmatter fills the queryable index over each stored
// document (design doc 0046 §3.11), for the entries migration 0027
// listed.
//
// Go rather than SQL for the reason the link-form backfill is: reading a
// key out of a document means parsing YAML the way the parser parses it,
// dates included, and a second reading written in SQL would be a second
// definition of what the document says.
//
// No timestamp moves and no hash changes. Building an index over what an
// entry already said is not a change to what it says.
func (s *Store) backfillFrontmatter(ctx context.Context) error {
	for {
		rows, err := s.pool.Query(ctx,
			`SELECT id, doc FROM object
			 WHERE id IN (SELECT id FROM frontmatter_backfill) ORDER BY id LIMIT $1`,
			backfillBatch)
		if err != nil {
			if isUndefinedTable(err) {
				return nil // migration 0027 has not run yet on this database
			}
			return err
		}
		type row struct{ id, doc string }
		batch, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
			var out row
			err := r.Scan(&out.id, &out.doc)
			return out, err
		})
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		ids := make([]string, len(batch))
		for i, b := range batch {
			ids[i] = b.id
			fm, err := storedFrontmatter(b.doc)
			if err != nil {
				// An index that cannot be built is a gap in querying, not
				// a reason to hold up a start: the document still answers
				// everything the index would have.
				s.log.Warn("cannot index an entry's frontmatter", "id", b.id, "err", err)
				continue
			}
			if _, err := s.pool.Exec(ctx,
				`UPDATE object SET frontmatter = $2 WHERE id = $1`, b.id, fm); err != nil {
				return err
			}
		}
		if _, err := s.pool.Exec(ctx, `DELETE FROM frontmatter_backfill WHERE id = ANY($1)`, ids); err != nil {
			return err
		}
	}
}

var (
	// ochakaiTargetRe matches the retired scheme where it was a markdown
	// link's target; only the target is rewritten, and the anchor text
	// the writer chose stays theirs.
	ochakaiTargetRe = regexp.MustCompile(`\]\(ochakai://([^)\s]+)\)`)
	// ochakaiBareRe matches what is left: an autolink, or a URI written
	// as bare prose. The trailing sentence punctuation English and
	// Japanese put after one is excluded, exactly as the parser excluded
	// it while the form still existed — it was never part of the id.
	ochakaiBareRe = regexp.MustCompile(`<?ochakai://([^\s<>()\[\]"'。、，．！？]*[^\s<>()\[\]"'。、，．！？.,;:!?])>?`)
)

// rewriteOchakaiURIs turns every ochakai://<id> in a body into the form
// SPEC §6 recommends.
//
// A bare or autolinked URI becomes a whole markdown link rather than a
// bare path: "/metrics/revenue.md" sitting in prose is a path, not a
// link, and would leave the entry with one fewer edge than its writer
// wrote. The anchor text is the id, which is what the bare form
// displayed anyway.
func rewriteOchakaiURIs(body string) string {
	body = ochakaiTargetRe.ReplaceAllString(body, "](/$1.md)")
	return ochakaiBareRe.ReplaceAllString(body, "[$1](/$1.md)")
}

// anyConverted reports whether any of the batch now has a document, so a
// batch that failed wholesale ends the loop instead of repeating it.
func anyConverted(ctx context.Context, s *Store, batch []domain.Knowledge) bool {
	ids := make([]string, len(batch))
	for i := range batch {
		ids[i] = batch[i].ID
	}
	var n int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM object WHERE id = ANY($1) AND doc <> ''`, ids).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

// backfillRevisionDocuments renders each stored snapshot as the document
// that revision held. A snapshot the current shape cannot read loses what
// it cannot express — design doc 0043 §3.9 accepts that in exchange for a
// history whose shape does not depend on knowing every past release.
func (s *Store) backfillRevisionDocuments(ctx context.Context) error {
	for {
		rows, err := s.pool.Query(ctx,
			`SELECT id, rev, snapshot FROM knowledge_revision
			 WHERE doc = '' AND snapshot IS NOT NULL ORDER BY id, rev LIMIT $1`, backfillBatch)
		if err != nil {
			return err
		}
		type row struct {
			id       string
			rev      int
			snapshot []byte
		}
		batch, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
			var v row
			return v, r.Scan(&v.id, &v.rev, &v.snapshot)
		})
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		for _, b := range batch {
			doc := ""
			var k domain.Knowledge
			if err := json.Unmarshal(b.snapshot, &k); err != nil {
				s.log.Warn("cannot read a revision snapshot", "id", b.id, "rev", b.rev, "err", err)
			} else if rendered, err := okf.Document(&k); err != nil {
				s.log.Warn("cannot compose a revision document", "id", b.id, "rev", b.rev, "err", err)
			} else {
				doc = string(rendered)
			}
			// An unreadable snapshot is marked with a placeholder rather
			// than left empty: empty means "not yet converted", and the
			// loop would come back to it every start.
			if doc == "" {
				doc = unreadableRevision
			}
			if _, err := s.pool.Exec(ctx,
				`UPDATE knowledge_revision SET doc = $3 WHERE id = $1 AND rev = $2`,
				b.id, b.rev, doc); err != nil {
				return err
			}
		}
	}
}

// backfillBatch bounds how much of the knowledge base the backfill holds
// at once, the way the exporter batches for the same reason.
const backfillBatch = 200

// unreadableRevision stands in for a snapshot the current shape cannot
// read. It is a valid OKF document, so every consumer of a revision gets
// a document — and it says plainly that the content is gone rather than
// pretending the revision was empty.
const unreadableRevision = "---\ntype: Reference\nstatus: deprecated\n---\n\n" +
	"This revision was written in a shape this release cannot read, and its\n" +
	"content did not survive the move to document storage (design doc 0043 §3.9).\n"

// ErrEmbeddingUnavailable reports that the vector storage behind
// semantic search could not be set up — the pgvector extension is not
// there and this role may not create it, or the tables and indexes were
// refused. Everything else about the database is fine.
//
// It exists so a caller can tell that apart from a database that cannot
// be migrated at all: a deployment that asked for semantic search by name
// is told either way, while one that got it as the default of running on
// Google Cloud carries on lexical-only rather than refusing to serve
// knowledge over an index it never asked for (design doc 0053 §2.3).
var ErrEmbeddingUnavailable = errors.New("vector storage for semantic search is unavailable")

// migrateEmbedding sets up pgvector storage. Runs only when an embedding
// provider is configured, keeping plain PostgreSQL sufficient by default.
//
// It is the last thing Migrate does, so a caller that decides to carry on
// without semantic search has a fully migrated database already.
func (s *Store) migrateEmbedding(ctx context.Context, dim int) error {
	if _, err := s.pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS vector`); err != nil {
		return fmt.Errorf("%w: pgvector is required for semantic search, and this role may not create it (create it as the admin user, or set OCHAKAI_EMBEDDINGS=off): %w", ErrEmbeddingUnavailable, err)
	}
	// Before the tables are ensured, because CREATE TABLE IF NOT EXISTS
	// would keep a column of the old width and say nothing.
	if err := s.resizeVectorSpace(ctx, dim); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS knowledge_embedding (
			id         text NOT NULL PRIMARY KEY,
			model      text NOT NULL,
			embedding  vector(%d) NOT NULL,
			truncated  boolean NOT NULL DEFAULT false,
			updated_at timestamptz NOT NULL DEFAULT now()
		)`, dim)); err != nil {
		return fmt.Errorf("%w: create knowledge_embedding: %w", ErrEmbeddingUnavailable, err)
	}
	// For a table that already exists: CREATE TABLE IF NOT EXISTS keeps
	// the shape it found. The column records that the vector above covers
	// only the front of its concept, which is the one thing about a
	// vector a reader cannot recompute from the object it describes —
	// the model's window at the moment it was written is not stored
	// anywhere else. Existing rows default to false and are corrected by
	// the next write or by `ochakai reembed`; a vector is derived, and
	// wrong-by-default here means "not known to be truncated", which is
	// what an un-reembedded row honestly is.
	if _, err := s.pool.Exec(ctx,
		`ALTER TABLE knowledge_embedding ADD COLUMN IF NOT EXISTS truncated boolean NOT NULL DEFAULT false`); err != nil {
		return fmt.Errorf("%w: knowledge_embedding.truncated: %w", ErrEmbeddingUnavailable, err)
	}
	if err := s.rekeyAttachmentVectors(ctx); err != nil {
		return err
	}
	// Attachment vectors (design doc 0020): one row per embedded file,
	// keyed by the file's path, which is its address (design doc 0075).
	// The owning concept is found through `object` at search time rather
	// than stored here — a file may be attributed by a body link from
	// anywhere in the bundle, and which concept that is can change
	// without the bytes changing.
	if _, err := s.pool.Exec(ctx, fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS attachment_embedding (
			path       text NOT NULL PRIMARY KEY,
			model      text NOT NULL,
			embedding  vector(%d) NOT NULL,
			updated_at timestamptz NOT NULL DEFAULT now()
		)`, dim)); err != nil {
		return fmt.Errorf("%w: create attachment_embedding: %w", ErrEmbeddingUnavailable, err)
	}
	return s.migrateVectorIndexes(ctx, dim)
}

// rekeyAttachmentVectors drops an attachment_embedding still keyed by
// (knowledge_id, name), so the create below rebuilds it keyed by path.
//
// The old key predates files being objects. Since design doc 0075 a
// file's address *is* its path, and (owner, last segment) only
// reconstructs that for a file sitting at the canonical `<id>/<name>` —
// a concept whose body links `charts/q1.png` and `tables/q1.png` keyed
// both rows the same way, and the second vector overwrote the first.
// The read paths moved to `object` in the release that introduced it and
// this table was left for later (migration 0029); this is later.
//
// Dropped rather than backfilled, which is what a vector table is for:
// every row here can be recomputed from the bytes it describes, the
// product already rebuilds this space when the dimension moves, and a
// backfill would have to guess exactly the mapping this key was
// ambiguous about. The log line names `ochakai reembed`, as the resize
// does; until it runs, files are found by name, which is where a
// deployment without embeddings already is (design doc 0080 §5).
//
// Schema-qualified for resizeVectorSpace's reason: a store whose
// search_path carries a second schema must not drop that schema's table.
func (s *Store) rekeyAttachmentVectors(ctx context.Context) error {
	var old bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_attribute
		  WHERE attrelid = to_regclass(quote_ident(current_schema()) || '.attachment_embedding')
		    AND attname = 'knowledge_id' AND NOT attisdropped)`).Scan(&old)
	if err != nil {
		return fmt.Errorf("read the attachment vector key: %w", err)
	}
	if !old {
		return nil
	}
	if _, err := s.pool.Exec(ctx,
		`DO $$ BEGIN
			EXECUTE format('DROP TABLE IF EXISTS %I.attachment_embedding', current_schema());
		 END $$`); err != nil {
		return fmt.Errorf("%w: drop the attachment vectors of the previous key: %w",
			ErrEmbeddingUnavailable, err)
	}
	s.log.Warn("attachment vectors were keyed by (concept, filename), which cannot tell two files of the same name apart; they are keyed by path now and the old rows have been dropped. Files are found by name until `ochakai reembed` rebuilds them")
	return nil
}

// resizeVectorSpace rebuilds the vector tables when the configured
// dimension is not the one their columns were created with.
//
// CREATE TABLE IF NOT EXISTS keeps the old column, so a changed dimension
// used to start cleanly and break everything downstream: every embedding
// write fails on the width, every search fails outright because the query
// vector cannot be compared against a column of another width, and
// `ochakai reembed` — the thing the deploy guide points at for a model
// change — fails on every entry for the same reason. Startup refused
// instead, and told the operator to run `DROP TABLE knowledge_embedding,
// attachment_embedding` by hand. That refusal is why the feature read as
// advanced: the one operational hazard in the product was a schema change
// the product would not perform (design doc 0053 §3).
//
// It performs it now, because of what these tables are. **A vector is
// derived, not recorded**: every one of them can be recomputed from the
// object it describes, which is why they live outside the versioned
// migrations, why `reembed` exists, and why a model change already leaves
// them behind without anybody calling that data loss. Dropping a space
// nothing queries costs the embedding calls to refill it, and nothing
// else — the knowledge is in `object`, untouched.
//
// What it does cost is those calls, so it says so rather than doing it
// quietly, and leaves the refill to `ochakai reembed`: search stays
// lexical until somebody spends that money deliberately (0053 §3.2).
func (s *Store) resizeVectorSpace(ctx context.Context, dim int) error {
	var stored int
	// to_regclass rather than a cast: the cast raises when the table is
	// absent, and "absent" is the one case with nothing to disagree about.
	//
	// Qualified by current_schema(), and so is the drop below. Both used
	// to name the tables bare, which resolves through the whole
	// search_path — so a store whose path is "<schema>,public" could
	// probe or drop *another schema's* table. The creates a few lines
	// down are unqualified and therefore land in current_schema(), so
	// that is the schema this function is about; reading or dropping
	// anywhere else was never what it meant.
	//
	// Nothing in a deployment has a second schema on its path, which is
	// why this held in production. It did not hold under test: the
	// scoped store used for the destructive migration tests puts public
	// second, has knowledge_embedding of its own and no
	// attachment_embedding — so the bare drop took its own table and
	// then public's, and the tests sharing public failed later,
	// intermittently and somewhere else.
	err := s.pool.QueryRow(ctx,
		`SELECT atttypmod FROM pg_attribute
		 WHERE attrelid = to_regclass(quote_ident(current_schema()) || '.knowledge_embedding')
		   AND attname = 'embedding'`).
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
	if _, err := s.pool.Exec(ctx,
		`DO $$ BEGIN
			EXECUTE format('DROP TABLE IF EXISTS %I.knowledge_embedding, %I.attachment_embedding',
				current_schema(), current_schema());
		 END $$`); err != nil {
		return fmt.Errorf("%w: drop the vector tables of the previous dimension: %w",
			ErrEmbeddingUnavailable, err)
	}
	s.log.Warn("the embedding dimension changed; the old vectors were in a space nothing would query and have been dropped. Search is lexical until `ochakai reembed` rebuilds them",
		"stored", stored, "configured", dim)
	return nil
}

// hnswMaxDim is pgvector's indexing limit for the vector type. Above it
// no ANN index can be built (halfvec would raise it to 4000, but storing
// half precision is a different decision than indexing), so those
// deployments keep the exact scan.
const hnswMaxDim = 2000

// migrateVectorIndexes builds the HNSW indexes behind semantic search.
// 0001 left them out on the grounds that an exact scan is fine at
// knowledge-base scale — true at a few thousand rows, but a table
// specification plus a glossary plus the computations and insights the
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
