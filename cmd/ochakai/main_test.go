package main

import (
	"os"
	"strings"
	"testing"
)

// Guard: the help text addresses entries by path (design doc 0017). The
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

// Guard: every `ochakai create` we ship names the entry's id. The id is an
// argument and an OKF document carries none (design doc 0017), so a
// flags-only invocation is one an agent following our own instructions
// cannot complete — it fails on the server with a format complaint about
// an id it was never told to pass.
func TestShippedCreateExamplesPassAnID(t *testing.T) {
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
			_, rest, found := strings.Cut(line, "ochakai create")
			if !found {
				continue
			}
			rest = strings.TrimLeft(rest, " ")
			// `ochakai create -h` prints the help that explains the id; it is
			// the one invocation that needs none.
			if strings.HasPrefix(rest, "-h") || strings.HasPrefix(rest, "--help") {
				continue
			}
			if rest == "" || strings.HasPrefix(rest, "-") {
				t.Errorf("%s documents `ochakai create` without an id: %s", f, strings.TrimSpace(line))
			}
		}
	}
}

// Guard: instructions we ship to agents name types in the 0023 vocabulary.
// Free types mean a stale name is not rejected — an agent following the old
// wording silently grows a "query"-typed entry beside the Golden Queries,
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
