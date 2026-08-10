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
	// The append path lives in withFrontmatterKey, which the type
	// dropdown shares; a second copy is what this guard exists to stop.
	fn := section(t, page, "function withStatus(doc, status)", "\n}")
	if !strings.Contains(fn, "withFrontmatterKey(doc, 'status', status)") {
		t.Errorf("withStatus no longer goes through the shared line edit:\n%s", fn)
	}
	shared := section(t, page, "function withFrontmatterKey(doc, key, value)", "\n}")
	if !strings.Contains(shared, "doc.match(") {
		t.Error("withFrontmatterKey only replaces a line; a document that names no such key would come back unchanged")
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
	// The page is Japanese (C8), so the sentence is checked in the
	// language a reader meets it in — what is pinned is that the banner
	// still says verifying does not empty this queue, not which words
	// say it.
	if !strings.Contains(page, "<strong>検証してもこのキューは片付きません</strong>") {
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

// The editor's type dropdown shipped inert from v0.16.0 to v0.19.1: the
// reseed was gated on the form's dirty flag, and a <select> fires `input`
// before its `change`, so the flag the select itself raised was always up
// by the time the handler read it. Picking a type did nothing, silently,
// on the one control that tells a writer an Attested Computation needs a
// runtime before the server's 400 does.
//
// What the gate has to ask is whether the document is still the one the
// dropdown seeded — that is the question the flag was standing in for.
func TestTheTypeDropdownAsksTheDocumentNotTheDirtyFlag(t *testing.T) {
	page := string(Index)
	body := section(t, page, "typeSel.addEventListener('change'", "\n    });")
	if strings.Contains(body, "dirty") {
		t.Errorf("the type change is gated on the form's dirty flag again, which the select's own `input` raises before this runs:\n%s", body)
	}
	for _, want := range []string{"doc === seeded", "withType(doc, typeSel.value)"} {
		if !strings.Contains(body, want) {
			t.Errorf("the type change no longer does %q:\n%s", want, body)
		}
	}
}

// Setting the type on a document somebody is already writing has to carry
// the keys that type is refused without, or the dropdown hands back a
// document the server rejects — which is worse than the nothing it used
// to do, because it looks like it worked. Only Attested Computation has
// one: runtime is the single place the write path holds a type to a
// schema (design doc 0036 §5), and internal/service is where that is
// enforced.
//
// The illustrative half must stay out of it. A sample `year` parameter
// pasted into a half-written document is the page putting words in its
// writer's mouth.
func TestATypeChangeCarriesWhatTheTypeIsRefusedWithout(t *testing.T) {
	page := string(Index)
	required := section(t, page, "const TYPE_REQUIRED = {", "\n};")
	if !strings.Contains(required, "'Attested Computation': 'runtime:") {
		t.Errorf("an Attested Computation no longer requires a runtime here, but the write path still does:\n%s", required)
	}
	if strings.Contains(required, "parameters:") || strings.Contains(required, "resource:") {
		t.Errorf("TYPE_REQUIRED carries illustrative keys, which a type change would paste into a written document:\n%s", required)
	}
	// The seed half is what only ever opens an empty document.
	seed := section(t, page, "const TYPE_SEED = {", "\n};")
	if !strings.Contains(seed, "parameters:") || !strings.Contains(seed, "resource:") {
		t.Errorf("the seed templates no longer show the shape of a type:\n%s", seed)
	}
	// A key the writer already named is theirs. Overwriting it would be
	// the dropdown deciding a runtime somebody chose was wrong.
	fn := section(t, page, "function withType(doc, type)", "\n}")
	if !strings.Contains(fn, "TYPE_REQUIRED[type]") {
		t.Errorf("withType does not add the keys the type requires:\n%s", fn)
	}
	if !strings.Contains(fn, "if (!new RegExp('^' + key + ':', 'm').test(out))") {
		t.Errorf("withType no longer checks whether the document already names the key:\n%s", fn)
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
		{"Remove on a file", "data-remove-file="},
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
		{"adding a file", `id="att-upload"`, `<div class="toolbar write-only"`, "</div>"},
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

// Since design doc 0013 a bundle file may be any type, not only an image, and
// a plain link is the only notation that can reference a non-image one —
// the files tab prints exactly that form for authors to paste. The
// link renderer used to consult only the entry resolver, which returns
// null for anything that is not a *.md, so those references rendered as
// raw markdown: the page told people to write a reference it would not
// draw. Both notations resolve through the same file resolver now.
func TestBodyLinksToFilesResolve(t *testing.T) {
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
		t.Errorf("a body link no longer falls back to the entry's files:\n%s", body)
	}
	// The image notation shares the resolver; a rename that split them
	// again would take one of the two back to literal text.
	if !strings.Contains(page, "const img = (m, alt, ref)") || !strings.Contains(page, "resolveFile && resolveFile(ref)") {
		t.Error("the image renderer no longer shares the file resolver")
	}
}

// A description keeps its line structure through the store — OKF writes
// a multi-line one as a `description: |-` block — and the page used to
// drop that on the floor, putting the text in an element where HTML
// collapses every newline into a space. A description written as
// several lines, or opening with a heading, arrived as one run-on line.
// Every place that shows one now goes through the same renderer the
// body does, so the guard is that no call site escapes the text into an
// element by hand again.
func TestADescriptionIsRenderedAsTheMarkdownItIs(t *testing.T) {
	page := string(Index)
	body := section(t, page, "function descHTML(", "\n}")
	if !strings.Contains(body, "md(text)") {
		t.Errorf("descHTML no longer renders the description as markdown:\n%s", body)
	}
	// The old failure mode, in the form it would come back.
	for _, gone := range []string{"esc(h.description)", "esc(entry.description)"} {
		if strings.Contains(page, gone) {
			t.Errorf("a description is escaped into an element again (%s), so its line breaks are lost", gone)
		}
	}
	if n := len(indexesOf(page, "descHTML(")); n < 4 {
		t.Errorf("only %d descHTML references; a place that shows a description stopped using it", n)
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

// The page must branch on the error code, never on the sentence beside
// it. The compatibility policy says the sentence may be reworded in any
// release, and this page proved the point: it decided whether to offer
// "view the existing concept" by matching /already exists/, against a
// message the server has since rewritten twice — once to name what holds
// the id and what to do about it.
//
// Two invariants, and they fail for different reasons. The first is that
// api() hands the code up at all: an error object that drops it leaves
// prose as the only thing to read, which is how the regex got there. The
// second is that every code the page names is one the product can
// actually say — a branch on a code with a typo in it is a branch that
// never runs, and nothing else would notice.
func TestThePageBranchesOnErrorCodesRatherThanProse(t *testing.T) {
	page := string(Index)

	body := section(t, page, "async function api(", "\n}")
	if !strings.Contains(body, "err.code = code") {
		t.Errorf("api() no longer carries the error code up to its caller:\n%s", body)
	}

	// Reading a code the product cannot say is a branch that never runs.
	codes := 0
	for _, i := range indexesOf(page, ".code === '") {
		rest := page[i+len(".code === '"):]
		end := strings.IndexByte(rest, '\'')
		if end < 0 {
			t.Fatalf("unterminated code literal at offset %d", i)
		}
		codes++
		if got := rest[:end]; !domain.ValidErrorCode(got) {
			t.Errorf("the page branches on error code %q, which is not one of %v",
				got, domain.ErrorCodes)
		}
	}
	if codes == 0 {
		t.Error("no branch reads an error code; the page went back to reading prose")
	}

	// The shapes prose-matching comes back in.
	for _, gone := range []string{
		".test(e.message)", ".test(err.message)",
		"e.message.includes(", "err.message.includes(",
		"e.message.match(", "err.message.match(",
	} {
		if strings.Contains(page, gone) {
			t.Errorf("the page decides something by matching an error sentence (%s); "+
				"the sentence may be reworded in any release, the code may not", gone)
		}
	}
}

// A save from this page is conditional on the version it was opened at.
//
// It was not, and that is the failure this guards: the form PUT the
// document with no precondition, so two curators on the same concept —
// or one who left a tab open over lunch — silently produced one winner
// and lost the other's prose. The REST API has had If-Match since design
// doc 0030 and the CLI sends it; the web UI was the one client that did
// not, on the surface this product says only a person can operate.
//
// The version is the ETag the server sent, carried up by api(). Not the
// content hash rebuilt from the body — that field is on the wire, but a
// page that reassembled the validator out of it would be a second
// opinion about quoting that nothing checks.
func TestTheEditorSavesAgainstTheVersionItOpened(t *testing.T) {
	page := string(Index)

	body := section(t, page, "async function api(", "\n}")
	if !strings.Contains(body, "If-Match") {
		t.Errorf("api() cannot send a precondition:\n%s", body)
	}

	if !strings.Contains(body, "Object.defineProperty(data, 'etag'") {
		t.Errorf("api() drops the version each read saw:\n%s", body)
	}

	editor := section(t, page, "async function viewEditor(", "\n}")
	if !strings.Contains(editor, "v.etag") {
		t.Error("the editor does not read the version it is editing against")
	}

	// The save call itself. Reading the hash and never sending it would
	// pass the two checks above and change nothing.
	if !strings.Contains(page, "ifMatch: version") {
		t.Error("the save does not carry the version the editor read")
	}

	// And the refusal is told apart by its code, not by the server's
	// sentence — 412 answers two conditions here, and the other one
	// (already_exists, on create) wants a different message.
	if !strings.Contains(page, "e.code === 'precondition_failed'") {
		t.Error("a save refused by the precondition is not distinguished from any other failure")
	}
}
