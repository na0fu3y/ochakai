package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The design records 0001–0152. They are frozen history now: the current
// state lives in docs/spec and the reasons in docs/decisions
// (CONTRIBUTING.md, "Design: spec and decisions"), so the checks that held
// a record, its two indexes and its Status: headers against each other are
// retired with the bookkeeping they checked. What stays is what a frozen
// corpus can still get wrong from outside: a citation spelled with a
// filename that no longer exists, and operator-facing text citing a record
// that a later one replaced.
const designDir = "../../docs/design"

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
// opens it to the Date: line that ends it. It runs to several lines: it
// carries a link to every record this one supersedes or amends. A record
// revised in place before release (0048 §2.3) states that as a second
// paragraph, blank-line separated from the first — 0047 and 0049 both do
// this — so an internal blank line does not end the header the way it
// ends most other Markdown blocks; only Date: does, and every record has
// exactly one such line (TestDesignRecordsCarryAHeader).
func statusHeader(content string) string {
	var header []string
	for line := range strings.SplitSeq(content, "\n") {
		if len(header) == 0 {
			if !strings.HasPrefix(line, "Status:") {
				continue
			}
			header = append(header, line)
			continue
		}
		if strings.HasPrefix(line, "Date:") {
			break
		}
		header = append(header, line)
	}
	return strings.Join(header, "\n")
}

// contributing declares the ceilings a contributor reads (DECISION-LINES).
const contributing = "../../CONTRIBUTING.md"

// A record cites its neighbours by number and spells the number as a
// filename — [0086](0086-a-second-way-to-say-who-is-calling.md) — and a
// record is renamed while the decision it holds stays put. When that
// happens the citing record is left pointing at a name nothing answers
// to, and the reader gets a 404 for a decision that is sitting right
// there under a different title.
//
// TestManualLinksResolve deliberately leaves design records out: a link
// inside an immutable record is a record of what was true when it was
// written, and a target that has since been folded away should stay
// pointed at. This asks the narrower question that immutability does not
// protect — the cited *number* still exists, so nothing about the
// decision has changed and only the spelling is wrong. Repairing that
// leaves the citation saying exactly what its author said.
//
// Superseded records are included as citation targets rather than
// resolved onward: a tombstone forwards the reader in its own Status:
// header, which is the mechanism that exists for exactly this.
func TestDesignRecordsCiteNumbersThatResolve(t *testing.T) {
	records := designRecords(t)
	byNumber := make(map[string]string, len(records))
	for _, r := range records {
		byNumber[r.number] = r.file
	}
	// Only links whose target names a numbered record: a record also
	// links to source files, to GitHub, and to the manual, and none of
	// those is this check's business.
	citationRe := regexp.MustCompile(`\((\d{4})-[^)\s]*\.md\)`)
	cited := 0
	for _, r := range records {
		for _, m := range citationRe.FindAllStringSubmatch(r.body, -1) {
			target, number := strings.Trim(m[0], "()"), m[1]
			file, ok := byNumber[number]
			if !ok {
				// A number with no record at all is a different mistake
				// and not one this check can repair by renaming.
				continue
			}
			cited++
			if target != file {
				t.Errorf("%s cites %s, which does not exist; record %s is %s now.\n\n"+
					"A renamed record keeps its number, so this citation is right about the\n"+
					"decision and wrong about the spelling: point it at the current filename.",
					r.file, target, number, file)
			}
		}
	}
	if cited == 0 {
		t.Fatal("no record cited another by filename: this check now guards nothing")
	}
}

// operatorFacing is the text a person reads while deciding what ochakai
// does and while deploying it: the manual pages, the deployment module and
// its guide, and the bundles shipped as examples. Source files are not in
// it — a comment beside code cites the record that was current when the
// code was written, and rewriting those on every supersession would make
// the citation a maintenance chore instead of a pointer to a decision.
// Design records are not in it either, for the same reason and for their
// own: they are immutable.
var operatorFacing = []string{
	"../../README.md",
	"../../deploy",
	"../../docs",
	"../../examples",
}

// designCitationRe reads a citation of a numbered record out of prose, in
// either language and with or without the section that follows it.
var designCitationRe = regexp.MustCompile(`(?:design docs?|設計ドキュメント)\s*\[?(\d{4})`)

// A citation is a pointer, and the reader follows it. Pointed at a record
// whose Status: says Superseded, they land on a tombstone — one sentence
// and a link to the commit that held the text — which answers a question
// about the history of the project rather than the one they asked, which
// was how the thing in front of them works.
//
// The deployment module was where this had gone furthest: fifteen of its
// citations named records 0066 and 0065 had folded away, so `terraform
// output posture` printed a record number whose own header disowned it,
// while the two READMEs beside it cited the current one. Records are
// immutable and their numbers are not reused, so nothing about a
// supersession reaches the prose that cites it — something has to read it
// back (design doc 0035).
func TestOperatorFacingTextCitesCurrentRecords(t *testing.T) {
	replacedBy := map[string]string{}
	for _, r := range designRecords(t) {
		if m := supersededByRe.FindStringSubmatch(r.status); m != nil {
			replacedBy[r.number] = m[1]
		}
	}
	if len(replacedBy) == 0 {
		t.Fatal("no Superseded record found: this check now guards nothing")
	}

	cited := 0
	for _, root := range operatorFacing {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			// docs/design holds the records themselves and their two
			// indexes; the indexes are checked by their own tests above.
			if d.IsDir() && path == filepath.Join("../..", "docs", "design") {
				return fs.SkipDir
			}
			if d.IsDir() || !citableFile(path) {
				return nil
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range designCitationRe.FindAllStringSubmatch(string(body), -1) {
				cited++
				current, superseded := replacedBy[m[1]]
				if !superseded {
					continue
				}
				t.Errorf(`%s cites design doc %s, which %s superseded.

A reader following that number lands on a tombstone. Point it at %s, and at
the section of it that decides what the sentence is about — the decision
survived the renumbering, only the record that states it moved.`,
					filepath.Clean(path), m[1], current, current)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	if cited == 0 {
		t.Fatal("no design record cited outside docs/design: this check now guards nothing")
	}
}

// citableFile is the prose and the configuration an operator reads —
// markdown pages, and the Terraform module whose descriptions, outputs and
// error messages are read straight off a `terraform` run.
func citableFile(path string) bool {
	switch filepath.Ext(path) {
	case ".md", ".tf", ".example":
		return true
	}
	return false
}
