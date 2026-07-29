// Package service implements ochakai's behavior shared by the MCP server
// and the REST API: knowledge CRUD with the verification policy, hybrid
// search, and the usage/outcome write-back loop.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/na0fu3y/ochakai/internal/config"
	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/embed"
	"github.com/na0fu3y/ochakai/internal/httpauth"
	"github.com/na0fu3y/ochakai/internal/okf"
	"github.com/na0fu3y/ochakai/internal/store"
)

type Service struct {
	Store    *store.Store
	Embedder embed.Embedder // nil when semantic search is disabled
	Config   *config.Config
	Log      *slog.Logger
}

// ErrReadOnly is returned by every operation that would change knowledge
// when the deployment runs with OCHAKAI_READ_ONLY (design doc 0040).
//
// The check lives here rather than in the transports because REST and MCP
// both come through this package: a write endpoint added later is covered
// without its author having to know a middleware exists. It is not
// authorization — it never looks at the actor, and it refuses the
// operator exactly as it refuses anyone else (0040 §2.1).
var ErrReadOnly = errors.New("this ochakai is read-only: it serves knowledge but does not change it")

// readOnly reports whether writes are refused. Usage telemetry does not
// go through it: recording a search hit is the server's own observation,
// not content a caller wrote, and freezing it would empty the usage feeds
// on exactly the deployments most likely to be demonstrating them
// (0040 §2.2).
func (s *Service) readOnly() error {
	if s.Config != nil && s.Config.ReadOnly {
		return ErrReadOnly
	}
	return nil
}

// InvalidInputError marks a failure caused by the client's input, so
// transport layers can classify it (REST: 400) without string matching.
type InvalidInputError struct{ msg string }

func (e *InvalidInputError) Error() string { return e.msg }

// Invalidf builds an InvalidInputError from a format string.
func Invalidf(format string, args ...any) error {
	return &InvalidInputError{msg: fmt.Sprintf(format, args...)}
}

// UnsupportedError marks an operation this deployment cannot perform —
// not a client mistake and not a crash, but a missing capability the
// operator would have to configure (REST: 501).
type UnsupportedError struct{ msg string }

func (e *UnsupportedError) Error() string { return e.msg }

// Unsupportedf builds an UnsupportedError from a format string.
func Unsupportedf(format string, args ...any) error {
	return &UnsupportedError{msg: fmt.Sprintf(format, args...)}
}

// --- knowledge CRUD ---

func (s *Service) Get(ctx context.Context, id string) (*domain.Knowledge, error) {
	id = domain.Normalize(id)
	k, err := s.Store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	// Metadata only — the bytes are a separate, deliberate fetch
	// (design doc 0008): images are heavy in agent context.
	if k.Attachments, err = s.Store.ListAttachments(ctx, id); err != nil {
		return nil, err
	}
	s.recordUsage(ctx, domain.EventFetched, []string{id})
	return k, nil
}

func (s *Service) Create(ctx context.Context, k *domain.Knowledge, actor domain.Actor) (*domain.Knowledge, error) {
	if err := s.readOnly(); err != nil {
		return nil, err
	}
	return s.create(ctx, k, actor, false)
}

// CreateKeepingCurated is Create for surfaces that must not replace a
// human ruling (MCP, design doc 0015 §3.1). Creating on a soft-deleted id
// revives that row in place, status and status_note included, which is
// the one way past the update and delete guards — they read a live row
// and a tombstone is not one. The check runs first so the refusal can
// explain itself, and the store repeats it inside the create transaction
// so a curation landing in between loses the race rather than being
// erased by it.
func (s *Service) CreateKeepingCurated(ctx context.Context, k *domain.Knowledge, actor domain.Actor) (*domain.Knowledge, error) {
	if err := s.readOnly(); err != nil {
		return nil, err
	}
	if err := s.RefuseIfRevivingCurated(ctx, k.ID); err != nil {
		return nil, err
	}
	created, err := s.create(ctx, k, actor, true)
	if errors.Is(err, store.ErrCuratedTombstone) {
		// The window closed on us: re-read for the same advice the
		// pre-check would have given.
		if err := s.RefuseIfRevivingCurated(ctx, k.ID); err != nil {
			return nil, err
		}
	}
	if errors.Is(err, store.ErrAlreadyExists) {
		return nil, s.explainOccupiedID(ctx, k.ID, err)
	}
	return created, err
}

// explainOccupiedID says what holds an id and what to do instead. Every
// other refusal this surface can meet names the next move — the update,
// delete and revival guards all end in report_outcome failed or a draft
// at another id — and this one did not: creating over a live entry
// returned the store's bare "knowledge already exists", the one wall on
// the surface with no door drawn on it.
//
// A live rejection is the case that matters. Design doc 0015 §3.1 turns
// on agents reading a rejection before re-proposing what it turned down,
// and the caller that lands here is precisely the one that skipped that
// read: it needs to be told the id holds a ruling, not that something
// unnamed is in the way. The error stays wrapped, so a caller matching
// on ErrAlreadyExists still matches.
func (s *Service) explainOccupiedID(ctx context.Context, id string, err error) error {
	k, getErr := s.Store.Get(ctx, domain.Normalize(id))
	if getErr != nil {
		return err
	}
	ruling := k.Ruling()
	var instead string
	switch ruling {
	case domain.RulingRejected:
		instead = "The rejection is the record of a decision and carries the reason; read it " +
			"(get_concept) before proposing this again. If you disagree, put_concept a new " +
			"entry at a different id and let a human judge it."
	case domain.RulingVerified:
		instead = "If it is wrong, say so with report_outcome failed — that puts it in the " +
			"re-verification feed. If you have something better, put_concept it at a " +
			"different id and let a human judge it."
	case domain.RulingDeprecated:
		instead = "Deprecated means it was correct and is no longer recommended. If it is worth " +
			"reviving, put_concept a draft at a different id that says why."
	default:
		return fmt.Errorf("%w: %s is a live %s entry. Nobody has ruled on it: "+
			"put_concept replaces it in place", err, id, k.Status)
	}
	return fmt.Errorf("%w: %s is a live %s entry. %s", err, id, ruling, instead)
}

func (s *Service) create(ctx context.Context, k *domain.Knowledge, actor domain.Actor, keepCuratedTombstones bool) (*domain.Knowledge, error) {
	normalizeKeys(k)
	if err := validate(k); err != nil {
		return nil, err
	}
	deriveLinks(k)
	// A document that names no status is not a draft — it is silent, and
	// OKF says a silent document is stable (SPEC §5.4). The index column
	// holds what a reader would read, and the document keeps its silence:
	// ochakai does not write a key its writer left out (design doc 0046
	// §3.9). Whether anybody has confirmed the entry is a separate
	// question, which the trust tier answers (§3.10).
	k.Status = k.Lifecycle()
	k.CreatedBy, k.UpdatedBy = actor, actor
	if err := s.Store.Create(ctx, k, keepCuratedTombstones); err != nil {
		return nil, err
	}
	s.updateEmbedding(ctx, k)
	return k, nil
}

