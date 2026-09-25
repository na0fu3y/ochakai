package service

import (
	"context"
	"encoding/base64"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/httpauth"
	"github.com/na0fu3y/ochakai/internal/okf"
	"github.com/na0fu3y/ochakai/internal/store"
)

// A turn's texts are cut to this, like a miss's query: they are what the
// loop reads to see what was asked, not a transcript. A turn kept from
// outside is refused over it instead (KeepAgentTurn).
const maxTurnText = 1000

// The other two bounds on a turn kept from outside (design doc 0144 §2).
const (
	maxTurnRead = 100
	maxTurnSQL  = 16 << 10
)

// RecordAgentTurn keeps the shape of one turn the deployment's own agent
// answered and returns its id (design doc 0142 §6). Like a usage event it
// is best effort: a turn that could not be kept is logged, and the answer
// still goes out — an id of "" says there is nothing to judge.
//
// The producer is this build, whatever the caller sent: the turn says
// which agent answered, and here that is ochakai's (design doc 0144 §3).
func (s *Service) RecordAgentTurn(ctx context.Context, asked, latest string, read []string, sql string, drafts []string, revisions []store.TurnRevision) string {
	actor := httpauth.Actor(ctx)
	actor.Producer = s.agentProducer()
	id, err := s.Store.RecordAgentTurn(ctx, actor,
		cutText(asked, maxTurnText), cutText(latest, maxTurnText), read, sql, drafts, revisions)
	if err != nil {
		if s.Log != nil {
			s.Log.Warn("agent turn not kept", "error", err)
		}
		return ""
	}
	return id
}

// AgentActor is who a draft the deployment's own agent writes is by
// (design doc 0142 §3): the agent, on behalf of the person who asked,
// using this build. It cannot be mistaken for something the person wrote,
// and a listing narrowed to created_by=process:ochakai finds every one.
func (s *Service) AgentActor(ctx context.Context) domain.Actor {
	return domain.Actor{
		Kind:     domain.ActorProcess,
		Name:     "ochakai",
		Via:      domain.PrincipalOf(httpauth.Actor(ctx)),
		Producer: s.agentProducer(),
	}
}

// agentProducer is the SPEC §7 producer the deployment's own agent's
// turns carry.
func (s *Service) agentProducer() string {
	v := "dev"
	if s.Config != nil && s.Config.Version != "" {
		v = s.Config.Version
	}
	return "ochakai/" + v
}

// TurnIn is a turn some other agent answered, as its application sends
// it (design doc 0144 §2): the question, what was read to answer it, and
// the query proposed, if any. Never the answer or a query's result.
type TurnIn struct {
	Asked string   `json:"asked"`
	Read  []string `json:"read"`
	SQL   string   `json:"sql"`
}

// KeepAgentTurn keeps a turn an agent other than the deployment's own
// answered, as the calling actor's, and returns it (design doc 0144).
// Unlike RecordAgentTurn it is a request rather than a side effect, so
// what it cannot keep whole it refuses rather than cuts, and every
// concept it names must be one the caller can read — a turn that read
// what is not there has nobody to report against when it is judged.
func (s *Service) KeepAgentTurn(ctx context.Context, in TurnIn) (*store.AgentTurn, error) {
	if err := s.readOnly(); err != nil {
		return nil, err
	}
	switch {
	case strings.TrimSpace(in.Asked) == "":
		return nil, Invalidf("asked is required: the question the answer was to")
	case len(in.Asked) > maxTurnText:
		return nil, Invalidf("asked exceeds %d bytes", maxTurnText)
	case in.Read == nil:
		return nil, Invalidf("read is required: the concepts the answer read, [] for none")
	case len(in.Read) > maxTurnRead:
		return nil, Invalidf("read names %d concepts; at most %d", len(in.Read), maxTurnRead)
	case len(in.SQL) > maxTurnSQL:
		return nil, Invalidf("sql exceeds %d bytes", maxTurnSQL)
	}
	read := make([]string, 0, len(in.Read))
	for _, id := range in.Read {
		if id = domain.Normalize(id); !slices.Contains(read, id) {
			read = append(read, id)
		}
	}
	sc, err := s.scope(ctx)
	if err != nil {
		return nil, err
	}
	found, err := s.Store.GetManyDocs(ctx, read)
	if err != nil {
		return nil, err
	}
	for _, id := range read {
		// Missing and out of scope are one answer: outside a grant the
		// address holds nothing (design doc 0109 §4).
		if found[id] == nil || !sc.MayRead(id) {
			return nil, Invalidf("read names %q, which is not a concept you can read", id)
		}
	}
	id, err := s.Store.RecordAgentTurn(ctx, httpauth.Actor(ctx), in.Asked, in.Asked, read, in.SQL, nil, nil)
	if err != nil {
		return nil, err
	}
	return s.Store.AgentTurn(ctx, id)
}

