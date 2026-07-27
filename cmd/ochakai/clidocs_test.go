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
take their configuration from the environment rather than from flags — the
README's [Configuration](../README.md#configuration) table lists it.

The same text is what ` + "`ochakai <command> -h`" + ` prints, so nothing here can
disagree with the binary you are running.

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

	for _, name := range slices.Sorted(maps.Keys(clientCommands)) {
		fmt.Fprintf(&b, "\n## ochakai %s\n\n```\n%s```\n", name, helpFor(t, name))
	}
	return b.String()
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