// Update replaces an entry's content. changed=false means the payload
// matched the stored content exactly, so nothing was written: revisions
// are the audit trail of changes and updated_at means "content last
// changed" — recurring bundle imports and agents re-saving what they
// just read must not bury real history under identical snapshots.
//
// ifMatch is an optional optimistic-concurrency precondition (design doc
// 0030): when non-nil it is the content hash the caller based the edit
// on (design doc 0043 §3.4), and an entry that has changed since —
// whether before this call's read, or in the read-modify-write window
// below — yields store.ErrConflict rather than silently clobbering the
// other write. nil keeps the prior last-write-wins behavior for callers
// that do not opt in.
func (s *Service) Update(ctx context.Context, k *domain.Knowledge, actor domain.Actor, ifMatch *string) (updated *domain.Knowledge, changed bool, err error) {
	if err := s.readOnly(); err != nil {
		return nil, false, err
	}
	normalizeKeys(k)
	if err := validate(k); err != nil {
		return nil, false, err
	}
	deriveLinks(k)
	old, err := s.Store.Get(ctx, k.ID)
	if err != nil {
		return nil, false, err
	}
	// The caller's version is already stale if the stored entry moved on
	// between their read and ours — reject before doing any work.
	if ifMatch != nil && old.ContentHash != *ifMatch {
		return nil, false, store.ErrConflict
	}
	// A document's own trust family is a claim and is kept as one (design
	// doc 0046 §2.2) — but the export form of this very entry is not a
	// claim. It is what this instance observed, handed back by get → edit
	// → PUT or by export → review → import, and only here is there an
	// entry to compare it against. Recognizing it is what keeps a round
	// trip storing the bytes it stored before.
	if okf.IsObservation(k.Attrs[okf.ClaimKey], old) {
		okf.DropClaim(k)
	}
	// A PUT is a full replacement, so an omitted status is not "keep the
	// one you had": it is the document saying nothing, which reads as
	// OKF's default (design doc 0046 §3.9).
	k.Status = k.Lifecycle()
	k.CreatedBy = old.CreatedBy
	k.CreatedAt = old.CreatedAt
	// The ledgers belong to the instance, not to the payload: an update
	// replaces the document and rules on nothing (design doc 0043
	// §§3.2-3.3). Carrying them over keeps the returned entry honest.
	k.Verifications, k.Rejection = old.Verifications, old.Rejection
	// Three outcomes, because the document being the writer's own bytes
	// (design doc 0046 §2.2) separates two questions that used to be one.
	//
	// The same bytes: nothing happened. No row written, no revision, and
	// the surface says so.
	doc, _, err := store.StoredDocument(k)
	if err != nil {
		return nil, false, err
	}
	if doc == old.Doc {
		return old, false, nil
	}
	// Different bytes saying the same thing — a reformat, a comment, a
	// reordered frontmatter. The file changed, so it is written and its
	// version moves; what the entry says did not, so generated stays with
	// whoever the content already stood by (design doc 0046 §3.4).
	if k.SameContent(old) {
		k.UpdatedBy, k.ContentChangedAt = old.UpdatedBy, old.ContentChangedAt
	} else {
		// Past this point the content really changes, so the caller is who
		// the entry now stands by — OKF's generated (design doc 0036 §3.3).
		k.UpdatedBy, k.ContentChangedAt = actor, store.NowStored()
	}
	// Pass the precondition through so the write is also guarded against a
	// concurrent update landing between the Get above and the write.
	if err := s.Store.Update(ctx, k, actor, ifMatch); err != nil {
		return nil, false, err
	}
	s.updateEmbedding(ctx, k)
	return k, true, nil
}

// Put writes k over whatever holds its id, creating the entry when the id
// is free: one operation, because a document PUT is a statement about
// what the entry should say and not about whether it already existed
// (design doc 0043 §3.5).
//
// ifMatch makes it conditional, which also makes it an update: a
// precondition names a version, and a version the caller read cannot
// belong to an entry that does not exist. A missing entry is then a 404
// rather than a silent create.
//
// created reports which way it went, so a surface can answer 201 or 200;
// changed is Update's no-op signal, always true for a create.
func (s *Service) Put(ctx context.Context, k *domain.Knowledge, actor domain.Actor, ifMatch *string) (out *domain.Knowledge, created, changed bool, err error) {
	updated, changed, err := s.Update(ctx, k, actor, ifMatch)
	if err == nil {
		return updated, false, changed, nil
	}
	if ifMatch != nil || !errors.Is(err, store.ErrNotFound) {
		return nil, false, false, err
	}
	// The id was free at the read above. If another writer took it in the
	// window, this is an update after all — retry once rather than
	// handing back an ErrAlreadyExists the caller cannot act on, since
	// what it asked for ("make the entry say this") is still achievable.
	made, err := s.Create(ctx, k, actor)
	if err == nil {
		return made, true, true, nil
	}
	if !errors.Is(err, store.ErrAlreadyExists) {
		return nil, false, false, err
	}
	updated, changed, err = s.Update(ctx, k, actor, nil)
	if err != nil {
		return nil, false, false, err
	}
	return updated, false, changed, nil
}

// RefuseIfCurated reports an error when id names an entry a human has
// already ruled on — verified, rejected, or deprecated — for surfaces that
// must not overwrite that ruling in place. It returns the entry's version
// so the caller can pass it as an If-Match precondition and close the
// window between this check and the write.
//
// The rule is about preconditions, not authority. Design doc 0002 settled
// that ochakai has no authorization and that verification is judged from
// provenance, and this does not reopen it: an agent may still create, may
// still edit drafts, may still promote a draft to verified. What it may
// not do from a surface with no If-Match channel (MCP, design doc 0030
// §3.4) is silently replace a state a human put there.
//
// Rejected matters at least as much as verified, and it is the one the
// obvious rule would have missed. Create refuses a live entry, so an agent
// blocked by a rejection has a two-call path around it — delete, then
// create, which revives the tombstone as a fresh draft and takes the
// status_note with it. That erases the memory of no that the write-back
// loop is built on (design doc 0025) and that the shipped instructions
// tell agents to consult before re-proposing. Nobody notices: unlike a
// deleted verified entry, a deleted rejection leaves nothing anyone was
// using.
func (s *Service) RefuseIfCurated(ctx context.Context, id, op string) (*string, error) {
	k, err := s.Store.Get(ctx, domain.Normalize(id))
	if err != nil {
		return nil, err
	}
	ruling := k.Ruling()
	var instead string
	switch ruling {
	case domain.RulingVerified:
		instead = "If it is wrong, say so with report_outcome failed — that puts it in the " +
			"re-verification feed. If you have something better, put_concept a new draft."
	case domain.RulingRejected:
		instead = "The rejection is the record of a decision and carries the reason; read it " +
			"before proposing this again. If you disagree, put_concept a new entry at a " +
			"different id and let a human judge it."
	case domain.RulingDeprecated:
		instead = "Deprecated means it was correct and is no longer recommended. If it is worth " +
			"reviving, put_concept a draft that says why."
	default:
		return &k.ContentHash, nil
	}
	return nil, Invalidf("cannot %s %s from this surface: it is %s, and this surface has no "+
		"If-Match precondition to replace curated knowledge safely. %s A human changes curated "+
		"entries from the web UI or CLI.", op, id, ruling, instead)
}

// RefuseIfRevivingCurated reports an error when id names a soft-deleted
// entry that was ruled on — verified, rejected or deprecated — before it
// was deleted. For surfaces that must not overwrite a ruling in place,
// create is the second way to do it: Create revives a tombstone (ON
// CONFLICT ... WHERE deleted_at IS NOT NULL) with the incoming status and
// status_note, so the reason an entry was turned down is replaced by a
// fresh draft, and the entry that comes back looks like it was never
// judged.
//
// RefuseIfCurated cannot see this: the row it reads is not live. Closing
// the delete step alone (design doc 0015 §3.1) leaves every tombstone that
// already exists — deleted before that rule, deleted from the REST/CLI
// surfaces where deleting a rejection is legitimate housekeeping — still
// revivable in one call. A draft tombstone stays revivable: nobody ruled
// on it, and reviving an abandoned draft is how a create on a deleted id
// is supposed to work.
func (s *Service) RefuseIfRevivingCurated(ctx context.Context, id string) error {
	k, err := s.Store.GetTombstone(ctx, domain.Normalize(id))
	if errors.Is(err, store.ErrNotFound) {
		// A free id, or a live entry — Create's own ErrAlreadyExists
		// covers the second case.
		return nil
	}
	if err != nil {
		return err
	}
	ruling := k.Ruling()
	var instead string
	switch ruling {
	case domain.RulingRejected:
		instead = "The rejection is the record of a decision and carries the reason; read it " +
			"(search_concepts with rejected=true) before proposing this again. If you disagree, " +
			"put_concept a new entry at a different id and let a human judge it."
	case domain.RulingVerified:
		instead = "It was verified knowledge when it was deleted. Propose the replacement at a " +
			"different id and let a human judge it against the history this id still holds."
	case domain.RulingDeprecated:
		instead = "Deprecated means it was correct and is no longer recommended. If it is worth " +
			"reviving, put_concept a draft at a different id that says why."
	default:
		return nil
	}
	return Invalidf("cannot create %s from this surface: the id holds a deleted %s entry, and "+
		"creating here would revive it as a fresh draft, replacing that ruling. %s A human reuses "+
		"the id from the web UI or CLI.", id, ruling, instead)
}

