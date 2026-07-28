package webui

import (
	"slices"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
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

// The page carries its own copy of the type vocabulary — an icon per type
// and the hint under the new-entry form — because it is a static asset
// with no build step to interpolate domain.Types into. That copy has
// drifted before: when Attested Computation joined the vocabulary the
// icons gained it and the form hint did not, and neither noticed. A
// retired type is the worse direction, since free types mean the UI would
// go on recommending a spelling nothing else does, forever, without
// failing anything (design doc 0038 §4.4).
func TestTypeVocabularyMatchesDomain(t *testing.T) {
	page := string(Index)
	icons := section(t, page, "const TYPE_ICONS = {", "};")
	hint := section(t, page, "What the entry is:", "</div>")
	for _, ty := range domain.Types {
		if !strings.Contains(icons, "'"+string(ty)+"'") {
			t.Errorf("TYPE_ICONS has no icon for %q", ty)
		}
		if !strings.Contains(hint, string(ty)) {
			t.Errorf("the new-entry hint omits the recommended type %q", ty)
		}
	}
	for _, retired := range []string{"Semantic Model", "Golden Query"} {
		if strings.Contains(icons, retired) || strings.Contains(hint, retired) {
			t.Errorf("the page still offers %q, retired by design doc 0038", retired)
		}
	}
}

// A read-only deployment must not offer a control that can only ever 403:
// a button that is pressed and always fails is a lie (design doc 0040
// §2.3). The whole mechanism on this page is one CSS rule — a write
// affordance is hidden exactly when the class `write-only` reaches it, on
// the control itself or on a container.
//
// The tree's per-directory ＋ shipped visible on a read-only deployment
// because it carried the class in a *second* class attribute: HTML keeps
// the first `class` and drops the rest, so the rule never matched. Nothing
// failed, because nothing on this page was checked for read-only at all.
// This is that check: every affordance below writes through /api/v1, so
// every one of them has to be gated.
func TestWriteAffordancesAreHiddenOnAReadOnlyDeployment(t *testing.T) {
	page := string(Index)
	// The rule and the state that drives it. Renaming either without
	// renaming the class everywhere would unhide the lot.
	for _, want := range []string{
		"body.read-only .write-only { display: none !important; }",
		"Ochakai-Read-Only",
		"classList.toggle('read-only', on)",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("the read-only mechanism is gone: %q", want)
		}
	}

	// Controls that carry the class themselves. Every link into the editor
	// counts: the sidebar ＋, the tree's per-directory ＋, and the two the
	// search view offers.
	if n := strings.Count(page, `href="#/new`); n != 4 {
		t.Errorf("the page has %d literal links to the editor, not the 4 this guard knows; gate the new one and update the count", n)
	}
	for _, i := range indexesOf(page, `href="#/new`) {
		if _, tag := tagAt(t, page, i); !hidesOnReadOnly(tag) {
			t.Errorf("a link to the editor is not hidden on a read-only deployment:\n%s", tag)
		}
	}
	for _, aff := range []struct{ what, marker string }{
		{"the directory view's New entry here", `href="${newHref}"`}, // the fifth, built as a variable
		{"the status picker on an entry", `id="act-status"`},
		{"Detach on an attachment", "data-detach="},
		{"the editor's save button", `type="submit"`},
	} {
		if _, tag := tagAt(t, page, indexOf(t, page, aff.marker)); !hidesOnReadOnly(tag) {
			t.Errorf("%s is not hidden on a read-only deployment:\n%s", aff.what, tag)
		}
	}

	// Controls gated by the container they sit in: the entry's action bar
	// and the review card's. Each marker must occur once, so a second copy
	// added somewhere ungated fails here rather than shipping visible.
	for _, aff := range []struct{ what, marker, open, close string }{
		{"attaching a file", `id="att-upload"`, `<div class="toolbar write-only"`, "</div>"},
		{"Edit", `href="#/edit/`, `<span class="actions write-only">`, "</span>"},
		{"Verify", `id="act-verify"`, `<span class="actions write-only">`, "</span>"},
		{"Reject…", `id="act-reject"`, `<span class="actions write-only">`, "</span>"},
		{"Lift rejection", `id="act-unreject"`, `<span class="actions write-only">`, "</span>"},
		{"Move…", `id="act-move"`, `<span class="actions write-only">`, "</span>"},
		{"Delete…", `id="act-delete"`, `<span class="actions write-only">`, "</span>"},
		{"queue Verify", `data-act="verify"`, `<span class="actions write-only" style="margin-left:auto`, "</span>"},
		{"queue Reject", `data-act="reject"`, `<span class="actions write-only" style="margin-left:auto`, "</span>"},
	} {
		if n := strings.Count(page, aff.marker); n != 1 {
			t.Errorf("%s appears %d times; every copy needs gating, so this guard needs updating", aff.what, n)
			continue
		}
		if !strings.Contains(section(t, page, aff.open, aff.close), aff.marker) {
			t.Errorf("%s is no longer inside an action bar that hides it on a read-only deployment", aff.what)
		}
	}

	// Drag-to-move is a write affordance with no button: dropping an entry
	// on a directory calls moveEntry, which can only 403 here. The rendered
	// attribute stops the gesture being offered; the handler is what makes
	// it impossible.
	if !strings.Contains(page, `draggable="${READ_ONLY ? 'false' : 'true'}"`) {
		t.Error("tree entries are draggable unconditionally, so a read-only deployment still invites a move")
	}
	if drag := section(t, page, "tree.addEventListener('dragstart'", "\n  });"); !strings.Contains(drag, "if (READ_ONLY)") {
		t.Errorf("the dragstart handler does not check READ_ONLY, so a drag-to-move can still start:\n%s", drag)
	}
}

