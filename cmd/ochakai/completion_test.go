package main

import (
	"context"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// Guard: the hand-written completion scripts stay in sync with the real
// command set and the enum flag values. Type and status values come from
// the domain package so a new enum value fails this test until every
// script (and only the scripts) is updated by hand.
func TestCompletionScriptsStayInSync(t *testing.T) {
	admin := []string{"serve", "serve-ui", "version"}
	enums := []string{
		"zsh bash fish", // completion <shell>
		"verified_at",   // --sort
	}
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
			for _, v := range strings.Fields(values) {
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
