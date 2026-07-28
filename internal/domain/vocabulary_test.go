package domain

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A lifecycle vocabulary spelled anywhere but Statuses can only fall
// behind, and nothing fails when it does: the list is prose to the
// compiler. The write path's invalid-status error proved it — it went on
// offering `verified` and `rejected` long after design doc 0043 §§3.2-3.3
// moved confirmation into the verification ledger and rejection into the
// rejection ledger, and it never named `stable`, the value a writer who
// hit the error most likely wanted. A caller told to retry with `verified`
// fails the same way, twice.
//
// So this walks the tree and reads every run that begins with `draft` as a
// claim about the vocabulary: a hand-written list, an enum, a JSON Schema
// tag the compiler will not let us interpolate into. Each one has to name
// Statuses, in order. Prose is left alone — "promote a draft to stable"
// names no list — and a run that starts anywhere else is some other
// sentence ("verified, rejected, or deprecated" is the curation guards'
// three rulings, not three statuses).
//
// TestTypeVocabularyMatchesDomain does this for types on the one page that
// carries a copy; the lifecycle values are short, ordinary English words,
// so they turn up in far more places and are worth catching everywhere.
func TestNoSurfaceSpellsItsOwnStatusVocabulary(t *testing.T) {
	root := repoRoot(t)
	for _, f := range vocabularyFiles(t, root) {
		content, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		s := string(content)
		for _, m := range statusRun.FindAllStringIndex(s, -1) {
			run := s[m[0]:m[1]]
			got := strings.ToLower(strings.Join(statusWord.FindAllString(run, -1), ", "))
			if got == StatusesHint() {
				continue
			}
			t.Errorf("%s:%d spells a lifecycle vocabulary of its own — %q, where Statuses says %q.\n"+
				"Render it from domain.Statuses (StatusesHint) so it cannot fall behind again.",
				f, 1+strings.Count(s[:m[0]], "\n"), run, StatusesHint())
		}
	}
}

// A run of lifecycle-ish words beginning with draft: comma-, pipe-, slash-
// or "or"/"and"-separated, tolerating the quotes a JS array or a YAML enum
// puts around each value. The words include the ones that are no longer
// statuses, which is the point — a stale list has to match here to be
// reported as stale.
var (
	statusWord = regexp.MustCompile(`(?i)\b(?:draft|stable|deprecated|verified|rejected|retired|published|archived|approved|active)\b`)
	statusRun  = regexp.MustCompile(`(?i)\bdraft\b(?:['"` + "`" +
		`]?\s*(?:[,|/]\s*(?:or\s+|and\s+)?|(?:or|and)\s+)['"` + "`" +
		`]?(?:draft|stable|deprecated|verified|rejected|retired|published|archived|approved|active)\b)+`)
)

// vocabularyFiles lists the files that carry text a user reads: the
// program's own sources, the wire contract, and the single-page web UI.
// Design docs are excluded — a decision record says what was decided when
// it was decided, and stays as written (docs/design/README.md).
func vocabularyFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if d.IsDir() {
			switch {
			case rel == ".":
				return nil
			case strings.HasPrefix(d.Name(), "."), d.Name() == "testdata":
				return fs.SkipDir
			case rel == filepath.Join("docs", "design"):
				return fs.SkipDir
			}
			return nil
		}
		switch {
		case rel == filepath.Join("internal", "domain", "vocabulary_test.go"):
			// This file spells the retired values on purpose.
		case strings.HasSuffix(rel, ".go"),
			rel == filepath.Join("api", "openapi.yaml"),
			rel == filepath.Join("internal", "webui", "index.html"):
			files = append(files, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	// A guard that silently walks nothing passes forever.
	if len(files) < 50 {
		t.Fatalf("only %d files to check under %s; the walk is not seeing the tree", len(files), root)
	}
	return files
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("no go.mod at %s: %v", root, err)
	}
	return root
}
