// Package service implements ochakai's behavior shared by the MCP server
// and the REST API: knowledge CRUD with the verification policy, hybrid
// search, and the usage/outcome write-back loop.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/na0fu3y/ochakai/internal/config"
	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/embed"
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
// when the deployment runs with OCHAKAI_MODE=read-only (design doc 0040).
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

// checkedLimit is the one rule every paged or windowed read applies to
// its limit (design doc 0064): 0 (unset — queryInt cannot tell that
// apart from an explicit 0, and there is no meaningful request for zero
// rows) is def, and anything else out of [1, max] is refused rather than
// silently substituted. Before this, a request over max was clamped down
// on some endpoints and refused on others (days was already the strict
// one) — a caller sending a stale limit got a smaller answer it read as
// complete, the same silent-success shape design doc 0061 refused for
// dry_run.
func checkedLimit(limit, def, max int) (int, error) {
	if limit == 0 {
		return def, nil
	}
	if limit < 0 || limit > max {
		return 0, Invalidf("limit must be between 1 and %d, not %d", max, limit)
	}
	return limit, nil
}

// --- knowledge CRUD ---

func (s *Service) Get(ctx context.Context, id string) (*domain.Knowledge, error) {
	id = domain.Normalize(id)
	sc, err := s.scope(ctx)
	if err != nil {
		return nil, err
	}
	// Outside the caller's scope the address holds nothing, which is the
	// whole of what a read boundary is (design doc 0109 §4). Checked
	// before the store so that a 404 costs the same whether or not the
	// concept is there: a refusal that took longer when the answer
	// existed would say so.
	if !sc.MayRead(id) {
		return nil, store.ErrNotFound
	}
	k, err := s.Store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	// Metadata only — the bytes are a separate, deliberate fetch
	// (design doc 0008): images are heavy in agent context.
	if k.Files, err = s.Store.ListAttachments(ctx, id); err != nil {
		return nil, err
	}
	// A concept's files are derived from its body (0075 §5), so they can
	// name paths outside the caller's scope. The row is dropped rather
	// than the read refused — the concept itself is readable, and what
	// its prose says about a file nobody here can fetch is the limit
	// this feature has (0109 §4.1), not a reason to refuse the concept.
	k.Files = scopeRows(sc, k.Files, func(f domain.File) string { return f.Path })
	// What links at this entry — the one question its own document cannot
	// answer (design doc 0106). Rows, not documents: each is a pointer the
	// caller decides to spend a fetch on, so nothing here is recorded as
	// fetched. The complete, pageable reverse lookup stays on search's
	// links_to; these are the first maxBacklinks rows of the same answer,
	// in the same address order.
	linkers, err := s.Store.ListByAddress(ctx, store.Filter{LinksTo: id}, nil, maxBacklinks)
	if err != nil {
		// A read without its backlink rows is worth more than no read;
		// the entry itself is already in hand.
		s.Log.Warn("backlink lookup failed", "id", id, "error", err)
	}
	// The reverse lookup runs unfiltered in the store and is narrowed
	// here: what points at a concept can sit anywhere in the bundle, and
	// a backlink row names an id. This is the surface 0106 added, and it
	// is the one a scope has to be told about by hand — the filter that
	// narrows a search does not pass through it.
	linkers = scopeRows(sc, linkers, func(k domain.Knowledge) string { return k.ID })
	for i := range linkers {
		k.LinkedFrom = append(k.LinkedFrom, domain.BacklinkOf(&linkers[i]))
	}
	s.recordUsage(ctx, domain.EventFetched, []string{id})
	return k, nil
}

