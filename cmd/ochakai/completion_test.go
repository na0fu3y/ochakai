package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// Guard: the hand-written completion scripts stay in sync with the real
// command set and the enum flag values. Type, status and sort values come
// from the domain package so a new enum value fails this test until every
// script (and only the scripts) is updated by hand. Sort used to be a
// hand-written subset here, and the scripts had gone a release without
// stale_after because of it.
func TestCompletionScriptsStayInSync(t *testing.T) {
	// The list main dispatches on, not a second copy of it: a copy is
	// what let sort go a release without stale_after, one paragraph up.
	admin := adminCommands
	enums := []string{"zsh bash fish"} // completion <shell>
	enums = append(enums, domain.ListSorts...)
	enums = append(enums, domain.Outcomes...) // report <outcome>
	for _, typ := range domain.Types {
		enums = append(enums, string(typ)) // --type
	}
	for _, s := range domain.Statuses {
		enums = append(enums, string(s)) // --status
	}
	// The #compdef header is what fpath-installed files are matched by;
	// without it only the source <(...) route works.
	if !strings.HasPrefix(zshCompletion, "#compdef ochakai\n") {
		t.Error("zsh script must start with '#compdef ochakai' for fpath installs")
	}
	for shell, script := range map[string]string{"zsh": zshCompletion, "bash": bashCompletion, "fish": fishCompletion} {
		for name := range clientCommands {
			if !strings.Contains(script, name) {
				t.Errorf("%s script misses client command %q", shell, name)
			}
		}
		for _, name := range admin {
			if !strings.Contains(script, name) {
				t.Errorf("%s script misses admin command %q", shell, name)
			}
		}
		for _, values := range enums {
			for v := range strings.FieldsSeq(values) {
				if !strings.Contains(script, v) {
					t.Errorf("%s script misses enum value %q", shell, v)
				}
			}
		}
		// Presence checks cannot catch a removed flag lingering in a
		// script, so removed surface is pinned by name.
		for _, gone := range []string{"--keep-root", "--dialect", "import-ossie"} {
			if strings.Contains(script, gone) {
				t.Errorf("%s script still offers removed flag/command %q", shell, gone)
			}
		}
	}
}

// commandLongFlags is the canonical long-flag surface of every client
// command — the spec all three hand-written scripts are checked against,
// both ways (a flag missing from one shell, and a flag a shell offers
// that the command does not have). Short flags (-f) are outside the
// check.
//
// It is read from the commands rather than transcribed beside them. Each
// one registers its flags inside its own body, so they exist only once
// the command has run: "-h" runs it precisely that far — every command
// parses before it acts — and leaves its FlagSet in commandFlagSets,
// the same probe docs/cli.md is rendered by. A hand-copied list is what
// let --source live on `search` for two releases while the table and all
// three scripts agreed it did not exist.
func commandLongFlags(t *testing.T) map[string][]string {
	t.Helper()
	long := map[string][]string{}
	for name, names := range commandFlags(t) {
		for _, f := range names {
			if len(f) > 1 { // a one-letter name is a short flag (-f)
				long[name] = append(long[name], f)
			}
		}
	}
	return long
}