func (s *Service) Delete(ctx context.Context, id string, actor domain.Actor) error {
	if err := s.readOnly(); err != nil {
		return err
	}
	return s.Store.SoftDelete(ctx, domain.Normalize(id), actor)
}

// Verify appends a verification against the entry as it stands, whether
// this is the first confirmation or the tenth re-check.
//
// Re-checking is the case Update could not express, and without it
// neither review feed had an exit (design doc 0025 §6): a reviewer who
// re-read a verified entry and found it still correct had no way to say
// so, so it stayed at the top of the verification-age feed, and an entry
// that was reported wrong and then fixed stayed in the re-verification
// feed for good. A queue nobody can empty stops being read.
//
// It does not touch the document, so the entry's status and its ETag stay
// put (design doc 0043 §3.2). Promoting a draft to stable is a separate
// edit by a writer; confirming and publishing are different acts, and a
// draft somebody checked is a state OKF can express (SPEC §§5.3-5.4).
//
// There is no promotion restriction (design doc 0002): anyone who can
// reach ochakai may verify, and the ledger records who did — trust is
// judged from provenance.
//
// Not an MCP tool. Verification is the human ruling the write-back loop
// turns on; an agent that finds an entry wrong reports the outcome or
// drafts a replacement (design docs 0015 §3.1, 0025).
func (s *Service) Verify(ctx context.Context, id string, actor domain.Actor) (*domain.Knowledge, error) {
	if err := s.readOnly(); err != nil {
		return nil, err
	}
	id = domain.Normalize(id)
	k, err := s.Store.Verify(ctx, id, actor)
	if err != nil {
		return nil, err
	}
	s.Log.Info("knowledge verified", "id", id, "actor", actor.String())
	return k, nil
}

// Reject records that a reviewer turned the entry down, with the reason.
// Like Verify it is a ruling and not an edit: the document keeps its
// lifecycle status and its ETag, and the entry drops out of searches that
// did not ask for rejected ones (design doc 0043 §3.3).
//
// This is the memory of no that the write-back loop is built on (design
// doc 0001 §9.1) — it is what stops an agent re-proposing knowledge a
// human already declined. Not an MCP tool, for the same reason Verify is
// not: it is the human's side of the loop.
func (s *Service) Reject(ctx context.Context, id, note string, actor domain.Actor) (*domain.Knowledge, error) {
	if err := s.readOnly(); err != nil {
		return nil, err
	}
	if len(note) > maxRejectionNote {
		return nil, Invalidf("rejection note exceeds %d bytes", maxRejectionNote)
	}
	id = domain.Normalize(id)
	k, err := s.Store.Reject(ctx, id, actor, note)
	if err != nil {
		return nil, err
	}
	s.Log.Info("knowledge rejected", "id", id, "actor", actor.String())
	return k, nil
}

// LiftRejection withdraws a ruling, returning the entry to the ordinary
// pool. ErrNotFound when the entry carries no rejection: lifting nothing
// is a mistake worth reporting rather than a silent success.
func (s *Service) LiftRejection(ctx context.Context, id string, actor domain.Actor) (*domain.Knowledge, error) {
	if err := s.readOnly(); err != nil {
		return nil, err
	}
	id = domain.Normalize(id)
	k, err := s.Store.LiftRejection(ctx, id, actor)
	if err != nil {
		return nil, err
	}
	s.Log.Info("knowledge rejection lifted", "id", id, "actor", actor.String())
	return k, nil
}

// Purge hard-deletes an already soft-deleted entry, freeing its id for a
// move (design doc 0021: Move cannot revive a tombstone the way Create
// can). Not an MCP tool: this is the one operation that destroys history,
// so it belongs to the human surfaces (design doc 0015).
//
// Which surface exposes it is not the same as who can reach it — ochakai
// has no authorization, so any caller that reaches the REST API can purge.
// The actor is therefore recorded twice over: in knowledge_purge, the one
// table a purge does not touch, and in the log. An operation that leaves
// no trace has no place in a product whose value is provenance.
func (s *Service) Purge(ctx context.Context, id string, actor domain.Actor) error {
	if err := s.readOnly(); err != nil {
		return err
	}
	id = domain.Normalize(id)
	if err := s.Store.Purge(ctx, id, actor); err != nil {
		return err
	}
	s.Log.Info("knowledge purged", "id", id, "actor", actor.String())
	return nil
}

// Move renames an entry to newID, carrying every id-keyed record along
// (revisions, usage, attachments, embeddings) and rewriting inbound
// references so nothing breaks (design doc 0021). Moving to the current
// id is a no-op read.
func (s *Service) Move(ctx context.Context, id, newID string, actor domain.Actor) (*domain.Knowledge, error) {
	if err := s.readOnly(); err != nil {
		return nil, err
	}
	id, newID = domain.Normalize(id), domain.Normalize(newID)
	if !domain.ValidID(newID) {
		return nil, Invalidf(`invalid destination id %q (path segments separated by "/", e.g. sales/orders; segments must not start with "." and the last must not be "index" or "log")`, newID)
	}
	if newID == id {
		return s.Store.Get(ctx, id)
	}
	return s.Store.Move(ctx, id, newID, actor)
}

// normalizeKeys rewrites a write payload's byte-compared keys — the id
// and the type — into NFC (design doc 0022); content fields are kept as
// written. The type joined them in 0023: it is free text now, so a
// foreign bundle can carry a non-ASCII type, and search filters compare
// against it. Filters fold case themselves (domain.FoldType), but not
// width or accent composition — so the type is indexed NFC and trimmed,
// leaving lower() enough to fold the column.
//
// The casing is not touched. A recommended type used to settle on its
// canonical spelling here, which meant the stored entry no longer said
// what its writer wrote — and once the document is what is stored
// (design doc 0046 §2.2), it would also mean the index disagreeing with
// the document it is derived from. The type is the writer's vocabulary
// (design doc 0038); matching folds case, storage keeps it.
//
// Links are not normalized here because they are not read from the
// payload at all — deriveLinks recomputes them from the body, normalizing
// each target on the way (design doc 0024).
func normalizeKeys(k *domain.Knowledge) {
	k.ID = domain.Normalize(k.ID)
	k.Type = domain.Type(strings.TrimSpace(domain.Normalize(string(k.Type))))
}

// deriveLinks replaces whatever links the payload carried with the ones
// the body actually contains. The body is the sole source of edges
// (design doc 0024): a writer links by writing a markdown link, and the
// links column is a derived index that backlinks and get_context read.
// Called on every write path, so no route can store links the body does
// not back.
func deriveLinks(k *domain.Knowledge) {
	k.Links = domain.LinksFromBody(k.ID, k.Body)
}

func validate(k *domain.Knowledge) error {
	if !domain.ValidType(k.Type) {
		return Invalidf(`invalid type %q (one line, no "/", up to 128 bytes; recommended: %s)`, k.Type, domain.TypesHint())
	}
	if !domain.ValidID(k.ID) {
		return Invalidf(`invalid id %q (path segments separated by "/", e.g. sales/orders; segments must not start with "." and the last must not be "index" or "log")`, k.ID)
	}
	if k.Status != "" && !domain.ValidStatus(k.Status) {
		return Invalidf("invalid status %q (valid: %s)", k.Status, domain.StatusesHint())
	}
	if !domain.ValidStaleAfter(k.StaleAfter) {
		return Invalidf("invalid stale_after %q (an absolute date, YYYY-MM-DD, e.g. 2026-12-31)", k.StaleAfter)
	}
	return validateOKF(k)
}

