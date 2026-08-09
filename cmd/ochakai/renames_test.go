package main

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/mcpserver"
)

// serverCommands are the words main dispatches outside clientCommands.
// They are how the binary is run rather than things it knows, which is
// why the surface count leaves them out — but a rename can still point
// at one, so the check below has to know they exist.
var serverCommands = []string{"serve", "serve-ui", "version", "help"}

// retiredName is the two lists read as one. MCP tools and CLI commands
// are different surfaces with different costs, and design doc 0088 gives
// them the same window, so the rule about how long that window lasts is
// checked in one place.
type retiredName struct {
	surface, old, instead, release string
}

func retiredNames() []retiredName {
	var all []retiredName
	for _, r := range mcpserver.RetiredToolNames {
		all = append(all, retiredName{"MCP", r.Old, r.Instead, r.Release})
	}
	for _, r := range retiredCommands {
		all = append(all, retiredName{"CLI", r.Old, r.Instead, r.Release})
	}
	return all
}

// The window is one release, and this is what makes that true.
//
// An entry is written during the development of the release that renames
// something, so its Release is either unreleased (newer than anything in
// the changelog) or the newest release there. It stops being either the
// moment a *later* release is cut — and the release PR is where this
// fails, which is the right place: cutting a release is a reviewed PR
// (CONTRIBUTING.md, Releases), and deleting last release's retired names
// is one line of it.
//
// Without this, a window is a promise, and the failure mode of a promise
// like this one is not that it is broken but that it is never closed:
// every alias ever added still answering, a shadow vocabulary nothing
// lists and nobody can find out about (design doc 0088 §4).
func TestARetiredNameLastsOneRelease(t *testing.T) {
	newest := versionIn(t, "../../CHANGELOG.md",
		`(?m)^## \[(\d+\.\d+\.\d+)\] - \d{4}-\d{2}-\d{2}$`)
	for _, r := range retiredNames() {
		cmp, err := compareVersions(r.release, newest)
		if err != nil {
			t.Errorf("%s %q carries Release %q: %v", r.surface, r.old, r.release, err)
			continue
		}
		if cmp < 0 {
			t.Errorf("%s %q was retired in %s and the newest release is %s — its release has been "+
				"cut, so delete the entry (design doc 0088: the window is one release)",
				r.surface, r.old, r.release, newest)
		}
	}
}

// A retired name that is also a live name is a name that answers as
// something else. On the CLI that is a command silently running a
// different one; the MCP half of the same check is in
// TestRetiredToolNamesPointAtToolsThatExist, where the live list is the
// server's own tools/list rather than a map in this package.
func TestARetiredNameIsNotAlsoLive(t *testing.T) {
	live := make(map[string]bool, len(clientCommands)+len(serverCommands))
	for name := range clientCommands {
		live[name] = true
	}
	for _, name := range serverCommands {
		live[name] = true
	}
	seen := map[string]bool{}
	for _, r := range retiredCommands {
		if live[r.Old] {
			t.Errorf("%q is retired and also a command: a live name is not a retired one", r.Old)
		}
		if !live[r.Instead] {
			t.Errorf("%q forwards to %q, which nothing dispatches", r.Old, r.Instead)
		}
		if seen[r.Old] {
			t.Errorf("%q is retired twice: two answers to one name", r.Old)
		}
		seen[r.Old] = true
	}
}

// The forwarding itself, on a fixture rather than a live entry — the
// list is empty most of the time, and a mechanism that only runs in the
// release after somebody needs it is one nobody can rely on the day they
// do.
func TestARetiredCommandRunsAndSaysSo(t *testing.T) {
	retiredCommands = []retiredCommand{{Old: "knowledge", Instead: "get", Release: "9.9.9"}}
	t.Cleanup(func() { retiredCommands = nil })

	var notice bytes.Buffer
	if got := commandAfterRenames("knowledge", &notice); got != "get" {
		t.Errorf("a retired name resolved to %q, not the command that replaced it", got)
	}
	for _, want := range []string{"knowledge", "get", "9.9.9", "next release"} {
		if !strings.Contains(notice.String(), want) {
			t.Errorf("the notice %q does not name %q", notice.String(), want)
		}
	}

	notice.Reset()
	if got := commandAfterRenames("search", &notice); got != "search" {
		t.Errorf("a live command resolved to %q", got)
	}
	if got := commandAfterRenames("wisdom", &notice); got != "wisdom" {
		t.Errorf("an unknown command resolved to %q — main reports it, this does not rewrite it", got)
	}
	if notice.Len() != 0 {
		t.Errorf("a command nobody retired printed %q", notice.String())
	}
}

// The retired names are not offered anywhere a reader looks: not in the
// usage text, not in docs/cli.md, not in the completion scripts. That is
// the point of the window — it keeps a name working without teaching it
// — and the three places it could leak into all read clientCommands,
// which retired names are deliberately not in.
func TestARetiredNameIsNotAdvertised(t *testing.T) {
	for _, r := range retiredCommands {
		if _, ok := clientCommands[r.Old]; ok {
			t.Errorf("%q is in clientCommands: usage, docs/cli.md and completion would all offer it", r.Old)
		}
	}
	for _, r := range mcpserver.RetiredToolNames {
		if strings.Contains(usageText(t), r.Old) {
			t.Errorf("the usage text names the retired MCP tool %q", r.Old)
		}
	}
}

func usageText(t *testing.T) string {
	t.Helper()
	var b bytes.Buffer
	usage(&b)
	return b.String()
}

// compareVersions orders two "x.y.z" strings, reporting -1, 0 or 1.
func compareVersions(a, b string) (int, error) {
	pa, err := parseVersion(a)
	if err != nil {
		return 0, err
	}
	pb, err := parseVersion(b)
	if err != nil {
		return 0, err
	}
	for i := range pa {
		switch {
		case pa[i] < pb[i]:
			return -1, nil
		case pa[i] > pb[i]:
			return 1, nil
		}
	}
	return 0, nil
}

func parseVersion(v string) ([3]int, error) {
	var out [3]int
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, fmt.Errorf("%q is not a released version of the form x.y.z", v)
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, fmt.Errorf("%q is not a released version of the form x.y.z", v)
		}
		out[i] = n
	}
	return out, nil
}