// Judgment is the person's verdict on one answer.
type Judgment struct {
	Verdict string   `json:"verdict"` // good or bad
	Note    string   `json:"note"`
	Blame   []string `json:"blame"` // bad only: the concepts that misled it
	Keep    bool     `json:"keep"`  // good only: use the question for comparison
}

// JudgeResult is the judged turn and the outcome reports it became.
type JudgeResult struct {
	Turn     *store.AgentTurn `json:"turn"`
	Reported []string         `json:"reported"`
}

// JudgeAgentTurn records the verdict of the person who asked, once, and
// turns it into their own outcome reports (design doc 0142 §3): good is
// "worked" for every concept the answer read, bad is "failed" only for
// the ones the person blamed. A verdict is not a verification and moves
// no tier.
func (s *Service) JudgeAgentTurn(ctx context.Context, id string, j Judgment) (*JudgeResult, error) {
	if err := s.readOnly(); err != nil {
		return nil, err
	}
	t, err := s.Store.AgentTurn(ctx, id)
	if err != nil {
		return nil, err
	}
	// Only the person who asked judges the answer. Anybody else is told
	// the turn is not there, the way a read outside a grant is (0109).
	if t.Actor != domain.PrincipalOf(httpauth.Actor(ctx)) {
		return nil, store.ErrNotFound
	}
	switch j.Verdict {
	case "good":
		if len(j.Blame) > 0 {
			return nil, Invalidf("blame goes with a bad verdict: a good answer blames nothing")
		}
	case "bad":
		if j.Keep {
			return nil, Invalidf("keep goes with a good verdict: a question is kept for comparison with the answer it should get")
		}
		for _, b := range j.Blame {
			if !slices.Contains(t.Read, b) {
				return nil, Invalidf("blame names %q, which this answer did not read", b)
			}
		}
	default:
		return nil, Invalidf("verdict is %q; it is good or bad", j.Verdict)
	}
	if len(j.Note) > maxOutcomeNote {
		return nil, Invalidf("note exceeds %d bytes", maxOutcomeNote)
	}
	fresh, err := s.Store.JudgeAgentTurn(ctx, id, j.Verdict, j.Note, j.Blame, j.Keep)
	if err != nil {
		return nil, err
	}
	if !fresh {
		return nil, Invalidf("this answer has already been judged")
	}
	outcome, targets := domain.EventWorked, t.Read
	if j.Verdict == "bad" {
		outcome, targets = domain.EventFailed, j.Blame
	}
	res := &JudgeResult{Reported: []string{}}
	for _, cid := range targets {
		// A concept deleted or moved since the answer read it has nothing
		// to report against; the verdict stands without it.
		if _, err := s.ReportOutcome(ctx, cid, outcome, j.Note); err != nil {
			if errors.Is(err, store.ErrNotFound) || errors.Is(err, ErrForbidden) {
				continue
			}
			return nil, err
		}
		res.Reported = append(res.Reported, cid)
	}
	if res.Turn, err = s.Store.AgentTurn(ctx, id); err != nil {
		return nil, err
	}
	return res, nil
}

// AgentTurns lists turns for the agent's own reading: an administrator's
// covers everybody, anyone else's only what they asked themselves
// (design doc 0142 §6).
func (s *Service) AgentTurns(ctx context.Context, verdict string, keep bool, limit int) ([]store.AgentTurn, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	page, err := s.AgentTurnPage(ctx, verdict, keep, limit, "")
	if err != nil {
		return nil, err
	}
	return page.Turns, nil
}

// TurnPage is one page of turns, and the cursor to the next when there
// may be one.
type TurnPage struct {
	Turns  []store.AgentTurn `json:"turns"`
	Cursor string            `json:"cursor,omitempty"`
}