// validateOKF holds the write path to the shape OKF v0.2 defines for its
// provenance (SPEC §5.1) and Attested Computation (SPEC §10.2) families.
//
// Only the write path. Bundle import is deliberately more forgiving: SPEC's
// conformance rule tells consumers to accept a document without rejecting
// it, so the parser drops the offending value and reports a note instead
// (design doc 0036 §3.4). Here the caller is naming a value it means, and
// silently dropping it would be the worse answer.
func validateOKF(k *domain.Knowledge) error {
	// An attr that shadows an envelope key used to survive in attrs and
	// then vanish from the export, where reservedKeys drops it. Saying so
	// beats losing it quietly (design doc 0036 §3.5).
	for _, key := range domain.EnvelopeKeys {
		if _, ok := k.Attrs[key]; ok {
			return Invalidf("attrs must not carry %q: it is an OKF envelope field, so write it at the top level of the payload instead", key)
		}
	}
	for i, src := range k.Sources {
		if src.Resource == "" {
			return Invalidf("sources[%d] needs a resource (the artifact it cites); id and title alone do not name one", i)
		}
		if !domain.ValidDate(src.LastModified) {
			return Invalidf("sources[%d]: invalid last_modified %q (an absolute date, YYYY-MM-DD)", i, src.LastModified)
		}
		if !src.UsageWindow.Valid() {
			return Invalidf("sources[%d]: usage_window needs absolute dates (YYYY-MM-DD)", i)
		}
		if src.UsageCount != nil && *src.UsageCount < 0 {
			return Invalidf("sources[%d]: usage_count must not be negative", i)
		}
	}
	if !k.UsageWindow.Valid() {
		return Invalidf("usage_window needs absolute dates (YYYY-MM-DD), e.g. {from: 2026-06-01, to: 2026-06-30}")
	}
	for i, p := range k.Parameters {
		if !p.Valid() {
			return Invalidf("parameters[%d] needs both a name and a type (SPEC §10.2)", i)
		}
	}
	if !k.Executor.Valid() {
		return Invalidf("executor needs both a resource and a non-empty receipt (SPEC §10.2); ochakai records the contract and never runs it")
	}
	if !k.Attester.Valid() {
		return Invalidf("attester needs a resource (SPEC §10.2)")
	}
	// The one place ochakai holds a type to a schema. SPEC §10.2 makes
	// runtime required, and it is what gives the parameters their meaning —
	// a contract missing it cannot be executed by any consumer. The
	// exception stops here; no other type gains a requirement (design docs
	// 0036 §5, §3.10).
	if domain.EqualType(k.Type, domain.TypeComputations) && k.Runtime == "" {
		return Invalidf("an %s needs a runtime (e.g. bigquery, postgres, dbt, python): it is what tells a consumer how to read the parameters", domain.TypeComputations)
	}
	return nil
}

// --- search ---

// Search runs lexical search, and when an embedder is configured, fuses
// it with vector search via reciprocal rank fusion.
func (s *Service) Search(ctx context.Context, query string, f store.Filter, limit int) ([]domain.SearchHit, error) {
	// Stored text is NFC (design doc 0022); an NFD query (pasted from a
	// macOS path) must still match it byte-wise.
	query = domain.Normalize(query)
	// An empty query has nothing to rank by: SearchLexical splits the
	// query into fragments and gets none, so guard here — once, for
	// every surface — and point the caller at the listing modes.
	if strings.TrimSpace(query) == "" {
		// The modes come from domain.ListSorts rather than a sentence:
		// this message named three of them for as long as there were
		// three, and stale_after (design doc 0037) never reached it.
		return nil, Invalidf("search needs a query; use sort=%s to list entries without one, "+
			"or source=URI to list what cites a resource", strings.Join(domain.ListSorts, "|"))
	}
	f, err := checkedFilter(f)
	if err != nil {
		return nil, err
	}
	hits, err := s.search(ctx, query, f, limit)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(hits))
	for i, h := range hits {
		ids[i] = h.ID
	}
	s.recordUsage(ctx, domain.EventSearchHit, ids)
	// A search that found nothing is recorded as itself: it is the one
	// observation that says what this knowledge base is missing, and
	// until design doc 0051 it was the only one being discarded. Here
	// rather than in each surface, so every way of asking is counted —
	// get_context searches through this function too.
	if len(hits) == 0 {
		s.recordMiss(ctx, query)
	}
	return hits, nil
}

func (s *Service) search(ctx context.Context, query string, f store.Filter, limit int) ([]domain.SearchHit, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	lexical, err := s.Store.SearchLexical(ctx, query, f, limit*2)
	if err != nil {
		return nil, err
	}
	if s.Embedder == nil {
		if len(lexical) > limit {
			lexical = lexical[:limit]
		}
		return lexical, nil
	}

	vecs, err := s.Embedder.Embed(ctx, embed.TaskQuery, []string{query})
	if err != nil {
		// Degrade to lexical-only rather than failing the search.
		s.Log.Warn("query embedding failed; falling back to lexical-only", "error", err)
		if len(lexical) > limit {
			lexical = lexical[:limit]
		}
		return lexical, nil
	}
	vector, err := s.Store.SearchVector(ctx, vecs[0], s.Embedder.Model(), f, limit*2)
	if err != nil {
		return nil, err
	}
	// Entries whose attachments match are the third list (design doc
	// 0020): an entry matching in both body and attachment gains rank
	// from both, so evidence-backed entries surface first.
	attachments, err := s.Store.SearchVectorAttachments(ctx, vecs[0], s.Embedder.Model(), f, limit*2)
	if err != nil {
		return nil, err
	}
	fused := rrfFuse(limit, lexical, vector, attachments)
	return fused, nil
}

// ContextResult is the one-call context pack behind get_context and
// `ochakai context`: the ranking, plus the full entries behind the top
// hits expanded one hop through links. Entries that did not fit the
// caller's budget are listed in Outline instead — named, not delivered.
//
// Hits is the ranking only (design doc 0033). The knowledge travels once,
// in Entries; a hit that was not expanded into an entry is a pointer the
// caller can spend a round trip on.
type ContextResult struct {
	Hits      []domain.ContextRank    `json:"hits"`
	Entries   []domain.View           `json:"entries"`
	Outline   []domain.ContextOutline `json:"outline,omitempty"`
	Truncated int                     `json:"truncated,omitempty"`
}

// ContextRequest is the input to Context. Budget caps the serialized size
// of the response — the entries returned in full and the outline rows
// naming the rest, which carry a description and are not free; 0 means no
// cap.
type ContextRequest struct {
	Query    string
	Filter   store.Filter
	Limit    int
	MinScore float64
	Budget   int
}

