package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/okf"
	"github.com/na0fu3y/ochakai/internal/store"
)

// Directory browsing (design docs 0014, 0016): the human-facing
// complement to search. IDs are full bundle paths, and this reads that
// hierarchy one level at a time; the root is the top-level segments.
// Not a search — no usage is recorded: walking the tree to see what
// exists is not the demand signal search hits measure.

// BrowseResult is one level of the tree: the subdirectories, entries and
// files directly under the prefix — the three sections the index.md OKF
// SPEC §8 describes carries (design doc 0046 §3.7).
type BrowseResult struct {
	Dirs      []store.DirCount    `json:"dirs,omitempty"`
	Entries   []store.BrowseEntry `json:"entries,omitempty"`
	Files     []store.BrowseFile  `json:"files,omitempty"`
	Truncated bool                `json:"truncated,omitempty"`
}

// Browse lists one level of the knowledge hierarchy: the subdirectories,
// entries and files directly under prefix ("" is the root). prefix
// accepts "a/b" or "a/b/".
func (s *Service) Browse(ctx context.Context, prefix string) (*BrowseResult, error) {
	prefix, err := normalizePrefix(prefix)
	if err != nil {
		return nil, err
	}
	if prefix != "" {
		prefix += "/"
	}
	lvl, err := s.Store.Browse(ctx, prefix)
	if err != nil {
		return nil, err
	}
	return &BrowseResult{Dirs: lvl.Dirs, Entries: lvl.Entries, Files: lvl.Files, Truncated: lvl.Truncated}, nil
}

// normalizePrefix cleans one path prefix, for every surface that takes
// one — browsing a directory and scoping a search to it (design doc 0041)
// must agree on what a path is, or a directory the tree can open would be
// rejected as a filter. NFC because stored ids are (design doc 0022) and
// a path pasted from a macOS filesystem arrives decomposed; no trailing
// slash because "a/b/" and "a/b" name the same directory. The empty
// prefix is the root, and valid.
// NormalizePrefix is normalizePrefix for callers outside this package —
// the archive face reads a bundle path as a subtree scope, and what a
// scope means has to be decided in one place (design doc 0015 §2).
func NormalizePrefix(p string) (string, error) { return normalizePrefix(p) }

func normalizePrefix(p string) (string, error) {
	p = domain.Normalize(strings.TrimSuffix(p, "/"))
	if !domain.ValidIDPrefix(p) {
		return "", Invalidf(`invalid prefix %q (slug segments separated by "/")`, p)
	}
	return p, nil
}

// IndexDocument renders one directory as the index.md OKF SPEC §8
// defines (design doc 0046 §3.7). It is the same listing Browse
// returns — one level, subdirectories with counts, entries with their
// descriptions, files with their size and type, cut off at the same
// limit — in the shape the format already has a place for.
//
// The files carry a heading and the other two do not. A subdirectory
// line ends in "/" and a concept line does not, so those two read
// unambiguously as one list; a file line would not, and the heading is
// where the reader is told which kind they are looking at. It also
// leaves every index.md of a bundle that holds no loose files
// byte-for-byte as it was.
//
// Two representations of one thing, at one address: a caller that wants
// to render the tree asks for JSON, a caller that wants the file gets
// the file. The JSON is not a second surface, it is the same listing
// without asking a browser to parse markdown.
func (s *Service) IndexDocument(ctx context.Context, prefix string) ([]byte, error) {
	res, err := s.Browse(ctx, prefix)
	if err != nil {
		return nil, err
	}
	dir, err := normalizePrefix(prefix)
	if err != nil {
		return nil, err
	}
	var dirs, concepts []okf.IndexLine
	for _, d := range res.Dirs {
		noun := "concepts"
		if d.Count == 1 {
			noun = "concept"
		}
		dirs = append(dirs, okf.IndexLine{Text: d.Name + "/", Target: d.Name + "/index.md",
			Description: fmt.Sprintf("%d %s", d.Count, noun)})
	}
	for _, e := range res.Entries {
		name := e.ID
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
		title := e.Title
		if title == "" {
			title = name // the filename is the name (design doc 0022)
		}
		concepts = append(concepts, okf.IndexLine{Text: title, Target: name + ".md", Description: e.Description})
	}
	// A file is listed by the name it lives under, described by what it
	// is: OKF's §8 line wants a description, and for a file the honest
	// one is its media type and size rather than prose nobody wrote.
	var files []okf.IndexLine
	for _, f := range res.Files {
		files = append(files, okf.FileIndexLine(f.Name, f.MediaType, f.Size))
	}
	var notes []string
	if res.Truncated {
		// The cut-off says so in the document. A listing that stops
		// silently is a listing that misdescribes the directory
		// (design doc 0014's decision, now visible to whoever reads
		// the file rather than only to whoever read the JSON).
		notes = append(notes, fmt.Sprintf("_This listing was cut off at %d entries._", store.MaxBrowseEntries))
	}
	return okf.IndexDocument(dir, []okf.IndexSection{
		{Lines: dirs}, {Lines: concepts}, {Heading: okf.FileSection, Lines: files},
	}, notes...), nil
}

