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

// A status change must not rebuild the entry from what the page holds.
// It did once, and it erased resource because the payload builder had not
// been taught about it — twice, in fact: design doc 0036 §4 records it
// vanishing from the detail view's status change, and again from the
// editor, which had no resource field at all.
//
// The page now holds strictly less than it did — the projection, not the
// entry (design doc 0043 §3.5) — so rebuilding would erase the citations,
// the contract and the body as well. The fix is that it rebuilds nothing:
// it reads the document, changes the status line, and writes it back.
func TestAStatusChangeEditsTheDocumentRatherThanRebuildingIt(t *testing.T) {
	page := string(Index)
	if !strings.Contains(page, "function withStatus(doc, status)") {
		t.Fatal("the status change no longer goes through a document edit")
	}
	body := section(t, page, "async function applyStatus(", "\n}")
	if !strings.Contains(body, "api(conceptURL(id))") {
		t.Error("applyStatus does not read the document first, so it cannot be editing one")
	}
	if !strings.Contains(body, "withStatus(") {
		t.Error("applyStatus does not use withStatus")
	}
	// The old failure mode, in the form it would come back.
	if strings.Contains(page, "function toDocument(") || strings.Contains(page, "const WRITABLE") {
		t.Error("the page rebuilds a document from fields again; a projection cannot be rebuilt into an entry")
	}
	// A document that names no status has to gain one. The server does
	// not write a status its writer left out (design doc 0046 §3.9), and
	// the document a read hands back is the one that was written (§2.2) —
	// so a replace-only edit would report success and change nothing.
	fn := section(t, page, "function withStatus(doc, status)", "\n}")
	if !strings.Contains(fn, "doc.match(") {
		t.Error("withStatus only replaces a status line; a document that names none would come back unchanged")
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

// The page carries its own copy of the type vocabulary, because it is a
// static asset with no build step to interpolate domain.Types into. There
// is exactly one copy — TYPE_ICONS, which KNOWN_TYPES and every list in
// the page derive from. It used to be two, and they drifted: when
// Attested Computation joined the vocabulary the icons gained it and the
// new-entry form's hint did not, and neither noticed. The second copy
// went with the form itself (design doc 0044); this guards the one that
// is left, in the direction that fails silently — a retired type would
// otherwise go on being recommended by the UI forever (design doc 0038
// §4.4).
func TestTypeVocabularyMatchesDomain(t *testing.T) {
	page := string(Index)
	icons := section(t, page, "const TYPE_ICONS = {", "};")
	if !strings.Contains(page, "const KNOWN_TYPES = Object.keys(TYPE_ICONS);") {
		t.Error("KNOWN_TYPES no longer derives from TYPE_ICONS; the page has a second copy of the vocabulary again")
	}
	for _, ty := range domain.Types {
		if !strings.Contains(icons, "'"+string(ty)+"'") {
			t.Errorf("TYPE_ICONS has no icon for %q", ty)
		}
	}
	for _, retired := range []string{"Semantic Model", "Golden Query", "Playbook", "API Endpoint"} {
		if strings.Contains(icons, retired) {
			t.Errorf("the page still offers %q, retired by design doc 0038 or 0063", retired)
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
		{"Withdraw rejection", `id="act-withdraw"`, `<span class="actions write-only">`, "</span>"},
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

// The loop strip is the human side of design doc 0051: the review page
// is where the person who runs the loop already is, so the instance's
// numbers are drawn there rather than on a page of their own.
//
// The one thing it must not do is draw a deployment that keeps no
// questions as one that was asked none — "0 searches found nothing" is
// the opposite of the truth, and it is the reading a reader would take
// from a zero.
func TestLoopStatsDistinguishNotRecordedFromZero(t *testing.T) {
	page := string(Index)
	if !strings.Contains(page, "/api/v1/stats") {
		t.Fatal("the review page no longer reads the instance stats")
	}
	body := section(t, page, "async function loadLoopStats(", "\n}")
	if !strings.Contains(body, "misses?.recording") {
		t.Errorf("the loop strip draws misses without asking whether they are recorded:\n%s", body)
	}
}
