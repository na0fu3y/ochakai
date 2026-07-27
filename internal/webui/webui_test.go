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

// A PUT is a full replacement, so a writable field missing from a payload
// is a field erased. That failure is silent — the save succeeds and the
// value is simply gone — and it shipped twice: design doc 0036 §4 records
// resource vanishing when a status was changed from the detail view, and
// again when an entry was saved from the editor, which had no resource
// field at all.
//
// One list, WRITABLE, is the fix; this guard is what keeps a new envelope
// field from being added to the server and forgotten here.
func TestEveryWritableFieldReachesBothSavePaths(t *testing.T) {
	page := string(Index)
	i := strings.Index(page, "const WRITABLE = [")
	if i < 0 {
		t.Fatal("WRITABLE is gone; the writable-field list moved without updating this guard")
	}
	list := page[i:]
	if end := strings.Index(list, "];"); end >= 0 {
		list = list[:end]
	}
	for _, want := range []string{
		"'resource'", "'sources'", "'usage_window'", "'stale_after'",
		"'runtime'", "'parameters'", "'computation'", "'executor'", "'attester'",
	} {
		if !strings.Contains(list, want) {
			t.Errorf("WRITABLE is missing %s, so a status change erases it:\n%s", want, list)
		}
	}
	// The editor builds its own payload rather than reading WRITABLE (it
	// reads form controls), so each field has to appear there too.
	j := strings.Index(page, "const payload = {")
	if j < 0 {
		t.Fatal("the editor's payload builder moved without updating this guard")
	}
	payload := page[j:]
	if end := strings.Index(payload, "\n    };"); end >= 0 {
		payload = payload[:end]
	}
	for _, want := range []string{
		"resource:", "sources:", "usage_window:", "stale_after:",
		"runtime:", "parameters:", "computation:", "executor:", "attester:",
	} {
		if !strings.Contains(payload, want) {
			t.Errorf("the editor payload is missing %s, so saving from the form erases it:\n%s", want, payload)
		}
	}
}

// A stored source carries only the keys it has (JSON omitempty), so a
// loaded row can lack id, title and author entirely. The save path trims
// every one of those to tell an abandoned blank row from a half-filled
// one — and on a source stored with only a resource, clearing that
// resource in the editor made the walk reach the missing keys: the click
// handler threw, no banner appeared, and the save died silently instead
// of saying "Source N needs a resource". The editor therefore fills in
// string defaults when it copies entry.sources into the form's backing
// array (usage_count stays absent — absent and 0 are different claims).
func TestLoadedSourcesGetStringDefaults(t *testing.T) {
	page := string(Index)
	i := strings.Index(page, "const sources = (entry.sources || [])")
	if i < 0 {
		t.Fatal("the editor's sources initializer moved without updating this guard")
	}
	init := page[i:]
	if end := strings.Index(init, "));"); end >= 0 {
		init = init[:end]
	}
	for _, want := range []string{"resource: ''", "id: ''", "title: ''", "author: ''", "last_modified: ''"} {
		if !strings.Contains(init, want) {
			t.Errorf("loaded sources get no default for %s, so the save path can trim undefined:\n%s", want, init)
		}
	}
	if !strings.Contains(init, "...s") {
		t.Errorf("the defaults no longer merge under the stored source, so stored values would be lost:\n%s", init)
	}
	if strings.Contains(init, "usage_count: ") {
		t.Errorf("usage_count has a default; absent and 0 are different claims (OKF SPEC §5.1):\n%s", init)
	}
}

// Date inputs were left out of the input rule, so stale_after arrived with
// the UA's own font, padding and intrinsic width — and, with no
// color-scheme declared, a white calendar popup on a dark page. Both are
// easy to reintroduce by editing the selector list, and both look like a
// bug in the form rather than in the stylesheet.
func TestDateInputsAreStyledLikeEveryOtherField(t *testing.T) {
	page := string(Index)
	if strings.Count(page, "input[type=date]") < 2 {
		t.Error("input[type=date] is not in both the base and the touch-size input rules")
	}
	if !strings.Contains(page, "color-scheme:") {
		t.Error("no color-scheme is declared, so UA-drawn controls ignore the dark theme")
	}
}

// ochakai records the Attested Computation contract and never acts on it:
// no running the executor, no checking a receipt, no fetching the attester
// (design docs 0001, 0036 §3.6, §5). The curation surface is where a
// human reads what the product promises, so the promise has to be the
// narrow one.
func TestContractIsPresentedAsRecordedNotExecuted(t *testing.T) {
	page := string(Index)
	if !strings.Contains(page, "never runs, checks, or fetches") {
		t.Error("the contract sections no longer say ochakai does not run them")
	}
	if !strings.Contains(page, "never fetches or checks them") {
		t.Error("the sources hint no longer says ochakai does not fetch a source's resource")
	}
}

// Every key OKF v0.2 defines is an envelope field (design doc 0036 §2).
// The page must read them from the entry, not from attrs, or it renders
// nothing for entries written after the promotion.
func TestOKFFamiliesAreReadFromTheEnvelope(t *testing.T) {
	page := string(Index)
	for _, want := range []string{"entry.sources", "entry.usage_window", "entry.runtime", "entry.executor", "entry.attester"} {
		if !strings.Contains(page, want) {
			t.Errorf("the detail view does not read %s", want)
		}
	}
	if strings.Contains(page, "attrs.sources") || strings.Contains(page, `attrs['sources']`) {
		t.Error("the page still reads sources out of attrs; it is an envelope field now")
	}
}

// "Stale" already meant two different things on this page — the
// verification-age feed and a draft nobody searches for — before
// stale_after made a third (design doc 0037). The feed variable was
// renamed to match its own label; reintroducing the old name would put
// two unrelated queues behind one word again.
func TestFeedNamesDoNotCollideOnStale(t *testing.T) {
	page := string(Index)
	if strings.Contains(page, "staleFeed") {
		t.Error("staleFeed is back; the verification-age feed is ageFeed, and stale_after's is expiredFeed")
	}
	for _, want := range []string{"ageFeed", "expiredFeed", "'stale_after'"} {
		if !strings.Contains(page, want) {
			t.Errorf("page does not contain %s", want)
		}
	}
	// The three feeds are listing modes, so each must be reachable by its
	// own route — the filter bar was once the only way in (0025 §6).
	for _, route := range []string{"'verification-age'", "'reported-wrong'", "'stale'", "'cites'"} {
		if !strings.Contains(page, route) {
			t.Errorf("route %s is gone", route)
		}
	}
}

// The stale feed is the one queue verifying does not empty: the date is
// the writer's declaration, so it clears by editing (design doc 0037
// §2.2). A reviewer who does not know that will verify entries and watch
// the queue stay full, so the page has to say it.
func TestStaleFeedSaysVerifyingDoesNotClearIt(t *testing.T) {
	page := string(Index)
	if !strings.Contains(page, "Verifying does <em>not</em> clear this one") {
		t.Error("the stale feed banner no longer explains that verifying does not empty it")
	}
}
