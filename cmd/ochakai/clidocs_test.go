package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"
)

// docs/cli.md is the CLI's own help, rendered. The help is generated from
// the domain model where the values come from it (--type, --status), so a
// hand-written copy would be a third spelling of something already spelled
// twice — this keeps the copy derived and fails when it is stale, the way
// the OpenAPI contract test does for the REST surface (design doc 0035).
const cliDocsPath = "../../docs/cli.md"

var updateCLIDocs = flag.Bool("update", false, "rewrite docs/cli.md from the CLI's own help")

const cliDocsHeader = `<!-- Generated from the CLI's own help. Do not edit by hand:
     go test ./cmd/ochakai -run TestCLIReferenceIsCurrent -update -->

# CLI reference

` + "`ochakai`" + ` is a thin client of the REST API — every client command
below is one HTTP call against the server named by ` + "`--url`" + `, ` + "`$OCHAKAI_URL`" + `,
or the ` + "`ochakai use`" + ` selection, in that order (design docs
[0004](design/0004-cli.md), [0007](design/0007-api-only-cli.md)). ` + "`serve`" + `
and ` + "`serve-ui`" + ` are the exception: they are the deployed services, and they
take their configuration from the environment rather than from flags —
[Requirements and configuration](configuration.md) (Japanese) lists it.

Every section is what ` + "`ochakai <command> -h`" + ` prints, so nothing here
can disagree with the binary you are running — except ` + "`search`" + ` and
` + "`list`" + `, which narrow the same way (design doc
[0062](design/0062-a-listing-is-not-a-search.md)) and point at
[Shared filters](#shared-filters) instead of repeating it; ` + "`-h`" + ` on
either command still prints its own copy in full.

## Commands

`