// commandFlags is every flag each command registers, short ones
// included. docs/surface.md counts these — a flag is a name somebody has
// to learn, whichever length it is — while the completion check above
// looks only at the long ones, which are all the scripts offer.
func commandFlags(t *testing.T) map[string][]string {
	t.Helper()
	// The --url default reads the CLI config: keep the probe off the
	// developer's own. The help every -h prints is noise here.
	t.Setenv("OCHAKAI_URL", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	prev := helpOutput
	helpOutput = io.Discard
	defer func() { helpOutput = prev }()

	flags := map[string][]string{}
	for name, cmd := range clientCommands {
		delete(commandFlagSets, name)
		if err := cmd(context.Background(), []string{"-h"}); !errors.Is(err, flag.ErrHelp) {
			t.Errorf("`ochakai %s -h` = %v, want flag.ErrHelp: a command must register and parse its flags before it does anything", name, err)
			continue
		}
		fs := commandFlagSets[name]
		if fs == nil {
			t.Errorf("%q registered no FlagSet under its own name; pass %q to newFlagSet/newBareFlagSet", name, name)
			continue
		}
		var names []string
		fs.VisitAll(func(f *flag.Flag) { names = append(names, f.Name) })
		flags[name] = names
	}
	return flags
}

// caseArms parses the "label) ... ;;" arms of a shell case statement,
// mapping every |-alternative of each label to the arm's text.
func caseArms(script string) map[string]string {
	arms := map[string]string{}
	label := regexp.MustCompile(`(?m)^\s*([a-z|_-]+)\)`)
	for _, loc := range label.FindAllStringSubmatchIndex(script, -1) {
		body := script[loc[1]:]
		if end := strings.Index(body, ";;"); end >= 0 {
			body = body[:end]
		}
		for name := range strings.SplitSeq(script[loc[2]:loc[3]], "|") {
			arms[name] += body
		}
	}
	return arms
}

// armFlags extracts the long flags each command's case arm offers.
func armFlags(arms map[string]string, flagPattern string) map[string]map[string]bool {
	re := regexp.MustCompile(flagPattern)
	flags := map[string]map[string]bool{}
	for cmd, body := range arms {
		flags[cmd] = map[string]bool{}
		for _, m := range re.FindAllStringSubmatch(body, -1) {
			flags[cmd][m[1]] = true
		}
	}
	return flags
}

// fishFlags maps each command to the long flags declared for it: every
// `complete ... -n '__fish_seen_subcommand_from a b c' ... -l flag` line
// attributes the flag to each listed command.
func fishFlags(script string) map[string]map[string]bool {
	cond := regexp.MustCompile(`__fish_seen_subcommand_from ([a-z -]+)'`)
	long := regexp.MustCompile(`-l ([a-z-]+)`)
	flags := map[string]map[string]bool{}
	for line := range strings.SplitSeq(script, "\n") {
		c, l := cond.FindStringSubmatch(line), long.FindStringSubmatch(line)
		if c == nil || l == nil {
			continue
		}
		for cmd := range strings.FieldsSeq(c[1]) {
			if flags[cmd] == nil {
				flags[cmd] = map[string]bool{}
			}
			flags[cmd][l[1]] = true
		}
	}
	return flags
}

// Guard: each shell script offers exactly the long flags each command
// registers — per command, both directions — so a flag added to a
// command (or to one script) cannot silently miss the other shells.
func TestCompletionFlagsStayInSyncAcrossShells(t *testing.T) {
	byShell := map[string]map[string]map[string]bool{
		// zsh spells a flag "--name[help]", bash lists them in opts="...".
		"zsh":  armFlags(caseArms(zshCompletion), `--([a-z-]+)\[`),
		"bash": armFlags(caseArms(bashCompletion), `--([a-z-]+)`),
		"fish": fishFlags(fishCompletion),
	}
	longFlags := commandLongFlags(t)
	for shell, byCmd := range byShell {
		for cmd, want := range longFlags {
			for _, f := range want {
				if !byCmd[cmd][f] {
					t.Errorf("%s script misses --%s for %q", shell, f, cmd)
				}
			}
			for f := range byCmd[cmd] {
				if !slices.Contains(want, f) {
					t.Errorf("%s script offers --%s for %q, which that command does not have", shell, f, cmd)
				}
			}
		}
	}
}

func TestCompletionCommand(t *testing.T) {
	ctx := context.Background()
	for _, shell := range []string{"zsh", "bash", "fish"} {
		if err := cmdCompletion(ctx, []string{shell}); err != nil {
			t.Errorf("completion %s: %v", shell, err)
		}
	}
	if err := cmdCompletion(ctx, []string{"powershell"}); err == nil {
		t.Error("unknown shell accepted")
	}
	if err := cmdCompletion(ctx, nil); err == nil {
		t.Error("missing shell accepted")
	}
}
