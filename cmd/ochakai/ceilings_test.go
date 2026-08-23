package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Every ceiling this repository keeps is one number declared in a file
// somebody edits on purpose — docs/surface.md for what a user pays for,
// CONTRIBUTING.md for what a contributor reads — and one measurement of
// the tree, read back against each other here. The number is there so
// that growing cannot happen quietly: a PR that moves it says out loud
// that it is making ochakai, or its manual, or its records, bigger.
//
// There used to be one test per ceiling, and the three that bound an
// amount of prose — the manual, the contract, the design corpus — were
// the same seventy lines three times, each with its own wording of the
// same two failures. Seven mechanisms for one idea is how the eighth
// gets written as a copy of the seventh. What differs between ceilings
// is small enough to be data: the name, where it is declared, which of
// three rules it follows, what it measures, and the sentence that tells
// the author what to do. The table below holds that; the one test under
// it holds the rule.
//
// Three rules:
//
//   - exact: the declared number is the measured number. A list of names
//     (REST, CLI, ENV …) carries no tolerance, because adding a name is
//     already a visible diff; a fold that lowers the count lowers the
//     number in the same PR, so headroom never banks. The same rule is a
//     ratchet when the number may only move down (INDEX-ROW-RECORDS).
//   - grid: an amount of prose, declared on multiples of a stated slack.
//     The measurement may sit at most one slack under the ceiling: a
//     ceiling fitted to today's count puts the next PR back on that line
//     (and two PRs on that line is the conflict the grid was widened to
//     stop — 474df51), while one left standing after a fold is budget
//     nobody has to justify before spending (d28c3c8).
//   - under: each of several things stays at or below the number. Per
//     record, per tombstone: a ceiling on the one, not on the sum.
//
// The check holds the arithmetic, not the judgment. Whether the addition
// earned its place is the reviewer's, and docs/surface.md's three
// questions are what to ask.

// surfaceCapRe is a ceiling line in docs/surface.md's 上限 list: "- REST: 14".
// Deliberately not the backticked shape an item takes, so the list cannot
// be mistaken for a counted surface.
var surfaceCapRe = regexp.MustCompile(`^- ([A-Z-]+): (\d+)$`)

type ceilingRule int

const (
	exact ceilingRule = iota
	grid
	under
)

// measurement is one thing measured against a ceiling: what it is and
// how big. exact and grid ceilings measure one thing; under measures many.
type measurement struct {
	what string
	n    int
}

type ceiling struct {
	name    string // "DOC-LINES"
	in      string // the file that declares it
	rule    ceilingRule
	measure func(t *testing.T) []measurement
	// advice is the sentence printed under a failure: what the number
	// protects and what to do — not how far over, which the test prints.
	advice string
}

// declaredNumber reads "NAME: 123" from file, as a "- NAME: 123" bullet
// (docs/surface.md's 上限 list) or an indented "NAME: 123" line
// (CONTRIBUTING.md's code blocks). A ceiling that is not declared guards
// nothing, so that is a failure rather than a skip.
func declaredNumber(t *testing.T, file, name string) int {
	t.Helper()
	content, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	re := regexp.MustCompile(`(?m)^\s*(?:- )?` + regexp.QuoteMeta(name) + `: (\d+)$`)
	m := re.FindStringSubmatch(string(content))
	if m == nil {
		t.Fatalf("%s declares no %s: this check now guards nothing", file, name)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("%s: %s %q: %v", file, name, m[1], err)
	}
	return n
}

// one wraps a single measurement for the exact and grid rules.
func one(what string, n int) []measurement { return []measurement{{what, n}} }