// maxBacklinks caps the linked_from rows a single read carries. A hub
// concept — a glossary term half the base links at — would otherwise make
// its own read unboundedly large; past the cap, the rest of the set is
// what ?links_to= pages through.
const maxBacklinks = 20

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
// at another id — and this one did not: creating over a live concept
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
			"concept at a different id and let a human judge it."
	case domain.RulingVerified:
		instead = "If it is wrong, say so with report_outcome failed — that puts it in the " +
			"re-verification feed. If you have something better, put_concept it at a " +
			"different id and let a human judge it."
	case domain.RulingDeprecated:
		instead = "Deprecated means it was correct and is no longer recommended. If it is worth " +
			"reviving, put_concept a draft at a different id that says why."
	default:
		return fmt.Errorf("%w: %s is a live %s concept. Nobody has ruled on it: "+
			"put_concept replaces it in place", err, id, k.Status)
	}
	return fmt.Errorf("%w: %s is a live %s concept. %s", err, id, ruling, instead)
}

func (s *Service) create(ctx context.Context, k *domain.Knowledge, actor domain.Actor, keepCuratedTombstones bool) (*domain.Knowledge, error) {
	if err := s.mayWrite(ctx, domain.Normalize(k.ID)); err != nil {
		return nil, err
	}
	normalizeKeys(k)
	if err := validate(k); err != nil {
		return nil, err
	}
	deriveLinks(k)
	// A document that names no status is not a draft — it is silent, and
	// OKF says a silent document is stable (SPEC §5.4). The index column
	// holds what a reader would read, and the document keeps its silence:
	// ochakai does not write a key its writer left out (design doc 0046
	// §3.9). Whether anybody has confirmed the concept is a separate
	// question, which the trust tier answers (§3.10).
	k.Status = k.Lifecycle()
	k.CreatedBy, k.UpdatedBy = actor, actor
	if err := s.Store.Create(ctx, k, keepCuratedTombstones); err != nil {
		return nil, err
	}
	s.updateEmbedding(ctx, k)
	return k, nil
}

