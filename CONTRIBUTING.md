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
`CGO_ENABLED=0 go build -trimpath ./...`, golangci-lint, zizmor over
`.github`, and govulncheck, in that order. A step at a time when you are
iterating:

```sh
scripts/check core     # the four fast ones — this is what CI's test job runs
scripts/check lint
scripts/check actions
scripts/check vuln
```

CI calls the same script for `core` and reads the pinned golangci-lint
and zizmor versions out of [ci.yaml](.github/workflows/ci.yaml), so there
is no second copy of any of this to keep in sync.

`actions` audits [.github](.github) — the workflows and `dependabot.yml`,
the part of this repository that decides what runs with which token and
that no other check reads. Both this and CI hand zizmor the repository
root and let it collect, so neither can end up looking at a smaller set
than the other. It reports nothing today, which is the condition below
for a check existing at all: the properties it looks for (actions pinned
by commit SHA, `persist-credentials: false`, the narrowest `permissions`,
no untrusted value reaching a `run:`, a cooldown before a version update
lands) are ones this repository keeps, so from here it buys not cleanup
but that the next file added there cannot quietly drop one. It needs
`uvx` (or a `zizmor` on `PATH`) and skips with a note when neither is
there, the way the web UI step skips without node.

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
is a skip locally and a failure under `OCHAKAI_TEST_SMOKE_REQUIRED=1`, which
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

### The search eval harness

The standing measurement is `TestSearchEvalIntegration`
(`internal/service/searcheval_integration_test.go`): a golden set of
questions, each paired with the concept a good ranking puts in front of
the reader, scored as recall@10 and MRR over one base holding two of the
bundles this repository ships — `examples/demo` and `kb/bundle`,
which with the two concepts the file defines inline is twenty-nine
concepts across two domains that share nothing but the language they are
written in. It runs the set twice — lexical only,
which is the search a deployment without Vertex AI gets, and lexical
fused with a vector ranking from the stand-in encoder beside it, which
is the configuration every Google Cloud deployment runs (design doc 0080
§1.1). The stand-in is deterministic, so the fused half needs no API key
and reaches no network.

Beside recall@10 and MRR it reports **reach**: how many concepts each
question touched at all, meaned over the set. Ranking numbers read one
position and are blind to what arrives under it — migration 0042 was
found outside this harness because every katakana question had degraded
from a lookup to a prefix scan while recall stayed 1.00 and those cases
stayed at rank 1. Beside it, and the reason the base holds two bundles
rather than one, is **leak**: how many of the concepts a question
reached came from the domain it was not about, which is the search
matching on Japanese rather than on subject. A corpus about one subject
cannot ask that question — there, reaching every neighbour is honest.
Both carry a ceiling that fails in both directions, like the floors, and
reach is read *with* the recall floor: recall says nothing fell out,
reach says how much came with it. It is measured on the lexical list
alone — cosine is never zero, so a fused reach would be the corpus size
for every query.

Each case is labelled with the dimension of query behaviour it
exercises — natural questions, keyword lookups, warehouse English
inside a Japanese sentence, English questions, spelling variants,
synonyms, two-character terms — and both runs log a recall/MRR line per
dimension beside the aggregate. The floors hold only the aggregate; the
dimension lines are where to look when deciding what to improve next,
and where a change to search should show its effect landing. A question
the bundle deliberately answers in several linked places lists the
other correct concepts in `accept`, and the first acceptable concept's
rank is what scores.

```sh
scripts/check --db      # it runs with everything else
OCHAKAI_TEST_DATABASE_URL=… go test ./internal/service/ \
    -run TestSearchEvalIntegration -v      # -v prints the per-case ranks
```

