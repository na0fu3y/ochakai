package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// Guard: the help text addresses concepts by path (design doc 0017). The
// two-part "<type>/<id>" form predates it and no longer parses as two
// arguments — an id is one path.
func TestUsageAddressesEntriesByPath(t *testing.T) {
	var b strings.Builder
	usage(&b)
	// "type[/prefix]" is the same claim in browse's clothing: a prefix is
	// a path prefix, and its first segment is a directory, not a type.
	for _, stale := range []string{"<type>/<id>", "type[/"} {
		if strings.Contains(b.String(), stale) {
			t.Errorf("usage() still documents the pre-0017 address form %q:\n%s", stale, b.String())
		}
	}
}

// Guard: every `ochakai put` we ship names the concept's id. The id is an
// argument and an OKF document carries none (design doc 0017), so a
// flags-only invocation is one an agent following our own instructions
// cannot complete — it fails on the server with a format complaint about
// an id it was never told to pass.
func TestShippedPutExamplesPassAnID(t *testing.T) {
	for _, f := range []string{
		"../../README.md",
		"../../CONTRIBUTING.md",
		"../../examples/claude-code/CLAUDE.md",
		"../../examples/claude-code/hooks/ochakai-write-back.sh",
		"../../docs/guides/golden-query-canary.md",
	} {
		content, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		for _, line := range strings.Split(string(content), "\n") {
			_, rest, found := strings.Cut(line, "ochakai put")
			if !found {
				continue
			}
			rest = strings.TrimLeft(rest, " ")
			// `ochakai put -h` prints the help that explains the id; it is
			// the one invocation that needs none.
			if strings.HasPrefix(rest, "-h") || strings.HasPrefix(rest, "--help") {
				continue
			}
			if rest == "" || strings.HasPrefix(rest, "-") {
				t.Errorf("%s documents `ochakai put` without an id: %s", f, strings.TrimSpace(line))
			}
		}
	}
}

// Guard: instructions we ship to agents name types in the 0023 vocabulary.
// Free types mean a stale name is not rejected — an agent following the old
// wording silently grows a "query"-typed concept beside the Golden Queries,
// so these files are the only thing standing between the vocabulary and
// drift.
func TestShippedInstructionsUseCurrentTypeVocabulary(t *testing.T) {
	for _, f := range []string{
		"../../examples/claude-code/hooks/ochakai-write-back.sh",
		"../../docs/guides/golden-query-canary.md",
	} {
		content, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		s := string(content)
		for _, stale := range []string{"type: query", "type: insight", "type=query", "type=insight"} {
			if strings.Contains(s, stale) {
				t.Errorf("%s uses the pre-0023 type name %q", f, stale)
			}
		}
		// Design doc 0038 retired two spellings. They stay valid to write,
		// which is exactly why a doc can keep recommending one for years
		// without anything failing — so the retired spellings are pinned
		// here in the forms that tell a reader to use the type.
		for _, retired := range []string{"type: Golden Query", "type=Golden%20Query",
			"--type 'Golden Query'", "type: Semantic Model", "--type 'Semantic Model'"} {
			if strings.Contains(s, retired) {
				t.Errorf("%s still instructs the reader to use the retired type: %q", f, retired)
			}
		}
		if !strings.Contains(s, "Attested Computation") {
			t.Errorf("%s never names the Attested Computation type", f)
		}
	}
}

// shippedInvocation finds an invocation we ship — "ochakai search", the
// command being any word — and its offset, so the flags after it are
// attributed to it and not to the sentence before.
var shippedInvocation = regexp.MustCompile(`ochakai ([a-z][a-z-]*)`)

// shippedLongFlag matches a long flag written as a reader would type it.
// The leading class keeps it off "---" in a frontmatter fence and off the
// tail of a longer token.
var shippedLongFlag = regexp.MustCompile(`(?:^|[^-\w])--([a-z][a-z0-9-]*)`)

