package service

import (
	"context"
	"path"
	"regexp"
	"strings"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/okf"
)

// indexEntryRe is one SPEC §8 line: "* [Title](target) - description".
// The dash before the description may be any of the spellings people
// write (-, –, —, :), and the description may be absent.
var indexEntryRe = regexp.MustCompile(`^\s*[*+-]\s+\[([^\]]*)\]\(([^)\s]+)\)\s*(?:[-–—:]\s*(.*?))?\s*$`)

// generatedCountRe is the line ochakai writes for a directory nobody has
// described. Read back from its own export, it is not a description.
var generatedCountRe = regexp.MustCompile(`^\d+ concepts?$`)

// DescribeDirectories reads a directory's index.md as a producer wrote
// it and keeps what in it ochakai does not generate: what each
// subdirectory is for (decision 0154), and, in the bundle-root index.md,
// the frontmatter that says what the bundle is — title, description,
// license, any extension key (OKF SPEC §8.1, which requires a producer
// that regenerates index.md to keep it). The listing itself is not read —
// ochakai generates it from what the directory holds — and neither are
// the concept lines, whose descriptions live in the concepts.
//
// The root's frontmatter is the whole of what the writer says about the
// bundle: a root index.md without one (or with okf_version alone) clears
// what was kept. A frontmatter that is not one is a 400. Below the root
// an index.md has no frontmatter (SPEC §8), and one written there is not
// read.
//
// A subdirectory the document lists with no description, or with the
// count ochakai writes for one nobody described, has its description
// cleared: the document is the whole of what the writer says about the
// directories it lists. One it does not list is left as it is.
//
// The answer is the plan (unchanged or updated), and with dry nothing is
// written.
func (s *Service) DescribeDirectories(ctx context.Context, dir string, doc []byte, actor domain.Actor, dry bool) (string, error) {
	if err := s.readOnly(); err != nil {
		return "", err
	}
	dir, err := normalizePrefix(dir)
	if err != nil {
		return "", err
	}
	// A directory's index.md is written by whoever may write the
	// directory — the root's by whoever may write the whole bundle — and
	// that is decided before a line of it is read. A grant covers
	// everything beneath its prefix, so each subdirectory it describes is
	// the caller's to describe as well.
	if err := s.mayWrite(ctx, dir); err != nil {
		return "", err
	}
	var meta *string
	if dir == "" {
		m, err := okf.BundleMetadata(doc)
		if err != nil {
			return "", Invalidf("index.md: %v", err)
		}
		have, err := s.Store.BundleMetadata(ctx)
		if err != nil {
			return "", err
		}
		if m != have {
			meta = &m
		}
	}
	want := map[string]string{}
	for line := range strings.SplitSeq(domain.Normalize(string(doc)), "\n") {
		m := indexEntryRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		child, ok := subdirectoryOf(dir, m[2])
		if !ok {
			continue
		}
		desc := strings.TrimSpace(m[3])
		if generatedCountRe.MatchString(desc) {
			desc = ""
		}
		want[child] = desc
	}
	changes, err := s.descriptionChanges(ctx, want)
	if err != nil {
		return "", err
	}
	if len(changes) == 0 && meta == nil {
		return domain.PlanUnchanged, nil
	}
	if !dry {
		if err := s.Store.KeepIndex(ctx, changes, meta, actor); err != nil {
			return "", err
		}
	}
	return domain.PlanUpdated, nil
}

// descriptionChanges is the part of want that differs from what is kept.
func (s *Service) descriptionChanges(ctx context.Context, want map[string]string) (map[string]string, error) {
	if len(want) == 0 {
		return nil, nil
	}
	paths := make([]string, 0, len(want))
	for p := range want {
		paths = append(paths, p)
	}
	have, err := s.Store.DirectoryDescriptions(ctx, paths)
	if err != nil {
		return nil, err
	}
	changes := map[string]string{}
	for p, d := range want {
		if have[p] != d {
			changes[p] = d
		}
	}
	return changes, nil
}

// subdirectoryOf resolves an index.md link target against dir and says
// whether it names a direct subdirectory of it — "sub/", "sub/index.md",
// "./sub", or the same spelled bundle-absolute. A concept's line
// (target.md), a file's, a URL, and anything deeper or elsewhere are not.
func subdirectoryOf(dir, target string) (string, bool) {
	if i := strings.IndexAny(target, "#?"); i >= 0 {
		target = target[:i]
	}
	if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
		return "", false
	}
	var p string
	if abs, ok := strings.CutPrefix(target, "/"); ok {
		p = abs
	} else {
		p = path.Join(dir, target)
	}
	p = strings.TrimSuffix(path.Clean("/"+p), "/index.md")
	p = strings.TrimPrefix(p, "/")
	if p == "" || p == "." || strings.HasSuffix(p, ".md") || strings.HasPrefix(p, "..") {
		return "", false
	}
	parent := path.Dir(p)
	if parent == "." {
		parent = ""
	}
	if parent != dir {
		return "", false // the directory itself, one deeper, or elsewhere
	}
	return domain.Normalize(p), true
}