A change to ranking lands with these numbers in its PR. **The floors
beside them fail in both directions.** Under a floor is the regression
the floor exists to catch. More headroom over it than
`evalFloorSlackCases` allows is a floor nobody raised when the ranking
improved, which is `DOC-LINES-SLACK`'s argument applied to a floor
rather than a ceiling: banked headroom is budget the next regression
spends without moving a number anybody reads. The tolerance is derived
from the size of the set instead of declared here, because the noise it
is sized against is one case moving and the set has been 14, 36, 41, 37
and 56 cases.

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
On a contributor's machine the session-start hook also brings that
instance up — `docker compose up -d` when Docker is running and nothing
answers on :8080, and a first load of `kb/bundle` when the instance is
empty and the bundle carries no `verified:` key, so that nothing is
ruled on. Both loop hooks stay silent by design when the instance is
down, which for a fortnight read as "nothing relevant" while the stack
was simply not running; recall now says so once per session instead.
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
way carries the banner — and an アクセス tab, because dev's anonymous
caller reads as an administrator — and the committed ones do not: what
the pictures are for is the product, and both of those are about the
machine you happen to be running it on. Remove them from the page before
capturing — `.dev-note`, `.sandbox-note` and `#nav-access`. Neither of
the two obvious alternatives works. A crop cannot reach the banner,
which sits inside `.content` below the header rather than being a strip
across the top, so taking it off takes the navigation with it; and no
other posture can be shot from here, because the default one asks for an
identity `ochakai ui` has none to give (`/api/v1` answers 401) while
`read-only` and `public` refuse the writes the review queue is a picture
of.

The bundle already carries the two drafts the review queue needs and a
draft whose `stale_after` has passed. The re-verification feed is the one
thing it cannot ship, because a failure report is an event rather than a
document — report one, and read a few entries so the usage counts on the
cards are not all zero:

```sh
go run ./cmd/ochakai report queries/sales/monthly-revenue failed --note "先週の
  レポートと数字が合わない。receipt を確かめたら先週の実行に job_id が無く、
  比較が成立していなかった。"
go run ./cmd/ochakai search "なぜ売上が落ちているのか"
go run ./cmd/ochakai get metrics/repeat-purchase-rate   # the queue's cards
go run ./cmd/ochakai get queries/sales/revenue-by-traffic-source
go run ./cmd/ochakai verify metrics/revenue   # the entry shot's ✓ human-reviewed
```

Then serve the UI as yourself:

```sh
go run ./cmd/ochakai ui --port 8098
```

Shoot four hash routes, at a 1440 CSS-px width and a device scale factor
of 1.25 — 1800 px wide, which is what the committed files are and what
keeps all four under 1 MB together. The heights are chosen so each shot
ends just under its last card; the UI is a fixed-viewport shell whose
panes scroll internally, so the height is a crop, not a measurement, and
a UI change moves them.

| Route | File | Height |
|---|---|---|
| `#/review` | `webui-review.png` | 790 |
| `#/k/metrics/revenue` | `webui-entry.png` | 360 |
| `#/search/reported-wrong` | `webui-wrong.png` | 395 |
| `#/` | `webui-tree.png` | 700 |

Three things have to be forced, or the pictures come out wrong in ways
that are easy to miss:

- **Light scheme.** Headless Chrome defaults to dark.
- **A Japanese font on the machine.** The UI asks for `system-ui` and
  `sans-serif` and takes whatever fontconfig hands back, which on a
  machine carrying no Japanese font is a Chinese one — every page then
  renders in glyph forms a Japanese reader notices and nothing in the
  shot explains. Install one (`fonts-noto-cjk` on Debian) and check
  `fc-match 'sans-serif:lang=ja'` before shooting.
- **A `ja-JP` locale.** The entry page prints its dates with
  `toLocaleDateString()`, which reads ICU's default locale, while the
  rest of the page is the Japanese UI serving a Japanese knowledge base
  — so a shot taken under an English locale is Japanese everywhere
  except its dates. `--lang`, `--accept-lang` and `LANG`/`LC_ALL` do not
  settle it: `navigator.language` reports what you asked for while the
  dates follow the machine. Override it over the DevTools protocol with
  `Emulation.setLocaleOverride` (light scheme via
  `Emulation.setEmulatedMedia`), and check the result in the page:
  `Intl.DateTimeFormat().resolvedOptions().locale` must be `ja-JP`.

