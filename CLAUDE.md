# CLAUDE.md

ochakai is a knowledge store for data-analysis agents. Two decisions
frame everything: no LLM inside and no SQL execution (0081), and
zero secrets — Cloud Run IAM + Cloud SQL IAM on Google Cloud,
in-process OIDC verification off it (0086), never tokens or passwords
(0065, 0003).

## Design docs

Architecture decisions live in [docs/design](docs/design) as numbered,
immutable decision records (mostly Japanese).
[docs/design/README.md](docs/design/README.md) is the index — its
opening table says which doc describes each area today; the prose below
it carries the history, and a superseded or amended doc says so in its
`Status:` header.

A numbered doc is for a decision **a user can observe**
([0048](docs/design/0048-decision-records-for-wire-contracts.md)): the
shape of the wire, the stored form and its round trip, what identity and
provenance mean, a new Google Cloud dependency, or something ochakai
refuses to do. Internal changes that leave the outside unchanged belong
in the PR description instead. **A doc that has not reached a release is
revised by replacing it, not by taking a new number** — immutability is a
promise about decisions somebody could be depending on.

When one is needed, it lands in the same PR as the change and the index
stays truthful — the `design-doc` skill has the full procedure (new
number, older docs' `Status:` headers, the index entry, and why amendment
chains get a replacement instead). The bookkeeping half of it is checked:
`cmd/ochakai/designdocs_test.go` fails on a record missing from an index,
an index that disagrees with a record's `Status:` header about whether it
is current, or a supersession recorded at only one end.

**A record has a ceiling, and so does the corpus.** CONTRIBUTING.md
declares `RECORD-LINES` for one record's length and
`RECORD-CORPUS-LINES` for all of `docs/design` together, Superseded
records included; the same test file reads both back. The corpus ceiling
sits on a grid the width of its own slack, so adding a record usually
moves no number at all — it moves when the corpus crosses a boundary,
which is when the paragraph explaining it is worth reading. 0048 narrowed
what earns a number, not how many pile up: records have grown 4.1x since
v0.10.0 against non-test Go's 2.5x, and it is the tombstone rule, not a
slowdown, that keeps their line total under the code's.
Over a line usually means two decisions, or a record restating
one that already exists — not a record that needs denser prose, and not a
corpus whose answer is a bigger number rather than a look at what it is
carrying.

## Surface, and the default answer

What ochakai costs the person using it is its **surface** — the endpoints
they can call, the tool schemas that spend their agent's context, the
commands they have to learn, the variables they have to set, the pages of
the manual they have to read — not the code behind it.
[docs/surface.md](docs/surface.md) counts all nine dimensions in one
place, and `cmd/ochakai/surface_test.go` fails when the count and the
build disagree, so an addition shows up as a heading moving from `(19)`
to `(20)` instead of disappearing into a spec diff. **Prose is counted
too**: the manual has a ceiling on its line count, so explaining a fold
at length is a decision like any other.

That document opens with the **eight conditions** ochakai exists to
satisfy — the knowledge stays the user's, secret-zero on Google Cloud,
OKF v0.2, no forward-deployed engineer, usable from Claude Code, a small
embeddable REST API, a measurable improvement loop, and being one of the
best choices available to a Japanese-speaking user weighing similar
services. Read them before proposing anything a user would touch. **A
proposal that serves none of the eight is one to decline**, and saying so
is the more useful answer; naming which one it serves is where a proposal
that survives begins.
Serving a condition is necessary and not sufficient — every condition has
infinitely many mechanisms that would serve it.

**The default answer to a new feature is no.** Before writing code that
widens a surface, answer that document's three questions in the PR
description: who actually got stuck, whether an existing surface already
covers it (if it does, don't add), and what can be folded away in
exchange. Per-surface defaults are
[0067](docs/design/0067-four-faces-and-what-they-decline.md) — REST is the only
contract, CLI is the completeness surface, MCP's default is *no* because
tool schemas are paid for out of the agent's context window, and the web
UI is a curation surface rather than a BI tool. Proposing the smaller
thing, or nothing, is the useful answer more often than not.

## Checks and conventions

Run the checks CI runs:

```sh
scripts/check          # everything; `scripts/check core` is CI's test job
scripts/check --db     # plus a throwaway PostgreSQL for the store tests
```

CI calls the same script, so the two cannot drift. Linters are
configured in [.golangci.yml](.golangci.yml) and chosen so a clean tree
reports nothing ([docs/design/0035](docs/design/0035-verifiability.md));
[CONTRIBUTING.md](CONTRIBUTING.md) explains the store test setup and
fuzzing.

- The public wire surface is [api/openapi.yaml](api/openapi.yaml); keep
  it, `internal/restapi`, `internal/mcpserver`, and `internal/apiclient`
  in sync.
- New features must land consistently across surfaces per
  [docs/design/0067](docs/design/0067-four-faces-and-what-they-decline.md)
  (REST / MCP / CLI / Web UI — including deliberate omissions), and
  [docs/surface.md](docs/surface.md) has to stay true to what shipped.
- Write commit messages and code comments in English.
- **A new manual page under `docs/` is written in Japanese** — C8 is why,
  and the pages a person reads to operate and curate have all moved.
  English stays for the front door and the contract: `README.md`,
  `api/openapi.yaml`, the generated `docs/cli.md`,
  `docs/compatibility.md`, `ROADMAP.md`, `SECURITY.md`, `SUPPORT.md`,
  `CONTRIBUTING.md` and this file. No page gets a translated mirror —
  one page, one language, because nobody can tell which half of a pair
  went stale (CONTRIBUTING.md, *Translating a manual page*).
- Cutting a release is a reviewed PR, then a tag, then verification —
  use the `release` skill rather than working from memory. A pushed tag
  is permanent.

## The project's own knowledge base

[kb/](kb) is ochakai's own OKF bundle — development and operating
knowledge, dogfooded through a local instance (see
[kb/README.md](kb/README.md)). When that instance is up, a hook recalls
relevant concepts into your context, and the ochakai MCP tools are the
write path: learnings land as drafts via `put_concept`, outcomes via
`report_outcome`. Never write through the `ochakai` CLI here — the
checked-in deny list enforces it — because the dev instance records CLI
callers as the anonymous human, while the MCP connection carries your
process identity, and that distinction is what keeps the trust tier
honest about who reviewed what
([kb/bundle/policies/ai-human-identity.md](kb/bundle/policies/ai-human-identity.md)).
Rulings — verify and reject — belong to the human.
