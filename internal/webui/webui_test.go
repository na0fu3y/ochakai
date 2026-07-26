package webui

import (
	"strings"
	"testing"
)

// The page is plain JavaScript with no build step and no test runner, so
// the few invariants worth pinning are pinned by reading the source.
//
// Provenance rendering is one of them: an entry written by a person
// through an embedded application carries the delegating caller in
// actor.via (design doc 0027), and dropping it here would make that write
// indistinguishable from one the person made directly — on the very
// surface where a human decides whether to trust it.
func TestActorRenderingKeepsDelegation(t *testing.T) {
	page := string(Index)
	i := strings.Index(page, "function actorStr(")
	if i < 0 {
		t.Fatal("actorStr is gone; provenance rendering moved without updating this guard")
	}
	body := page[i:]
	if end := strings.Index(body, "\n}"); end >= 0 {
		body = body[:end]
	}
	if !strings.Contains(body, "a.via") {
		t.Errorf("actorStr drops the delegating caller:\n%s", body)
	}
}

// The re-verification feed is the exit design doc 0025 §6 built for the
// review loop, and it was reachable only by opening the search filter
// bar and knowing which chip meant it. Both feeds have their own routes
// now, and the review queue links them; a rename that drops the routes
// would quietly hide the queues again.
func TestFeedsAreLinkableFromTheReviewLoop(t *testing.T) {
	page := string(Index)
	for _, want := range []string{
		"'reported-wrong'",               // the route the links point at
		"'verification-age'",             // its verification-age sibling
		`href="#/search/reported-wrong"`, // linked, not only routable
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page does not contain %s", want)
		}
	}
	// "Review" is the draft queue in the top nav; a chip labelled with the
	// same word for a different queue is the one naming collision here.
	if strings.Contains(page, ">needs review<") {
		t.Error("the failed-report chip is labelled \"needs review\" again, colliding with the Review tab")
	}
}