// Update replaces a concept's content. changed=false means the payload
// matched the stored content exactly, so nothing was written: revisions
// are the audit trail of changes and updated_at means "content last
// changed" — recurring bundle imports and agents re-saving what they
// just read must not bury real history under identical snapshots.
//
// ifMatch is an optional optimistic-concurrency precondition (design doc
// 0030): when non-nil it is the content hash the caller based the edit
// on (design doc 0043 §3.4), and a concept that has changed since —
// whether before this call's read, or in the read-modify-write window
// below — yields store.ErrConflict rather than silently clobbering the
// other write. nil keeps the prior last-write-wins behavior for callers
// that do not opt in.
func (s *Service) Update(ctx context.Context, k *domain.Knowledge, actor domain.Actor, ifMatch *string) (updated *domain.Knowledge, changed bool, err error) {
	old, changed, err := s.settleUpdate(ctx, k, ifMatch)
	if err != nil {
		return nil, false, err
	}
	if !changed {
		return old, false, nil
	}
	// Different bytes saying the same thing — a reformat, a comment, a
	// reordered frontmatter. The file changed, so it is written and its
	// version moves; what the concept says did not, so generated stays with
	// whoever the content already stood by (design doc 0046 §3.4).
	if k.SameContent(old) {
		k.UpdatedBy, k.ContentChangedAt = old.UpdatedBy, old.ContentChangedAt
	} else {
		// Past this point the content really changes, so the caller is who
		// the concept now stands by — OKF's generated (design doc 0036 §3.3).
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

// settleUpdate is everything an update decides before it writes: the
// validation, the read of what is there, the precondition, the rules that
// fill k in from it, and the comparison that says whether writing k would
// change anything at all. It leaves k as the row an update would store.
//
// It is a separate step because a dry run is exactly this step and no
// more (design doc 0061): a plan that re-derived the answer beside the
// write path would be a second opinion, and the two would drift.
func (s *Service) settleUpdate(ctx context.Context, k *domain.Knowledge, ifMatch *string) (old *domain.Knowledge, changed bool, err error) {
	if err := s.readOnly(); err != nil {
		return nil, false, err
	}
	if err := s.mayWrite(ctx, domain.Normalize(k.ID)); err != nil {
		return nil, false, err
	}
	normalizeKeys(k)
	if err := validate(k); err != nil {
		return nil, false, err
	}
	deriveLinks(k)
	old, err = s.Store.Get(ctx, k.ID)
	if err != nil {
		return nil, false, err
	}
	// The caller's version is already stale if the stored concept moved on
	// between their read and ours — reject before doing any work.
	if ifMatch != nil && old.ContentHash != *ifMatch {
		return nil, false, store.ErrConflict
	}
	// A document's own trust family is a claim and is kept as one (design
	// doc 0046 §2.2) — but the export form of this very concept is not a
	// claim. It is what this instance observed, handed back by get → edit
	// → PUT or by export → review → import, and only here is there an
	// concept to compare it against. Recognizing it is what keeps a round
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
	// §§3.2-3.3). Carrying them over keeps the returned concept honest.
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
	return old, doc != old.Doc, nil
}

// Put writes k over whatever holds its id, creating the concept when the id
// is free: one operation, because a document PUT is a statement about
// what the concept should say and not about whether it already existed
// (design doc 0043 §3.5).
//
// ifMatch makes it conditional, which also makes it an update: a
// precondition names a version, and a version the caller read cannot
// belong to a concept that does not exist. A missing concept is then a 404
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
	// what it asked for ("make the concept say this") is still achievable.
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

// Plan is Put with the write withheld: the same validation, the same read
// of what is there, the same rules filling k in from it, and the same
// three-way answer — and then nothing is written (design doc 0061).
//
// It exists because the answer is the server's and only the server's. A
// document's own trust family becomes a claim or is recognized as this
// instance's observation coming home by comparing it with the stored
// concept (settleUpdate); whether the payload says anything new is a
// comparison against stored bytes; and whether the concept is writable at
// all is validate's ruling. A caller that guessed at those from the
// bundle alone — as `ochakai import --dry-run` did — reported a clean
// parse and then met a different verdict from the write.
//
// out is the concept as the write would leave it, so the caller can read
// the claim off it exactly as it reads it off a real write (okf.NoteClaim).
func (s *Service) Plan(ctx context.Context, k *domain.Knowledge, actor domain.Actor, ifMatch *string) (out *domain.Knowledge, created, changed bool, err error) {
	// Checked here as well as inside settleUpdate, because this function
	// reads ErrNotFound as "the id is free, so the write would create" —
	// and a scope refusal is spelled ErrNotFound on purpose (design doc
	// 0109 §4). Without this, a dry run outside the caller's scope
	// answered that the write was fine.
	if err := s.mayWrite(ctx, domain.Normalize(k.ID)); err != nil {
		return nil, false, false, err
	}
	old, changed, err := s.settleUpdate(ctx, k, ifMatch)
	if err == nil {
		if !changed {
			return old, false, false, nil
		}
		// The values the write would stamp, stamped without the write:
		// the same rules as Update above, the same clock as the store.
		// Leaving them zero put an empty actor and a zero time on the
		// wire, where every real write resolves both (design doc 0064).
		if k.SameContent(old) {
			k.UpdatedBy, k.ContentChangedAt = old.UpdatedBy, old.ContentChangedAt
		} else {
			k.UpdatedBy, k.ContentChangedAt = actor, store.NowStored()
		}
		k.UpdatedAt = store.NowStored()
		doc, hash, err := store.StoredDocument(k)
		if err != nil {
			return nil, false, false, err
		}
		k.Doc, k.ContentHash = doc, hash
		return k, false, true, nil
	}
	if ifMatch != nil || !errors.Is(err, store.ErrNotFound) {
		return nil, false, false, err
	}
	// The id is free, so the write would create. Everything a create
	// validates has already run inside settleUpdate, which reaches the
	// store only after it; what is left is the two rules create applies to
	// the concept it stores, so out says what a created concept would say.
	k.Status = k.Lifecycle()
	k.CreatedBy, k.UpdatedBy = actor, actor
	now := store.NowStored()
	k.CreatedAt, k.UpdatedAt, k.ContentChangedAt = now, now, now
	doc, hash, err := store.StoredDocument(k)
	if err != nil {
		return nil, false, false, err
	}
	k.Doc, k.ContentHash = doc, hash
	return k, true, true, nil
}

// RefuseIfCurated reports an error when id names a concept a human has
// already ruled on — verified, rejected, or deprecated — for surfaces that
// must not overwrite that ruling in place. It returns the concept's version
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
// obvious rule would have missed. Create refuses a live concept, so an agent
// blocked by a rejection has a two-call path around it — delete, then
// create, which revives the tombstone as a fresh draft and takes the
// status_note with it. That erases the memory of no that the write-back
// loop is built on (design doc 0025) and that the shipped instructions
// tell agents to consult before re-proposing. Nobody notices: unlike a
// deleted verified concept, a deleted rejection leaves nothing anyone was
// using.
//
// It is a step of a write, so it is scoped like one (design doc 0109 §4).
// Without that, its refusal was a read of somebody else's subtree: the
// sentence names the id and says a human ruled on it, which is exactly
// what a caller outside the scope is meant to receive a 404 for.
func (s *Service) RefuseIfCurated(ctx context.Context, id, op string) (*string, error) {
	id = domain.Normalize(id)
	if err := s.mayWrite(ctx, id); err != nil {
		return nil, err
	}
	k, err := s.Store.Get(ctx, id)
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
			"before proposing this again. If you disagree, put_concept a new concept at a " +
			"different id and let a human judge it."
	case domain.RulingDeprecated:
		instead = "Deprecated means it was correct and is no longer recommended. If it is worth " +
			"reviving, put_concept a draft that says why."
	default:
		return &k.ContentHash, nil
	}
	return nil, Invalidf("cannot %s %s from this surface: it is %s, and this surface has no "+
		"If-Match precondition to replace curated knowledge safely. %s A human changes curated "+
		"concepts from the web UI or CLI.", op, id, ruling, instead)
}

// RefuseIfRevivingCurated reports an error when id names a soft-deleted
// concept that was ruled on — verified, rejected or deprecated — before it
// was deleted. For surfaces that must not overwrite a ruling in place,
// create is the second way to do it: Create revives a tombstone (ON
// CONFLICT ... WHERE deleted_at IS NOT NULL) with the incoming status and
// status_note, so the reason a concept was turned down is replaced by a
// fresh draft, and the concept that comes back looks like it was never
// judged.
//
// RefuseIfCurated cannot see this: the row it reads is not live. Closing
// the delete step alone (design doc 0015 §3.1) leaves every tombstone that
// already exists — deleted before that rule, deleted from the REST/CLI
// surfaces where deleting a rejection is legitimate housekeeping — still
// revivable in one call. A draft tombstone stays revivable: nobody ruled
// on it, and reviving an abandoned draft is how a create on a deleted id
// is supposed to work.
//
// Scoped like RefuseIfCurated and for the same reason (design doc 0109
// §4): a tombstone is still something at an address, and saying a
// deleted ruling sits at one answers the question the boundary refuses.
// It runs ahead of create's own guard, so without this the refusal
// arrived before the 404 that should have replaced it.
func (s *Service) RefuseIfRevivingCurated(ctx context.Context, id string) error {
	id = domain.Normalize(id)
	if err := s.mayWrite(ctx, id); err != nil {
		return err
	}
	k, err := s.Store.GetTombstone(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		// A free id, or a live concept — Create's own ErrAlreadyExists
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
			"put_concept a new concept at a different id and let a human judge it."
	case domain.RulingVerified:
		instead = "It was verified knowledge when it was deleted. Propose the replacement at a " +
			"different id and let a human judge it against the history this id still holds."
	case domain.RulingDeprecated:
		instead = "Deprecated means it was correct and is no longer recommended. If it is worth " +
			"reviving, put_concept a draft at a different id that says why."
	default:
		return nil
	}
	return Invalidf("cannot create %s from this surface: the id holds a deleted %s concept, and "+
		"creating here would revive it as a fresh draft, replacing that ruling. %s A human reuses "+
		"the id from the web UI or CLI.", id, ruling, instead)
}

// ifMatch is the same optional If-Match precondition Put takes (design
// docs 0030, 0064): non-nil, the delete lands only if the concept still
// has that version.
func (s *Service) Delete(ctx context.Context, id string, actor domain.Actor, ifMatch *string) error {
	if err := s.readOnly(); err != nil {
		return err
	}
	id = domain.Normalize(id)
	if err := s.mayWrite(ctx, id); err != nil {
		return err
	}
	return s.Store.SoftDelete(ctx, id, actor, ifMatch)
}

// Verify appends a verification against the concept as it stands, whether
// this is the first confirmation or the tenth re-check.
//
// Re-checking is the case Update could not express, and without it
// neither review feed had an exit (design doc 0025 §6): a reviewer who
// re-read a verified concept and found it still correct had no way to say
// so, so it stayed at the top of the verification-age feed, and a concept
// that was reported wrong and then fixed stayed in the re-verification
// feed for good. A queue nobody can empty stops being read.
//
// It does not touch the document, so the concept's status and its ETag stay
// put (design doc 0043 §3.2). Promoting a draft to stable is a separate
// edit by a writer; confirming and publishing are different acts, and a
// draft somebody checked is a state OKF can express (SPEC §§5.3-5.4).
//
// There is no promotion restriction: anyone who may write the concept
// may verify it, and the ledger records who did — trust is judged from
// provenance rather than gated on a role (design doc 0065 §1). Where a
// deployment has drawn a write boundary, that boundary is the one this
// follows too (design doc 0109 §4): a ruling is a change to the ledger
// of a concept, so it takes the same grant an edit takes.
//
// Not an MCP tool. Verification is the human ruling the write-back loop
// turns on; an agent that finds a concept wrong reports the outcome or
// drafts a replacement (design docs 0015 §3.1, 0025).
func (s *Service) Verify(ctx context.Context, id string, actor domain.Actor) (*domain.Knowledge, error) {
	if err := s.readOnly(); err != nil {
		return nil, err
	}
	id = domain.Normalize(id)
	if err := s.mayWrite(ctx, id); err != nil {
		return nil, err
	}
	k, err := s.Store.Verify(ctx, id, actor)
	if err != nil {
		return nil, err
	}
	s.Log.Info("knowledge verified", "id", id, "actor", actor.String())
	return k, nil
}

// Reject records that a reviewer turned the concept down, with the reason.
// Like Verify it is a ruling and not an edit: the document keeps its
// lifecycle status and its ETag, and the concept drops out of searches that
// did not ask for rejected ones (design doc 0043 §3.3).
//
// This is the memory of no that the write-back loop is built on (design
// doc 0081 §4) — it is what stops an agent re-proposing knowledge a
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
	if err := s.mayWrite(ctx, id); err != nil {
		return nil, err
	}
	k, err := s.Store.Reject(ctx, id, actor, note)
	if err != nil {
		return nil, err
	}
	s.Log.Info("knowledge rejected", "id", id, "actor", actor.String())
	return k, nil
}

