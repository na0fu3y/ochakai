package apiclient

import (
	"time"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// BrowseResult mirrors the JSON representation of a directory's index.md
// (design docs 0014, 0016, and 0046 §3.7 for where it now lives): one
// level of the ID hierarchy — the subdirectories, concepts and files
// directly under the prefix ("" is the root).
// TestBrowseResultMatchesServerWire pins it to service.BrowseResult.
type BrowseResult struct {
	Dirs      []BrowseDir     `json:"dirs,omitempty"`
	Concepts  []BrowseConcept `json:"concepts,omitempty"`
	Files     []BrowseFile    `json:"files,omitempty"`
	Truncated bool            `json:"truncated,omitempty"`

	// Cursor resumes the level where this page ended, and is absent on
	// the last one (design doc 0101).
	Cursor string `json:"cursor,omitempty"`
}

// BrowseDir is one subdirectory (ID segment) with the number of concepts
// anywhere beneath it. Concepts only: a directory holding nothing but
// files is not one of these (design doc 0046 §3.7).
type BrowseDir struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// BrowseFile is one file sitting directly in the directory — the third
// thing an index.md lists. A file is an object in the bundle rather than
// a property of a concept (design doc 0046 §3.3), so a directory can hold
// one that no concept shows.
type BrowseFile struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	MediaType string    `json:"media_type"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
}

// BrowseConcept is the light projection of a concept in a tree listing:
// no body, no links, no attrs. Description rides along so a directory
// listing can render as an index page.
type BrowseConcept struct {
	Type        string        `json:"type"`
	ID          string        `json:"id"`
	Title       string        `json:"title,omitempty"` // empty means the id's last segment (design doc 0136 §1)
	Description string        `json:"description,omitempty"`
	Status      domain.Status `json:"status"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

// AgentMessage is one message of a conversation with the deployment's
// agent (POST /api/v1/agent): the whole conversation travels on every
// call, since the server keeps none (design doc 0118).
type AgentMessage struct {
	Role string `json:"role"` // "user" or "agent"
	Text string `json:"text"`
}

// AgentAnswer is the agent's reply. SQL is a proposal for the person to
// run, never something the server ran (design doc 0142 §4); Turn is
// absent on a dry run, which keeps none (design doc 0146).
type AgentAnswer struct {
	Text   string         `json:"text"`
	Read   []string       `json:"read"`
	SQL    *AgentProposal `json:"sql,omitempty"`
	Drafts []string       `json:"drafts,omitempty"`
	Turn   string         `json:"turn,omitempty"`
}

// AgentProposal is one query the agent asks the person to run.
type AgentProposal struct {
	Query   string `json:"query"`
	Purpose string `json:"purpose"`
}

// AgentTurn is the kept shape of one answer (design doc 0142 §6):
// what was asked, what was read, what was proposed, and how the person
// who asked judged it. Never the answer or a query's result.
type AgentTurn struct {
	ID          string    `json:"id"`
	At          time.Time `json:"at"`
	By          string    `json:"by"`
	Via         string    `json:"via,omitempty"`
	Producer    string    `json:"producer,omitempty"`
	Asked       string    `json:"asked"`
	Latest      string    `json:"latest"`
	Read        []string  `json:"read"`
	ProposedSQL string    `json:"proposed_sql,omitempty"`
	Verdict     string    `json:"verdict,omitempty"`
	Note        string    `json:"note,omitempty"`
	Keep        bool      `json:"keep,omitempty"`
}
