// Package store persists knowledge in PostgreSQL, the only runtime
// dependency of ochakai.
package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/na0fu3y/ochakai/internal/blob"
	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/okf"
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

// ErrNoRejection is returned by WithdrawRejection for a live entry that
// carries no rejection to withdraw — a state conflict, the same shape as
// ErrNotDeleted, not a missing resource (design doc 0064: this used to
// come back as ErrNotFound, indistinguishable on the wire from "no such
// concept").
var ErrNoRejection = errors.New("knowledge carries no rejection to withdraw")

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
	// blobs holds file bytes (GCS, design doc 0013); metadata stays
	// in PostgreSQL. When nil, files are unsupported — markdown concepts
	// only.
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
	// dropped counts what the buffer refused and what a failed flush took
	// with it, under the same lock. The next flush that reaches the
	// database writes it to usage_drop and clears it (migration 0038), so
	// the number outlives the instance that lost the events; until then it
	// is this process's memory of its own gap.
	usageMu       sync.Mutex
	usageBuf      []usageEvent
	missBuf       []missEvent
	droppedEvents int64
	droppedMisses int64
	flushStop     chan struct{}
	flushWG       sync.WaitGroup
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

const knowledgeCols = `type, id, title, description, resource, tags, status, status_note, stale_after,
	sources, usage_window, runtime, parameters, computation, executor, attester,
	created_by_kind, created_by_name, created_by_via, created_by_producer,
	updated_by_kind, updated_by_name, updated_by_via, updated_by_producer,
	links, attrs, body, created_at, updated_at, content_changed_at, doc, frontmatter, content_hash`

// ledgerCols is the two instance ledgers — verifications and the
// rejection — read as JSON beside the entry's own columns (design doc
// 0043 §§3.2-3.3). alias names the knowledge table in the surrounding
// query, since some read it as "knowledge" and the ones that join give
// it "k".
//
// They are correlated subqueries rather than joins so that no caller has
// to remember them: every read path selects knowledgeSelect or
// knowledgeSelectK, and an entry that arrives without its ledgers would
// silently read as unverified — which is the one wrong answer that looks
// like a valid one.
func ledgerCols(alias string) string {
	return `(SELECT jsonb_agg(jsonb_build_object(
			'by', jsonb_build_object('kind', v.by_kind, 'name', v.by_name, 'via', v.by_via, 'producer', v.by_producer),
			'at', v.at) ORDER BY v.seq)
		FROM knowledge_verification v WHERE v.id = ` + alias + `.id),
		(SELECT jsonb_build_object(
			'by', jsonb_build_object('kind', r.by_kind, 'name', r.by_name, 'via', r.by_via, 'producer', r.by_producer),
			'at', r.at, 'note', r.note)
		FROM knowledge_rejection r WHERE r.id = ` + alias + `.id)`
}

// lastVerifiedAt is the newest verification's timestamp for the entry
// aliased by alias, or NULL when it has never been verified. The feeds
// that used to read a verified_at column read this instead.
func lastVerifiedAt(alias string) string {
	return `(SELECT max(v.at) FROM knowledge_verification v WHERE v.id = ` + alias + `.id)`
}

// unansweredFailure is what puts an entry in the re-verification feed: it
// has failure reports, and none of them has been answered by a
// verification since (design doc 0025 §6.2). Needs usageLateral joined as
// u. Written once because the instance stats count the same feed, and a
// queue whose depth disagrees with the queue is worse than no depth at
// all (design doc 0051 §3.5).
func unansweredFailure(alias string) string {
	v := lastVerifiedAt(alias)
	return `u.failed > 0 AND (` + v + ` IS NULL OR u.last_failed_at > ` + v + `)`
}

// pastExpiry is what puts an entry in the stale_after feed: the writer
// declared an expiry and it has passed (design doc 0037 §2.1). "Today" is
// UTC, spelled out rather than left to current_date — the column is a
// bare date so staleness needs no timezone (design doc 0036 §3.9), and
// current_date would hand that decision to whatever TimeZone the database
// session happens to carry, making the same entry stale on one deployment
// and not on another.
func pastExpiry(prefix string) string {
	return prefix + `stale_after IS NOT NULL AND ` + prefix + `stale_after <= (now() AT TIME ZONE 'UTC')::date`
}