The entry route scrolls its sidebar to reveal the selected node, which
clips the panel header; scroll back to the top before capturing, and
blur whatever the navigation focused or a focus ring frames the panel.

Read every PNG before committing it. Reject any that is dark, dated in
English, mostly empty, or showing an error or empty state — a dead
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

That picks the *areas*. It does not pick the *grain*, and for four weeks
after 0048 the corpus grew at the same rate as before it, one rule at a
time: who may write the first policy row, what a scoped caller's counts
include, whether a prefix can hold its own administrator — each
observable, each inside a decision already on file. So
[0128](docs/design/0128-a-number-is-for-an-area-not-a-rule-inside-it.md)
adds the grain: **a number is for an area's decision; a rule inside it
takes none.** If the parent record and the CHANGELOG answer the question
somebody reopens in three months, the PR and the CHANGELOG are where the
rule goes. Only when the parent's own text now contradicts the rule is
it an amendment — and then see the replacement rule below, not a diff.

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
- Amend a *released* record by replacing it, not by stacking a diff on
  it (0128 §2.2): the new record states the area's whole current picture
  and marks the parent and every amendment on it Superseded. This used
  to be a step in the `design-doc` skill that nothing checked, and 0109
  took five amendments in a week; what checks it now is the table
  ceiling below.
- Keep it under the ceiling below. A record is prose somebody reads.

### How long a record gets

    RECORD-LINES: 300
    RECORD-CAP-FROM: 0065
    INDEX-ROW-RECORDS: 10

`INDEX-ROW-RECORDS` is the third number, and the only one that moves in
one direction. It bounds how many records one row of the index's opening
table may cite, and `TestCeilings` holds it equal to the
widest row — so folding a row is what lowers it and nothing lowers it
quietly. The table is the one line a reader reaches an area's current
state from (0048 §2.4); on 2026-08-23 four rows cited eight to ten
records each, which is the amendment chain moved into the table. The
number starts at that day's maximum and comes down one fold per PR,
widest row first, until it reaches 3 — a record, its amendment, and one
more — and stays there. A PR that would cite a fourth writes the fold
instead (0128 §2.3, §2.4).

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
`TestCeilings` (`cmd/ochakai/ceilings_test.go`) reads both numbers back out of
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
`TestCeilings` holds a Superseded record to
`TOMBSTONE-LINES` and requires the commit link; going over it means the
record is still carrying prose nobody rereads.

### When a record's premise turns out to be wrong

A record states two kinds of thing: what was decided, and what was true
about the world when it was decided. There are doors for the first —
supersede it, or amend it by replacing it (0128 §2.2) — and until now
none for the second, which meant a record whose decision still stands had
no way to say that one of its reasons does not.

[0133](docs/design/0133-an-okf-moment-is-an-instant.md) is the case that
found this. Its §1 read ochakai's old date-only code as having misquoted
OKF; the code had in fact matched the spec as published, which changed a
normative rule seven days before 0133 was written without moving its
version. **The decision 0133 reached was right, and is still right** —
which is exactly why there was nothing to do about the premise. The error
then propagated: it was copied into the English index summary, into the
CHANGELOG, and into `docs/positioning.md`, and the correction reached one
of the three before the other two were noticed a week later.

So: **a factual premise is corrected in the `Status:` block, and the body
is not edited.** One line, naming what the body says, what is true, and
where that was established. Had 0133's decision not also moved — it did,
so 0139 superseded it and its body is a tombstone now — the line it would
carry is the shape to copy:

    Corrected(2026-09-05): §1 reads the old date-only rule as a misquote
    of SPEC §5; it matched the spec as published on 2026-07-24, which
    changed on 2026-08-21 without moving its version (0139 §1.1).

Then apply the same correction to that record's row in
[README.en.md](docs/design/README.en.md), which restates the record in the
present tense and would otherwise keep repeating the error.