// WithdrawRejection withdraws a ruling, returning the concept to the
// ordinary pool. ErrNotFound when the concept is gone, ErrNoRejection
// when it is live but carries no rejection: lifting nothing is a mistake
// worth reporting rather than a silent success, and design doc 0064 tells
// the two apart on the wire rather than answering both as 404.
func (s *Service) WithdrawRejection(ctx context.Context, id string, actor domain.Actor) (*domain.Knowledge, error) {
	if err := s.readOnly(); err != nil {
		return nil, err
	}
	id = domain.Normalize(id)
	if err := s.mayWrite(ctx, id); err != nil {
		return nil, err
	}
	k, err := s.Store.WithdrawRejection(ctx, id, actor)
	if err != nil {
		return nil, err
	}
	s.Log.Info("knowledge rejection withdrawn", "id", id, "actor", actor.String())
	return k, nil
}

// Purge hard-deletes an already soft-deleted concept, freeing its id for a
// move (design doc 0021: Move cannot revive a tombstone the way Create
// can). Not an MCP tool: this is the one operation that destroys history,
// so it belongs to the human surfaces (design doc 0015).
//
// Which surface exposes it is not the same as who can reach it: on a
// deployment with no access policy, any caller that reaches the REST API
// can purge, and on one with a policy it takes a write grant over the id
// (design doc 0109). The actor is therefore recorded twice over: in
// knowledge_purge, the one table a purge does not touch, and in the log.
// An operation that leaves no trace has no place in a product whose
// value is provenance.
func (s *Service) Purge(ctx context.Context, id string, actor domain.Actor) error {
	if err := s.readOnly(); err != nil {
		return err
	}
	id = domain.Normalize(id)
	if err := s.mayWrite(ctx, id); err != nil {
		return err
	}
	if err := s.Store.Purge(ctx, id, actor); err != nil {
		return err
	}
	s.Log.Info("knowledge purged", "id", id, "actor", actor.String())
	// The promise of a purge reaches the file bytes: what the rows above
	// stopped referencing leaves the blob store too (design doc 0031,
	// condition C1).
	s.sweepBlobs(ctx)
	return nil
}