// LogDocument renders a subtree's history as the log.md OKF SPEC §9
// defines (design doc 0046 §3.8): date-grouped, newest first.
//
// It publishes an instance ledger, which is why an import skips it: the
// history of what happened here is this instance's observation, exactly
// as generated and verified are (design doc 0009). Publishing it makes
// it readable by whoever holds the bundle; reading it back would make it
// a claim anyone could assert.
func (s *Service) LogDocument(ctx context.Context, prefix string, limit int) ([]byte, error) {
	prefix, err := normalizePrefix(prefix)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > MaxLogLines {
		limit = MaxLogLines
	}
	rows, err := s.Store.ListRevisionsUnder(ctx, prefix, limit)
	if err != nil {
		return nil, err
	}
	return RenderLog(prefix, rows), nil
}

// LogRows is LogDocument's listing without the rendering: the same
// changes under the same path, for the caller that wants to draw its
// own history rather than read OKF's (design doc 0046 §3.8).
//
// The structured form of a generated file is the same thing the file
// says — that is what "two representations at one address" means. It
// used to be the ledger of the *one concept* named by the directory,
// which made the JSON at a directory or at the root a 404 while the
// markdown beside it answered; the per-object ledger has its own
// address now (§3.5's `?history`).
func (s *Service) LogRows(ctx context.Context, prefix string, limit int) ([]store.LogRow, error) {
	prefix, err := normalizePrefix(prefix)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > MaxLogLines {
		limit = MaxLogLines
	}
	return s.Store.ListRevisionsUnder(ctx, prefix, limit)
}

// RenderLog is the log.md of one directory, given the ledger rows under
// it. Split from the read above because the export renders the same
// file from a snapshot of its own (design doc 0046 §3.8), and a log.md
// whose shape depended on which caller asked would be two formats — the
// same reason the index.md renderer is shared.
//
// prefix is already normalized. The root's document is titled for what
// it is; every other one is titled by its path, which is what a reader
// extracting the bundle sees above the list.
func RenderLog(prefix string, rows []store.LogRow) []byte {
	lines := make([]okf.LogLine, 0, len(rows))
	for _, r := range rows {
		lines = append(lines, okf.LogLine{
			At: r.ChangedAt, Change: r.Change, Path: r.Path, Title: r.Title, By: r.ChangedBy,
		})
	}
	title := "Update Log"
	if prefix != "" {
		title = prefix
	}
	return okf.LogDocument(title, lines)
}

// ObjectHistory is the changes to the one object at a bundle path — the
// ledger behind ?history (design doc 0046 §3.5), for a file as much as
// for a concept. ErrNotFound when nothing ever happened at the path,
// because a history of an object that never existed is not an empty
// history.
func (s *Service) ObjectHistory(ctx context.Context, path string, limit int) ([]store.LogRow, error) {
	if limit <= 0 || limit > MaxLogLines {
		limit = MaxLogLines
	}
	rows, err := s.Store.ListRevisionsAt(ctx, domain.Normalize(path), limit)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, store.ErrNotFound
	}
	return rows, nil
}

// MaxLogLines bounds a generated history the way the tree listing is
// bounded: a log is for reading, and a directory with a hundred thousand
// changes under it does not become more readable by containing all of
// them. The whole ledger is still there — ask a narrower path.
//
// The export applies it per directory too, so a log.md in an archive
// says the same thing the one at its address says.
const MaxLogLines = 1000