// shippedInstructionFiles is everything we hand somebody that tells them
// how to invoke the CLI: the prose a person reads, the instructions an
// agent is given, and the hooks that run unattended. docs/cli.md is
// generated from the help, but the worked examples inside that help are
// hand-written strings in client.go, so a stale flag reaches a reader
// through it exactly as through the rest.
func shippedInstructionFiles(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, glob := range []string{
		"../../README.md", "../../CONTRIBUTING.md",
		"../../docs/*.md", "../../docs/guides/*.md",
		"../../deploy/*/README.md",
		"../../examples/*.md", "../../examples/*/README.md",
		"../../examples/claude-code/CLAUDE.md",
		"../../examples/claude-code/hooks/*.sh",
	} {
		matches, err := filepath.Glob(glob)
		if err != nil {
			t.Fatalf("glob %s: %v", glob, err)
		}
		out = append(out, matches...)
	}
	if len(out) == 0 {
		t.Fatal("no shipped instructions found: this check now guards nothing")
	}
	return out
}

// Guard: every flag we write beside a command is a flag that command
// has. A reader cannot tell a typo from a feature, and an agent handed
// one gets a usage error where it expected knowledge —
// examples/claude-code/CLAUDE.md told agents to run `ochakai search
// --verified true` for two releases after the flag became --trust, and
// nothing failed. The type vocabulary and `put`'s id were already
// pinned here; the flags were the third thing that could go stale in the
// same file for the same reason.
//
// Flags are read from the commands themselves (commandLongFlags), so
// this cannot be satisfied by keeping a second list up to date.
func TestShippedInstructionsNameFlagsThatExist(t *testing.T) {
	flags := commandLongFlags(t)
	for _, path := range shippedInstructionFiles(t) {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		// A shell continuation carries one invocation across two lines;
		// join them so the flags stay with their command.
		text := strings.ReplaceAll(string(content), "\\\n", " ")
		for _, line := range strings.Split(text, "\n") {
			cmds := shippedInvocation.FindAllStringSubmatchIndex(line, -1)
			if len(cmds) == 0 {
				continue
			}
			for _, m := range shippedLongFlag.FindAllStringSubmatchIndex(line, -1) {
				name := line[m[2]:m[3]]
				// The nearest invocation to the left owns this flag.
				cmd := ""
				for _, c := range cmds {
					if c[0] < m[2] {
						cmd = line[c[2]:c[3]]
					}
				}
				known, ok := flags[cmd]
				if !ok {
					continue // "ochakai serve", or prose that reads like one
				}
				if !slices.Contains(known, name) {
					t.Errorf("%s tells the reader to run `ochakai %s --%s`, which has no such flag (it has: %s)",
						path, cmd, name, strings.Join(known, ", "))
				}
			}
		}
	}
}

// Guard: what we hand somebody who is about to write a concept keeps
// frontmatter flat. OKF SPEC §4.1 puts a producer's own key beside the
// ones the spec defines, and ochakai exports them that way (design doc
// 0043) — "attrs" is an internal field name that was never on the wire.
// Two shipped files told agents to write `attrs.question`, which lands
// as a key named attrs holding a map: valid YAML, and not the question
// anyone can search for.
//
// The list is the authoring instructions alone, not every shipped file:
// the deploy guide names the pre-0.4 nested form on purpose, in the
// paragraph about importing a bundle written back then.
func TestConceptAuthoringInstructionsKeepFrontmatterFlat(t *testing.T) {
	for _, path := range []string{
		"../../README.md",
		"../../docs/knowledge.md",
		"../../docs/guides/golden-query-canary.md",
		"../../examples/README.md",
		"../../examples/golden-query.md",
		"../../examples/claude-code/CLAUDE.md",
		"../../examples/claude-code/hooks/ochakai-write-back.sh",
	} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, nested := range []string{"attrs.", "attrs:"} {
			if strings.Contains(string(content), nested) {
				t.Errorf("%s tells the reader to nest frontmatter under %q; "+
					"a producer's key is written at the top level (OKF SPEC §4.1)", path, nested)
			}
		}
	}
}
