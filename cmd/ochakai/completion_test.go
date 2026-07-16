package main

import (
	"context"
	"strings"
	"testing"
)

// Guard: the hand-written completion scripts stay in sync with the real
// command set and the enum flag values.
func TestCompletionScriptsStayInSync(t *testing.T) {
	admin := []string{"serve", "import-ossie", "export-okf", "version"}
	enums := []string{
		"metric query insight term table", // --type
		"draft verified deprecated",       // --status
		"bigquery ansi",                   // --dialect
		"zsh bash fish",                   // completion <shell>
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