// AgentTurnPage lists turns newest first under the same scope as
// AgentTurns — the REST face of what the agent reads (design doc 0144
// §4). Unlike the agent's tool it says when a value is not one it takes.
func (s *Service) AgentTurnPage(ctx context.Context, verdict string, keep bool, limit int, cursor string) (*TurnPage, error) {
	if verdict != "" && verdict != "good" && verdict != "bad" {
		return nil, Invalidf("verdict is %q; it is good or bad", verdict)
	}
	if limit == 0 {
		limit = 30
	}
	if limit < 0 || limit > 100 {
		return nil, Invalidf("limit is %d; it is 1 to 100", limit)
	}
	f := store.AgentTurnFilter{Verdict: verdict, Keep: keep, Limit: limit}
	if cursor != "" {
		after, err := decodeTurnCursor(cursor)
		if err != nil {
			return nil, err
		}
		f.After = after
	}
	if err := s.RequireAdmin(ctx, "read other people's agent turns"); err != nil {
		f.Actor = domain.PrincipalOf(httpauth.Actor(ctx))
	}
	turns, err := s.Store.AgentTurns(ctx, f)
	if err != nil {
		return nil, err
	}
	page := &TurnPage{Turns: turns}
	if page.Turns == nil {
		page.Turns = []store.AgentTurn{}
	}
	if len(turns) == limit {
		last := turns[len(turns)-1]
		page.Cursor = base64.RawURLEncoding.EncodeToString(
			[]byte(last.At.UTC().Format(time.RFC3339Nano) + "|" + last.ID))
	}
	return page, nil
}

func decodeTurnCursor(enc string) (*store.AgentTurnKey, error) {
	raw, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return nil, malformedCursor()
	}
	at, id, ok := strings.Cut(string(raw), "|")
	if !ok || id == "" {
		return nil, malformedCursor()
	}
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return nil, malformedCursor()
	}
	return &store.AgentTurnKey{At: t, ID: id}, nil
}

func cutText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n]
}

// ApplyAgentRevision writes one revision the deployment's own agent
// proposed in a turn, at the asking person's say-so (design doc 0149).
//
// What is written is the document the turn kept, never one the caller
// sends: the record says the agent wrote it (process:ochakai via the
// person), and that has to be true of every byte. Only the person who
// asked may apply it, as only they may judge the answer. The draft must
// still be a draft nobody has ruled on, and still be what the agent read
// — the kept content hash is the write's precondition, so a draft edited
// or applied since is a 412 rather than an overwrite.
func (s *Service) ApplyAgentRevision(ctx context.Context, turnID, conceptID string) (*domain.Knowledge, error) {
	if err := s.readOnly(); err != nil {
		return nil, err
	}
	t, err := s.Store.AgentTurn(ctx, turnID)
	if err != nil {
		return nil, err
	}
	if t.Actor != domain.PrincipalOf(httpauth.Actor(ctx)) {
		return nil, store.ErrNotFound
	}
	id := domain.Normalize(conceptID)
	var rev *store.TurnRevision
	for i := range t.Revisions {
		if t.Revisions[i].ID == id {
			rev = &t.Revisions[i]
		}
	}
	if rev == nil {
		return nil, store.ErrNotFound
	}
	k, err := s.RevisableDraft(ctx, id, rev.Document)
	if err != nil {
		return nil, err
	}
	updated, _, err := s.Update(ctx, k, s.AgentActor(ctx), &rev.Base)
	return updated, err
}

// RevisableDraft parses document as the next version of the draft at id
// and says whether the agent may propose it: the draft is the caller's to
// write, nobody has ruled on it, it is still a draft, and the document
// keeps it one (design doc 0149). It returns the knowledge to write.
func (s *Service) RevisableDraft(ctx context.Context, id, document string) (*domain.Knowledge, error) {
	if err := s.readOnly(); err != nil {
		return nil, err
	}
	if _, err := s.RefuseIfCurated(ctx, id, "revise"); err != nil {
		return nil, err
	}
	cur, err := s.Store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if cur.Status != domain.StatusDraft {
		return nil, Invalidf("%s is %s, not a draft: only a draft nobody has ruled on is revised this way; write a new draft that links it instead", id, cur.Status)
	}
	d, _, err := okf.Parse([]byte(document))
	if err != nil {
		return nil, err
	}
	k := d.Knowledge
	k.ID = id
	switch k.Status {
	case "", domain.StatusDraft:
		k.Status = domain.StatusDraft
	default:
		return nil, Invalidf("the document says status: %s; a revision keeps the draft a draft", k.Status)
	}
	return &k, nil
}
