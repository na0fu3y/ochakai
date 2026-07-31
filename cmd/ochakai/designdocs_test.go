package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The design records and the two indexes that describe them. These guards
// live beside the CLI's own because this is where the repository's
// documentation checks already are (clidocs_test.go, release_version_test.go)
// — prose does not fail to compile, so something has to read it.
const (
	designDir          = "../../docs/design"
	designIndex        = designDir + "/README.md"
	designEnglishIndex = designDir + "/README.en.md"
)

// A decision costs four places: the record itself, its entry in the index,
// its English summary, and the `Status:` header of every older record it
// supersedes or amends (design doc 0048 §1). Only the third was checked, and
// the fourth is the one the procedure itself calls easiest to forget — so an
// index could go on presenting a superseded record as current and nothing
// would notice. The checks below read the four back against each other
// (design doc 0035: check the invariant from outside rather than trust the
// convention).

// designRecord is one numbered decision record, with the header block that
// says where it stands.
type designRecord struct {
	file   string // "0046-bundle-address-space.md"
	number string // "0046"
	status string // the Status: header, up to the Date: line
	body   string
}

// supersededByRe reads "Superseded by [0046](…)" out of a Status: header —
// the form every superseded record uses, in either language.
var supersededByRe = regexp.MustCompile(`Superseded by \[(\d{4})\]`)

// supersedesRe reads the claim from the other end: the new record's header
// names the older ones it retires. Japanese is the maintainer's habit and
// English is allowed (CONTRIBUTING.md), so both spellings count.
var supersedesRe = regexp.MustCompile(`\[(\d{4})\]\([^)]*\)\s*を\s*\*{0,2}Superseded|[Ss]upersedes \[(\d{4})\]`)

func designRecords(t *testing.T) []designRecord {
	t.Helper()
	paths, err := filepath.Glob(designDir + "/[0-9]*.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatalf("no design records under %s: these checks now guard nothing", designDir)
	}
	records := make([]designRecord, 0, len(paths))
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		name := filepath.Base(path)
		records = append(records, designRecord{
			file:   name,
			number: strings.SplitN(name, "-", 2)[0],
			status: statusHeader(string(content)),
			body:   string(content),
		})
	}
	return records
}

// statusHeader returns the record's Status: header — from the line that
// opens it to the Date: line or the blank line that ends it, whichever
// comes first. It runs to several lines: it carries a link to every record
// this one supersedes or amends.
func statusHeader(content string) string {
	var header []string
	for _, line := range strings.Split(content, "\n") {
		if len(header) == 0 {
			if !strings.HasPrefix(line, "Status:") {
				continue
			}
			header = append(header, line)
			continue
		}
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "Date:") {
			break
		}
		header = append(header, line)
	}
	return strings.Join(header, "\n")
}

// Guard: a record without a Status: header is a record the index cannot be
// read against, and every check below reads that header.
func TestDesignRecordsCarryAHeader(t *testing.T) {
	dated := regexp.MustCompile(`(?m)^Date: \d{4}-\d{2}-\d{2}`)
	for _, r := range designRecords(t) {
		if r.status == "" {
			t.Errorf("docs/design/%s has no `Status:` header. It is what says whether "+
				"the record is Accepted, Proposed or Superseded, and what it supersedes "+
				"or amends (design doc 0048)", r.file)
		}
		if !dated.MatchString(r.body) {
			t.Errorf("docs/design/%s has no `Date: YYYY-MM-DD` header", r.file)
		}
		if !strings.HasPrefix(r.body, "# ") {
			t.Errorf("docs/design/%s does not open with a `# ` title", r.file)
		}
	}
}