// Context gathers what an agent should read before answering a data
// question, in one call: search (verified entries rank higher), fetch
// the entries behind the top hits in full, and follow their links one
// hop so companion knowledge — the insight that says how to read a
// metric, the golden query that answers the question — arrives without
// further round trips.
//
// req.MinScore drops hits scoring below it before expansion, for callers
// that inject the pack automatically (hooks) and prefer nothing over
// junk. It defaults to 0 (off) because scores are search-mode dependent
// and uncalibrated: matched-fragment weight plus boosts in lexical mode, RRF
// rank fusion (~0.02 scale) in hybrid mode — a floor meaningful in one
// mode is nonsense in the other.
//
// req.Budget caps how many bytes of full entries come back; the rest
// become outline rows (packWithinBudget). Callers whose context window is
// the scarce resource — an agent, not a UI — should always set it.
func (s *Service) Context(ctx context.Context, req ContextRequest) (*ContextResult, error) {
	if strings.TrimSpace(req.Query) == "" {
		return nil, Invalidf("invalid context request: query is required")
	}
	limit := req.Limit
	if limit <= 0 || limit > 20 {
		limit = 5
	}
	hits, err := s.Search(ctx, req.Query, req.Filter, 2*limit)
	if err != nil {
		return nil, err
	}
	if req.MinScore > 0 {
		kept := hits[:0]
		for _, h := range hits {
			if h.Score >= req.MinScore {
				kept = append(kept, h)
			}
		}
		hits = kept
	}
	seen := map[string]bool{}
	var entries []domain.Knowledge
	addFetched := func(k *domain.Knowledge) {
		if len(entries) >= 2*limit || seen[k.ID] || k.Rejection != nil {
			return // rejected companions stay out of the pack
		}
		seen[k.ID] = true
		entries = append(entries, *k)
	}
	add := func(id string) {
		if len(entries) >= 2*limit || seen[id] {
			return
		}
		k, err := s.Store.Get(ctx, id)
		if err != nil {
			return // deleted targets stay out of the pack
		}
		addFetched(k)
	}
	for _, h := range hits {
		if len(entries) >= limit {
			break
		}
		add(h.ID)
	}
	// One hop through the primary entries' links, both directions: the
	// query a metric links to, and the insight that links to the metric
	// (rel: explains points at the metric, not the other way round).
	// Companions share the 2*limit cap and are never expanded themselves.
	primaries := len(entries)
	for i := range primaries {
		for _, l := range entries[i].Links {
			add(l.Target)
		}
		linking, err := s.Store.ListLinkingToDocs(ctx, entries[i].ID, 2*limit)
		if err != nil {
			s.Log.Warn("backlink lookup failed", "id", entries[i].ID, "error", err)
			continue
		}
		for j := range linking {
			addFetched(&linking[j])
		}
	}
	// Entries become views here: a context pack hands over the document
	// (design doc 0043 §3.5), and the budget below has to measure what is
	// actually sent.
	views := make([]domain.View, 0, len(entries))
	for i := range entries {
		v, err := okf.ViewOf(&entries[i])
		if err != nil {
			// The entry came from the store, so this means the renderer
			// cannot express something it holds. Naming it and leaving it
			// out beats failing the whole pack.
			s.Log.Warn("cannot render an entry for a context pack", "id", entries[i].ID, "error", err)
			continue
		}
		views = append(views, v)
	}
	kept, outline := packWithinBudget(views, req.Budget)
	// Only delivered entries count as fetched. An outline row names an
	// entry; it does not hand over the knowledge, so counting it as a use
	// would inflate the demand signal that drives the review feeds.
	ids := make([]string, len(kept))
	for i := range kept {
		ids[i] = kept[i].ID
	}
	s.recordUsage(ctx, domain.EventFetched, ids)
	return &ContextResult{
		Hits: domain.ContextRanks(hits), Entries: kept,
		Outline: outline, Truncated: len(outline),
	}, nil
}

// packWithinBudget splits entries into the ones that fit within budget
// serialized bytes and outline rows for the rest, preserving rank order in
// both. budget <= 0 means no cap.
//
// Entries are atomic: an entry is delivered whole or not at all. Cutting a
// body mid-way would be worse than dropping it — half of an attested
// computation's SQL still looks executable, and half a model spec is not a
// spec. Nothing downstream can tell a truncated field from a short one.
//
// Packing is greedy rather than a prefix cut: one oversized entry high in
// the ranking (a model entry carries its entire spec in attrs) would
// otherwise starve everything below it. An entry larger than the whole
// budget always becomes an outline row, even at rank 1 — the caller sees
// its size and can fetch it deliberately.
//
// The outline is inside the budget, not on top of it. Naming what was left
// out costs bytes too, and an outline row carries a description, which is
// unbounded on an entry: a budget that governed only the delivered entries
// left the response as a whole ungoverned, which is the thing callers
// actually have to fit in a context window. So rows are counted, their
// descriptions are capped, and entries are demoted from the bottom of the
// ranking until the two halves fit together.
//
// The floor is the outline of everything: a request whose budget cannot
// even hold the names of what matched gets those names anyway. Silently
// dropping entries the caller never hears about is worse than a response
// slightly over budget, and the caller can raise the budget only if it
// knows there was something there.
func packWithinBudget(entries []domain.View, budget int) ([]domain.View, []domain.ContextOutline) {
	if budget <= 0 {
		return entries, nil
	}
	sizes := make([]int, len(entries))
	rows := make([]domain.ContextOutline, len(entries))
	rowSizes := make([]int, len(entries))
	deliver := make([]bool, len(entries))
	for i := range entries {
		sizes[i] = serializedSize(&entries[i])
		rows[i] = outlineRow(&entries[i], sizes[i])
		rowSizes[i] = jsonSize(rows[i])
	}
	used, outlineBytes := 0, 0
	for i := range entries {
		if used+sizes[i] <= budget {
			deliver[i] = true
			used += sizes[i]
			continue
		}
		outlineBytes += rowSizes[i]
	}
	// Demote from the bottom of the ranking until the outline the skipped
	// entries cost fits alongside what was kept. Each demotion frees the
	// entry's bytes and pays for its row, which is strictly smaller — the
	// guard is there so a pathological entry cannot spin this loop.
	for used+outlineBytes > budget {
		last := -1
		for i := range deliver {
			if deliver[i] {
				last = i
			}
		}
		if last < 0 || sizes[last] <= rowSizes[last] {
			break
		}
		deliver[last] = false
		used -= sizes[last]
		outlineBytes += rowSizes[last]
	}
	kept := make([]domain.View, 0, len(entries))
	var outline []domain.ContextOutline
	for i := range entries {
		if deliver[i] {
			kept = append(kept, entries[i])
			continue
		}
		outline = append(outline, rows[i])
	}
	return kept, outline
}

// outlineDescriptionBytes caps the description an outline row carries.
// The description is what makes a row worth reading — it is the caller's
// only basis for spending a round trip — but nothing bounds it on the
// entry, so one entry with a page-long description could outweigh the
// budget on its own. Enough for a sentence or two; the rest is what the
// fetch is for.
const outlineDescriptionBytes = 200

func outlineRow(v *domain.View, size int) domain.ContextOutline {
	return domain.ContextOutline{
		ID: v.Summary.ID, Type: v.Summary.Type, Title: v.Summary.Title,
		Description: truncateUTF8(v.Summary.Description, outlineDescriptionBytes),
		Status:      v.Summary.Status, Bytes: size,
	}
}

// serializedSize is what the entry costs on the wire. It measures the
// whole view, document included: the document is the entry, so a measure
// that skipped it would be budgeting for a fraction of what is sent.
func serializedSize(v *domain.View) int {
	b, err := json.Marshal(v)
	if err != nil {
		return len(v.Document) // unreachable in practice; never drop on a marshal quirk
	}
	return len(b)
}

// jsonSize is what any part of the response costs on the wire.
func jsonSize(v any) int {
	b, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	return len(b)
}

// SearchOrList answers the query surface REST (GET /api/v1/search) and
// MCP (search_concepts) both expose, so the two cannot drift: one place
// decides whether a request searches or lists, validates the mode, and
// runs it. A sort mode names one of the listing feeds (see list); with no
// sort, a source filter and no query is the reverse lookup; otherwise it
// is a search.
//
// Sort and query are mutually exclusive rather than combined: a feed is a
// queue a reviewer works through, and ranking it by relevance to a query
// would leave the reviewer with neither the whole queue nor the best
// matches. The CLI refuses the same pair before it ever gets here.
//
// cursor resumes a listing where the previous page ended, and is refused
// on a search: a listing has a total order to resume from, a ranking has
// a window (design doc 0050 §2.2).
func (s *Service) SearchOrList(ctx context.Context, query, sort, cursor string, f store.Filter, limit int) (*Listing, error) {
	if sort != "" {
		if !domain.ValidListSort(sort) {
			return nil, Invalidf("invalid sort %q (valid: %s)", sort, strings.Join(domain.ListSorts, ", "))
		}
		if strings.TrimSpace(query) != "" {
			return nil, Invalidf("sort=%s lists entries; it cannot be combined with a search query", sort)
		}
		return s.list(ctx, sort, cursor, f, limit)
	}
	// A reverse lookup with nothing to search by lists rather than
	// searches: "what derives from this material" (design doc 0037 §2.3)
	// and "what links at this entry" both have a set for an answer and no
	// text to rank it by. Both list in address order, and the mode is
	// named so a cursor from one cannot resume the other.
	if strings.TrimSpace(query) == "" {
		switch {
		case f.Source != "":
			return s.list(ctx, sortBySource, cursor, f, limit)
		case f.LinksTo != "":
			return s.list(ctx, sortByLinksTo, cursor, f, limit)
		}
	}
	if cursor != "" {
		return nil, searchBoundError()
	}
	hits, err := s.Search(ctx, query, f, limit)
	if err != nil {
		return nil, err
	}
	return &Listing{Hits: hits}, nil
}

