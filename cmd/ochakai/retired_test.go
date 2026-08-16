package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
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
		//
		// The plural spellings are the filter's, and they are here because
		// the server's own get_context hint taught `statuses=["rejected"]`
		// long after the value stopped being a status — a call every
		// surface answers with a 400 (service.checkedFilter), handed to
		// the hosts that have no Stop hook and see this string only. The
		// singular forms above did not match it: `["` sits between the
		// key and the value. Both quotings are listed because a Go string
		// literal escapes the inner quotes and no other file does.
		forms: []string{
			"status: verified", "status: rejected",
			"status=verified", "status=rejected",
			"--status verified", "--status rejected",
			`"status": "verified"`, `"status": "rejected"`,
			`statuses=["verified"`, `statuses=["rejected"`,
			`statuses=[\"verified\"`, `statuses=[\"rejected\"`,
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
	{
		// The context pack retired from every surface at once (design doc
		// 0108): an agent's read is search → get, and the pack's reverse
		// hop arrives as linked_from on the concept itself (0106). The
		// command and tool spellings are caught by the command and tool
		// walks; what this guards is prose teaching the address.
		forms:   []string{"GET /api/v1/context", "`/api/v1/context`"},
		instead: "GET /api/v1/search and GET /api/v1/bundle/{id}.md (design doc 0108)",
	},
	{
		// Design doc 0064 dropped the "X-" prefix from both request
		// headers with no dual-accept window (httpauth.OnBehalfOfHeader,
		// httpauth.ProducerHeader). Functional code that has to name the
		// retired spelling — stripDelegation strips both, so a staggered
		// upgrade of the two-service web UI can't forward one past a
		// filter that only knew the current name (issue #410) — builds
		// it as "X-"+httpauth.OnBehalfOfHeader instead of writing the
		// retired literal, so it never trips this guard.
		forms:   []string{"X-Ochakai-"},
		instead: "Ochakai-On-Behalf-Of or Ochakai-Producer, no X- prefix (design doc 0064)",
	},
	{
		// domain.ValidActorKind accepts only "human" and "process"; a
		// caller that sends "agent:" gets a 400 (httpauth.parseOnBehalfOf).
		// The two forms are the shapes an identity string actually takes —
		// `agent:` opening a backtick span, and the "human:x via agent:y"
		// join — chosen so that explaining the rename ("`agent:` became
		// `process:`") is not itself a hit.
		forms:   []string{"`agent:", "via agent:"},
		instead: "human: or process:, the SPEC §5.3 kinds (design doc 0043 §3.8)",
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
//   - docs/design's numbered records are not rewritten after they are
//     accepted — a record that named an endpoint goes on naming it, and
//     its Status header says what replaced it. Its two indexes,
//     README.md and README.en.md, are not records: CLAUDE.md requires
//     them to say what holds today, so they stay in the walk (issue
//     #420 found README.en.md's own summary of 0027 teaching "via
//     agent:" after the kind was renamed to process:).
//   - CHANGELOG.md is the list of removals; it cannot announce one
//     without spelling it.
//   - _test.go files pin what became of each retired spelling, so the
//     spellings have to appear there to be pinned.
func repoTextFiles(t *testing.T) []string {
	t.Helper()
	const root = "../.."
	skipDirs := map[string]bool{
		".git": true, "node_modules": true,
	}
	designIndexes := map[string]bool{
		"docs/design/README.md": true, "docs/design/README.en.md": true,
	}
	text := map[string]bool{
		".go": true, ".md": true, ".yaml": true, ".yml": true, ".json": true,
		".html": true, ".css": true, ".js": true, ".sh": true, ".sql": true,
		".tf": true,
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
			// .github holds prose a reader follows too — issue templates,
			// most namely — so it is not skipped as a dot-directory the
			// way .git and other tooling directories are.
			if skipDirs[rel] || strings.HasPrefix(d.Name(), ".") && rel != "." && rel != ".github" {
				return fs.SkipDir
			}
			return nil
		}
		if rel == "CHANGELOG.md" || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		if strings.HasPrefix(rel, "docs/design/") && !designIndexes[rel] {
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

// A reservation answered in two places is a path one half accepts and
// the other refuses, which is what the four spellings of "is this
// index.md or log.md" had become: domain.ReservedBundleName folded case
// while restapi, okf and the import skip each compared the two literals
// inline, and okf's copy had no callers at all. The address took a
// concept at "Index.md" and refused a file there.
//
// domain.ReservedBundleName is the only one now. This reads the tree for
// the shape that grew the others — a comparison against the reserved
// filenames written out by hand — because nothing else would notice a
// fifth appearing beside it. Prose may name them; only code that decides
// is a hit, which is what the quotes around the literal pick out.
func TestOneSpellingDecidesWhatIsReserved(t *testing.T) {
	forms := []string{`== "index.md"`, `== "log.md"`, `!= "index.md"`, `!= "log.md"`,
		`"index.md", "log.md"`, `"log.md", "index.md"`}
	const decides = "internal/domain/file.go" // where the decision lives
	found := false
	for _, path := range repoTextFiles(t) {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if filepath.Ext(path) != ".go" {
			continue
		}
		rel := filepath.ToSlash(strings.TrimPrefix(filepath.ToSlash(path), "../../"))
		for _, form := range forms {
			if !strings.Contains(string(content), form) {
				continue
			}
			if rel == decides {
				found = true
				continue
			}
			t.Errorf("%s decides what is reserved by comparing %s — call domain.ReservedBundleName instead",
				rel, form)
		}
	}
	if !found {
		t.Errorf("%s no longer spells the reserved names: this check now guards nothing", decides)
	}
}

// An address the contract mentions but does not declare: everything
// matching /api/v1/... in api/openapi.yaml, minus what its own `paths`
// covers.
var wireAddressRe = regexp.MustCompile(`/api/v1/[A-Za-z0-9_.{}/-]*`)

// pastTense are the words that turn a retired address into history. A
// removal cannot be explained without spelling what was removed, so the
// signal cannot be the address — it has to be the tense around it.
var pastTense = []string{"gone", "retired", "replaced", "used to", "was", "were", "old"}

// Guard: the contract names no address it does not have, except to say
// that address is gone.
//
// TestRetiredSpellingsAreNotTaught reads the tree for a list of literals
// somebody has to remember to extend, and 0055's fold showed what that
// costs: `POST /api/v1/verify/{id}` and `POST /api/v1/reject/{id}`
// became one `POST /api/v1/review/{id}`, and four schema descriptions in
// api/openapi.yaml went on saying a verification is "written by POST
// /api/v1/verify/{id}" and a rejection is "cleared by DELETE". Every
// counter in surface_test.go agreed the surface was 11 operations — the
// endpoints were gone from `paths`, which is all those checks read —
// while the prose beside them documented three addresses that answer
// 404. It is the mistake issue #275 found in comments, one file over.
//
// Here nothing has to be remembered: `paths` is the list of addresses
// that exist, so the list of addresses that do not is whatever else the
// file says. The tense is what tells them apart — the four stale lines
// were all in the present, and every note that records a removal was
// already in the past.
func TestContractNamesNoAddressItDoesNotHave(t *testing.T) {
	spec := readWireSpec(t)
	// A declared path matches by its static head: {path...} and {id...}
	// are wildcards, and prose spells them filled in ("/api/v1/bundle/"
	// is the root archive, "/api/v1/usage/queries/x" one entry's usage).
	prefixes := make([]string, 0, 2*len(spec.Paths))
	for path := range spec.Paths {
		head, _, _ := strings.Cut(path, "{")
		prefixes = append(prefixes, head, strings.TrimSuffix(head, "/"))
	}
	const contract = "../../api/openapi.yaml"
	content, err := os.ReadFile(contract)
	if err != nil {
		t.Fatalf("read %s: %v", contract, err)
	}
	lines := strings.Split(string(content), "\n")
	checked := 0
	for i, line := range lines {
		for _, mention := range wireAddressRe.FindAllString(line, -1) {
			address := strings.TrimRight(mention, ".,;:)`")
			if hasAnyPrefix(address, prefixes) {
				continue
			}
			checked++
			// A description wraps, so the sentence that says "gone" can
			// sit a line or two above the address it is about.
			if saysPast(lines[max(0, i-2) : i+1]) {
				continue
			}
			t.Errorf("%s:%d names %s, which the contract does not declare — "+
				"say it in the past tense if it is history, or fix the line\n\t%s",
				contract, i+1, address, strings.TrimSpace(line))
		}
	}
	if checked == 0 {
		t.Log("the contract mentions no retired address: nothing to tell apart today")
	}
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func saysPast(lines []string) bool {
	for _, line := range lines {
		lower := strings.ToLower(line)
		for _, word := range pastTense {
			if strings.Contains(lower, word) {
				return true
			}
		}
	}
	return false
}
