package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
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