// Guard: docs/design/README.md is the index, and design doc 0048 §2.4 makes
// it the way to an area's current state. A record that lands without an entry
// there is a record the index does not know about — and the index is the
// record of what was decided, not this repository's memory of it (ROADMAP.md).
func TestDesignIndexEntersEveryRecord(t *testing.T) {
	for _, r := range designRecords(t) {
		for _, index := range []string{designIndex, designEnglishIndex} {
			if indexEntry(t, index, r.file) == "" {
				t.Errorf("%s has no entry for %s. A link from elsewhere in the file is "+
					"not an entry: the record needs the bullet that introduces it, so a "+
					"reader meets it where its area is described",
					strings.TrimPrefix(index, "../../"), r.file)
			}
		}
	}
}

// Guard: docs/design/README.en.md summarizes the decision records for
// people who cannot read them, and the records are cited by number from
// the README, the architecture doc and api/openapi.yaml. A record that
// lands without a summary leaves those citations dead ends again, and
// nothing else would notice — the English file is prose, and prose does
// not fail to compile.
//
// The check above subsumes this one, which is kept under the name design
// doc 0048 §2.5 gives it: that record states what the English index owes a
// reader by pointing at this test, and a record does not get rewritten.
func TestEnglishDesignIndexCoversEveryRecord(t *testing.T) {
	content, err := os.ReadFile(designEnglishIndex)
	if err != nil {
		t.Fatalf("read %s: %v", designEnglishIndex, err)
	}
	summarized := string(content)

	for _, r := range designRecords(t) {
		if !strings.Contains(summarized, r.file) {
			t.Errorf("docs/design/README.en.md never links %s. Summarize it there, "+
				"or an English reader hits a Japanese dead end", r.file)
		}
	}
}

// Guard: supersession is recorded at both ends. The new record's header
// names what it retires, and every record it retires says so itself — the
// second half is step 4 of the design-doc procedure, "the step that is
// easiest to forget and the one the whole scheme rests on". Half of it
// leaves a retired decision reading as current to anyone who opens it
// directly.
func TestSupersessionIsRecordedAtBothEnds(t *testing.T) {
	records := designRecords(t)
	byNumber := map[string]designRecord{}
	for _, r := range records {
		byNumber[r.number] = r
	}

	for _, r := range records {
		for _, number := range capturedNumbers(supersedesRe, r.status) {
			retired, ok := byNumber[number]
			if !ok {
				t.Errorf("docs/design/%s says it supersedes %s, which is not a record",
					r.file, number)
				continue
			}
			if !supersededByRe.MatchString(retired.status) {
				t.Errorf("docs/design/%s marks %s superseded, but %s's own `Status:` header "+
					"does not say so. Add the line pointing at %s — a reader who opens %s "+
					"directly has only that header to tell them the decision moved",
					r.file, retired.file, retired.file, r.number, retired.file)
			}
		}
		for _, number := range capturedNumbers(supersededByRe, r.status) {
			if _, ok := byNumber[number]; !ok {
				t.Errorf("docs/design/%s says it is superseded by %s, which is not a record",
					r.file, number)
			}
		}
	}
}

// Guard: the index says the same thing about a record as the record does.
// The index's promise is that an area's current state is reachable from it
// (design doc 0048 §2.4), and an entry that still reads as current for a
// record whose header says Superseded breaks exactly that — as does the
// reverse, an entry that retires a record still standing.
func TestIndexEntriesAgreeWithTheRecordsStatus(t *testing.T) {
	for _, r := range designRecords(t) {
		retired := supersededByRe.MatchString(r.status)
		for _, index := range []string{designIndex, designEnglishIndex} {
			entry := indexEntry(t, index, r.file)
			if entry == "" {
				continue // TestDesignIndexEntersEveryRecord reports this one
			}
			name := strings.TrimPrefix(index, "../../")
			switch listed := strings.Contains(entry, "Superseded"); {
			case retired && !listed:
				t.Errorf("%s still lists %s as current; its `Status:` header says Superseded. "+
					"Mark the entry **Superseded by NNNN** and point it at the record that "+
					"replaced it", name, r.file)
			case !retired && listed:
				t.Errorf("%s lists %s as superseded; its `Status:` header does not say so. "+
					"One of the two is stale, and the record is the canonical one", name, r.file)
			}
		}
	}
}

