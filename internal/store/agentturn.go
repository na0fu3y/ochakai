package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// AgentTurn is one answered turn, as kept for the loop (design doc 0142
// §6): its shape, not its contents. The agent that answered may be the
// deployment's own or any other that read the base (design doc 0144), and
// Via and Producer say which.
type AgentTurn struct {
	ID          string     `json:"id"`
	At          time.Time  `json:"at"`
	Actor       string     `json:"by"`
	Via         string     `json:"via,omitempty"`
	Producer    string     `json:"producer,omitempty"`
	Asked       string     `json:"asked"`
	Latest      string     `json:"latest"`
	Read        []string   `json:"read"`
	ProposedSQL string     `json:"proposed_sql,omitempty"`
	Drafts      []string   `json:"drafts,omitempty"`
	Verdict     string     `json:"verdict,omitempty"`
	Note        string     `json:"note,omitempty"`
	Blamed      []string   `json:"blamed,omitempty"`
	Keep        bool       `json:"keep,omitempty"`
	JudgedAt    *time.Time `json:"judged_at,omitempty"`
}

// RecordAgentTurn keeps one turn and returns its id. drafts are the
// concepts the turn wrote, which only the deployment's own agent does.
func (s *Store) RecordAgentTurn(ctx context.Context, actor domain.Actor, asked, latest string, read []string, sql string, drafts []string) (string, error) {
	if read == nil {
		read = []string{}
	}
	if drafts == nil {
		drafts = []string{}
	}
	var id string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO agent_turn (actor_kind, actor_name, actor_via, producer, asked, latest, read_ids, proposed_sql, drafts)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id::text`,
		actor.Kind, actor.Name, actor.Via, actor.Producer, asked, latest, read, sql, drafts).Scan(&id)
	if err != nil {
		return "", err
	}
	s.maybePruneEvents(ctx)
	return id, nil
}

const agentTurnColumns = `id::text, at, actor_kind || ':' || actor_name, actor_via, producer, asked, latest, read_ids,
	proposed_sql, drafts, verdict, note, blamed, keep, judged_at`

func scanAgentTurn(row pgx.Row) (*AgentTurn, error) {
	t := &AgentTurn{}
	err := row.Scan(&t.ID, &t.At, &t.Actor, &t.Via, &t.Producer, &t.Asked, &t.Latest, &t.Read,
		&t.ProposedSQL, &t.Drafts, &t.Verdict, &t.Note, &t.Blamed, &t.Keep, &t.JudgedAt)
	return t, err
}

// AgentTurn returns one turn, or ErrNotFound. An id that is not a uuid is
// a turn that does not exist rather than a malformed request.
func (s *Store) AgentTurn(ctx context.Context, id string) (*AgentTurn, error) {
	t, err := scanAgentTurn(s.pool.QueryRow(ctx,
		`SELECT `+agentTurnColumns+` FROM agent_turn WHERE id::text = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return t, err
}

// JudgeAgentTurn records the person's judgment, once. It reports whether
// the turn was still unjudged; a turn already judged is left alone.
func (s *Store) JudgeAgentTurn(ctx context.Context, id, verdict, note string, blamed []string, keep bool) (bool, error) {
	if blamed == nil {
		blamed = []string{}
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE agent_turn SET verdict = $2, note = $3, blamed = $4, keep = $5, judged_at = now()
		WHERE id::text = $1 AND verdict = ''`, id, verdict, note, blamed, keep)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// AgentTurnFilter narrows a listing of turns.
type AgentTurnFilter struct {
	// Actor, when set, is the only asker whose turns are listed
	// ("kind:name").
	Actor   string
	Verdict string // "", "good" or "bad"
	Keep    bool
	Limit   int
	// After, when set, is the last turn of the previous page: the listing
	// resumes strictly below it in (at, id) order.
	After *AgentTurnKey
}

// AgentTurnKey is a turn's place in the newest-first order.
type AgentTurnKey struct {
	At time.Time
	ID string
}

// AgentTurns lists turns newest first.
func (s *Store) AgentTurns(ctx context.Context, f AgentTurnFilter) ([]AgentTurn, error) {
	var afterAt *time.Time
	afterID := ""
	if f.After != nil {
		afterAt, afterID = &f.After.At, f.After.ID
	}
	rows, err := s.pool.Query(ctx, `SELECT `+agentTurnColumns+` FROM agent_turn
		WHERE ($1 = '' OR actor_kind || ':' || actor_name = $1)
		  AND ($2 = '' OR verdict = $2)
		  AND (NOT $3 OR keep)
		  AND ($5::timestamptz IS NULL OR (at, id::text) < ($5, $6))
		ORDER BY at DESC, id::text DESC LIMIT $4`, f.Actor, f.Verdict, f.Keep, f.Limit, afterAt, afterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AgentTurn
	for rows.Next() {
		t, err := scanAgentTurn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// AgentTurnCounts is what the window saw: turns answered, and how the
// people who asked judged them.
func (s *Store) AgentTurnCounts(ctx context.Context, since time.Time) (turns, good, bad int64, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT count(*), count(*) FILTER (WHERE verdict = 'good'), count(*) FILTER (WHERE verdict = 'bad')
		FROM agent_turn WHERE at >= $1`, since).Scan(&turns, &good, &bad)
	return
}