// ceilings is every ceiling the repository keeps, except the counted
// surfaces' own: those come from docs/surface.md's headings, one exact
// ceiling per "## NAME (n)" section, and surfaceCeilings below adds them
// so that a new counted surface needs no row here.
var ceilings = []ceiling{
	{
		name: "DOC-LINES", in: surfaceDoc, rule: grid,
		measure: func(t *testing.T) []measurement {
			_, lines := userDocs(t)
			return one("the manual", lines)
		},
		advice: `What a reader has to get through is a cost like any other, and the manual
is counted in lines because a page is read, not learned once the way a
name is. Going past the ceiling is a decision: raise it in the same PR,
having answered docs/surface.md's three questions — or find the pages that
say the same thing twice.`,
	},
	{
		name: "REST-LINES", in: surfaceDoc, rule: grid,
		measure: func(t *testing.T) []measurement {
			spec, err := os.ReadFile(openAPISpec)
			if err != nil {
				t.Fatalf("read %s: %v", openAPISpec, err)
			}
			return one("the contract (api/openapi.yaml)", strings.Count(strings.TrimRight(string(spec), "\n"), "\n")+1)
		},
		advice: `Prose in api/openapi.yaml is paid for by everybody who reads the contract
and by every generator that has to carry it — and it was the one piece of
prose no other ceiling reached. Raise it in the same PR, having answered
docs/surface.md's three questions, or move the argument to the design
record it is retelling, where the reasoning is already written down.`,
	},
	{
		name: "RECORD-CORPUS-LINES", in: contributing, rule: grid,
		measure: func(t *testing.T) []measurement {
			records := designRecords(t)
			total := 0
			for _, r := range records {
				total += strings.Count(r.body, "\n")
			}
			return one(fmt.Sprintf("docs/design (%d records, Superseded ones included)", len(records)), total)
		},
		advice: `A record can stay under RECORD-LINES on its own and still add to what a
reader gets through if enough records do it at once, which is what this
ceiling is for. Superseded records count — they ship in the tree and a
Status: trail still opens them. Crossing a boundary upward is a decision
to make in the same PR, and the paragraph saying why is the whole point
of the number; falling back below one returns the budget the same way.`,
	},
	{
		name: "RECORD-LINES", in: contributing, rule: under,
		measure: func(t *testing.T) []measurement {
			from := fmt.Sprintf("%04d", declaredNumber(t, contributing, "RECORD-CAP-FROM"))
			var ms []measurement
			for _, r := range designRecords(t) {
				if r.number < from {
					continue // immutable before the rule; a ceiling cannot reach back
				}
				ms = append(ms, measurement{"docs/design/" + r.file, strings.Count(r.body, "\n")})
			}
			if len(ms) == 0 {
				t.Fatalf("no record numbered %s or later: this check now guards nothing", from)
			}
			return ms
		},
		advice: `A record is prose somebody reads, and going over usually means the decision
is two decisions — split it and take two numbers. If it is restating what an
earlier record already settled, cite that record instead. If it really is
that large, raise the ceiling in this PR and say why: the number is there so
the raise cannot happen quietly, not to be worked around by writing denser
Japanese.`,
	},
	{
		name: "TOMBSTONE-LINES", in: contributing, rule: under,
		measure: func(t *testing.T) []measurement {
			var ms []measurement
			for _, r := range designRecords(t) {
				if supersededByRe.MatchString(r.status) {
					ms = append(ms, measurement{"docs/design/" + r.file, strings.Count(r.body, "\n")})
				}
			}
			if len(ms) == 0 {
				t.Fatal("no Superseded record found: this check now guards nothing")
			}
			return ms
		},
		advice: `A record whose Status: says Superseded shrinks to a tombstone: the title,
the header, one sentence of what it decided, and a link to the commit that
held it in full (CONTRIBUTING.md, "What a superseded record keeps"). Nobody
can be depending on a decision that has already been replaced, and the full
text stays one git show away — it does not need to stay in this file too.`,
	},
	{
		name: "INDEX-ROW-RECORDS", in: contributing, rule: exact,
		measure: func(t *testing.T) []measurement {
			row, n := widestIndexRow(t)
			return one(fmt.Sprintf("the widest row of the design index's opening table (%q)", row), n)
		},
		advice: `A row of the index's opening table is the one line a reader reaches an
area's current state from (0048 §2.4); a row citing nine records has moved
the amendment chain into the table. The number only moves down (0128 §2.3):
do not add a record to the widest row — write the one that restates the
area and supersedes the rest (0128 §2.2), and lower the number in that PR.`,
	},
}

// surfaceCeilings is one exact ceiling per counted surface in
// docs/surface.md: "## REST (12)" is held to "- REST: 12" in the same
// file. The counters (TestSurfaceDocCounts*) hold the heading to the
// build; this holds the heading to the agreed size.
func surfaceCeilings(t *testing.T) []ceiling {
	t.Helper()
	var cs []ceiling
	for name, section := range surfaceSections(t) {
		cs = append(cs, ceiling{
			name: name, in: surfaceDoc, rule: exact,
			measure: func(t *testing.T) []measurement {
				return one(fmt.Sprintf("%s's %s section", surfaceDoc, name), section.declared)
			},
			advice: `The number is the size this project agreed this surface should be — not a
limit not to cross. Going past it is a decision: make it by editing the
number in the same PR, with docs/surface.md's three questions answered in
the description and ideally something folded away in exchange. A fold that
lowers what is counted lowers the number in the same PR, so the next
addition cannot spend the gap without moving a line anyone would see.`,
		})
	}
	return cs
}