// The listing modes a reverse-lookup filter with no query selects.
// Neither is one of domain.ListSorts: no caller can ask for them by name,
// because the filter already says what is wanted. They are distinct so
// that a cursor names the listing it came from.
const (
	sortBySource  = "source"
	sortByLinksTo = "links_to"
)

// askableKeys are the frontmatter keys "fm." answers, and askableHint is
// the list a refusal shows. Both come from the domain vocabulary, so the
// server, the OpenAPI text, the MCP schemas and the CLI help cannot
// disagree about what is askable.
var (
	askableKeys = domain.AskableFrontmatterKeys()
	askableHint = strings.Join(askableKeys, ", ")
)

// checkedFilter returns f with its path scopes normalized, rejecting one
// that could not lead an id, and refuses a frontmatter filter naming a key
// "fm." does not answer. Every surface hands its filter through here — the
// contract lives on the server (design doc 0015 §2), so REST, MCP and the
// CLI cannot disagree about whether "teams/growth/" is the same scope as
// "teams/growth", nor about which spellings of a question exist.
//
// An empty prefix is the root, which narrows nothing: it drops out rather
// than erroring, because asking for everything is a request the server can
// answer, not a mistake to report.
//
// This is not an access check. A caller may pass any prefix or none, and
// none returns the whole base — reading is bounded by who can reach the
// service, not by what they ask for (design doc 0002, and 0041 §4).
func checkedFilter(f store.Filter) (store.Filter, error) {
	// Sorted, so a request naming two bad keys reports the same one every
	// time rather than whichever the map handed over first.
	for _, key := range slices.Sorted(maps.Keys(f.Frontmatter)) {
		if use, ok := domain.FilterOwnedKeys[key]; ok {
			return f, Invalidf("fm.%s is not a filter: ochakai reads %s through a column of its own, "+
				"which does not answer what the frontmatter says — use %s", key, key, use)
		}
		if !slices.Contains(askableKeys, key) {
			return f, Invalidf("fm.%s is not a filter: OKF does not define %q, and the filter "+
				"vocabulary is OKF's — a producer's own key is stored and handed back exactly as "+
				"written, not asked for. Askable: %s", key, key, askableHint)
		}
	}
	if len(f.Prefixes) == 0 {
		return f, nil
	}
	scoped := make([]string, 0, len(f.Prefixes))
	for _, p := range f.Prefixes {
		clean, err := normalizePrefix(p)
		if err != nil {
			return f, err
		}
		if clean != "" {
			scoped = append(scoped, clean)
		}
	}
	f.Prefixes = scoped
	return f, nil
}

// list runs one of the listing feeds. None of them is a search: no usage
// is recorded (reading a review queue must not inflate the very signal it
// ranks by), and every hit carries score 0, so the REST and MCP responses
// keep the exact wire shape of a search across all modes.
//
//   - verified_at — by verification age, oldest first (never-verified
//     last): the feed for canary runs over verified golden queries (see
//     docs/guides/golden-query-canary.md).
//   - usage — by demand, most-searched first, never-used drafts
//     oldest-first at the bottom: the web UI's draft review queue. Hits
//     carry their usage totals so the reviewer sees the evidence inline.
//   - failed — entries whose failure reports are still unanswered, worst
//     first: the re-verification feed (design doc 0025), the
//     evidence-based counterpart to verified_at. Verifying (or rejecting)
//     takes an entry out of it, so a base that is kept up yields an empty
//     one. Hits carry their usage totals.
//   - stale_after — entries whose declared expiry has passed, most
//     overdue first (design doc 0037 §2.1); only entries stale today, a
//     date that has not come due is not work yet. The one feed verifying
//     does not empty: verified_at and failed are server observations,
//     while stale_after is what the writer declared, so clearing it means
//     editing the entry to declare a new expiry (design doc 0037 §2.2).
//   - source — the entries citing one resource, the reverse of
//     sources[].resource (design doc 0037 §2.3).
//
// Every mode pages: each has a total order ending in the id, so a cursor
// carries a position in it and the caller walks past the cap instead of
// stopping at it (design doc 0050 §2.1). One row beyond the page is read
// to tell a full page from the last one — without it, a listing whose
// length happens to equal the limit would hand out a cursor onto nothing.
func (s *Service) list(ctx context.Context, sort, cursor string, f store.Filter, limit int) (*Listing, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	f, err := checkedFilter(f)
	if err != nil {
		return nil, err
	}
	after, err := decodeCursor(cursor, sort, cursorSortKeys(sort))
	if err != nil {
		return nil, err
	}
	hits, err := s.listPage(ctx, sort, f, after, limit+1)
	if err != nil {
		return nil, err
	}
	more := len(hits) > limit
	if more {
		hits = hits[:limit]
	}
	return &Listing{Hits: hits, Cursor: nextCursor(sort, hits, more)}, nil
}

// listPage runs one feed's query. The two usage feeds rank by the usage
// totals and hand them back with each hit, so they are hits already; the
// rest return entries to project.
func (s *Service) listPage(ctx context.Context, sort string, f store.Filter, after *store.After, limit int) ([]domain.SearchHit, error) {
	switch sort {
	case "usage":
		return s.Store.ListByUsage(ctx, f, after, limit)
	case "failed":
		return s.Store.ListByFailed(ctx, f, after, limit)
	}
	list := s.Store.ListByVerifiedAt
	switch sort {
	case "stale_after":
		list = s.Store.ListByStaleAfter
	case sortBySource, sortByLinksTo:
		// Both reverse lookups are the same query with a different
		// filter, in address order (design doc 0050: a listing pages).
		list = s.Store.ListByAddress
	}
	entries, err := list(ctx, f, after, limit)
	if err != nil {
		return nil, err
	}
	hits := make([]domain.SearchHit, len(entries))
	for i := range entries {
		hits[i] = domain.SearchHit{Summary: domain.SummaryOf(&entries[i])}
	}
	return hits, nil
}

// Revisions returns an entry's change history, newest first — the
// audit surface behind "every change kept as a revision". Not a search:
// no usage is recorded (auditing an entry is not using it).
func (s *Service) Revisions(ctx context.Context, id string, limit int) ([]domain.Revision, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.Store.ListRevisions(ctx, domain.Normalize(id), limit)
}

// Usage returns usage totals for one entry (404 when the entry is gone).
func (s *Service) Usage(ctx context.Context, id string) (*domain.Usage, error) {
	id = domain.Normalize(id)
	if _, err := s.Store.Get(ctx, id); err != nil {
		return nil, err
	}
	return s.Store.Usage(ctx, id)
}

// maxOutcomeNote bounds the free-form note recorded with an outcome
// report; notes live in raw knowledge_event rows (pruned after 180 days).
const maxOutcomeNote = 2000

// maxRejectionNote bounds a rejection's reason, matching the outcome
// note: both are a human sentence or two explaining a judgment, and
// neither is a place to paste a transcript.
const maxRejectionNote = 2000

// ReportOutcome records a worked/failed report against one entry and
// returns the updated usage totals. The last edge of the write-back
// loop: an agent that ran a golden query and got a wrong number can say
// so, instead of the next agent trusting the same entry blind. Unlike
// passive usage recording, a failed write is returned — the reporter
// should know the report was lost.
func (s *Service) ReportOutcome(ctx context.Context, id, outcome, note string) (*domain.Usage, error) {
	if err := s.readOnly(); err != nil {
		return nil, err
	}
	id = domain.Normalize(id)
	if !domain.ValidOutcome(outcome) {
		return nil, Invalidf("invalid outcome %q (valid: %s)", outcome, strings.Join(domain.Outcomes, ", "))
	}
	if len(note) > maxOutcomeNote {
		return nil, Invalidf("note exceeds %d bytes", maxOutcomeNote)
	}
	if _, err := s.Store.Get(ctx, id); err != nil {
		return nil, err
	}
	actor := httpauth.Actor(ctx)
	if err := s.Store.RecordOutcome(ctx, outcome, actor, id, note); err != nil {
		return nil, err
	}
	return s.Store.Usage(ctx, id)
}

