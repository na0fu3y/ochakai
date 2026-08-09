package main

import (
	"fmt"
	"io"
)

// retiredCommand is a CLI command name a release renamed away from,
// together with the name that runs instead (design doc 0088). It is the
// CLI's half of the same window MCP gets in internal/mcpserver: the old
// spelling keeps working for the release that renamed it, and no longer.
//
// The fields are the MCP list's fields under different names for the same
// reason the two lists are separate — a tool is not a command, and the one
// test that enforces the release rule reads both.
type retiredCommand struct {
	// Old is the command that no longer exists.
	Old string
	// Instead is the command that runs. It has to be one clientCommands
	// or main's switch actually dispatches.
	Instead string
	// Release is the release that renamed it, and the only release this
	// entry survives.
	Release string
}

// retiredCommands is empty between renames. Unlike the MCP list it costs
// nothing even when it is not — a command name is a word in a script, not
// a schema in a context window — but it expires on the same schedule,
// because a shadow vocabulary the help does not list is exactly what
// nobody can find out about later.
var retiredCommands []retiredCommand

// commandAfterRenames maps a command a user typed to the one that runs,
// printing the notice when they are not the same word.
//
// A live command always wins: the caller looks itself up before asking
// here, and TestARetiredNameIsNotAlsoLive keeps the tables from
// disagreeing about a name in the first place.
func commandAfterRenames(name string, w io.Writer) string {
	for _, r := range retiredCommands {
		if r.Old != name {
			continue
		}
		fmt.Fprintf(w, "ochakai: %q was renamed to %q in %s, and %q ran. "+
			"The old name stops working in the next release.\n", r.Old, r.Instead, r.Release, r.Instead)
		return r.Instead
	}
	return name
}