// Move renames a concept to newID, carrying every id-keyed record along
// (revisions, usage, attachments, embeddings) and rewriting inbound
// references so nothing breaks (design doc 0021). Moving to the current
// id is a no-op read.
//
// Where a policy exists, a move runs when its whole rewrite fits inside
// what the caller may write (design doc 0129). Three things have to hold
// and all three are the same right: the concept, the destination, and
// every live concept that references the concept — because a move writes
// an update revision into each of those, and one that landed outside the
// caller's scope would be a revision signed by a principal who cannot
// read what it changed (0065 §2).
//
// Refused whole, never narrowed. Skipping the referrers the caller may
// not write would leave those links pointing at an id that is gone,
// which is the one thing move exists to prevent — so the answer to a
// rewrite that reaches outside is no, and an administrator's move is the
// way it gets done.
func (s *Service) Move(ctx context.Context, id, newID string, actor domain.Actor) (*domain.Knowledge, error) {
	if err := s.readOnly(); err != nil {
		return nil, err
	}
	id, newID = domain.Normalize(id), domain.Normalize(newID)
	if !domain.ValidID(newID) {
		return nil, Invalidf(`invalid destination id %q (path segments separated by "/", e.g. sales/orders; segments must not start with "." and the last must not be "index" or "log")`, newID)
	}
	sc, err := s.scope(ctx)
	if err != nil {
		return nil, err
	}
	if err := mayWriteIn(sc, id); err != nil {
		return nil, err
	}
	// The destination is refused by name rather than hidden, unlike the
	// source: nothing is stored there yet, so saying "outside what you
	// may write" answers no question about what the bundle holds — and a
	// 404 for a place the caller is trying to create would send them
	// looking for a concept that was never the point.
	if !sc.MayWrite(newID) {
		return nil, fmt.Errorf("%w: the destination %s is outside the directories this caller may write",
			ErrForbidden, newID)
	}
	if newID == id {
		return s.Store.Get(ctx, id)
	}
	// nil is unbounded, so a scoped caller is given a list that is empty
	// rather than absent when they hold no write grant at all. That case
	// cannot get here — mayWriteIn refused it — and the store must not
	// depend on that for its answer to be the safe one.
	var within []string
	if !sc.Everything() {
		within = append([]string{}, sc.Write...)
	}
	moved, err := s.Store.Move(ctx, id, newID, actor, within)
	if errors.Is(err, store.ErrOutsideScope) {
		// What leaked is one bit, and it is about the caller's own
		// concept: something, somewhere, links at it. No address, no
		// count, no content — see 0129 §4 for why that is the one
		// disclosure this feature is worth.
		return nil, fmt.Errorf("%w: %s is referenced from outside the directories this caller may write, and a "+
			"move rewrites every reference to it rather than skipping any: an administrator can move it",
			ErrForbidden, id)
	}
	return moved, err
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
// canonical spelling here, which meant the stored concept no longer said
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
		return Invalidf(`invalid type %q (one line, up to 128 bytes; recommended: %s)`, k.Type, domain.TypesHint())
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
		return Invalidf("executor needs a resource (the run instructions or code); ochakai records the contract and never runs it")
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
