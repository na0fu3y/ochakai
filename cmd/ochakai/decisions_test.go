package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// docs/decisions holds the short, never-rewritten entries that say why a
// choice went one way (CONTRIBUTING.md, "Design: spec and decisions").
// Their numbers continue the frozen records in docs/design, so a citation
// "0153" means one thing wherever it appears.
const decisionsDir = "../../docs/decisions"

// firstDecision is the first number docs/decisions may use: records
// 0001–0152 are docs/design's.
const firstDecision = 153

type decisionEntry struct {
	file   string // "0153-….md"
	number string // "0153"
	body   string
}

func decisionEntries(t *testing.T) []decisionEntry {
	t.Helper()
	paths, err := filepath.Glob(decisionsDir + "/[0-9]*.md")
	if err != nil {
		t.Fatal(err)
	}
	entries := make([]decisionEntry, 0, len(paths))
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		name := filepath.Base(path)
		entries = append(entries, decisionEntry{
			file:   name,
			number: strings.SplitN(name, "-", 2)[0],
			body:   string(content),
		})
	}
	return entries
}

var (
	decisionFileRe  = regexp.MustCompile(`^\d{4}-[a-z0-9-]+\.md$`)
	decisionDateRe  = regexp.MustCompile(`(?m)^Date: \d{4}-\d{2}-\d{2}$`)
	decisionAreaRe  = regexp.MustCompile(`(?m)^領域: \[[^\]]+\]\(\.\./spec/[a-z0-9-]+\.md\)$`)
	decisionTitleRe = regexp.MustCompile(`^# (\d{4}): \S`)
)

// TestDecisionEntriesAreWellFormed holds each entry to the shape
// docs/decisions/README.md gives it: a number of its own, after the
// frozen records, repeated in the title; a date; and the spec document
// of the area it belongs to — the entry says why, the spec says what,
// and a reader has to be able to get from one to the other.
func TestDecisionEntriesAreWellFormed(t *testing.T) {
	records := map[string]bool{}
	for _, r := range designRecords(t) {
		records[r.number] = true
	}
	seen := map[string]string{}
	for _, d := range decisionEntries(t) {
		if !decisionFileRe.MatchString(d.file) {
			t.Errorf("docs/decisions/%s: name it NNNN-kebab-case-slug.md", d.file)
		}
		if n, err := strconv.Atoi(d.number); err != nil || n < firstDecision {
			t.Errorf("docs/decisions/%s: numbers start at %04d — 0001–0152 are docs/design's records", d.file, firstDecision)
		}
		if records[d.number] {
			t.Errorf("docs/decisions/%s reuses %s, a docs/design record's number", d.file, d.number)
		}
		if other, dup := seen[d.number]; dup {
			t.Errorf("docs/decisions/%s and %s share the number %s", d.file, other, d.number)
		}
		seen[d.number] = d.file
		if m := decisionTitleRe.FindStringSubmatch(d.body); m == nil || m[1] != d.number {
			t.Errorf("docs/decisions/%s does not open with `# %s: <title>`", d.file, d.number)
		}
		if !decisionDateRe.MatchString(d.body) {
			t.Errorf("docs/decisions/%s has no `Date: YYYY-MM-DD` line", d.file)
		}
		if !decisionAreaRe.MatchString(d.body) {
			t.Errorf("docs/decisions/%s has no `領域: [<area>](../spec/<doc>.md)` line", d.file)
		}
	}
}
