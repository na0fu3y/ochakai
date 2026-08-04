package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/okf"
)

// apiPathRe picks the first segment after /api/v1/ out of a line, in any
// language: a URL in a shell here-doc, a Python f-string, a JavaScript
// template literal. The segment is the surface being called.
var apiPathRe = regexp.MustCompile(`/api/v1/([A-Za-z0-9_-]+)`)

// Every address an example calls is an address this build serves.
//
// examples/ is not documentation about the API — it is programs written
// against it, and a program that names an endpoint the server folded away
// does not fail a build, a lint, or any contract test. The release review
// found the BigQuery sync still writing through `/api/v1/knowledge`,
// removed three surfaces ago (design doc 0046 §3.5), in a file the
// project's own README tells the reader to run. Nothing was checking,
// because the checks read the tree for prose and the tree for Go.
//
// The contract is the source of truth, as everywhere else (design doc
// 0015 §2): the valid segments come out of openapi.yaml rather than a
// list kept here, so folding a surface away turns every example that
// calls it red in the same commit.
//
// It reads the first segment only. Whether `/api/v1/bundle/x.md` is a
// path this instance holds is a question about somebody's knowledge base,
// not about the API, and this cannot answer it — what it answers is
// whether `bundle` is still a face at all.
func TestExamplesCallAddressesThisBuildServes(t *testing.T) {
	served := map[string]bool{}
	for name := range readWireSpec(t).operations(t) {
		_, path, ok := strings.Cut(name, " ")
		if !ok {
			continue
		}
		if m := apiPathRe.FindStringSubmatch(path); m != nil {
			served[m[1]] = true
		}
	}
	if len(served) == 0 {
		t.Fatal("no /api/v1 paths in openapi.yaml: this check now guards nothing")
	}

	files := exampleProgramFiles(t)
	if len(files) == 0 {
		t.Fatal("no example programs walked: this check now guards nothing")
	}
	for _, path := range files {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for i, line := range strings.Split(string(content), "\n") {
			for _, m := range apiPathRe.FindAllStringSubmatch(line, -1) {
				if served[m[1]] {
					continue
				}
				t.Errorf("%s:%d calls /api/v1/%s, which this build does not serve — the faces are %s\n\t%s",
					path, i+1, m[1], strings.Join(sortedKeys(served), ", "), strings.TrimSpace(line))
			}
		}
	}
}

// Every bundle this repo ships is read as the bundle it looks like.
//
// examples/bigquery-catalog/bundle is where ochakai shows the Attested
// Computation contract whole — a computation file, an attester file, and
// body links that attribute both — and none of that was reachable from
// Go, so a drift in it failed nothing. FromBundle is where a directory
// stops being a directory and becomes concepts and files, which makes it
// the first place a drift shows: an index.md copied in to match another
// producer's layout (OKF reserves the name and ochakai regenerates it, so
// a hand-written one imports as a note), a body link that stopped
// resolving after a rename, a file nobody links and therefore nobody
// finds (design doc 0077).
//
// The bundle is read through the CLI's own readBundle, so this fails for
// the same reasons `ochakai import` would, minus the server.
func TestExampleBundlesReadCleanly(t *testing.T) {
	roots := exampleBundleRoots(t)
	if len(roots) == 0 {
		t.Fatal("no bundles walked: this check now guards nothing")
	}
	for _, root := range roots {
		t.Run(strings.TrimPrefix(root, "../../"), func(t *testing.T) {
			files, err := readBundle(root)
			if err != nil {
				t.Fatalf("read %s: %v", root, err)
			}
			concepts, atts, loose, skipped, notes := okf.FromBundle(files)
			if len(concepts) == 0 {
				t.Fatal("bundle holds no concepts")
			}
			for _, n := range notes {
				t.Errorf("note: %s", n)
			}
			for _, s := range skipped {
				t.Errorf("skipped: %s", s)
			}
			// A file no concept links is legal in a bundle — what enters
			// leaves — but in an example it is a file the reader is never
			// sent to, which is the state design doc 0077 named.
			for _, l := range loose {
				t.Errorf("%s is attributed to no concept: nothing's body links it", l.Path)
			}
			t.Logf("%d concepts, %d attributed files", len(concepts), len(atts))
		})
	}
}

// exampleBundleRoots lists the OKF bundles under examples/, and fails when
// a concept document turns up outside every one of them. Listing the roots
// is unavoidable — a bundle root is not visible in a single file — but a
// list that silently stops covering things is the failure this whole file
// exists about, so the list is checked against the tree rather than
// trusted (design doc 0035).
func exampleBundleRoots(t *testing.T) []string {
	t.Helper()
	roots := []string{"../../examples/bigquery-catalog/bundle", "../../examples/demo"}
	// examples/golden-query.md is one document for `ochakai put -f`, not a
	// bundle; TestGoldenQueryExampleParses in internal/service pins it.
	exempt := map[string]bool{"../../examples/golden-query.md": true}

	err := filepath.WalkDir("../../examples", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(p) != ".md" {
			return err
		}
		content, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		// The same test docs/surface.md's DOC section applies: a file with
		// frontmatter under examples/ is knowledge, not prose about ochakai.
		if !strings.HasPrefix(string(content), "---\n") {
			return nil
		}
		slash := filepath.ToSlash(p)
		if exempt[slash] {
			return nil
		}
		for _, root := range roots {
			if strings.HasPrefix(slash, root+"/") {
				return nil
			}
		}
		t.Errorf("%s is a concept document in no known bundle: add its bundle root to exampleBundleRoots", slash)
		return nil
	})
	if err != nil {
		t.Fatalf("walk examples: %v", err)
	}
	return roots
}

// exampleProgramFiles lists the runnable files under examples/: the ones
// that call the API rather than describe it. Markdown is left out because
// examples/README.md and the guides beside it explain what was removed
// and have to be able to name it — the same exemption docs/design gets
// from TestRetiredSpellingsAreNotTaught, for the same reason.
func exampleProgramFiles(t *testing.T) []string {
	t.Helper()
	const root = "../../examples"
	runnable := map[string]bool{
		".py": true, ".sh": true, ".js": true, ".ts": true, ".go": true, ".json": true,
	}
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && runnable[filepath.Ext(path)] {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(out)
	return out
}