// The ＋ above was hidden in review and still shipped visible, because the
// class it was given was the second `class` attribute on the tag and the
// browser had already taken the first. Nothing about that reads as wrong
// in a diff, and it silences any guard written in terms of the class.
func TestNoTagDeclaresClassTwice(t *testing.T) {
	page := string(Index)
	seen := map[int]bool{}
	for _, i := range indexesOf(page, "class=") {
		start, tag := tagAt(t, page, i)
		if seen[start] {
			continue
		}
		seen[start] = true
		if n := strings.Count(tag, "class="); n > 1 {
			t.Errorf("this tag declares class %d times; the browser keeps the first and drops the rest:\n%s", n, tag)
		}
	}
}

// hidesOnReadOnly reports whether a tag carries write-only in the class
// the browser actually applies — the first class attribute, since a
// second one is dropped rather than merged.
func hidesOnReadOnly(tag string) bool {
	i := strings.Index(tag, "class=")
	if i < 0 {
		return false
	}
	rest := tag[i+len("class="):]
	if rest == "" || (rest[0] != '"' && rest[0] != '\'') {
		return false
	}
	end := strings.IndexByte(rest[1:], rest[0])
	if end < 0 {
		return false
	}
	return slices.Contains(strings.Fields(rest[1:1+end]), "write-only")
}

// tagAt returns the tag enclosing offset i — from the "<" that opens it to
// the ">" that closes it — with the offset it starts at.
func tagAt(t *testing.T, page string, i int) (int, string) {
	t.Helper()
	start := strings.LastIndex(page[:i], "<")
	end := strings.Index(page[i:], ">")
	if start < 0 || end < 0 {
		t.Fatalf("no tag encloses offset %d", i)
	}
	return start, page[start : i+end+1]
}

// indexOf finds the first marker, naming it when it is gone.
func indexOf(t *testing.T, page, marker string) int {
	t.Helper()
	i := strings.Index(page, marker)
	if i < 0 {
		t.Fatalf("%q is gone from the page; the guard needs updating", marker)
	}
	return i
}

func indexesOf(page, marker string) []int {
	var out []int
	for i := 0; ; {
		j := strings.Index(page[i:], marker)
		if j < 0 {
			return out
		}
		out = append(out, i+j)
		i += j + len(marker)
	}
}

// section returns the text between the first open and the next close, so a
// guard reads one construct rather than the whole page.
func section(t *testing.T, page, open, close string) string {
	t.Helper()
	i := strings.Index(page, open)
	if i < 0 {
		t.Fatalf("%q is gone from the page; the guard needs updating", open)
	}
	rest := page[i:]
	end := strings.Index(rest, close)
	if end < 0 {
		t.Fatalf("%q never closes with %q", open, close)
	}
	return rest[:end]
}

// Since design doc 0013 an attachment is any file, not only an image, and
// a plain link is the only notation that can reference a non-image one —
// the attachments tab prints exactly that form for authors to paste. The
// link renderer used to consult only the entry resolver, which returns
// null for anything that is not a *.md, so those references rendered as
// raw markdown: the page told people to write a reference it would not
// draw. Both notations resolve through the same file resolver now.
func TestBodyLinksToAttachmentsResolve(t *testing.T) {
	page := string(Index)
	i := strings.Index(page, "const link = (m, text, ref)")
	if i < 0 {
		t.Fatal("the markdown link renderer moved without updating this guard")
	}
	body := page[i:]
	if end := strings.Index(body, "\n  };"); end >= 0 {
		body = body[:end]
	}
	if !strings.Contains(body, "resolveFile") {
		t.Errorf("a body link no longer falls back to the entry's attachments:\n%s", body)
	}
	// The image notation shares the resolver; a rename that split them
	// again would take one of the two back to literal text.
	if !strings.Contains(page, "const img = (m, alt, ref)") || !strings.Contains(page, "resolveFile && resolveFile(ref)") {
		t.Error("the image renderer no longer shares the attachment resolver")
	}
}