// withoutDoc drops the stored document from a column list. A read does
// not need it: the entry's own columns are the index derived from it
// (design doc 0043 §3.1), and carrying the whole document beside the
// body it contains would double every search result for nothing. Writes
// keep it — they are what the index is derived from.
// withoutFrontmatter drops only the index column, for the reads that do
// want the document itself.
func withoutFrontmatter(cols string) string {
	var kept []string
	for _, c := range strings.Split(cols, ",") {
		if t := strings.TrimSpace(c); !strings.HasSuffix(t, "frontmatter") {
			kept = append(kept, t)
		}
	}
	return strings.Join(kept, ", ")
}

func withoutDoc(cols string) string {
	var kept []string
	for _, c := range strings.Split(cols, ",") {
		t := strings.TrimSpace(c)
		if t == "doc" || t == "k.doc" || t == "object.doc" {
			continue
		}
		// The frontmatter index is never read back either: it is a
		// question-answering copy of what the document already carries
		// (design doc 0046 §3.11), so every read leaves it in the
		// database where the filters use it.
		if t == "frontmatter" || t == "k.frontmatter" || t == "object.frontmatter" {
			continue
		}
		kept = append(kept, t)
	}
	return strings.Join(kept, ", ")
}

var (
	// knowledgeSelect and knowledgeSelectK are what a read selects: the
	// entry's columns (minus the stored document) plus its ledgers.
	knowledgeSelect  = withoutDoc(knowledgeCols) + ", " + ledgerCols("object")
	knowledgeSelectK = withoutDoc(knowledgeColsK) + ", " + ledgerCols("k")
	// knowledgeSelectDoc is the same read with the stored document, for
	// the paths that hand one out (design doc 0046 §2.2): a single get,
	// an export, and the move that rewrites a body in place.
	knowledgeSelectDoc = withoutFrontmatter(knowledgeCols) + ", " + ledgerCols("object")
)

// sortedKeys orders a filter's frontmatter keys so a query's arguments —
// and therefore its prepared-statement text — are the same on every call
// with the same filter. Map iteration order would otherwise make the
// plan cache miss for no reason.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// notCurated is Knowledge.Curated() as a SQL predicate, for the guards
// that must agree with it inside a transaction. alias names the knowledge
// table. Curation is no longer a property of the status alone, so a guard
// that only compared statuses would stop protecting a verified or
// rejected entry the moment those became ledgers.
func notCurated(alias string) string {
	return alias + `.status <> 'deprecated'
		AND NOT EXISTS (SELECT 1 FROM knowledge_verification v WHERE v.id = ` + alias + `.id)
		AND NOT EXISTS (SELECT 1 FROM knowledge_rejection r WHERE r.id = ` + alias + `.id)`
}

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
//
// doc is included only for withDoc: a listing does not select the stored
// document (withoutDoc), because the index columns beside it already
// answer the query and carrying the document per row would double every
// result for nothing. The reads that hand out a document — one entry, an
// export, a move about to rewrite a body — do.
func knowledgeDest(k *domain.Knowledge) (dests []any, finish func() error) {
	return knowledgeDestFor(k, false)
}

func knowledgeDestWithDoc(k *domain.Knowledge) (dests []any, finish func() error) {
	return knowledgeDestFor(k, true)
}

