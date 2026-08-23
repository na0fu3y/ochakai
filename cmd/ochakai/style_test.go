package main

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/apiclient"
	"github.com/na0fu3y/ochakai/internal/domain"
)

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// The rule the whole thing rests on: a styled line and a plain one are
// the same line (design doc 0111). Strip the escapes and the bytes have
// to be what a pipe would have received — every field, every separator,
// in the same order — because that is what makes the styling a rendering
// rather than a second output format nobody can grep.
func TestStyledLineStripsBackToThePlainOne(t *testing.T) {
	page := &apiclient.SearchResult{Hits: []domain.SearchHit{{
		ID:          "metrics/revenue",
		Status:      domain.StatusStable,
		Title:       "売上",
		Description: "完了した注文の合計",
		Snippet:     "…返品を差し引く…",
	}}}

	plain, _ := capture(t, func() { printHits(page, "") })

	styled = true
	t.Cleanup(func() { styled = false })
	fancy, _ := capture(t, func() { printHits(page, "") })

	if fancy == plain {
		t.Fatal("styling was on and changed nothing")
	}
	if got := ansi.ReplaceAllString(fancy, ""); got != plain {
		t.Errorf("a styled line must strip back to the plain one:\n got %q\nwant %q", got, plain)
	}

	// Where the weight goes: the address a reader copies, and the prose
	// that trails the row. Nothing between them — the status is a value
	// compared down the column, not a label.
	line := strings.TrimSuffix(fancy, "\n")
	if !strings.HasPrefix(line, ansiBold+"ochakai://metrics/revenue"+ansiReset+"\t") {
		t.Errorf("the address must be the bold field:\n%q", line)
	}
	if !strings.Contains(line, ansiDim+" — 完了した注文の合計") {
		t.Errorf("the description must be dimmed:\n%q", line)
	}
	if !strings.Contains(line, ansiDim+" · …返品を差し引く…") {
		t.Errorf("the snippet must be dimmed:\n%q", line)
	}
	if strings.Contains(line, ansiBold+"stable") || strings.Contains(line, ansiDim+"stable") {
		t.Errorf("the status carries no weight of its own:\n%q", line)
	}
}

// Styling is for an eye at a terminal. Everything else — a pipe, a
// redirect, the buffer a test reads — gets the bytes it always got, which
// is why no flag was needed to keep `ochakai search q | cut -f1` working.
func TestStylingReachesOnlyATerminalThatWantsIt(t *testing.T) {
	t.Setenv("NO_COLOR", "")

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close(); w.Close() })
	if terminalStyling(w) {
		t.Error("a pipe is not a terminal")
	}

	file, err := os.CreateTemp(t.TempDir(), "hits")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	if terminalStyling(file) {
		t.Error("a redirect to a file is not a terminal")
	}

	// A character device is the test, so the platform's own one stands in
	// for a terminal here: nothing in a CI container is attached to one.
	tty, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Skipf("no character device to stand in for a terminal: %v", err)
	}
	t.Cleanup(func() { tty.Close() })
	if !terminalStyling(tty) {
		t.Error("a character device is what a terminal is recognised by")
	}

	// The escape hatch for the person who is at a terminal and does not
	// want it: presence, not value, the way the convention is written.
	t.Setenv("NO_COLOR", "1")
	if terminalStyling(tty) {
		t.Error("NO_COLOR must turn styling off")
	}
}
