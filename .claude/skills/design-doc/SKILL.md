---
name: design-doc
description: Add a numbered design doc to docs/design and keep the index and older docs' Status headers truthful. Use when a change alters an accepted decision — a new interface, a new Google Cloud dependency, a change to the auth model or a surface.
---

# Adding a design doc

`docs/design` holds decision records. They are **immutable** — once
accepted, a doc is not rewritten. The incremental history lives in the
docs and in PRs. What the repo must keep readable is the *current* state
per area, and that is entirely the job of the `Status:` headers and
[the index](../../../docs/design/README.md).

So the deliverable is never just a new file. It is a new file **plus**
every older doc it touches **plus** the index, in one PR.

## When one is needed

A change that alters an accepted decision: a new interface, a new
dependency on a Google Cloud service, a change to the auth model, a
change to what a surface carries. Small fixes and additions *within* an
existing decision do not need one.

Two decisions to check any proposal against first:

- **No LLM inside, no SQL execution** (0001). Interpretation and
  execution belong to the client agent.
- **Google Cloud only, secret-zero** (0002, 0003). Cloud Run IAM + Cloud
  SQL IAM; no tokens, no passwords.

## Steps

**1. Read the area first.** Open the index and read every doc for the
area, following the `Status:` notes. You need to know exactly which
decisions you are overturning before you can write the header.

**2. Take the next free number.**

```bash
ls docs/design | tail -5
```

Gaps are fine — proposals that never landed stay in their PRs. Filename
is `NNNN-kebab-case-slug.md`.

**3. Write it.** Japanese is the maintainer's habit, not a rule — write
in English if that is what you think in, and say so in the PR. The
review cares that the decision **and its rejected alternatives** are
legible. The `Status:` header names what this doc supersedes or amends:

```
# ochakai 設計ドキュメント NNNN: <題>

Status: Accepted(YYYY-MM-DD)。[00XX](00XX-….md) を Superseded にし、
[00YY](00YY-….md) §N の「…」を改訂する。
Date: YYYY-MM-DD
```

Use 0038 or 0015 as models for the header style.

**4. Update the older docs' `Status:` headers.** Every doc the new one
supersedes or amends gets a line pointing at the new number. See 0011 or
0018 for the style. This is the step that is easiest to forget and the
one the whole scheme rests on.

**5. Update the index.** Add the doc to its area with a one-line summary
in the surrounding style (bold status, then what it decides), and adjust
the status notes of the docs it amends.

**6. Avoid amendment chains.** If this would be the *second* partial
amendment stacked on the same doc, do not add another diff. Write a full
replacement that states the area's whole current picture, and mark the
older docs **Superseded**.

## Surface consistency

If the change adds or changes a feature, [0015](../../../docs/design/0015-surface-consistency.md)
requires the PR to state, per surface, where it lands — **including the
deliberate omissions**:

- **REST** is the only contract. Everything lands on `/api/v1` or
  nowhere; the other three are its clients.
- **CLI**: yes by default — it is the completeness surface.
- **MCP**: no by default. Tool schemas cost agent context, so tool count
  is a budget. Yes only when an agent's loop needs it in one call.
- **Web UI**: yes if human curation needs it. Not a BI tool.

A new endpoint also needs an integration test, which is how it comes
under the OpenAPI contract check (`internal/restapi/openapi_test.go`).
Keep `api/openapi.yaml`, `internal/restapi`, `internal/mcpserver`, and
`internal/apiclient` in sync.

## Before opening the PR

Reread the index top to bottom and ask the question it exists to answer:
can someone learn an area's current state by reading that area's docs and
following the Status notes? If not, the index change is not done.