**Immutability does not object**, for the reason the tombstone rule gives:
it is a promise about *decisions* somebody could be depending on, and a
claim about the outside world is not one. The `Status:` block is already
the mutable part — 0064's carries a growing list of what later records
revised in it — and this is that same block being asked one more question.

**Do not reach for supersession here.** It requires restating the whole
area, and it would then shrink a record that is still the current answer
to a tombstone. If the decision moves too, that is an ordinary
supersession and this rule is not needed.

This is not a numbered record, by 0128's own rule: it is a rule inside an
area, not an area's decision, and nothing a user of ochakai can observe.

### How many records, and how much

`RECORD-LINES` bounds one record's thickness, and the tombstone rule
bounds what a replaced one keeps. Nothing bounds the total any more, and
what that gives up is worth stating plainly.

**The count is the number that moved**: 19 records at v0.10.0 against 137
today, while non-test Go grew 2.5x and REST retreated from 19 operations
to 11. Lines did not outrun the code — the corpus is under the code's own
total — but that is the tombstone rule doing the work, not the count
slowing down: a record still arrives about as often, it just stops costing
lines once something replaces it. **Count is what to watch**, and nothing
watches it. Folding a surface leaves a record behind, and
[docs/surface.md](docs/surface.md)'s DOC section names the rest of that
residue — an index entry in each language, an English summary, a paragraph
in that file or this one.

**`RECORD-CORPUS-LINES` was that ceiling and it is retired**, with
`DOC-LINES` and `REST-LINES`; [docs/surface.md](docs/surface.md)'s 上限
section holds the reasoning for all three. The short of it: the ceiling
asked for a paragraph every time the corpus crossed a 500-line boundary,
and those paragraphs — a crossing log, a grid rule, a slack number, and
the two widenings that made the grid — became the largest thing the
ceiling produced. What it was built to catch (fold a surface, then write
a long explanation of the fold) it caught once or twice in ten crossings.

What holds instead is what 0048 and 0128 already decide: a number is for
a decision a user can observe, and for an area rather than a rule inside
it. A corpus that grows because those two rules are being followed is a
corpus growing correctly, and one that grows because they are not is a
review problem rather than an arithmetic one. If that turns out to be
wishful, the ceiling comes back and this paragraph is what it argues
with.

Most of that list is checked rather than remembered
(`cmd/ochakai/designdocs_test.go`): a record needs an entry in both
indexes, the two indexes have to agree with the record's own `Status:`
header about whether it is current or superseded, a supersession **or an
amendment** has to be recorded at both ends — the new record naming what
it retires or revises, and the older one saying so — a new record has to
fit under its own
ceiling, and a Superseded one has to be a tombstone
(`TOMBSTONE-LINES`).
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
- **No line ceiling moves.** `DOC-LINES` used to cap the manual's total
  and is retired (the 上限 section of [docs/surface.md](docs/surface.md)
  says why); what `scripts/check core` still reads is the page count,
  and a translation adds no page. Expect the page to grow rather than
  shrink. Japanese puts no spaces between words, so a line was expected
  to carry more; a full-width character takes two columns, so the same
  70-column wrap holds about half as many. `loop.md` 114 → 120,
  `configuration.md` 75 → 84 — but `docs/README.md` 101 → 96, because a
  page of one-line summaries loses more to the wrap than it gains.

- **Check the English before translating it.** A translation should be
  faithful, which means a description that has gone stale gets fixed in
  Japanese rather than fixed. If a page says something the code or
  another page no longer supports, say so in an issue and leave that
  sentence for a separate PR — #451 is what happens otherwise.

## The web UI's Japanese

`internal/webui/static/` is the one surface a non-contributor reads
whole, and it is Japanese (C8). It was translated once in a single pass
and shipped 40-50 English strings behind a CHANGELOG line claiming every
one of them was done — because nothing in this repository read the copy.
`internal/webui/jstest/copy.test.js` reads it now, against the rules
below. It sees string literals only, including the ones inside a
template's `${...}`, which is where the half-translated tooltips hid.