// recordUsage writes usage events with the acting caller as provenance.
// Failures are logged, never returned: usage recording must not fail reads.
func (s *Service) recordUsage(ctx context.Context, event string, ids []string) {
	if len(ids) == 0 {
		return
	}
	if err := s.Store.RecordEvents(ctx, event, httpauth.Actor(ctx), ids); err != nil {
		s.Log.Warn("usage recording failed", "event", event, "error", err)
	}
}

// maxMissQuery bounds the query text kept for a search that found
// nothing. A question a person would answer fits easily; past this the
// string is a payload rather than a question, and the miss is worth
// counting without keeping all of it (design doc 0051 §3.3).
const maxMissQuery = 500

// recordMiss keeps one unanswered question, with the acting caller as
// provenance — same treatment as a usage event, and the same silence on
// failure: nothing about measuring may fail a read.
//
// Deployments that keep no questions at all (public, or
// OCHAKAI_RECORD_MISSES=false) stop here, not in the store: the decision
// is about what this ochakai keeps, and the store is where things are
// kept.
func (s *Service) recordMiss(ctx context.Context, query string) {
	if s.Config != nil && !s.Config.RecordMisses {
		return
	}
	query = strings.TrimSpace(query)
	if len(query) > maxMissQuery {
		// On a rune boundary: a query is text somebody will read, and
		// half a character is not.
		query = strings.ToValidUTF8(query[:maxMissQuery], "")
	}
	if query == "" {
		return
	}
	if err := s.Store.RecordMiss(ctx, query, httpauth.Actor(ctx)); err != nil {
		s.Log.Warn("search miss recording failed", "error", err)
	}
}

// maxStatsWindow is the longest window the stats will answer for, and it
// is the raw-event retention (design doc 0029 §3.4): past it the events
// and misses have been pruned, so a longer window would quietly answer
// with less than it was asked for. defaultStatsWindow is a month —
// long enough for a trend, short enough that a change shows in it.
const (
	maxStatsWindow     = 180
	defaultStatsWindow = 30
)

// Stats returns the instance-level view of the improvement loop over the
// last days days (design doc 0051 §3.5): what the base is made of, what
// review did, what callers reported, and what they asked for and did not
// find.
//
// days <= 0 means the default. A window past the retention is refused
// rather than clamped: a caller asking for a year and receiving six
// months' worth, silently, would read the answer as a year.
//
// prefixes scopes it to one or more subtrees (design doc 0041), which is
// how a team on a shared deployment asks about its own knowledge — and
// what let GET /api/v1/queues be folded into this face rather than
// dropped (design doc 0049 §3.1). Only the misses ignore it: a question
// that found nothing found it nowhere, so there is no id to scope by.
func (s *Service) Stats(ctx context.Context, days int, prefixes []string) (*domain.Stats, error) {
	if days == 0 {
		days = defaultStatsWindow
	}
	if days < 0 || days > maxStatsWindow {
		return nil, Invalidf("days must be between 1 and %d: raw events and misses are pruned after %d days, "+
			"so nothing answers for the time before that", maxStatsWindow, maxStatsWindow)
	}
	f, err := checkedFilter(store.Filter{Prefixes: prefixes})
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	st, err := s.Store.Stats(ctx, now.AddDate(0, 0, -days), f.Prefixes)
	if err != nil {
		return nil, err
	}
	st.At = now
	st.WindowDays = days
	st.Misses.Recording = s.Config == nil || s.Config.RecordMisses
	return st, nil
}