func knowledgeDestFor(k *domain.Knowledge, withDoc bool) (dests []any, finish func() error) {
	var staleAfter *time.Time
	var links, attrs []byte
	var sources, usageWindow, parameters, executor, attester []byte
	var verifications, rejection []byte
	dests = []any{&k.Type, &k.ID, &k.Title, &k.Description, &k.Resource, &k.Tags, &k.Status, &k.StatusNote, &staleAfter,
		&sources, &usageWindow, &k.Runtime, &parameters, &k.Computation, &executor, &attester,
		&k.CreatedBy.Kind, &k.CreatedBy.Name, &k.CreatedBy.Via, &k.CreatedBy.Producer,
		&k.UpdatedBy.Kind, &k.UpdatedBy.Name, &k.UpdatedBy.Via, &k.UpdatedBy.Producer,
		&links, &attrs, &k.Body, &k.CreatedAt, &k.UpdatedAt, &k.ContentChangedAt}
	if withDoc {
		dests = append(dests, &k.Doc)
	}
	dests = append(dests, &k.ContentHash, &verifications, &rejection)
	finish = func() error {
		defer func() { k.Trust = domain.TrustOf(k.Verifications) }()
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
			{verifications, &k.Verifications}, {rejection, &k.Rejection},
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

// storedDoc returns what the row holds as the entry's document, and the
// entry's version: the document as received, and the SHA-256 of exactly
// those bytes (design doc 0046 §§2.2, 3.4).
//
// A write that came from a document carries it. One that did not — an
// entry composed in memory — is written in the canonical form, which is
// the only document it has. Either way the hash covers what is stored, so
// the version means "these bytes", and neither a ruling nor a
// verification, which write no bytes here, can move it.
// storedFrontmatter is the queryable index over the document a write
// stores (design doc 0046 §3.11) — derived from the same bytes, in the
// same call, so the two cannot drift.
func storedFrontmatter(doc string) ([]byte, error) {
	return json.Marshal(okf.Frontmatter([]byte(doc)))
}

func storedDoc(k *domain.Knowledge) (doc, hash string, err error) {
	b := []byte(k.Doc)
	if len(b) > 0 && !documentSays(b, k) {
		b = nil
	}
	if len(b) == 0 {
		if b, err = okf.Canonical(k); err != nil {
			return "", "", err
		}
	}
	sum := sha256.Sum256(b)
	return string(b), hex.EncodeToString(sum[:]), nil
}

// StoredDocument is what a write of k would put in the row: the document
// as received when it still says what k says, and the canonical
// rendering otherwise (storedDoc). The service compares it with the
// stored one to tell a write that changes nothing at all from a write
// that changes only the layout (design doc 0046 §3.4).
func StoredDocument(k *domain.Knowledge) (doc, hash string, err error) { return storedDoc(k) }

// documentSays reports whether doc still says what k says. A caller that
// edited the entry's fields without editing the document it came from is
// not asking for those bytes to be stored — storing them anyway would
// leave the row's document contradicting the index derived from it, and
// the document is what wins that argument (design doc 0043 §3.1). So the
// document is dropped and the canonical form composed instead.
//
// This is the guard that lets every write path share one rule: hand over
// a document and it is stored verbatim; change fields and what is stored
// is what the fields say.
func documentSays(doc []byte, k *domain.Knowledge) bool {
	d, _, err := okf.Parse(doc)
	if err != nil {
		return false
	}
	// A document carries neither the entry's address — the path is
	// (design doc 0017) — nor its links, which are derived from the body
	// it does carry (design doc 0024).
	d.ID, d.Links = k.ID, k.Links
	// A document that names no status still agrees with the index column
	// derived from it: SameContent compares what a reader reads, and a
	// silent document reads as OKF's default (design doc 0046 §3.9).
	return d.SameContent(k)
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

// scanKnowledgeDoc scans a row selected with knowledgeSelectDoc: the same
// entry, with the document it is stored as.
func scanKnowledgeDoc(row pgx.CollectableRow) (domain.Knowledge, error) {
	var k domain.Knowledge
	dests, finish := knowledgeDestWithDoc(&k)
	if err := row.Scan(dests...); err != nil {
		return k, err
	}
	return k, finish()
}

func actorFrom(kind, name *string, via, producer string) *domain.Actor {
	if kind == nil || name == nil {
		return nil
	}
	return &domain.Actor{Kind: *kind, Name: *name, Via: via, Producer: producer}
}

// actorPtrs splits an optional actor into its columns. Kind and name are
// nullable together (no actor at all); via and producer are plain columns
// because an absent delegation and an empty one are the same thing, and so
// are an unnamed producer and an empty one (design docs 0027 §5.3, 0052
// §3.4).
func actorPtrs(a *domain.Actor) (kind, name *string, via, producer string) {
	if a == nil {
		return nil, nil, "", ""
	}
	return &a.Kind, &a.Name, a.Via, a.Producer
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
	return s.getOneFrom(ctx, s.pool, id, deleted)
}

// getOneTx is getOne inside a transaction, for the ledger writes that
// must read back what they just wrote before the commit.
func (s *Store) getOneTx(ctx context.Context, tx pgx.Tx, id string, deleted bool) (*domain.Knowledge, error) {
	return s.getOneFrom(ctx, tx, id, deleted)
}

// querier is the part of pgxpool.Pool and pgx.Tx that reads.

// bodyFiles is the paths of the non-concept objects an entry's body
// points at, as the column stores them (design doc 0046 §3.3). It is
// derived on every write for the same reason links are (design doc
// 0024): the body is the record, and a list beside it that a writer
// could edit separately would be a second one to keep true.
func bodyFiles(k *domain.Knowledge) []byte {
	files := domain.FilesFromBody(k.ID, k.Body)
	if files == nil {
		files = []string{}
	}
	b, err := json.Marshal(files)
	if err != nil { // a []string never fails to marshal
		return []byte("[]")
	}
	return b
}

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func (s *Store) getOneFrom(ctx context.Context, q querier, id string, deleted bool) (*domain.Knowledge, error) {
	cond := "deleted_at IS NULL"
	if deleted {
		cond = "deleted_at IS NOT NULL"
	}
	rows, err := q.Query(ctx,
		`SELECT `+knowledgeSelectDoc+` FROM object WHERE id = $1 AND `+cond, id)
	if err != nil {
		return nil, err
	}
	k, err := pgx.CollectExactlyOneRow(rows, scanKnowledgeDoc)
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

// ListLinkingToDocs returns live entries whose links point at id, most
// recently updated first, with their documents. It is the context pack's
// reverse edge: a pack delivers its companions as documents, and a
// companion read without one would come back rendered while the entries
// beside it came back as written (design doc 0046 §2.2).
//
// The reverse lookup a caller can ask for is the LinksTo filter instead
// — a row there is a projection, and the document would be paid for and
// dropped.
func (s *Store) ListLinkingToDocs(ctx context.Context, id string, limit int) ([]domain.Knowledge, error) {
	return s.listLinkingTo(ctx, id, limit, knowledgeSelectDoc, scanKnowledgeDoc)
}

// StatementCount reports how many times a statement has taken a
// connection from the pool since the store opened — one per query, and
// one per transaction however many statements it runs.
//
// It exists so that a caller's cost in round trips can be read from
// outside rather than reasoned about (design doc 0035). The context pack
// is the case: it used to read once per hit and once per link, and
// nothing about its result said so. The difference across a call is what
// TestContextReadsDoNotGrowWithThePack asserts against.
func (s *Store) StatementCount() int64 { return s.pool.Stat().AcquireCount() }

// GetManyDocs returns the live entries holding these ids, with their
// documents, keyed by id. Ids that name nothing live are absent rather
// than an error: the caller asked about several, and one deleted target
// is not a failed read of the others.
//
// The context pack is why this exists. It used to call Get once per hit
// and once per link, which is one round trip each — up to sixty for the
// one call an agent is told to make before a data question, all of them
// waiting on the previous one. The rows are the same rows; only the
// number of times the server asks for them changes.
func (s *Store) GetManyDocs(ctx context.Context, ids []string) (map[string]*domain.Knowledge, error) {
	out := map[string]*domain.Knowledge{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT `+knowledgeSelectDoc+` FROM object WHERE deleted_at IS NULL AND id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	found, err := pgx.CollectRows(rows, scanKnowledgeDoc)
	if err != nil {
		return nil, err
	}
	for i := range found {
		out[found[i].ID] = &found[i]
	}
	return out, nil
}

// LinkersByTargetDocs answers ListLinkingToDocs for several ids at once,
// grouped by the id linked at and each group ordered and capped exactly
// as the single-id form is.
//
// The lateral join is what makes it the same answer rather than a
// cheaper approximation: a union ordered globally would let one id's
// linkers crowd out another's, and the caller reads the groups in rank
// order precisely because the first id's companions matter most. An
// entry linking at two of the ids appears in both groups, as it did when
// each id was asked separately.
func (s *Store) LinkersByTargetDocs(ctx context.Context, ids []string, perID int) (map[string][]domain.Knowledge, error) {
	out := map[string][]domain.Knowledge{}
	if len(ids) == 0 || perID <= 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT `+knowledgeSelectDoc+`, t.target
		 FROM unnest($1::text[]) AS t(target)
		 CROSS JOIN LATERAL (
			SELECT * FROM object o
			WHERE o.deleted_at IS NULL
			  AND o.links @> jsonb_build_array(jsonb_build_object('target', t.target))
			ORDER BY o.updated_at DESC LIMIT $2
		 ) object`, ids, perID)
	if err != nil {
		return nil, err
	}
	linkers, err := pgx.CollectRows(rows, scanKnowledgeDocForTarget)
	if err != nil {
		return nil, err
	}
	for _, l := range linkers {
		out[l.target] = append(out[l.target], l.knowledge)
	}
	return out, nil
}

// targetedKnowledge is one row of LinkersByTargetDocs: the entry, and
// which of the asked-for ids its links pointed at.
type targetedKnowledge struct {
	target    string
	knowledge domain.Knowledge
}

func scanKnowledgeDocForTarget(row pgx.CollectableRow) (targetedKnowledge, error) {
	var t targetedKnowledge
	dests, finish := knowledgeDestWithDoc(&t.knowledge)
	if err := row.Scan(append(dests, &t.target)...); err != nil {
		return t, err
	}
	return t, finish()
}

func (s *Store) listLinkingTo(ctx context.Context, id string, limit int, cols string,
	scan func(pgx.CollectableRow) (domain.Knowledge, error),
) ([]domain.Knowledge, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+cols+` FROM object
		 WHERE deleted_at IS NULL AND links @> $1
		 ORDER BY updated_at DESC LIMIT $2`,
		fmt.Sprintf(`[{"target": %q}]`, id),
		limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scan)
}

// Create inserts a new entry. A live entry with the same id is
// ErrAlreadyExists — including rejected ones, so the memory of no
// survives. A soft-deleted entry is revived instead: the ID would
// otherwise be dead forever (the row still owns the primary key while
// Update refuses deleted rows), and its history stays in the revisions
// either way.
func (s *Store) Create(ctx context.Context, k *domain.Knowledge, keepCuratedTombstones bool) error {
	now := NowStored()
	k.CreatedAt, k.UpdatedAt, k.ContentChangedAt = now, now, now
	// Reviving a tombstone overwrites its status in place. When the caller
	// must not replace a human ruling (design doc 0015 §3.1), the revival
	// carries the same restriction as the UPDATE itself, so a curation
	// landing between a service-level check and this write loses the race
	// instead of being erased by it.
	revive := ""
	if keepCuratedTombstones {
		revive = " AND " + notCurated("object")
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
		doc, hash, err := storedDoc(k)
		if err != nil {
			return err
		}
		fm, err := storedFrontmatter(doc)
		if err != nil {
			return err
		}
		// The row's bytes go back onto the entry, so what a write returns
		// is what a later read returns (design doc 0046 §2.2). The two
		// differ whenever storedDoc fell back to the canonical form — a
		// caller that changed fields rather than a document — and a
		// surface rendering the payload back would show bytes nothing
		// stored.
		k.Doc, k.ContentHash = doc, hash
		args := []any{
			k.Type, k.ID, k.Title, k.Description, k.Resource, k.Tags, k.Status, k.StatusNote, staleAfter,
			j.sources, j.usageWindow, k.Runtime, j.parameters, k.Computation, j.executor, j.attester,
			k.CreatedBy.Kind, k.CreatedBy.Name, k.CreatedBy.Via, k.CreatedBy.Producer,
			k.UpdatedBy.Kind, k.UpdatedBy.Name, k.UpdatedBy.Via, k.UpdatedBy.Producer,
			j.links, j.attrs, k.Body, k.CreatedAt, k.UpdatedAt, k.ContentChangedAt, doc, fm, hash,
		}
		// The path is the row's key (design doc 0046 §3.1) and the id is
		// the concept's address within it, so a concept write states both.
		// ON CONFLICT still names the id: reviving a tombstone is about the
		// entry at that address, and for a concept the two keys move
		// together.
		args = append(args, domain.ConceptPath(k.ID), bodyFiles(k))
		tag, err := tx.Exec(ctx, `INSERT INTO object (`+knowledgeCols+`, path, files)
			VALUES (`+knowledgeParams+`, $`+strconv.Itoa(len(args)-1)+`, $`+strconv.Itoa(len(args))+`)
			ON CONFLICT (id) DO UPDATE SET `+knowledgeExcluded+`, files=EXCLUDED.files, deleted_at=NULL
			WHERE object.deleted_at IS NOT NULL`+revive,
			args...)
		// A file can already hold the path (design doc 0046 §3.5: one
		// address space, two kinds of object). ON CONFLICT names the id
		// and a file has none, so the primary key on the path is what
		// stops it — and the caller has to read the same "something is
		// already there" it reads for a live entry, not a 500 with a
		// constraint name in it.
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: %s is a file", ErrAlreadyExists, domain.ConceptPath(k.ID))
		}
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			if keepCuratedTombstones {
				// Either a live entry or a curated tombstone blocked the
				// write; only the second is this guard's business, and the
				// caller needs to tell them apart to explain itself.
				if t, err := s.GetTombstone(ctx, k.ID); err == nil && t.Curated() {
					return ErrCuratedTombstone
				} else if err != nil && !errors.Is(err, ErrNotFound) {
					return err
				}
			}
			return ErrAlreadyExists
		}
		// A create is a new entry even when it lands on a tombstone's id,
		// so it starts with no rulings. Carrying the old ones over would
		// have a verification vouch for content nobody verified.
		for _, ledger := range []string{"knowledge_verification", "knowledge_rejection"} {
			if _, err := tx.Exec(ctx, `DELETE FROM `+ledger+` WHERE id = $1`, k.ID); err != nil {
				return err
			}
		}
		k.Verifications, k.Rejection = nil, nil
		return s.addRevision(ctx, tx, k, "create", k.CreatedBy)
	})
}

// Update writes k over the live entry with the same id. When ifMatch is
// non-nil, the write is conditional on the stored updated_at equalling it
// (optimistic concurrency, design doc 0030): a mismatch means the entry
// changed since the caller read it and returns ErrConflict, closing the
// read-modify-write race that silently lost updates. A nil ifMatch keeps
// the prior last-write-wins behavior for callers that do not opt in.
func (s *Store) Update(ctx context.Context, k *domain.Knowledge, actor domain.Actor, ifMatch *string) error {
	k.UpdatedAt = NowStored()
	// The caller decides whether this write changed what the entry says;
	// only then does generated.at move (design doc 0046 §3.4). A write
	// that reformats the document leaves it where it was.
	if k.ContentChangedAt.IsZero() {
		k.ContentChangedAt = k.UpdatedAt
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
		doc, hash, err := storedDoc(k)
		if err != nil {
			return err
		}
		fm, err := storedFrontmatter(doc)
		if err != nil {
			return err
		}
		// The row's bytes go back onto the entry, so what a write returns
		// is what a later read returns (design doc 0046 §2.2). The two
		// differ whenever storedDoc fell back to the canonical form — a
		// caller that changed fields rather than a document — and a
		// surface rendering the payload back would show bytes nothing
		// stored.
		k.Doc, k.ContentHash = doc, hash
		cond := ""
		args := []any{k.ID, k.Type, k.Title, k.Description, k.Resource, k.Tags, k.Status, k.StatusNote, staleAfter,
			j.sources, j.usageWindow, k.Runtime, j.parameters, k.Computation, j.executor, j.attester,
			k.UpdatedBy.Kind, k.UpdatedBy.Name, k.UpdatedBy.Via, k.UpdatedBy.Producer,
			j.links, j.attrs, k.Body, k.UpdatedAt, k.ContentChangedAt, doc, fm, hash, bodyFiles(k)}
		if ifMatch != nil {
			args = append(args, *ifMatch)
			cond = fmt.Sprintf(" AND content_hash=$%d", len(args))
		}
		tag, err := tx.Exec(ctx, `UPDATE object SET
			type=$2, title=$3, description=$4, resource=$5, tags=$6, status=$7, status_note=$8, stale_after=$9,
			sources=$10, usage_window=$11, runtime=$12, parameters=$13, computation=$14, executor=$15, attester=$16,
			updated_by_kind=$17, updated_by_name=$18, updated_by_via=$19, updated_by_producer=$20,
			links=$21, attrs=$22, body=$23, updated_at=$24, content_changed_at=$25,
			doc=$26, frontmatter=$27, content_hash=$28, files=$29
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
					`SELECT EXISTS (SELECT 1 FROM object WHERE id=$1 AND deleted_at IS NULL)`,
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

// Verify records that a reviewer vouched for the entry as it stands: one
// row appended to the verification ledger, every time, even when the
// entry was already verified.
//
// Update cannot express this. It writes nothing at all when the content
// is unchanged (SameContent), so "I looked at this again and it is still
// right" had nowhere to land, and both review feeds had no exit: the
// verification-age feed ranks by the newest verification, and the
// re-verification feed lists entries whose last failure report is newer
// than it (design doc 0025 §6).
//
// Verifying does not touch the entry's row. The document did not change,
// so its lifecycle status, its updated_at and therefore its ETag all stay
// where they were (design doc 0043 §3.2) — an editor holding a
// precondition does not lose it because somebody else confirmed the
// entry. Promoting a draft to stable is a separate act by a writer, on
// the document.
//
// A rejection is cleared: an entry cannot be both vouched for and turned
// down, and confirming one is the plainest possible statement that the
// ruling no longer holds.
//
// The timestamp comes from the database, not from NowStored: the feed
// compares it against the last failure report, which RecordOutcome stamps
// with the database's now(). Two clocks a millisecond apart are enough to
// hide a report that arrived after the verification, or to keep one that
// arrived before it — so both sides read the same clock, and RETURNING
// hands back exactly what was stored.
func (s *Store) Verify(ctx context.Context, id string, actor domain.Actor) (*domain.Knowledge, error) {
	var k *domain.Knowledge
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		var at time.Time
		// The EXISTS guard makes the insert and the liveness check one
		// statement, closing the race with a concurrent delete.
		err := tx.QueryRow(ctx, `INSERT INTO knowledge_verification (id, seq, by_kind, by_name, by_via, by_producer, at)
			SELECT $1, COALESCE((SELECT MAX(seq) FROM knowledge_verification WHERE id=$1), 0) + 1,
				$2, $3, $4, $5, now()
			WHERE EXISTS (SELECT 1 FROM object WHERE id=$1 AND deleted_at IS NULL)
			RETURNING at`,
			id, actor.Kind, actor.Name, actor.Via, actor.Producer).Scan(&at)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM knowledge_rejection WHERE id = $1`, id); err != nil {
			return err
		}
		if k, err = s.getOneTx(ctx, tx, id, false); err != nil {
			return err
		}
		return s.addRevision(ctx, tx, k, "verify", actor)
	})
	if err != nil {
		return nil, err
	}
	return k, nil
}

// Reject records this instance's ruling that an entry was not accepted,
// replacing any earlier ruling. Like Verify it is a ledger write and
// leaves the document alone: rejecting is a judgment about the entry, not
// an edit of it, so the status and the ETag do not move (design doc 0043
// §3.3).
//
// Verifications are not cleared. A rejection that follows a confirmation
// is exactly the history worth keeping — somebody vouched for this, and
// somebody later ruled against it — and erasing the first half would make
// the record agree with whoever wrote last.
func (s *Store) Reject(ctx context.Context, id string, actor domain.Actor, note string) (*domain.Knowledge, error) {
	var k *domain.Knowledge
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO knowledge_rejection (id, by_kind, by_name, by_via, by_producer, at, note)
			SELECT $1, $2, $3, $4, $5, now(), $6
			WHERE EXISTS (SELECT 1 FROM object WHERE id=$1 AND deleted_at IS NULL)
			ON CONFLICT (id) DO UPDATE SET
				by_kind=EXCLUDED.by_kind, by_name=EXCLUDED.by_name, by_via=EXCLUDED.by_via,
				by_producer=EXCLUDED.by_producer, at=EXCLUDED.at, note=EXCLUDED.note`,
			id, actor.Kind, actor.Name, actor.Via, actor.Producer, note)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if k, err = s.getOneTx(ctx, tx, id, false); err != nil {
			return err
		}
		return s.addRevision(ctx, tx, k, "reject", actor)
	})
	if err != nil {
		return nil, err
	}
	return k, nil
}

// WithdrawRejection withdraws a ruling. ErrNotFound when the entry is
// gone; ErrNoRejection when it is live but was never rejected — lifting
// nothing is a mistake worth reporting, not a no-op to swallow, and the
// two are told apart on the wire (design doc 0064): a missing concept is
// still a 404, a live one with nothing to withdraw is a 409, the same
// shape purge-on-a-live-concept already answers.
func (s *Store) WithdrawRejection(ctx context.Context, id string, actor domain.Actor) (*domain.Knowledge, error) {
	var k *domain.Knowledge
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM knowledge_rejection r
			WHERE r.id = $1
			  AND EXISTS (SELECT 1 FROM object WHERE id=$1 AND deleted_at IS NULL)`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			var live bool
			if err := tx.QueryRow(ctx,
				`SELECT EXISTS (SELECT 1 FROM object WHERE id=$1 AND deleted_at IS NULL)`,
				id).Scan(&live); err != nil {
				return err
			}
			if live {
				return ErrNoRejection
			}
			return ErrNotFound
		}
		if k, err = s.getOneTx(ctx, tx, id, false); err != nil {
			return err
		}
		return s.addRevision(ctx, tx, k, "withdraw", actor)
	})
	if err != nil {
		return nil, err
	}
	return k, nil
}

// SoftDelete hides an entry from reads while keeping full history.
// Create on the same id revives it. ifMatch is the same optional
// optimistic-concurrency precondition Update takes (design docs 0030,
// 0064): non-nil, the delete lands only if the stored content_hash still
// equals it, closing the same read-then-act race a stale write would —
// deleting a version the caller never saw because a delete request
// crossed with somebody else's edit.
func (s *Store) SoftDelete(ctx context.Context, id string, actor domain.Actor, ifMatch *string) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		k, err := s.Get(ctx, id)
		if err != nil {
			return err
		}
		cond, args := "", []any{id}
		if ifMatch != nil {
			args = append(args, *ifMatch)
			cond = fmt.Sprintf(" AND content_hash=$%d", len(args))
		}
		// deleted_at IS NULL guards the race with a concurrent delete: the
		// Get above ran outside this transaction, and a double delete must
		// not record a second "delete" revision.
		tag, err := tx.Exec(ctx,
			`UPDATE object SET deleted_at = now(), updated_at = now()
			 WHERE id=$1 AND deleted_at IS NULL`+cond, args...)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			// No row matched. With a precondition, tell a live-but-changed
			// entry (ErrConflict) apart from a missing one (ErrNotFound) —
			// the same distinction Update makes.
			if ifMatch != nil {
				var live bool
				if err := tx.QueryRow(ctx,
					`SELECT EXISTS (SELECT 1 FROM object WHERE id=$1 AND deleted_at IS NULL)`,
					id).Scan(&live); err != nil {
					return err
				}
				if live {
					return ErrConflict
				}
			}
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
			`SELECT deleted_at IS NOT NULL FROM object WHERE id=$1 FOR UPDATE`, id).Scan(&deleted)
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
			(id, type, title, revisions, purged_by_kind, purged_by_name, purged_by_via, purged_by_producer)
			SELECT k.id, k.type, k.title,
			       (SELECT count(*) FROM knowledge_revision r WHERE r.id = k.id),
			       $2, $3, $4, $5
			  FROM object k WHERE k.id = $1`,
			id, actor.Kind, actor.Name, actor.Via, actor.Producer); err != nil {
			return err
		}
		for _, q := range []string{
			// starts_with, not LIKE: an id may contain "_", which LIKE
			// reads as a wildcard, and this statement deletes. Purging
			// "sales_2024" would have taken every file under
			// "salesX2024/" with it.
			`DELETE FROM object f WHERE f.id IS NULL AND starts_with(f.path, $1 || '/')`,
			// The files' own history goes with the files: purge destroys
			// the record (design doc 0031), and a ledger row pointing at
			// a path nothing holds is not a record of anything.
			`DELETE FROM knowledge_revision WHERE id IS NULL AND starts_with(path, $1 || '/')`,
			`DELETE FROM attachment WHERE knowledge_id=$1`,
			`DELETE FROM knowledge_usage WHERE knowledge_id=$1`,
			`DELETE FROM knowledge_event WHERE knowledge_id=$1`,
			`DELETE FROM knowledge_revision WHERE id=$1`,
			`DELETE FROM knowledge_verification WHERE id=$1`,
			`DELETE FROM knowledge_rejection WHERE id=$1`,
		} {
			if _, err := tx.Exec(ctx, q, id); err != nil {
				return err
			}
		}
		// The tombstone condition is repeated here so the destructive
		// statement carries the precondition itself, not just the lock
		// taken above: a purge that would hit a live row aborts the
		// transaction instead of erasing it.
		tag, err := tx.Exec(ctx, `DELETE FROM object WHERE id=$1 AND deleted_at IS NOT NULL`, id)
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
