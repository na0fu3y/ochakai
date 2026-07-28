package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// retiredSpelling is one way of writing something the project removed,
// together with what a reader should be told instead.
type retiredSpelling struct {
	// forms are the literal spellings that teach the retired thing:
	// an address a reader could call, a value they could write, a link
	// they could put in a body. Naming the retired thing while explaining
	// that it is gone is not one of these — the prose that records a
	// removal has to be able to say what was removed.
	forms   []string
	instead string
}

var retiredSpellings = []retiredSpelling{
	{
		// The two derived surfaces moved to the paths OKF reserves.
		forms:   []string{"GET /api/v1/browse", "`/api/v1/browse`"},
		instead: "GET /api/v1/bundle/{path}/index.md (design doc 0046 §3.7)",
	},
	{
		forms:   []string{"GET /api/v1/revisions", "`/api/v1/revisions"},
		instead: "GET /api/v1/bundle/{path}/log.md (design doc 0046 §3.8)",
	},
	{
		// OKF's lifecycle has three values (SPEC §5.4). Confirmation is
		// the ledger's business, and it answers in trust tiers.
		forms: []string{
			"status: verified", "status: rejected",
			"status=verified", "status=rejected",
			"--status verified", "--status rejected",
			`"status": "verified"`, `"status": "rejected"`,
		},
		instead: "draft | stable | deprecated, with verification carried by the ledger (design docs 0043 §3.2, 0046 §§3.9-3.10)",
	},
	{
		// The scheme survives as an address — MCP needs a URI, and that
		// URI never leaves this server. What it stopped being is a form a
		// body may link with, which is what these two spellings are.
		forms:   []string{"](ochakai://", "<ochakai://"},
		instead: "[text](/metrics/revenue.md) or [text](./revenue.md), the SPEC §6 forms (design doc 0046 §3.6)",
	},
}

// Guard: the spellings the project retired do not grow back in the prose
// that teaches them.
//
// The removals themselves are checked — a call to a gone endpoint fails,
// a legacy status parses as a draft, an ochakai:// body link yields no
// edge. What nothing checks is the writing around them, and the writing
// is what the next reader believes: issue #275 found eight comments and
// one OpenAPI note still describing surfaces and rules that no longer
// exist, several of them sitting directly above the code they
// misdescribed. Nothing there changed behaviour, which is exactly why it
// survived every other check we run.
//
// This gate reads only the forms that tell a reader to use the retired
// thing. Naming it as history is how a removal gets explained, so
// "/api/v1/browse and /api/v1/revisions/{id}. Design doc 0046 §§3.7-3.8
// stops inventing" is not a hit and should not be.
//
// TestNoSurfaceSpellsItsOwnStatusVocabulary reads the tree for the
// neighbouring mistake: a hand-written list of statuses, which falls
// behind Statuses whether or not any value in it was retired. The two do
// not overlap — a list is caught there by starting with `draft`, and one
// retired value written as a status ("--status rejected") begins with no
// such thing, which is why it survived until this guard.
func TestRetiredSpellingsAreNotTaught(t *testing.T) {
	files := repoTextFiles(t)
	if len(files) == 0 {
		t.Fatal("no files walked: this check now guards nothing")
	}
	for _, path := range files {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, line := range strings.Split(string(content), "\n") {
			for _, r := range retiredSpellings {
				for _, form := range r.forms {
					if strings.Contains(line, form) {
						t.Errorf("%s still teaches the retired %q — use %s\n\t%s",
							path, form, r.instead, strings.TrimSpace(line))
					}
				}
			}
		}
	}
}

// repoTextFiles lists the files the guard above reads: everything a
// reader learns the project from, minus the three places a retired
// spelling belongs.
//
//   - docs/design holds decision records, which are not rewritten after
//     they are accepted — a record that named an endpoint goes on naming
//     it, and its Status header says what replaced it.
//   - CHANGELOG.md is the list of removals; it cannot announce one
//     without spelling it.
//   - _test.go files pin what became of each retired spelling, so the
//     spellings have to appear there to be pinned.
func repoTextFiles(t *testing.T) []string {
	t.Helper()
	const root = "../.."
	skipDirs := map[string]bool{
		".git": true, "node_modules": true, "docs/design": true,
	}
	text := map[string]bool{
		".go": true, ".md": true, ".yaml": true, ".yml": true, ".json": true,
		".html": true, ".css": true, ".js": true, ".sh": true, ".sql": true,
	}
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if d.IsDir() {
			if skipDirs[rel] || strings.HasPrefix(d.Name(), ".") && rel != "." {
				return fs.SkipDir
			}
			return nil
		}
		if rel == "CHANGELOG.md" || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		if text[filepath.Ext(rel)] {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}