// TestCLIReferenceIsCurrent regenerates docs/cli.md and compares it with
// the checked-in copy.
func TestCLIReferenceIsCurrent(t *testing.T) {
	got := renderCLIDocs(t)
	if *updateCLIDocs {
		if err := os.WriteFile(cliDocsPath, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(cliDocsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Errorf("docs/cli.md no longer matches the CLI's help; regenerate it:\n"+
			"\tgo test ./cmd/ochakai -run TestCLIReferenceIsCurrent -update\n"+
			"first difference at byte %d", firstDiff(string(want), got))
	}
}

func firstDiff(a, b string) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}

// sharedFilterFlags are the flags `ochakai search -h` and `ochakai list
// -h` print with identical text, because both narrow the same way
// (design doc 0062): the nine `addSearchFilters` registers, plus --json
// and --url. docs/cli.md renders them once, under their own heading, and
// has each command's section point at it instead of repeating the words
// — --limit stays out, since its default differs between a ranking and
// a page.
var sharedFilterFlags = []string{
	"fm", "json", "links-to", "prefix", "rejected",
	"source", "status", "tag", "trust", "type", "url",
}

func renderCLIDocs(t *testing.T) string {
	t.Helper()
	// Without this the rendered --url default is whatever server this
	// machine happens to have selected, and the file differs per developer.
	t.Setenv("OCHAKAI_URL", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var b strings.Builder
	b.WriteString(cliDocsHeader)

	var index strings.Builder
	usage(&index)
	fmt.Fprintf(&b, "```\n%s```\n", index.String())

	names := slices.Sorted(maps.Keys(clientCommands))
	help := make(map[string]string, len(names))
	for _, name := range names {
		help[name] = helpFor(t, name)
	}
	shared := sharedFilterBlock(t, help["search"], help["list"])

	for _, name := range names {
		if name == "list" {
			fmt.Fprintf(&b, "\n## Shared filters\n\n"+
				"What `ochakai search -h` and `ochakai list -h` share (design doc\n"+
				"[0062](design/0062-a-listing-is-not-a-search.md)), once.\n\n"+
				"```\nFlags:\n%s```\n", shared)
		}
		fmt.Fprintf(&b, "\n## ochakai %s\n\n", name)
		if other, ok := sharesFiltersWith[name]; ok {
			writeFilteredHelp(t, &b, help[name], other)
			continue
		}
		fmt.Fprintf(&b, "```\n%s```\n", help[name])
	}
	return b.String()
}

// sharesFiltersWith names, for each command whose Flags: section is
// folded into Shared filters, the other command it shares them with —
// what the pointer left in its place reads.
var sharesFiltersWith = map[string]string{"search": "list", "list": "search"}

// helpSections splits one command's rendered `-h` output into the text
// through "Flags:\n" (kept as-is), the flag blocks themselves (one
// two-line block per flag, in the order flag.PrintDefaults printed
// them), and the "Examples:\n..." tail.
func helpSections(t *testing.T, help string) (preamble string, names []string, blocks map[string]string, examples string) {
	t.Helper()
	const flagsHeading = "Flags:\n"
	fi := strings.Index(help, flagsHeading)
	if fi < 0 {
		t.Fatalf("help has no %q section:\n%s", flagsHeading, help)
	}
	preamble = help[:fi+len(flagsHeading)]

	const examplesSep = "\n\nExamples:\n"
	rest := help[fi+len(flagsHeading):]
	ei := strings.Index(rest, examplesSep)
	if ei < 0 {
		t.Fatalf("help has no %q section:\n%s", examplesSep, help)
	}
	examples = "Examples:\n" + rest[ei+len(examplesSep):]

	lines := strings.Split(rest[:ei], "\n")
	if len(lines)%2 != 0 {
		t.Fatalf("flag help has an odd number of lines — a usage string must have wrapped:\n%s", rest[:ei])
	}
	blocks = make(map[string]string, len(lines)/2)
	for i := 0; i < len(lines); i += 2 {
		name, _, _ := strings.Cut(strings.TrimPrefix(lines[i], "  -"), " ")
		blocks[name] = lines[i] + "\n" + lines[i+1] + "\n"
		names = append(names, name)
	}
	return preamble, names, blocks, examples
}

// sharedFilterBlock renders sharedFilterFlags once, checking along the
// way that `search` and `list` actually agree on each one word for
// word. The moment they diverge — a different default, say — folding
// them into one block would make the file say something the binary
// doesn't, which is exactly what this generator exists to prevent.
func sharedFilterBlock(t *testing.T, searchHelp, listHelp string) string {
	t.Helper()
	_, _, searchBlocks, _ := helpSections(t, searchHelp)
	_, _, listBlocks, _ := helpSections(t, listHelp)
	var b strings.Builder
	for _, name := range sharedFilterFlags {
		sb, ok := searchBlocks[name]
		if !ok {
			t.Fatalf("`ochakai search -h` has no --%s to share", name)
		}
		lb, ok := listBlocks[name]
		if !ok {
			t.Fatalf("`ochakai list -h` has no --%s to share", name)
		}
		if sb != lb {
			t.Fatalf("--%s is not identical between `ochakai search -h` and `ochakai list -h`, so it cannot be folded into Shared filters:\nsearch: %q\nlist:   %q", name, sb, lb)
		}
		b.WriteString(sb)
	}
	return b.String()
}

// writeFilteredHelp renders one of search/list's own section: its
// synopsis and non-shared flags verbatim from `-h`, one line pointing at
// Shared filters in place of the ones folded there, and its examples
// verbatim — one fenced block, the same shape every other command gets.
func writeFilteredHelp(t *testing.T, b *strings.Builder, help, otherCommand string) {
	t.Helper()
	preamble, names, blocks, examples := helpSections(t, help)
	shared := make(map[string]bool, len(sharedFilterFlags))
	for _, n := range sharedFilterFlags {
		shared[n] = true
	}
	var kept strings.Builder
	for _, n := range names {
		if !shared[n] {
			kept.WriteString(blocks[n])
		}
	}
	fmt.Fprintf(b, "```\n%s%s  # shared with `ochakai %s` — see \"Shared filters\" above\n\n%s```\n",
		preamble, kept.String(), otherCommand, examples)
}

// helpFor runs one command with -h and returns what it printed.
func helpFor(t *testing.T, name string) string {
	t.Helper()
	var b bytes.Buffer
	prev := helpOutput
	helpOutput = &b
	defer func() { helpOutput = prev }()

	err := clientCommands[name](context.Background(), []string{"-h"})
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("`ochakai %s -h`: want flag.ErrHelp, got %v", name, err)
	}
	out := b.String()
	if out == "" {
		t.Fatalf("`ochakai %s -h` printed nothing", name)
	}
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out
}