// indexEntry returns the index bullet that introduces the named record — the
// one whose own first link is that record — or "" when the index has none. A
// mention inside another record's entry is not an entry.
func indexEntry(t *testing.T, index, file string) string {
	t.Helper()
	content, err := os.ReadFile(index)
	if err != nil {
		t.Fatalf("read %s: %v", index, err)
	}
	link := regexp.MustCompile(`\]\(([^)]+)\)`)
	// Bullets are separated by "\n- " and continuation lines are indented, so
	// the split leaves each entry whole. What comes before the first bullet is
	// the index's own prose, and is not an entry.
	for _, bullet := range strings.Split(string(content), "\n- ")[1:] {
		if !strings.HasPrefix(strings.TrimLeft(bullet, "*"), "[") {
			continue
		}
		if first := link.FindStringSubmatch(bullet); first != nil && first[1] == file {
			return bullet
		}
	}
	return ""
}

// capturedNumbers returns every capture of re in s, from whichever group
// matched: the alternations above capture the same number in different
// groups.
func capturedNumbers(re *regexp.Regexp, s string) []string {
	var numbers []string
	for _, m := range re.FindAllStringSubmatch(s, -1) {
		for _, group := range m[1:] {
			if group != "" {
				numbers = append(numbers, group)
			}
		}
	}
	return numbers
}

// The four checks above read a record against the index. None of them reads
// how much of it there is.
//
// 0048 narrowed what earns a number and said nothing about how much a number
// costs, so the corpus grew in the one direction nothing measured: 58 records
// and roughly 10,400 lines beside a 25-page manual of 5,700. The record for
// renaming two queue keys runs to 150 lines and leaves an entry in each index
// behind it. Explaining a fold has been outgrowing the fold for several
// releases, and docs/surface.md's DOC section now says so in numbers.
//
// The ceiling lives in CONTRIBUTING.md rather than here, for the same reason
// docs/surface.md keeps the surface caps in prose: raising it should be a
// line in a diff whose subject is the agreement, not a constant in a test.
const (
	contributing = "../../CONTRIBUTING.md"
	// firstCappedRecord is where the rule starts. Everything before it is
	// immutable — a ceiling cannot reach back, and a record that could be
	// edited to fit was never a decision somebody could depend on. 0062 is
	// the exception that is not one: it has not reached a release, and
	// CONTRIBUTING.md already says an unreleased record is revised by
	// replacing it, so nobody can be depending on its length either. It also
	// keeps this check from guarding nothing on the day it lands.
	firstCappedRecord = "0062"
)

var recordCeilingRe = regexp.MustCompile(`(?m)^\s*RECORD-LINES: (\d+)$`)

func TestDesignRecordsStayUnderTheirCeiling(t *testing.T) {
	content, err := os.ReadFile(contributing)
	if err != nil {
		t.Fatalf("read %s: %v", contributing, err)
	}
	m := recordCeilingRe.FindStringSubmatch(string(content))
	if m == nil {
		t.Fatalf("%s declares no RECORD-LINES ceiling: this check now guards nothing", contributing)
	}
	limit, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("%s: RECORD-LINES %q: %v", contributing, m[1], err)
	}
	capped := 0
	for _, r := range designRecords(t) {
		if r.number < firstCappedRecord {
			continue
		}
		capped++
		lines := strings.Count(r.body, "\n")
		if lines <= limit {
			continue
		}
		t.Errorf(`%s is %d lines, over the %d in %s.

A record is prose somebody reads, and going over usually means the decision
is two decisions — split it and take two numbers. If it is restating what an
earlier record already settled, cite that record instead. If it really is
that large, raise the ceiling in this PR and say why: the number is there so
the raise cannot happen quietly, not to be worked around by writing denser
Japanese.`, r.file, lines, limit, contributing)
	}
	if capped == 0 {
		t.Fatalf("no record numbered %s or later: this check now guards nothing", firstCappedRecord)
	}
}
