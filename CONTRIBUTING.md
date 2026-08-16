# Contributing to ochakai

Thanks for your interest! Issues and pull requests are welcome. This
project follows a short [code of conduct](CODE_OF_CONDUCT.md).

## Development setup

You need Go (the version pinned in [go.mod](go.mod)) and Docker.

Run the full local stack (server + PostgreSQL with pgvector):

```sh
docker compose -f deploy/compose.yaml up
```

Seed it and poke around:

```sh
export OCHAKAI_URL=http://localhost:8080
go run ./cmd/ochakai put queries/monthly-revenue -f examples/golden-query.md
go run ./cmd/ochakai search "revenue"
```

`OCHAKAI_URL` has to come first: there is no default server, and a client
command without one fails rather than guessing. `ochakai use` saves the
choice instead, but an exported variable keeps a checkout from editing the
config your everyday CLI reads.

## Tests and checks

One command, and it is the one CI runs — make it pass before opening a
PR:

```sh
scripts/check
```

That is `gofmt -l .`, `go vet ./...`, `go test -race -count=1 ./...`, a
`CGO_ENABLED=0 go build -trimpath ./...`, golangci-lint, and
govulncheck, in that order. A step at a time when you are iterating:

```sh
scripts/check core   # the four fast ones — this is what CI's test job runs
scripts/check lint
scripts/check vuln
```

CI calls the same script for `core` and reads the pinned golangci-lint
version out of [ci.yaml](.github/workflows/ci.yaml), so there is no
second copy of any of this to keep in sync.

The linters are configured in [.golangci.yml](.golangci.yml) and chosen so
a clean tree reports nothing — a finding means new code, not a linter's
taste ([design doc 0035](docs/design/0035-verifiability.md)). The most
load-bearing one is `exhaustive`: a `switch` over `domain.Status` or
`domain.Type` that names every case and no `default` must keep naming
every case when a value is added.

`go test` runs the fuzz targets' seed corpora like ordinary tests, which
is what CI does. Fuzzing proper is a local tool — reach for it when
touching the OKF parser, id validity, or link derivation:

```sh
go test ./internal/okf/ -run XXX -fuzz FuzzDocumentRoundTrip -fuzztime 60s
```

A failing input lands in `testdata/fuzz/` and becomes a permanent seed;
commit it with the fix.

Two checked-in files are generated, and the test that generates them
fails when the copy is stale. [docs/cli.md](docs/cli.md) is the CLI's own
help rendered — change a synopsis, a flag or an example and regenerate:

```sh
go test ./cmd/ochakai -run TestCLIReferenceIsCurrent -update
```

