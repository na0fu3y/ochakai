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
	Title       string        `json:"title,omitempty"` // empty means the id's last segment (design doc 0074 §1)
	Description string        `json:"description,omitempty"`
	Status      domain.Status `json:"status"`
	UpdatedAt   time.Time     `json:"updated_at"`
}
