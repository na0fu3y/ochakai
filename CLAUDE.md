# CLAUDE.md

ochakai is a knowledge store for data-analysis agents. Two decisions
frame everything: nothing but a ruling changes what is served — a
person's, or a confirmation job's recorded under its own `process:`
identity and read as `machine-confirmed`, never as human-reviewed; no
LLM rules — and the server executes no SQL (0142 — its own data agent is off by
default and rules on nothing), and zero secrets — Cloud Run IAM + Cloud SQL IAM on Google Cloud,
in-process OIDC verification off it (0086), never tokens or passwords
(0065, 0003).

## Design: spec and decisions

What ochakai does lives in [docs/spec](docs/spec/README.md), one
document per area, **rewritten in place**: a PR that changes what a user
can observe (the wire, the stored form and its round trip, what identity
and provenance mean, a Google Cloud dependency, something ochakai refuses
to do) edits that area's document in the same PR, in the present tense.
Internal changes that leave the outside unchanged belong in the PR
description.

Why it is not otherwise lives in [docs/decisions](docs/decisions/README.md):
short entries (`DECISION-LINES`, checked), never rewritten, numbered from
0153, written only when somebody is likely to reopen the choice. A rule
added inside an area is the spec and the CHANGELOG, not an entry.

[docs/design](docs/design/README.md) — records 0001–0152 and their
index — is frozen history: every area has its spec document, and nothing
there is edited any more. The `design-doc` skill has the procedure;
CONTRIBUTING.md's "Design: spec and decisions" has the reasons.

## Surface, and the default answer

What ochakai costs the person using it is its **surface** — the endpoints
they can call, the tool schemas that spend their agent's context, the
commands they have to learn, the variables they have to set, the pages of
the manual they have to read — not the code behind it.
[docs/surface.md](docs/surface.md) counts all nine dimensions in one
place, and `cmd/ochakai/surface_test.go` fails when the count and the
build disagree, so an addition shows up as a heading moving from `(19)`
to `(20)` instead of disappearing into a spec diff. **Prose is counted
too**, in pages: adding one to the manual moves `DOC` from `(27)` to
`(28)`. The line totals of the manual and the contract are no longer
capped — those ceilings are retired and 上限 says why — so a page that
grows without a new page appearing is caught by review, not by CI.

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
Rulings — verify and reject — belong to the human; a `process:`
verification is a confirmation job's (a CI canary), never an agent's.
