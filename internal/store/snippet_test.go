package store

import (
	"strings"
	"testing"
)

func TestSnippetFor(t *testing.T) {
	for _, tc := range []struct {
		name  string
		body  string
		frags []string
		want  string // "" means: no passage to show
		check func(t *testing.T, got string)
	}{{
		name:  "no fragment in the body is no snippet",
		body:  "受注は支払い確定時点で数える。",
		frags: []string{"原価"},
		want:  "",
	}, {
		name:  "an empty body is no snippet",
		frags: []string{"売上"},
		want:  "",
	}, {
		name:  "a short body comes back whole, with no ellipsis",
		body:  "八月は売上が落ちる。",
		frags: []string{"売上"},
		want:  "八月は売上が落ちる。",
	}, {
		name:  "case is ignored, as the scorer's own name test ignores it",
		body:  "Revenue is seasonal.",
		frags: []string{"revenue"},
		want:  "Revenue is seasonal.",
	}, {
		name:  "line breaks collapse: a hit is one row in a list",
		body:  "見出し\n\n売上の話。\n",
		frags: []string{"売上"},
		want:  "見出し 売上の話。",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			got := snippetFor(tc.body, tc.frags)
			if got != tc.want {
				t.Errorf("snippetFor() = %q, want %q", got, tc.want)
			}
		})
	}
}

// A long body is cut to a window around the match, marked where it was
// cut, and the match itself is inside what comes back — a passage that
// does not contain the word it was chosen for explains nothing.
func TestSnippetForWindowsALongBody(t *testing.T) {
	body := strings.Repeat("この文書は長い。", 40) + "八月の売上は季節性で落ちる。" +
		strings.Repeat("その後も長い。", 40)
	got := snippetFor(body, []string{"売上"})
	if !strings.Contains(got, "売上") {
		t.Fatalf("the window dropped the match it was chosen for: %q", got)
	}
	if n := len([]rune(got)); n > snippetWindow+4 { // +4: the two ellipses and slack
		t.Errorf("snippet is %d runes, over the %d window: %q", n, snippetWindow, got)
	}
	if !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") {
		t.Errorf("a cut passage says so at both ends: %q", got)
	}
}

// The earliest match wins, whichever fragment found it: a question's
// terms are scattered through a body, and the passage a reader wants to
// start at is the first one, not whichever fragment the splitter happened
// to put first.
func TestSnippetForTakesTheEarliestMatch(t *testing.T) {
	body := "原価の話が先にある。" + strings.Repeat("あ", 200) + "売上の話は後ろ。"
	got := snippetFor(body, []string{"売上", "原価"})
	if !strings.Contains(got, "原価") {
		t.Errorf("snippet = %q, want the passage around the earlier term", got)
	}
}
