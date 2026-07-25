package apiclient

import (
	"time"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// BrowseResult mirrors GET /api/v1/browse (design docs 0014, 0016): one
// level of the ID hierarchy — the subdirectories and entries directly
// under the prefix ("" is the root). TestBrowseResultMatchesServerWire
// pins it to service.BrowseResult.
type BrowseResult struct {
	Dirs      []BrowseDir   `json:"dirs,omitempty"`
	Entries   []BrowseEntry `json:"entries,omitempty"`
	Truncated bool          `json:"truncated,omitempty"`
}

// BrowseDir is one subdirectory (ID segment) with the number of entries
// anywhere beneath it.
type BrowseDir struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// BrowseEntry is the light projection of an entry in a tree listing:
// no body, no links, no attrs. Description rides along so a directory
// listing can render as an index page.
type BrowseEntry struct {
	Type        string        `json:"type"`
	ID          string        `json:"id"`
	Title       string        `json:"title,omitempty"` // empty means the id's last segment (design doc 0022)
	Description string        `json:"description,omitempty"`
	Status      domain.Status `json:"status"`
	UpdatedAt   time.Time     `json:"updated_at"`
}