The other is the version number in `api/openapi.yaml`, checked against
the newest release in `CHANGELOG.md` and the issue template's
placeholder; see [Releases](#releases).

The store integration test is skipped unless a real PostgreSQL is
available (CI runs one as a service container). `scripts/check --db`
starts one and removes it afterwards; by hand it is:

```sh
docker run -d --rm -p 55433:5432 -e POSTGRES_PASSWORD=t -e POSTGRES_USER=t -e POSTGRES_DB=t pgvector/pgvector:pg17
OCHAKAI_TEST_DATABASE_URL='postgres://t:t@localhost:55433/t?sslmode=disable' go test ./internal/store/
```

A test that needs the database **asks for it with
`internal/testdb`.URL**, which returns the URL or skips — one wording for
the skip, and a count. Without a database well over a hundred tests skip,
so a package that ran none of them prints how many it sent away. That
line reaches
`go test -v`, a bare `go test` in the package's directory, and any run
where something failed; it cannot reach a green `go test ./...`, which
throws away the output of a package that passed. That is why the summary
you should trust is `scripts/check`'s closing line.

The database is shared: CI runs one container for a whole run, and by
hand you keep one up across sessions. So **an integration test takes its
ids from `internal/testdb`.Unique**, which appends a run-unique number
and registers the sweep that removes every row written under it. A test
that rolls its own token instead leaves its rows behind, and the corpus
grows until a bounded ranking assertion somewhere else stops finding its
own concept — a failure that reads as a search bug in whatever change
happens to cross the threshold. `TestNoTestRollsItsOwnNamespace` fails
on a hand-rolled one.

**A cleanup written by hand is the same mistake wearing a tidier hat,
and it now misses half the rows.** Several tests delete their own rows
on the way in — `DELETE FROM knowledge_revision WHERE id LIKE 'it-x-%'`
— which was equivalent while every row a test wrote had an id. A file
stopped having one: it is an object at its own path (design docs 0075
§5, 0100), so its rows carry `path` and no `id`, and
`knowledge_revision`'s primary key is `(path, rev)`. An id-keyed delete
leaves a file's revisions behind, and the next run against that database
collides on the primary key. Worse, the file it leaves is *live*, and
its bytes exist only in the test's fake blob store — so
`internal/restapi`'s export tests answer 501 for that database from then
on, in a package that never wrote it. `testdb.Sweep` deletes on `id`
**or** `path`, which is the whole difference; ask `testdb.Unique` for a
namespace and delete nothing by hand.

`--db` prefers Docker, because `pgvector/pgvector:pg17` is the database
CI runs. Where Docker cannot serve one — no daemon, or a network that
will not let the image be pulled — it builds a throwaway cluster out of
whatever PostgreSQL the machine has installed, provided pgvector is
installed beside it, and deletes it on exit. That is a real run of the
store tests but not CI's, so the script's closing line names which
database it used.

### The web UI's tests

`scripts/check ui` runs `node --test` over `internal/webui/jstest`. Those
tests import the page's ES modules directly, so what they assert is what
a function returns — the markdown renderer, the formatters, and the
document edits, which are the modules that touch what somebody wrote.
There is no `package.json` and no `node_modules`: the runner ships with
Node, and a page with no build step should not need one to be tested
(design doc 0092 §2). Without Node the step says so and skips; CI runs
it.

The browser smoke beside them is not part of that step, because it needs
a page to load:

```sh
ochakai serve &                                     # against a database
ochakai import examples/demo
OCHAKAI_URL=http://127.0.0.1:8080 ochakai ui &
node internal/webui/jstest/smoke.mjs http://127.0.0.1:8098
```

Point it at a database holding `examples/demo` and **nothing else**. A
development database accumulates concepts at the root from the store
tests, and the sidebar renders those as entries — which is how the first
version of this file came to assert on a selector that a fresh import
could never match, and passed locally while failing in CI. `createdb`
first if the one you have has been used for anything.

It drives the Chromium it finds (`$CHROME` overrides the search) over the
DevTools Protocol, walks every route, and fails on any console error —
which is what a module that failed to load looks like, since the panel it
would have drawn is simply blank. A missing browser or an unreachable UI
is a skip locally and a failure under `OCHAKAI_SMOKE_REQUIRED=1`, which
is how CI runs it: a skip there would be a green run that tested nothing.

### Measuring before proposing

`scripts/duplicate-distances.sql` is the shape a measurement takes here:
read-only SQL, no surface, run against a base somebody actually curates.

```sh
psql "$OCHAKAI_DATABASE_URL" -f scripts/duplicate-distances.sql
```

It answers one question — how close the concepts in a base sit to each
other — because the feature it is upstream of ("return one of the
near-duplicates, not five") cannot be specified without a number nobody
can name from an armchair. A histogram with a bump near zero, separated
from the rest, is a threshold; one smooth slope is the finding that
there is not one, and that finding is worth as much as the feature.

The pattern generalizes, and it is the order the records follow: the
count that shows a problem exists ships before the mechanism that
answers it (design doc 0089 §4). A script like this is not a surface —
nobody has to learn it, no endpoint answers it — so it is cheap to write
and cheap to delete once it has been read.

## Working with Claude Code

The repository checks in a `.claude/settings.json` with four hooks and
one permissions rule. Two hooks serve the checks: one runs `gofmt` on Go
files as they are written, since that is the cheapest CI failure to
avoid; the other runs at session start and does nothing at all unless
`CLAUDE_CODE_REMOTE` says the session is a disposable cloud container,
where it installs pgvector beside the container's PostgreSQL so
`scripts/check --db` has something to fall back to, and tells the agent
what that environment cannot check. Docker has a CLI there but no
daemon, and starting `dockerd` does not help because the egress policy
refuses Docker Hub's blob CDN; the same policy refuses `vuln.go.dev`. So
the store tests run, `core` and `lint` are honest there, and the
`Dockerfile`, `deploy/compose.yaml` and `govulncheck` are CI's to verify
— which is worth saying plainly, because an agent that reports a blocked
host as a failing check is the expensive outcome.

The other two hooks and the permissions rule are the dogfood loop
([kb/README.md](kb/README.md)): recall injects concepts from the local
instance as a prompt comes in, write-back asks its two questions once
per session before the agent stops, and the deny list keeps agent
sessions off the CLI write commands, which the dev instance would record
as the anonymous human — agent writes go through the MCP tools, whose
connection carries the process identity
([kb/bundle/policies/ai-human-identity.md](kb/bundle/policies/ai-human-identity.md)).
That deny list is the one permissions entry that is not a personal
choice; everything else about permissions belongs in
`.claude/settings.local.json`, which is not tracked.

`.claude/skills/` holds the procedures that are long enough to get wrong
from memory: `release` and `design-doc`. They are the same procedures
written out below and in [CLAUDE.md](CLAUDE.md); if you change one,
change the other.

Unrelated to any of this, `examples/claude-code/` is a *product* example
— hooks that pull team knowledge out of a running ochakai and write back
to it. It is not part of this repository's own setup.

## Web UI screenshots

`docs/images/webui-review.png`, `webui-entry.png`, `webui-wrong.png` and
`webui-tree.png` are captures of the real Web UI serving
[examples/demo](examples/demo). They are shot from the committed bundle
rather than a corpus that only ever existed on one machine, so anyone can
reproduce them when the UI changes.

Plant the knowledge base — any empty Postgres will do; make a scratch
database and drop it when finished:

```sh
createdb shots   # or: docker exec <pg> psql -U t -d t -c 'CREATE DATABASE shots'
OCHAKAI_DATABASE_URL='postgres://…/shots?sslmode=disable' \
  OCHAKAI_MODE=dev PORT=8095 go run ./cmd/ochakai serve &

export OCHAKAI_URL=http://127.0.0.1:8095
go run ./cmd/ochakai import examples/demo
```

`dev` is the only posture that is writable without an identity in front,
and a dev deployment now says so on every page, so a capture taken this
way carries the banner and the committed ones do not. Crop it out, or
shoot against a deployment whose callers are authenticated: what the
pictures are for is the product, and the banner is about the machine you
happen to be running it on.

The bundle already carries the two drafts the review queue needs and a
draft whose `stale_after` has passed. The re-verification feed is the one
thing it cannot ship, because a failure report is an event rather than a
document — report one, and read a few entries so the usage counts on the
cards are not all zero:

```sh
go run ./cmd/ochakai report tables/shop-orders failed --note "Ran the daily
  revenue check at 07:40 JST and the day came out 13% short; MAX(created_at)
  was 05:58, so the 06:00 partition had not landed yet."
go run ./cmd/ochakai search "why is revenue down?"
go run ./cmd/ochakai get metrics/revenue
```

Then serve the UI as yourself:

```sh
go run ./cmd/ochakai ui --port 8098
```

Shoot four hash routes, at a 1440 CSS-px width and a device scale factor
of 2. The heights are chosen so each shot ends just under its last card;
the UI is a fixed-viewport shell whose panes scroll internally, so the
height is a crop, not a measurement.

| Route | File | Height |
|---|---|---|
| `#/review` | `webui-review.png` | 570 |
| `#/k/metrics/revenue` | `webui-entry.png` | 340 |
| `#/search/reported-wrong` | `webui-wrong.png` | 375 |
| `#/` | `webui-tree.png` | 510 |

Two things have to be forced, or the pictures come out wrong in ways
that are easy to miss:

- **Light scheme.** Headless Chrome defaults to dark.
- **An `en-US` locale.** The entry page prints its dates with
  `toLocaleDateString()`, which reads ICU's default locale — and on
  macOS that follows the system, so `--lang=en-US`, `--accept-lang` and
  `LANG`/`LC_ALL` all leave it Japanese (`2026年7月27日`) while
  `navigator.language` reports `en-US` and looks fine. Override it over
  the DevTools protocol with `Emulation.setLocaleOverride` (light scheme
  via `Emulation.setEmulatedMedia`), and check the result in the page:
  `Intl.DateTimeFormat().resolvedOptions().locale` must be `en-US`.

The entry route scrolls its sidebar to reveal the selected node, which
clips the panel header; scroll back to the top before capturing.

Downscale each to 1800px wide, which keeps all four under 1 MB together:

```sh
sips --resampleWidth 1800 docs/images/webui-*.png
```

Read every PNG before committing it. Reject any that is dark, dated in
Japanese, mostly empty, or showing an error or empty state — a dead
backend renders as a tidy "502 Bad Gateway" card that is easy to mistake
for content at a glance.

## Proposing a feature

The default answer is no, and [docs/surface.md](docs/surface.md) is where
that is made concrete. It counts every REST operation, MCP tool, CLI
command and environment variable in one place — what somebody using
ochakai actually pays for — and `cmd/ochakai/surface_test.go` fails when
the count and the build disagree, so an addition arrives as a heading
moving from `(19)` to `(20)` rather than as a few lines buried in a spec.

The same document opens with the eight conditions ochakai exists to
satisfy. **A proposal that serves none of them is a no before the
questions start**, and that is the cheapest place to find out. If it does
serve one, saying which is where the description begins — serving a
condition is necessary, not sufficient, since every condition has
infinitely many mechanisms that would serve it.

From there a PR that widens a surface answers three questions: who
actually got stuck and on what, whether an existing surface already
covers it (if it does, don't add), and what can be folded away in
exchange. The test holds the arithmetic; those answers are the part
review spends time on.

## Design docs

Architecture decisions live in [docs/design](docs/design) as numbered
documents (mostly Japanese). Start from the
[index](docs/design/README.md): its opening table says which document
describes each area today, and the prose below it carries the history. A
new record needs a row in
[README.en.md](docs/design/README.en.md) beside it, which a test checks.
[docs/architecture.md](docs/architecture.md) (Japanese) summarizes the
accepted ones if you would rather read the shape of the system first.

The Japanese is the maintainer's habit, not a rule — write your design
doc in English if that is what you think in, and say so in the PR. What
the review cares about is that the decision and its rejected
alternatives are legible, not which language they are legible in.

A numbered doc is for a decision **somebody using ochakai can observe**
([0048](docs/design/0048-decision-records-for-wire-contracts.md)): the
shape of the wire (REST, MCP, CLI, web UI), the stored form and its
round trip, what identity and provenance mean, a new dependency on a
Google Cloud service, or something the project refuses to do. Internal
work that leaves the outside unchanged — restructuring, queries and
indexes, performance, dependencies, a new spelling within an existing
decision — belongs in the PR description instead. If you are unsure, ask
whether anybody will reopen the question in three months.

Docs are immutable decision records; the incremental history lives in
them and in PRs. What the repo must keep readable is the *current*
state per area, so when a design doc lands:

- Take the next free number. Gaps are fine — proposals that never land
  stay in their PRs.
- **Unless the doc you are revising has not reached a release**, in which
  case replace it rather than numbering a successor: immutability is a
  promise about decisions somebody could be depending on, and nobody
  depends on an unreleased one. If the implementation is still in the
  changelog's `Unreleased` section, edit that doc — or delete it and fold
  it into the one replacing it, as 0034 did.
- Update the `Status:` header of every older doc the new one supersedes
  or amends, linking to the new number (see 0011 or 0018 for the style).
  A doc the new one supersedes outright also shrinks to a tombstone —
  see "What a superseded record keeps" below.
- Update [docs/design/README.md](docs/design/README.md): add the new
  doc to its area with a one-line summary, adjust the status notes of
  the docs it amends, and — if it becomes the doc to read for that area —
  update the area's row in the opening table.
- Add its row to `README.en.md`'s table, in the same area. A doc the new
  one supersedes drops to a bare pointer row (record and status only, no
  decision cell); full rows are kept only for what is current, and a test
  holds it — cutting the old row is part of superseding, not tidying for
  later.
- Avoid amendment chains. If the new decision would be the second
  partial amendment stacked on the same doc, don't add another diff:
  write it as a full replacement that states the area's whole current
  picture and mark the older docs Superseded.
- Keep it under the ceiling below. A record is prose somebody reads.

### How long a record gets

    RECORD-LINES: 300
    RECORD-CAP-FROM: 0065

0048 narrowed *what* earns a number. It said nothing about *how much*,
and the corpus grew accordingly — record count and total lines both
outrunning the manual's own count over the same stretch (see "How many
records, and how much" below for where things stand today). A decision
that renames two words has cost 150 lines of record, an entry in each
index and a paragraph in
[docs/surface.md](docs/surface.md) — the explanation outgrowing the
thing explained is the shape the numbers have been showing for several
releases.

So a record from 0065 on stays under that line, and
`TestDesignRecordsStayUnderTheirCeiling` reads both numbers back out of
this file.
The ceiling is the same bargain [docs/surface.md](docs/surface.md)
strikes for the surface: it is one number in one file and anybody can
raise it, in the same PR, having said why. What it buys is only that the
raise cannot happen quietly.

Going over usually means one of three things, in order of likelihood:

1. **It is two decisions.** Split it and take two numbers. Two records a
   reader can finish beat one they abandon.
2. **It is restating what another record already decided.** Cite it
   instead. The index exists so a record does not have to carry its
   ancestors.
3. **It really is that large.** Then raise the line above and say so.
   0046 rebuilt the address space in 601 lines and earned every one; it
   is a tombstone now, so 0064 is both the precedent and the customer.

What the ceiling must not buy is denser prose. Records earlier than 0065
are not measured: they are immutable, and immutability is a promise about
decisions somebody could be depending on, not a licence to keep writing
at whatever length the last one happened to be.

**891 → 300, and 0063 → 0065, is that batch closing.** This ceiling spent
its whole life so far being set by one record. 0064 is not one decision
the way most records are: it is the *last* batch that can break REST, so
every breaking change anyone still wanted had to be in it or never
happen, and each round of pre-release review found more — 364 → 554 →
789 → 891, every step the third case above and every step honest. The
review's recurring find is the part worth keeping: not a missing feature
but **a place where the contract and the code disagreed in silence**, and
once the fingerprint's own blind spot turned out to be one of them, **the
check that guards the freeze had a gap the freeze's own record had
already described** — which is the argument for reading an invariant from
outside rather than trusting that a record and a test agree
([0035](docs/design/0035-verifiability.md)).

A batch record grows until the batch closes, and this one closed: the
freeze shipped in v0.18.0, `/api/v1` cannot move again, so neither can
0064. The ceiling it was holding open is now holding nothing — a record
six times the median could land under it without a word — so the floor
moves past it and the line comes down to 300, which is where the records
actually written since the freeze put it (0069 at 282 is the tallest).
0064 stays exactly as tall as it is. It is released and immutable, and
the floor is how this file has always excused a record it cannot ask to
be shorter.

### What a superseded record keeps

    TOMBSTONE-LINES: 12

A record's own `Status:` header already says when it has been replaced.
Until now the body kept every word regardless of whether anyone still
read it: ten Superseded records carried 2,107 lines between them, and
1,071 of those were a two-hop chain — 0036 → 0043 → 0046 — that nobody
following a `Status:` header should start from in the first place.

So a record whose `Status:` says Superseded shrinks to a tombstone: the
title, the header, one sentence of what it decided, and a link to the
commit that held it in full. Immutability does not object — it is a
promise about decisions somebody could be depending on, and nobody can be
depending on a decision that has already been replaced. The full text is
not gone, only a `git show` away, and the tombstone's commit link points
at exactly that.

This is the rule
[0048](docs/design/0048-decision-records-for-wire-contracts.md) §2.5
already applies to *summaries* of a superseded record in
[README.en.md](docs/design/README.en.md) — a pointer, and no more
maintenance than that — extended to the record it summarizes.
`TestSupersededRecordsAreTombstones` holds a Superseded record to
`TOMBSTONE-LINES` and requires the commit link; going over it means the
record is still carrying prose nobody rereads.

### How many records, and how much

`RECORD-LINES` bounds one record's thickness. Nothing bounded how many
records there are, and that turned out to be the number that moved: 19
records at v0.10.0 against 78 today, 4.1x, while non-test Go grew 2.5x
(7,938 → 19,527) and REST retreated from 19 operations to 11. **Lines are
no longer the half that outruns the code** — the corpus is 2,536 → 5,121,
2.0x, under Go's own growth — but that is the tombstone rule and the
ceiling below doing their work, not the count slowing down: a record
still arrives about as often, it just stops costing lines once something
replaces it. Count is what to watch. Folding a surface leaves a record
behind, and [docs/surface.md](docs/surface.md)'s DOC section already names
that residue — an index entry in each language, an English summary, a
paragraph in that file or this one — for the manual it counts. Records are
the larger half of the same residue, and until now none of it was counted
anywhere. 60 → 61 is the one addition that does not fold into that
pattern: freezing REST at `/api/v1` (0064) is its own decision, not a
restatement of anything the other 60 already say, so there was no
existing record to fold it into. 8311 → 8339: issue
[#408](https://github.com/na0fu3y/ochakai/issues/408) found that 0064,
written the same week, stated two rules the implementation did not
follow and left two questions the freeze needs answered (a bar for
breaking it, whether DELETE is safe to retry) unwritten — 28 lines of
correction and decision, not restatement. 8339 → 8377: issue
[#411](https://github.com/na0fu3y/ochakai/issues/411) found that 0057
§3.2's exclusion of the JSON field name `entries` did not hold in two
of the three places it applied — 0064 §7 decides the question 0057
deferred and renames `entries` to `concepts`, still unreleased so
folded into 0064 rather than taking a new number. 8377 → 8378: issue
[#420](https://github.com/na0fu3y/ochakai/issues/420)'s amends-both-ends
check (`TestSupersessionIsRecordedAtBothEnds`) found that 0058, already
released, had revised 0004's own CLI-table row for `--min-score` with no
line in 0004 saying so — the one line 0004 gains points back at 0058, the
sole deliberate exception to "not one line rewritten" this pass makes,
since the record predates the check and cannot pass it any other way.

220 → 222 is what recording an amendment at both ends costs the record
doing the amending. 0064 sat at exactly 220 and revises four records now,
not two: #435 found that it renames the two request headers 0027 and 0052
decided and said so in neither place, so a reader opening either one met
the retired spelling with nothing pointing forward. Two lines of header is
what that back-link costs on the amending end; the ceiling says so rather
than the record being squeezed to hide it.

8,378 → 8,383 → 8,391 is more of the same kind. Reading the amendment
by pattern hid two of them: 0062's header revises 0049 §3.4 and 0059's
printed form in one clause, and the check read a header the way the file
wraps rather than the way a sentence runs, so a line break inside 改訂す
る hid one and the first match consumed the second. Both records now say
so. The verb is still the only half read by pattern — 足す is not, because
"add an entry to 0015 §3's list" and "take 0040's read-only as given and
add a posture beside it" differ by which noun a に governs, and a guess
there fails by blocking a PR that did nothing wrong. That one stays a
reviewer's job.

222 → 253, 8,391 → 8,422: issue
[#470](https://github.com/na0fu3y/ochakai/issues/470) found that §2's own
decision — an unrecognized query parameter is a 400, naming it — had only
half landed. A request body's unknown key was still a silent 200
(`readJSON` never called `DisallowUnknownFields`), and
`GET /api/v1/bundle/{path}` allowed `history`, `limit` and `files` for the
whole address while reading each in only one or two of its modes, so a
parameter sent to the wrong mode was ignored rather than refused — the
same shape as the query-parameter gap 0064 already closed once. 0064 §2
decides both, and §11 gains the two BREAKING bullets that follow from
deciding them. 31 lines on the one record still unreleased enough to
revise in place.

253 → 313, 8,422 → 8,482 is the same issue's audit closing its third
part: three places where `api/openapi.yaml` and the code it describes
disagreed — `?history`'s `limit` stated one ceiling while the code
enforced two, a concept's GET declared `If-None-Match` without ever
answering 304, and a file `PUT` declared an `ETag` response header it
never sent. None of the three change a stored shape or a wire
identifier, so 0064 gains a new section rather than a new record (0048
§2.3: still unreleased, so revised in place) — 60 more lines for three
findings, each with the disagreement, the decision and which side
moved.

61 → 63 while 8,482 → 7,986 is the first consolidation, and it moves the
two numbers in opposite directions on purpose. Seven records were current
for identity and the posture at once — the opening table named all seven,
so reaching that area's current state meant opening 928 lines — and 0065
and 0066 replace them, leaving seven tombstones. **The count goes up by
the two records added**: a tombstoned record keeps its number and its
file, which is what makes every citation of 0002 still land somewhere,
so a consolidation can only ever add. Read together the pair says what
happened — two more records, five hundred fewer lines, and an area that
now takes two records to read instead of seven. Neither number says that
alone, and `RECORD-COUNT` rising is not this corpus growing.

313 → 364, 7,986 → 8,037 is issue #470's third and last 0064 PR. §6
retired `attachments`/`Attachment` from the wire but left its own
change-verbs, `attach`/`detach`, behind, and three more places carried
the same "named for the act, not the object" gap §7 closed for
`entries`: `/context`'s redundant `truncated` count, a bundle listing
row spelled `updated_at` where `created_at` was the only fact the
underlying object actually held, and `reembed`'s `embedded`. Two new
sections (§8, §9) carry the four decisions, one migration, and the
scope boundary that keeps the CLI's `attach`/`detach` commands out of
it — the same shape §7 itself used for the same reason.

63 → 66 while 8,037 → 6,347 is the same consolidation reaching the two
areas that had nine current records apiece. Fifteen records became three —
0067 (what each surface is for), 0068 (the rules that add and retire a
face) and 0069 (the loop and what measures it) — and the pattern that
made an area-by-area pass impossible is worth recording: **four of those
fifteen carried a general rule and a topical decision in one record**.
0058 held "an entrance nobody arrives through comes down" beside removing
`min_score`; 0062 held "two capabilities, two commands" beside adding
`ochakai list`. A record cannot be split — it can only be superseded — so
consolidating the surface area and the loop area separately would have
left each pointing into the other's record. They were done as one pass.

The other finding is what a consolidation is for. **Six of the fifteen
were teaching spellings that no longer exist**: 0015's list of deliberate
omissions was written entirely in `search_knowledge` / `get_attachment` /
`POST /api/v1/knowledge`, 0049 taught `ochakai queues` and
`queues.reported_wrong`, 0051 taught `OCHAKAI_PUBLIC_READ_ONLY`, and
0058's compatibility note said an unknown query parameter is ignored —
which 0064 §2 had since reversed. Every one of those records had a
`Status:` header several revisions long, and none of the headers reached
the prose. A record is immutable, so the only way its body stops lying is
for something to supersede it.

66 → 72 while 6,347 → 4,270 is the pass that reached everything left. Six
records replace eighteen: 0070 (what was retired and the bar for it), 0071
(the type vocabulary), 0072 (the web UI), 0073 (search and embeddings),
0074 (the document and the vocabulary that asks it) and 0075 (the bundle
as the address space).

Two things this pass settled that the earlier ones only hinted at.
**An area with two or three records is not automatically fine.** The
first two passes went after areas with seven and nine current records and
left the rest, on the theory that small areas are healthy. That was the
wrong test: 0038 was 241 lines that the opening table already told nobody
to read, and 0017 was 229 lines the table called superseded in substance
while the file called itself current. **What matters is whether the area's
current picture is in one place, not how many records the area has.**

**And a record can outlive its own subject.** 0018's 233 lines were mostly
the specification of a compile path 0028 removed; 0019's third section
decided a model-resolution rule for the same removed feature; 0046's
604 lines still listed an eight-operation wire that six later records had
moved. None of it was wrong when written. **A record is immutable, so the
only thing that can stop its body from describing a world that ended is
something superseding it** — which is the whole argument for this program,
now with numbers.

4,500 → 5,000 is 0076, the first record to cross a boundary under the
grid below. It takes two tools off MCP, and both halves of its cost land
here: a record nothing supersedes, plus the lines 0067's `Status:` header
grew to acknowledge the amendment. That is what the ceiling is for — the
corpus grew because a decision was made, and the decision is named one
line above the number.

6,000 → 6,500 is two records that landed together, and they are the same
record twice: 0088 (a renamed MCP tool or CLI command answers for one
release) and 0089 (a half-embedded concept says so). Both say that a
thing the product was already doing *silently* has to announce itself —
a rename that left an agent's configuration pointing at nothing, and an
embedding that took the front of a concept and dropped the rest while
looking exactly like a success. Neither restates the other and neither
compresses: the decision in each is what gets said and to whom, and the
prose is where that lives. What the pair says about this corpus is that
the expensive decisions right now are not new mechanisms but **the
places where ochakai was quiet and should not have been** — which is
C7's argument arriving in the record ceiling.

5,500 → 6,000 is three records landing in one stretch of work, none of
them large: 0083 (an error carries a code), 0084 (a hit says why it
matched) and 0085 (the empty base, and what fills it). Each is a decision
a user can observe and none restates another, which is the test this
ceiling asks — what it catches is the case where the answer would have
been "compress the prose", and it is not this one. Worth naming: two of
the three record a *refusal* holding while the thing somebody wanted got
built anyway (RFC 9457 declined but errors made machine-readable; a
warehouse connector declined but the empty base filled), and a refusal
that is only ever asserted is the kind that quietly stops being true.

5,000 → 5,500 is 0079, the second crossing and the first by an ordinary
new record rather than by lines an existing one grew into. It states
where ochakai refused a document OKF SPEC §11 says a consumer must
accept, and two of those refusals had cited the spec for a requirement
the spec does not state — the kind of finding a record exists to correct
rather than to compress away.

6,500 → 7,000 is four records from one stretch of web-UI work, and what
they have in common is that each **retracts or completes something an
earlier record had left standing**: 0091 (a file's vector is keyed by its
path — 0075 said the path is the address, and one table had not been
told), 0092 (the page is modules, so it can be tested — 0072's "one
self-contained file" was how it said "no build step", and only the file
count changed), 0093 (the budget governs the whole response — 0067 §4 put
`hits` outside it, on a reason that was true and not enough) and 0094
(the page runs under a policy — 0092 §5 had listed CSP as made possible
and not decided). None of them is a new mechanism. **The expensive
records in this stretch are the ones that go back for a decision the
product had outgrown**, which is a different pressure from the one the
6,000 crossing named, and it is the healthier of the two: a corpus that
can only grow forwards is one where the earlier records quietly stop
being true.

7,000 → 7,500 is five records — 0095, 0096, 0097, 0098 and 0099 — and the
last two name a pressure the earlier crossings did not. Both were written
because a **review** found a record wrong, not because a feature needed
one: 0098 retracted 0064 §11's claim that no generator can produce a
working client once somebody ran one, and 0099 amends 0031 §3.2 after the
code had already reversed it — a purge reclaims the file bytes now, and
the record still said it never would, with the implementation citing that
same record as its authority. That is the failure mode a corpus of
immutable records has: the code moves, the record cannot be edited, and
nothing in the build compares the two, because `Status:` headers stay
self-consistent while the decision underneath them stops being true. The
6,500 crossing said the expensive records are the ones that go back for a
decision the product had outgrown; this one says the *cheapest* place to
find them is a release audit, and that the corpus grows when somebody
reads it against what shipped rather than when somebody builds.

7,500 → 8,000 is three records — 0100, 0101 and 0102 — and all three
break the REST freeze, which is what this crossing is about. 0064 §11
shipped with one reason a frozen contract may move (a security defect);
these add two more (output that does not conform to OKF, and folding away
a second spelling of something the format defines) and place two
additions outside the freeze entirely (a response-only property was
already there, an optional query parameter joins it). **A frozen contract
does not stop generating records — it changes what they are about.**
Before the freeze a record said what the wire would do next; after it, a
record says why the wire may move at all, and that argument has to be
written down at the length that makes it checkable, because the
fingerprint cannot see two of these three changes. The 7,000 crossing
said a corpus grows when somebody reads it against what shipped; this one
says it also grows when the rule for changing something is worth more
than the change.

8,000 → 8,500 is three records again — 0106, 0107 and 0108 — and this
crossing is about the freeze from the other side. 0100–0102 wrote down
why a frozen line may move; these write down that the freeze was drawn
around the wrong set — 0107 narrows the promise to the OKF core and is
the first record to retract a published promise rather than reinterpret
one, which is exactly the kind of argument that has to be on file at the
length that makes it checkable. 0108 retires the context pack from every
surface and 0106 completes the primitive read it leaves behind; both
lean on 0107 and neither could be a PR description, because all three
move what a user can observe.

    RECORD-CORPUS-LINES: 8500
    RECORD-CORPUS-LINES-SLACK: 500

`RECORD-CORPUS-LINES` counts every record under `docs/design`, Superseded
ones included: they still ship in the tree, and a reader following a
`Status:` header still opens them. Counting only what is current would let
a supersession buy headroom for the next addition — the file stays on disk
either way, so that would be the next escape hatch rather than a saving.

It is `DOC-LINES`'s argument applied to this corpus: a record that never
crosses `RECORD-LINES` can still add to what a reader gets through, and
enough of them doing it at once moves nothing else.

**It is also `DOC-LINES`'s width, and for `DOC-LINES`'s reason.** This
ceiling used to sit at the exact total with 15 lines of tolerance, beside
a second one — `RECORD-COUNT` — that had to match the number of records
exactly. Both are gone in favour of one number on a 500-line grid, after
six records were written in parallel and every one of them had to rewrite
the same two lines. [docs/surface.md](docs/surface.md) had already
diagnosed this and prescribed the cure for the manual: **an alarm that
sounds every time tells nobody anything**, and a line every concurrent PR
edits is a line every concurrent PR conflicts on. A ceiling meant to make
growth a sentence somebody writes cannot be a line somebody rewrites by
reflex.

So the ceiling is the corpus rounded up to the next 500, the slack is the
same 500, and the two facts together make it a single derived number —
which is what lets the check keep both directions. Growth still has to be
said out loud; it just gets said when the corpus crosses a boundary worth
a paragraph, roughly every third record, rather than on every record that
lands.

`RECORD-COUNT` is not replaced by anything, because it was never
independent: a record has lines, so nothing can raise the count without
raising the total. It was introduced for a shape a line total was thought
not to see — 0054/0057 and 0055/0056, one subject apiece told across two
numbers — but the fold that fixed those *lowered* the corpus by more than
any record adds, and the grid sees that in the direction that matters.
What is lost with it is the exact-match rule; what is bought is that
adding a record touches no shared line at all.

Every ceiling in this file and in [docs/surface.md](docs/surface.md) used
to check only one direction: over the number fails, under it is free. That
let headroom bank quietly — a fold could shrink what a ceiling measures
and leave the ceiling where it was, and the next addition would spend the
gap without moving a number anyone would see in the diff. It happened to
`DOC-LINES` once: a PR that shortened the deploy guide raised the ceiling
5,753 → 5,790 for room the fold needed, landed at 5,762, and left 28 lines
nobody returned ([#376](https://github.com/na0fu3y/ochakai/issues/376)).
`RECORD-CORPUS-LINES` is an amount, not a list, so it gets a stated
tolerance: `RECORD-CORPUS-LINES-SLACK` is how far the ceiling may
sit above the actual total before that gap is itself a failure, and the
ceiling has to be a multiple of it — the same pair of rules
`DOC-LINES` and `DOC-LINES-SLACK` already run under, so a fold that
lowers the total still has to lower the ceiling once it crosses a
boundary. Both are
read back by `TestDesignRecordCorpusStaysUnderItsCeiling`, and both are
checked the same way `RECORD-LINES` already was — one number in one file,
raised or lowered in the PR that earns it.

These live here, next to `RECORD-LINES`, rather than as an extra line in
[docs/surface.md](docs/surface.md)'s 上限 section. A record is read by
somebody changing ochakai, not somebody using it — that document's DOC
section already excludes `docs/design` from the manual on that basis — and
a ceiling for that same reader belongs beside the other ceiling for that
reader, not split across two files that both happen to hold a number.

Most of that list is checked rather than remembered
(`cmd/ochakai/designdocs_test.go`): a record needs an entry in both
indexes, the two indexes have to agree with the record's own `Status:`
header about whether it is current or superseded, a supersession **or an
amendment** has to be recorded at both ends — the new record naming what
it retires or revises, and the older one saying so — a new record has to
fit under its own
ceiling, a Superseded one has to be a tombstone
(`TestSupersededRecordsAreTombstones`), and the corpus as a whole has to
fit under `RECORD-CORPUS-LINES`, with no more slack than
`RECORD-CORPUS-LINES-SLACK` allows (`TestDesignRecordCorpusStaysUnderItsCeiling`).
What no test can read is the judgment: whether the opening table's row still
points at the doc somebody should actually read. That is where the attention goes.

Two decisions worth knowing before proposing features:

- **No LLM inside, no SQL execution.** ochakai stores and serves
  knowledge; interpretation and execution belong to the client agent
  (0081).
- **Secret-zero.** Auth is Cloud Run IAM + Cloud SQL IAM on Google
  Cloud, or in-process verification against an OIDC issuer's published
  keys off it (0086); features must not introduce tokens or passwords
  (0065, 0003).

## Translating a manual page

C8 (`docs/surface.md`) says ochakai should be one of the best choices
available to a Japanese-speaking team, and the manual it hands that team
was written in English while the contributor material — `docs/surface.md`
and the design records — is Japanese. The pages a person reads **to
operate and curate** moved to Japanese, one page per PR; `docs/loop.md`
is the worked example (#434) and `docs/positioning.md` was the last of
them. So this section is no longer a plan — it is the rule a **new**
manual page is written under, and the rule is that it is written in
Japanese to begin with.

**English stays** for the front door and the contract: `README.md` (an
OSS landing page is where somebody decides *whether* to use this, and
those readers are international), `api/openapi.yaml`, `docs/cli.md`
(generated from the commands' own `-h`, so translating the page means
translating the binary — a separate decision about the binary),
`docs/compatibility.md`, `ROADMAP.md`, `SECURITY.md`, `SUPPORT.md`, this
file and `CLAUDE.md`.

**No mirror.** One page, one language. A translated pair is two texts
saying the same thing, and which one went stale is exactly what nobody
can tell (`docs/faq.md`'s own rule, and the mechanism behind every stale
command this repository has shipped). The single deliberate exception is
`docs/design/README.en.md`, which a test keeps honest in both directions.

What does **not** change, because it is the product's vocabulary rather
than prose: command names, flags, environment variables, JSON keys, the
`status` / `trust` / `sort` values, HTTP header names, error strings
quoted from the binary, and design-doc citations. Quoted output — a log
line, a `--help` excerpt, a `stats` transcript — is a transcript of what
the binary prints, so it stays as printed; that is what
`docs/guides/operating.md` got wrong twice.

**Translate the example prompts.** A page showing a curator what to type
to their agent, in English, does not demonstrate the thing C8 claims.

Two pieces of bookkeeping, both easy to forget:

- **Keep the anchors.** Translating a heading moves the fragment it
  generates, and a dead fragment renders as an ordinary link that lands
  the reader at the top of the right page — wrong in a way that survives
  review. Put an explicit `<a id="…"></a>` above each translated heading
  that something links to; `docs/configuration.md` is the worked example.
  `TestManualLinksResolve` fails on a fragment with no heading and on a
  link to a file that is not there.
- **Mark the page where an English page links to it** — `(Japanese)`
  after the link text — so a reader is not surprised mid-click. **And
  take the markers off this page's own outbound links** to pages that are
  already Japanese: between two Japanese pages the mark says nothing, and
  the ones left behind are what a reader learns to stop trusting.
- **`DOC-LINES` usually needs nothing.** The ceiling moves in fives of
  hundreds (`DOC-LINES-SLACK: 500`), so most pages land inside the block
  the manual is already in and the number is not touched — which is what
  keeps nine translations from all conflicting on one line. Run
  `scripts/check core`; if it fails, round the ceiling to the next five
  hundred and write the paragraph, because crossing that grid line is
  the moment the manual actually got bigger.

  Expect it to grow rather than shrink where it moves at all. Japanese
  puts no spaces between words, so a line was expected to carry more; a
  full-width character takes two columns, so the same 70-column wrap
  holds about half as many. `loop.md` 114 → 120, `configuration.md`
  75 → 84 — but `docs/README.md` 101 → 96, because a page of one-line
  summaries loses more to the wrap than it gains. Translation never
  returns budget; only folding does.

- **Check the English before translating it.** A translation should be
  faithful, which means a description that has gone stale gets fixed in
  Japanese rather than fixed. If a page says something the code or
  another page no longer supports, say so in an issue and leave that
  sentence for a separate PR — #451 is what happens otherwise.

## Pull requests

Review is the scarce resource here, and code being cheap to write has
made it scarcer rather than less important: a large unsolicited PR
donates the half of the work that is now easy and creates the half that
is not. Issues and pull requests are still welcome — this is about how a
change should arrive, not whether. A small, self-contained fix is a PR.
Anything larger is worth proposing first, in an issue or a
[discussion](https://github.com/na0fu3y/ochakai/discussions): the default
answer to a feature is no ("Proposing a feature" above), and finding that
out from a review costs you the time you spent writing it, not only the
time it takes to read.

- Keep PRs small and focused; include tests for behavior changes.
- A PR that widens a surface — a REST operation, an MCP tool, a CLI
  command, an environment variable — names which of
  [docs/surface.md](docs/surface.md)'s eight conditions it serves, updates
  the count there, and answers the three questions in the description.
- The public wire surface is [api/openapi.yaml](api/openapi.yaml) — keep
  it, `internal/restapi`, `internal/mcpserver`, and `internal/apiclient`
  in sync (wire compatibility is pinned by tests). The REST half is
  checked, not just reviewed: the integration tests run every request and
  response past the spec (`internal/restapi/openapi_test.go`), so an
  endpoint whose shape drifts from `openapi.yaml` fails CI. A new
  endpoint needs an integration test to come under that check.
- Write commit messages and code comments in English.

## Releases

A release is a tag. Everything else is automation, but three files carry
a version number that the tag does not update on its own, so cutting a
release starts with a PR. (Working with Claude Code: the `release` skill
is this section as a running order.)

**1. Prepare, in a PR.** With CI green on `main`:

- `CHANGELOG.md` — turn `## [Unreleased]` into `## [x.y.z] - YYYY-MM-DD`,
  leave a fresh empty `Unreleased` above it, and add the compare link at
  the bottom (`[x.y.z]: …/compare/v<prev>...vx.y.z`, and repoint
  `[Unreleased]` at the new tag).
- `api/openapi.yaml` — `info.version`.
- `.github/ISSUE_TEMPLATE/bug_report.yml` — the version placeholder.

These three used to drift silently, since nothing fails when a version
number is stale. `TestReleaseVersionsAgree` now does: it reads the newest
dated heading in the changelog and requires the other two to say the same
thing, so the prep PR is green only once all three have moved.

The same PR is where a deprecation window closes. A renamed MCP tool or
CLI command answers under its old name for exactly one release
([0088](docs/design/0088-a-retired-name-answers-for-one-release.md)), so
delete any entry in `mcpserver.RetiredToolNames` or `retiredCommands`
whose `Release` is no longer the newest one. Nobody has to remember
this either: `TestARetiredNameLastsOneRelease` reads the same changelog
heading, so bumping it is what makes last release's names overdue.

While the major version is 0, a release with any **BREAKING** entry takes
the minor, not the patch.

**2. Tag the merged commit.** Annotated, and the message is the release
notes — that is the repo's habit, and Japanese is fine there even though
commits are English:

```sh
git fetch origin && git checkout -B rel origin/main
git tag -a v0.14.0 -F -   # then push: git push origin v0.14.0
```

Pushing the tag runs [release.yaml](.github/workflows/release.yaml): one
job publishes the multi-arch image to GHCR with SBOM and provenance, the
other runs goreleaser for the archives, `checksums.txt`, and provenance
over them, then packs the MCP bundle (`scripts/mcpb`) out of the binaries
goreleaser just built and attaches it. The bundle is the one-click
install for Claude Desktop: the same `ochakai mcp-stdio` bridge the
manual tells a reader to configure by hand, with the JSON written for
them.

**3. The release body arrives with the tag.** goreleaser creates the
GitHub release when none exists, and because its own changelog generation
is disabled (`.goreleaser.yaml` — the notes are written by hand) the body
would come out empty. So the workflow fills an empty body from the
annotated tag's message, which is where the notes already are, and the
Releases page is never a bare tag. Two cases still want a person:

- **A release drafted before the tag was pushed** keeps the body it was
  given — the workflow only writes into an empty one.
- **Anything beyond the tag message** — upgrade and install sections,
  links into `CHANGELOG.md` — is still an edit, and it overwrites:

```sh
git tag -l --format='%(contents)' v0.14.0 | tail -n +3 > notes.md
# then add the upgrade + install sections to notes.md
gh release edit v0.14.0 --notes-file notes.md
```

Say what an operator upgrading needs: which migrations run, whether
`updated_at` moves (held ETags, `generated.at`), whether re-embedding is
needed, and what a client reading the old shape must change.

**4. Check what shipped**, rather than trusting the green check:

```sh
gh release view v0.14.0 --json assets -q '.assets[].name'   # 6 archives, .mcpb, checksums
shasum -a 256 -c checksums.txt --ignore-missing
./ochakai version                                          # must print the tag
gh attestation verify oci://ghcr.io/na0fu3y/ochakai:0.14.0 -R na0fu3y/ochakai
gh attestation verify ochakai_0.14.0_linux_amd64.tar.gz -R na0fu3y/ochakai
gh attestation verify ochakai_0.14.0.mcpb -R na0fu3y/ochakai   # not in checksums.txt
curl -s https://proxy.golang.org/github.com/na0fu3y/ochakai/@latest
```

A tag is effectively permanent — the Go module proxy caches it forever,
and a bad one can only be retracted in `go.mod`, never withdrawn. That is
why the checks above come before announcing anything, and why the prep is
a reviewed PR rather than a push to `main`.
