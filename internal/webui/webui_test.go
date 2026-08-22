package webui

import (
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// What is left for Go now that the page is modules.
//
// The rules the modules can state themselves are stated in
// internal/webui/jstest, which runs them: `node --test` imports the pure
// ones — the renderer, the formatters, the document edits — and asserts
// on what they return rather than on how they are written. What stays
// here is what a module cannot check about itself: an invariant shared
// with Go (the type vocabulary), and the page-wide rules that are true
// only across several modules at once. Those are still read from the
// source, because there is nothing else to read them from.

// asset is one embedded file, by the path the browser asks for.
func asset(t *testing.T, name string) string {
	t.Helper()
	b, err := fs.ReadFile(Files, name)
	if err != nil {
		t.Fatalf("%s is not embedded: %v", name, err)
	}
	return string(b)
}

// wholePage is every embedded file at once, for the guards that are about
// the page rather than about one module — "nowhere does this any more" is
// a claim about all of it.
func wholePage(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	if err := fs.WalkDir(Files, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		c, err := fs.ReadFile(Files, p)
		b.Write(c)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// The browser fetches what is embedded, so a module that exists in the
// tree and not in the binary is a blank panel at runtime and nothing at
// build time. Every import the modules name has to resolve to a file that
// shipped.
func TestEveryModuleThePageImportsIsEmbedded(t *testing.T) {
	index := asset(t, "index.html")
	if !strings.Contains(index, `<script type="module" src="js/main.js">`) {
		t.Error("index.html no longer boots js/main.js")
	}
	if !strings.Contains(index, `<link rel="stylesheet" href="app.css">`) {
		t.Error("index.html no longer links app.css")
	}
	imports := regexp.MustCompile(`from '([^']+)'`)
	seen := 0
	err := fs.WalkDir(Files, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".js") {
			return err
		}
		seen++
		src, err := fs.ReadFile(Files, p)
		if err != nil {
			return err
		}
		for _, m := range imports.FindAllStringSubmatch(string(src), -1) {
			target := path.Join(path.Dir(p), m[1])
			if _, err := fs.Stat(Files, target); err != nil {
				t.Errorf("%s imports %s, which is not embedded", p, m[1])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen < 10 {
		t.Errorf("only %d modules embedded; the split collapsed back into one file", seen)
	}
}

// The re-verification feed is the exit design doc 0025 §6 built for the
// review loop, and it was reachable only by opening the search filter
// bar and knowing which chip meant it. Both feeds have their own routes
// now, and the review queue links them; a rename that drops the routes
// would quietly hide the queues again.
func TestFeedsAreLinkableFromTheReviewLoop(t *testing.T) {
	page := wholePage(t)
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
	page := wholePage(t)
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
	page := wholePage(t)
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
	page := wholePage(t)
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
	page := asset(t, "js/vocab.js")
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

// A grant names a principal the ledger can spell (design doc 0109 §2),
// and the page checks the spelling before it sends one. A page that
// accepted a kind the server does not know turns a typo into a 400 the
// operator has to read backwards from — on the one document where a
// silently wrong row is a boundary that is not there.
func TestPrincipalSpellingMatchesDomain(t *testing.T) {
	policy := asset(t, "js/policy.js")
	kinds := section(t, policy, "export const ACTOR_KINDS = [", "]")
	for _, kind := range []string{domain.ActorHuman, domain.ActorProcess} {
		if !strings.Contains(kinds, "'"+kind+"'") {
			t.Errorf("policy.js does not accept the actor kind %q: %s", kind, kinds)
		}
	}
	if n := strings.Count(kinds, "'") / 2; n != 2 {
		t.Errorf("policy.js names %d actor kinds and domain has 2: %s", n, kinds)
	}
	if want := "ANY_PRINCIPAL = '" + domain.AnyPrincipal + "'"; !strings.Contains(policy, want) {
		t.Errorf("the wildcard the page sends is not the one the server matches on (%s)", want)
	}
}

// Reading the access policy is an administrator's operation, so the tab
// that leads to it is absent until the server has answered for this
// caller — the same rule as a write affordance on a read-only
// deployment, and for the same reason: a control that can only ever 403
// is a lie.
//
// The CSS rule is half of it. `.topnav a` sets a display, which beats
// the [hidden] attribute's own, so without the rule below the tab ships
// visible to everybody it is meant to be hidden from and nothing here
// looks wrong.
func TestTheAccessTabWaitsForTheServerToAnswer(t *testing.T) {
	if tag := section(t, asset(t, "index.html"), `id="nav-access"`, ">"); !strings.Contains(tag, "hidden") {
		t.Errorf("the access tab ships visible: %s", tag)
	}
	if !strings.Contains(asset(t, "app.css"), ".topnav a[hidden] { display: none; }") {
		t.Error("nothing hides a hidden tab; .topnav a sets a display, which the attribute alone does not beat")
	}
	body := section(t, asset(t, "js/views/access.js"), "export async function markAccessTab", "\n}")
	shown := strings.Index(body, "hidden = false")
	asked := strings.Index(body, "await api('/api/v1/access')")
	if shown < 0 || asked < 0 || asked > shown {
		t.Errorf("the tab is not shown by the server's answer to a policy read:\n%s", body)
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
	page := asset(t, "js/views/editor.js")
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
	page := wholePage(t)
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
		// The whole access editor, container and all: a read-only
		// deployment refuses a policy write like any other (design doc
		// 0109 §5), and a boundary is the one thing whose editor must
		// not look available where it is frozen.
		{"the access policy editor", `id="access-edit"`},
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
	page := wholePage(t)
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

// A description keeps its line structure through the store — OKF writes
// a multi-line one as a `description: |-` block — and the page used to
// drop that on the floor, putting the text in an element where HTML
// collapses every newline into a space. A description written as
// several lines, or opening with a heading, arrived as one run-on line.
// Every place that shows one now goes through the same renderer the
// body does, so the guard is that no call site escapes the text into an
// element by hand again.
func TestADescriptionIsRenderedAsTheMarkdownItIs(t *testing.T) {
	page := wholePage(t)
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

// The loop strip is the human side of design doc 0069 §5: the review page
// is where the person who runs the loop already is, so the instance's
// numbers are drawn there rather than on a page of their own.
//
// The one thing it must not do is draw a deployment that keeps no
// questions as one that was asked none — "0 searches found nothing" is
// the opposite of the truth, and it is the reading a reader would take
// from a zero.
func TestLoopStatsDistinguishNotRecordedFromZero(t *testing.T) {
	page := asset(t, "js/views/review.js")
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
	page := wholePage(t)

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
	// Two modules: the call shape carries the validator, the editor uses it.
	body := section(t, asset(t, "js/api.js"), "async function api(", "\n}")
	if !strings.Contains(body, "If-Match") {
		t.Errorf("api() cannot send a precondition:\n%s", body)
	}

	if !strings.Contains(body, "Object.defineProperty(data, 'etag'") {
		t.Errorf("api() drops the version each read saw:\n%s", body)
	}

	page := asset(t, "js/views/editor.js")
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