func TestCeilings(t *testing.T) {
	all := append(surfaceCeilings(t), ceilings...)
	for _, c := range all {
		t.Run(c.name, func(t *testing.T) {
			limit := declaredNumber(t, c.in, c.name)
			ms := c.measure(t)
			switch c.rule {
			case exact:
				m := ms[0]
				if m.n == limit {
					return
				}
				t.Errorf("%s is %d; %s in %s says %d.\n\nThe number is exact. Set it to %d in the same PR that changed the count.\n\n%s",
					m.what, m.n, c.name, c.in, limit, m.n, c.advice)
			case grid:
				slack := declaredNumber(t, c.in, c.name+"-SLACK")
				if slack <= 0 || limit%slack != 0 {
					t.Errorf("%s is %d in %s, which is not a multiple of its %d slack.\n\n"+
						"The ceiling sits on a grid the width of the slack, so it is a derived number\n"+
						"rather than one to negotiate: the measurement rounded up to the next boundary.\n"+
						"Pinning it to the exact total leaves no room, and the next PR lands back on\n"+
						"this line — the conflict the grid exists to stop.", c.name, limit, c.in, slack)
					return
				}
				m := ms[0]
				next := ((m.n / slack) + 1) * slack
				switch headroom := limit - m.n; {
				case headroom < 0:
					t.Errorf("%s is %d lines, over the %s of %d in %s.\n\nIt has crossed a %d-line boundary: raise the ceiling to %d in the same PR and say why.\n\n%s",
						m.what, m.n, c.name, limit, c.in, slack, next, c.advice)
				case headroom > slack:
					t.Errorf("%s is %d lines, %d under the %s of %d in %s — more than the %d slack allows.\n\nIt has fallen back below a boundary: lower the ceiling to %d in the same PR that shrank it.\n\n%s",
						m.what, m.n, headroom, c.name, limit, c.in, slack, next, c.advice)
				}
			case under:
				for _, m := range ms {
					if m.n > limit {
						t.Errorf("%s is %d lines, over the %s of %d in %s.\n\n%s", m.what, m.n, c.name, limit, c.in, c.advice)
					}
				}
			}
		})
	}
}

// TestEveryCountedSurfaceHasACeilingAndEveryCeilingCountsSomething holds
// docs/surface.md's 上限 list to its own sections: a counted surface with
// no ceiling could grow without moving a number, and a ceiling on nothing
// is a number nobody reads. A -LINES or -BYTES name caps the amount of the
// section it is named for (MCP-BYTES is measured by internal/mcpserver's
// own test, from a real session's tools/list); a -SLACK softens the -LINES
// beside it and nothing else.
func TestEveryCountedSurfaceHasACeilingAndEveryCeilingCountsSomething(t *testing.T) {
	content, err := os.ReadFile(surfaceDoc)
	if err != nil {
		t.Fatalf("read %s: %v", surfaceDoc, err)
	}
	declared := map[string]bool{}
	for line := range strings.SplitSeq(string(content), "\n") {
		if m := surfaceCapRe.FindStringSubmatch(line); m != nil {
			declared[m[1]] = true
		}
	}
	if len(declared) == 0 {
		t.Fatalf("%s declares no ceilings: this check now guards nothing", surfaceDoc)
	}
	sections := surfaceSections(t)
	for name := range sections {
		if !declared[name] {
			t.Errorf("%s counts %s but sets it no ceiling; every counted surface has one", surfaceDoc, name)
		}
	}
	for name := range declared {
		if base, ok := strings.CutSuffix(name, "-SLACK"); ok {
			if !declared[base] {
				t.Errorf("%s declares %s but no %s for it to soften", surfaceDoc, name, base)
			}
			continue
		}
		counted := strings.TrimSuffix(strings.TrimSuffix(name, "-LINES"), "-BYTES")
		if _, ok := sections[counted]; !ok {
			t.Errorf("%s caps %s, which it does not count", surfaceDoc, name)
		}
	}
}