**Register follows the element, not the file.** The page used to pick a
form by where a string happened to sit — prose polite, tooltips plain,
and one tooltip both at once.

| Element | Form |
|---|---|
| Prose, banners, hints | ですます |
| Toasts, `confirm`, `prompt`, error banners | ですます, one sentence |
| Buttons, tabs, column headings, badges, tile labels | 体言止め, ~8 characters |
| `title` | ですます, one sentence, no 。, under 40 characters |
| `aria-label` | 体言止め — it is the control's *name*, not a description |

The two attributes are split because they are different things: a
`title` is a description a reader hovers for, an `aria-label` is the
accessible name a screen reader announces in place of the control. A
sentence in the second is not a label at all.

A tile label is a noun, not a clause: 「結果報告 / 利用」 rather than
「使われた concept のうち結果が返ってきたもの」. A tooltip is a sentence,
not a paragraph — needing a second one means the thing it explains
belongs in the prose above it, where there is room.

**One word for one thing.** 「ナレッジ」 is a concept as the reader meets
it, 「ナレッジベース」 is all of them, and 「ドキュメント」 is the stored
OKF text — the frontmatter and markdown the editor opens and `export`
writes. `concept` is the wire's word: it stays in ids, in tool names
(`put_concept`), and in `api/openapi.yaml`, and never reaches the page.

**What stays English** is the vocabulary a reader also types or sees
stored, where translating would put the screen and the frontmatter into
disagreement. Declared here so the list a writer consults and the list
the check enforces are one list, for the reason `RECORD-LINES` is:

    COPY-ENGLISH-TERMS: OKF SPEC YAML MiB REST MCP CLI API
      frontmatter markdown export import ochakai ui serve-ui access
      draft stable deprecated rejected unverified
      machine-confirmed human-reviewed outcome failed

Two or more English words in a row fail the check unless every word in
the run is on that list — which is what separates a name from a
sentence, because English prose always reaches for a word that is not.
Add a term when a reader would type it; translate it otherwise.

**Punctuation.** Half-width parentheses, as everywhere else here (the
design records use them 729 times and the full-width form none). But
full-width ？ and ！ after Japanese: no other page in this repository has
ever needed a question mark, so the UI is the one place with the rule to
pick, and half-width reads broken directly after kana.

**A Japanese paragraph is one source line.** A newline between two
Japanese characters is rendered as a space — browsers do not apply the
segment-break removal that would have spared it, which was measured
rather than assumed — and a half-width space mid-sentence is
conspicuous in a language that has none. The page carried 61 of them,
three in the home page's first paragraph, before anything looked. Lines
get long; that is the trade, and the check holds it. A space between
Japanese and Latin stays correct and is left alone: 「ナレッジ 5 件」
and 「<code>stale_after</code> を過ぎた」 both want one.

**Two rules the check reads that are not style.** A plural branch
(`n === 1 ? 'draft' : 'drafts'`) is untranslated copy by construction —
Japanese does not inflect, so the branch cannot survive a translation,
which makes it a reliable place to start grepping. And a failure message
names its operation: 「検証できませんでした」, not 「失敗しました」. One
sentence shared by six actions tells a curator nothing about which of
them it was.

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
- A behavior change gets a line under `## [Unreleased]` in
  `CHANGELOG.md`. The `changelog` workflow fails a PR that moves a
  non-test file under `internal/` or `cmd/` without touching the
  changelog, and one whose entry landed below the first released
  heading (a branch cut before a release, merged after it); the
  `no-changelog` label is the way out when the change is not behavior,
  and the description says why.
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
git tag -a v0.14.0 --cleanup=verbatim -F notes.md   # then push: git push origin v0.14.0
```

`--cleanup=verbatim` because the notes are Markdown: without it git
drops every `#`-led line as a comment, and the headings are gone before
anyone reads the tag.

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
