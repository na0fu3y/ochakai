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

The database is shared: CI runs one container for a whole run, and by
hand you keep one up across sessions. So **an integration test takes its
ids from `internal/testdb`.Unique**, which appends a run-unique number
and registers the sweep that removes every row written under it. A test
that rolls its own token instead leaves its rows behind, and the corpus
grows until a bounded ranking assertion somewhere else stops finding its
own concept — a failure that reads as a search bug in whatever change
happens to cross the threshold. `TestNoTestRollsItsOwnNamespace` fails
on a hand-rolled one.

`--db` prefers Docker, because `pgvector/pgvector:pg17` is the database
CI runs. Where Docker cannot serve one — no daemon, or a network that
will not let the image be pulled — it builds a throwaway cluster out of
whatever PostgreSQL the machine has installed, provided pgvector is
installed beside it, and deletes it on exit. That is a real run of the
store tests but not CI's, so the script's closing line names which
database it used.

## Working with Claude Code

The repository checks in a `.claude/settings.json` with **hooks only**.
One runs `gofmt` on Go files as they are written, since that is the
cheapest CI failure to avoid. The other runs at session start and does
nothing at all unless `CLAUDE_CODE_REMOTE` says the session is a
disposable cloud container, where it installs pgvector beside the
container's PostgreSQL so `scripts/check --db` has something to fall back
to, and tells the agent what that environment cannot check. Docker has a
CLI there but no daemon, and starting `dockerd` does not help because the
egress policy refuses Docker Hub's blob CDN; the same policy refuses
`vuln.go.dev`. So the store tests run, `core` and `lint` are honest there,
and the `Dockerfile`, `deploy/compose.yaml` and `govulncheck` are CI's to
verify — which is worth saying plainly, because an agent that reports a
blocked host as a failing check is the expensive outcome. Permissions
are a personal choice and belong in `.claude/settings.local.json`, which
is not tracked.

`.claude/skills/` holds the procedures that are long enough to get wrong
from memory: `release` and `design-doc`. They are the same procedures
written out below and in [CLAUDE.md](CLAUDE.md); if you change one,
change the other.

Unrelated to any of this, `examples/claude-code/` is a *product* example
— hooks that pull team knowledge out of a running ochakai and write back
to it. It is not part of this repository's own setup.

## Proposing a feature

The default answer is no, and [docs/surface.md](docs/surface.md) is where
that is made concrete. It counts every REST operation, MCP tool, CLI
command and environment variable in one place — what somebody using
ochakai actually pays for — and `cmd/ochakai/surface_test.go` fails when
the count and the build disagree, so an addition arrives as a heading
moving from `(19)` to `(20)` rather than as a few lines buried in a spec.

The same document opens with the seven conditions ochakai exists to
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
new record needs a summary in
[README.en.md](docs/design/README.en.md) beside it, which a test checks.
[docs/architecture.md](docs/architecture.md) summarizes the accepted ones
in English if you would rather read the shape of the system first.

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
- Update [docs/design/README.md](docs/design/README.md): add the new
  doc to its area with a one-line summary, adjust the status notes of
  the docs it amends, and — if it becomes the doc to read for that area —
  update the area's row in the opening table.
- Summarize it in `README.en.md`. A doc the new one supersedes drops to a
  one-line pointer at its replacement; full summaries are maintained only
  for what is current, and a test holds it — cutting the old summary is
  part of superseding, not tidying for later.
- Avoid amendment chains. If the new decision would be the second
  partial amendment stacked on the same doc, don't add another diff:
  write it as a full replacement that states the area's whole current
  picture and mark the older docs Superseded.
- Keep it under the ceiling below. A record is prose somebody reads.

### How long a record gets

    RECORD-LINES: 220

0048 narrowed *what* earns a number. It said nothing about *how much*,
and the corpus grew accordingly: 58 records and about 10,400 lines, on
top of a 25-page manual of 5,700. A decision that renames two words has
cost 150 lines of record, an entry in each index and a paragraph in
[docs/surface.md](docs/surface.md) — the explanation outgrowing the
thing explained is the shape the numbers have been showing for several
releases.

So a record from 0063 on stays under that line, and
`TestDesignRecordsStayUnderTheirCeiling` reads it back out of this file.
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
3. **It really is that large** — 0046 rebuilt the address space in 601
   lines and earned every one. Then raise the line above and say so.

What the ceiling must not buy is denser prose. Records earlier than 0063
are not measured: they are immutable, and immutability is a promise about
decisions somebody could be depending on, not a licence to keep writing
at whatever length the last one happened to be.

Most of that list is checked rather than remembered
(`cmd/ochakai/designdocs_test.go`): a record needs an entry in both
indexes, the two indexes have to agree with the record's own `Status:`
header about whether it is current or superseded, a supersession has
to be recorded at both ends — the new record naming what it retires, and
the retired one saying so — and a new record has to fit under the
ceiling. What no test can read is the judgment: whether the opening
table's row still points at the doc somebody should actually read. That
is where the attention goes.

Two decisions worth knowing before proposing features:

- **No LLM inside, no SQL execution.** ochakai stores and serves
  knowledge; interpretation and execution belong to the client agent
  (0001).
- **Google Cloud only, secret-zero.** Auth is Cloud Run IAM + Cloud SQL
  IAM; features must not introduce tokens or passwords (0002, 0003).

## Pull requests

- Keep PRs small and focused; include tests for behavior changes.
- A PR that widens a surface — a REST operation, an MCP tool, a CLI
  command, an environment variable — names which of
  [docs/surface.md](docs/surface.md)'s seven conditions it serves, updates
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
over them.

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
gh release view v0.14.0 --json assets -q '.assets[].name'   # 6 archives + checksums
shasum -a 256 -c checksums.txt --ignore-missing
./ochakai version                                          # must print the tag
gh attestation verify oci://ghcr.io/na0fu3y/ochakai:0.14.0 -R na0fu3y/ochakai
gh attestation verify ochakai_0.14.0_linux_amd64.tar.gz -R na0fu3y/ochakai
curl -s https://proxy.golang.org/github.com/na0fu3y/ochakai/@latest
```

A tag is effectively permanent — the Go module proxy caches it forever,
and a bad one can only be retracted in `go.mod`, never withdrawn. That is
why the checks above come before announcing anything, and why the prep is
a reviewed PR rather than a push to `main`.