// rrfFuse merges ranked lists with reciprocal rank fusion (k=60), adding a
// small boost for verified entries so certified knowledge surfaces first.
func rrfFuse(limit int, lists ...[]domain.SearchHit) []domain.SearchHit {
	const k = 60
	type entry struct {
		hit   domain.SearchHit
		score float64
	}
	byKey := map[string]*entry{}
	for _, list := range lists {
		for rank, hit := range list {
			key := hit.ID
			e, ok := byKey[key]
			if !ok {
				e = &entry{hit: hit}
				byKey[key] = e
			}
			e.score += 1.0 / float64(k+rank+1)
		}
	}
	out := make([]domain.SearchHit, 0, len(byKey))
	for _, e := range byKey {
		// Any confirmation earns the nudge, whichever tier it is: the
		// boost is about "somebody checked this", and weighting the tiers
		// against each other would be a ranking decision no record makes
		// (design docs 0033, 0046 §3.10).
		if e.hit.Trust == domain.TrustHuman || e.hit.Trust == domain.TrustMachine {
			e.score += 0.002
		}
		e.hit.Score = e.score
		out = append(out, e.hit)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].ID < out[j].ID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// updateEmbedding refreshes the stored document vector. Failures are logged,
// not returned: writes must not depend on the embedding provider being up.
func (s *Service) updateEmbedding(ctx context.Context, k *domain.Knowledge) {
	if s.Embedder == nil {
		return
	}
	vecs, err := s.embedDocument(ctx, embeddingText(k, s.embedBytes()))
	if err != nil {
		s.Log.Warn("document embedding failed; entry remains findable by the lexical half of search", "type", k.Type, "id", k.ID, "error", err)
		return
	}
	if err := s.Store.UpsertEmbedding(ctx, k.ID, s.Embedder.Model(), vecs[0]); err != nil {
		s.Log.Warn("storing embedding failed", "id", k.ID, "error", err)
	}
}

// embedBytes is how much text this deployment's model can be handed.
//
// The real limit is tokens, and UTF-8 bytes track tokens across scripts
// far better than characters do — but only roughly, and the ratio is
// worst where it matters most: Japanese runs three bytes per character
// and can approach one token per character. So the cap is deliberately
// conservative, covers the whole text (envelope included), and comes from
// the model rather than from a constant: the window it has to fit inside
// is a property of the model, and applying the smallest model's budget to
// a model with four times the window throws away three quarters of it —
// silently, since an entry truncated for no reason still embeds fine and
// just carries less of itself.
//
// A provider that does not say (any embedder but Vertex, including the
// fakes in tests) gets the conservative floor. The estimate is still an
// estimate, so embedDocument retries shorter rather than trusting it.
func (s *Service) embedBytes() int {
	if l, ok := s.Embedder.(embed.InputLimited); ok {
		return l.MaxInputBytes()
	}
	return embed.ConservativeInputBytes
}

// ReembedResult reports what a Reembed pass did. Missing is how many
// entries and attachments still have no vector once the pass is over —
// the number that decides whether the operator runs it again, so it
// counts what is actually left rather than what this pass did not get
// to. Cursor is where the next pass resumes; empty means the corpus is
// exhausted.
type ReembedResult struct {
	Embedded    int    `json:"embedded"`
	Attachments int    `json:"attachments"`
	Failed      int    `json:"failed"`
	Missing     int    `json:"missing"`
	Cursor      string `json:"cursor,omitempty"`
}

// A reembed pass is one HTTP request, and one entry is one call to the
// embedding provider — gemini-embedding-001 takes a single text per
// request, so the passes are sequential by construction. At a few hundred
// milliseconds each, a pass of 500 is minutes of request time, and Cloud
// Run's default request timeout is 300 seconds: a "bounded" pass that
// cannot finish inside a request is not bounded, it just fails late and
// reports nothing (the work it did is still written).
//
// So a pass is sized to finish, and the CLI repeats it until nothing is
// left. The ceiling is for operators who know their own timeout.
const (
	defaultReembedPass = 200
	maxReembedPass     = 5000
)

// Reembed fills in the vectors that entry writes never produced. Vectors
// are written on create and update only, so a base loaded before semantic
// search was reachable has none, and hybrid search silently stays
// lexical-only until every entry happens to be rewritten. Switching models
// has the same shape: the old vectors are in a space nothing queries any
// more (design doc 0020), as does a changed dimension, which rebuilds the
// tables empty (design doc 0053 §3).
//
// Failures are counted, not fatal: one entry the provider chokes on must
// not strand the rest of the corpus.
func (s *Service) Reembed(ctx context.Context, cursor string, limit int) (*ReembedResult, error) {
	if err := s.readOnly(); err != nil {
		return nil, err
	}
	if s.Embedder == nil {
		return nil, Unsupportedf("semantic search is not enabled on this deployment: it is the default on Google Cloud once the service identity may call Vertex AI (design doc 0053)")
	}
	if limit <= 0 || limit > maxReembedPass {
		limit = defaultReembedPass
	}
	entryCursor, attachCursor := splitReembedCursor(cursor)
	ids, err := s.Store.ListUnembedded(ctx, s.Embedder.Model(), entryCursor, limit)
	if err != nil {
		return nil, err
	}
	res := &ReembedResult{}
	for _, id := range ids {
		k, err := s.Store.Get(ctx, id)
		if err != nil {
			res.Failed++
			s.Log.Warn("reembed: entry disappeared", "id", id, "error", err)
			continue
		}
		vecs, err := s.embedDocument(ctx, embeddingText(k, s.embedBytes()))
		if err != nil {
			res.Failed++
			s.Log.Warn("reembed: embedding failed", "id", id, "error", err)
			continue
		}
		if err := s.Store.UpsertEmbedding(ctx, id, s.Embedder.Model(), vecs[0]); err != nil {
			res.Failed++
			s.Log.Warn("reembed: storing failed", "id", id, "error", err)
			continue
		}
		res.Embedded++
	}
	if len(ids) > 0 {
		entryCursor = ids[len(ids)-1]
	}
	// Attachments carry vectors of their own (design doc 0020) and only
	// attach writes them, so dropping the embedding tables to change
	// dimensions leaves every file findable by name alone. Entries come
	// first and attachments fill the rest of the pass, so a corpus with
	// both makes progress on both.
	if rest := limit - len(ids); rest > 0 {
		attachCursor, err = s.reembedAttachments(ctx, res, attachCursor, rest)
		if err != nil {
			return nil, err
		}
	}
	// Counted after the pass, so it is what is left — not what was left
	// minus what we did, which double-counts the work and reports a
	// bounded pass as a finished one. Entries this pass failed on are
	// still unembedded and belong in the total.
	missing, err := s.Store.CountUnembedded(ctx, s.Embedder.Model())
	if err != nil {
		// The pass itself succeeded; refusing to report it because the
		// tally failed would be worse than reporting it without one.
		s.Log.Warn("reembed: counting what is left failed", "error", err)
		return res, nil
	}
	attMissing, err := s.Store.CountUnembeddedAttachments(ctx, s.Embedder.Model())
	if err != nil {
		s.Log.Warn("reembed: counting attachments left failed", "error", err)
	}
	res.Missing = missing + attMissing
	if res.Missing > 0 {
		res.Cursor = joinReembedCursor(entryCursor, attachCursor)
	}
	return res, nil
}

// reembedAttachments re-embeds up to limit attachments, returning the
// cursor to resume from. Failures are counted like entry failures: a
// file the provider rejects must not strand the rest.
func (s *Service) reembedAttachments(ctx context.Context, res *ReembedResult, cursor string, limit int) (string, error) {
	afterID, afterName := splitReembedCursor(cursor)
	pending, err := s.Store.ListUnembeddedAttachments(ctx, s.Embedder.Model(), afterID, afterName, limit)
	if err != nil {
		return cursor, err
	}
	for _, a := range pending {
		att, data, err := s.Store.GetAttachment(ctx, a.KnowledgeID, a.Name)
		if err != nil {
			res.Failed++
			s.Log.Warn("reembed: attachment disappeared", "id", a.KnowledgeID, "name", a.Name, "error", err)
			continue
		}
		// The same path attach uses, so the vectors match what a fresh
		// attach would have produced; it logs and swallows its own
		// failures, which is why the count below is of rows written.
		before := s.attachmentVectorCount(ctx, a.KnowledgeID, a.Name)
		s.updateAttachmentEmbedding(ctx, a.KnowledgeID, att, data)
		if s.attachmentVectorCount(ctx, a.KnowledgeID, a.Name) > before {
			res.Attachments++
		} else {
			res.Failed++
		}
		afterID, afterName = a.KnowledgeID, a.Name
	}
	return joinReembedCursor(afterID, afterName), nil
}

// attachmentVectorCount reports whether a current-model vector exists,
// so a pass can tell an embedding that landed from one the provider (or
// the model's file support) declined.
func (s *Service) attachmentVectorCount(ctx context.Context, id, name string) int {
	n, err := s.Store.CountAttachmentEmbedding(ctx, id, name, s.Embedder.Model())
	if err != nil {
		return 0
	}
	return n
}

// The reembed cursor carries two positions — entries and attachments —
// in one opaque string, so the wire stays a single token the CLI can
// hand back unchanged. "\x00" cannot occur in an id (design doc 0017)
// or an attachment name.
func joinReembedCursor(a, b string) string {
	if a == "" && b == "" {
		return ""
	}
	return a + "\x00" + b
}

func splitReembedCursor(c string) (string, string) {
	a, b, _ := strings.Cut(c, "\x00")
	return a, b
}

// embedDocument embeds text, halving it and retrying while the model says
// it is too long. Byte budgets only approximate a token count, and the
// approximation is worst for Japanese; without this, an entry that
// overruns is dropped from vector search silently — logged, but with the
// write reported as a success. Halving converges in a couple of rounds
// and only runs on the rejection, so an outage still costs one call.
func (s *Service) embedDocument(ctx context.Context, text string) ([][]float32, error) {
	// Four attempts halve a 5000-byte text to 625, well under any
	// plausible window; the floor below stops it before it embeds a
	// fragment too short to mean anything.
	for attempt := range 4 {
		vecs, err := s.Embedder.Embed(ctx, embed.TaskDocument, []string{text})
		if err == nil {
			if attempt > 0 {
				s.Log.Info("embedded after shortening", "bytes", len(text), "attempts", attempt+1)
			}
			return vecs, nil
		}
		if !errors.Is(err, embed.ErrInputTooLong) || len(text) < 500 {
			return nil, err
		}
		text = truncateUTF8(text, len(text)/2)
	}
	return nil, fmt.Errorf("still over the embedding model's input limit after shortening")
}

// embeddingText builds the document text to embed: envelope fields plus the
// golden query question, body truncated to keep within model input limits.
// The id leads (design doc 0022): with title optional, the filename may be
// the entry's only name, and the path carries the domain hierarchy.
func embeddingText(k *domain.Knowledge, max int) string {
	parts := []string{k.ID, k.Title, k.Description, strings.Join(k.Tags, " ")}
	if q, ok := k.Attrs["question"].(string); ok {
		parts = append(parts, q)
	}
	parts = append(parts, k.Body)
	// The cap covers the joined text: an entry whose envelope is long
	// (a deep path, a paragraph of description) must not push the total
	// past the window just because the body fit on its own.
	return truncateUTF8(strings.TrimSpace(strings.Join(parts, "\n")), max)
}

// truncateUTF8 caps s at max bytes and then drops a trailing partial rune.
// A plain s[:max] lands mid-character two times in three on Japanese text
// (3 bytes per character), and half a character is not valid UTF-8.
func truncateUTF8(s string, max int) string {
	if len(s) > max {
		s = s[:max]
	}
	for len(s) > 0 {
		// DecodeLastRuneInString reports (RuneError, 1) for a byte that
		// cannot end a valid encoding; a real U+FFFD decodes with size 3.
		if r, size := utf8.DecodeLastRuneInString(s); r != utf8.RuneError || size > 1 {
			break
		}
		s = s[:len(s)-1]
	}
	return s
}
