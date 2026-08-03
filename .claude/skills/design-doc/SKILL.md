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

A numbered doc is for a decision **a user can observe** (0048 §2.1):

- **the shape of the wire** — a REST endpoint, parameter or response
  shape; an MCP tool added or removed; a CLI command added or removed;
  what the web UI carries
- **the stored form and its round trip** — the OKF document,
  canonicalization, what gets hashed, the bundle address space
- **what identity and provenance mean** — who a write is recorded as,
  what is refused, read-only and its edges
- **a new dependency on a Google Cloud service**, or anything touching
  secret-zero (0002, 0003)
- **something ochakai refuses to do** — the kind of judgment that lands
  in the ROADMAP's refusals

Everything else belongs in the PR description: internal restructuring
that leaves the outside unchanged, query and index work, performance,
dependencies, additions *within* an existing decision (a new type
spelling, an error message), and docs- or tests-only changes. When
unsure, ask whether somebody will reopen this in three months. If not,
the PR is enough.

**A doc that has not reached a release is revised by replacing it, not by
taking a new number** (0048 §2.3). Immutability is a promise about
decisions somebody could be depending on; nobody depends on an unreleased
one. Check the `Unreleased` section of [the changelog](../../../CHANGELOG.md):
if the implementation is still sitting there, edit that doc (or delete
and fold it into its successor, as 0034 did) instead of taking the next
number. The alternatives you dropped stay in the PR history.

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

Use 0038 or 0067 as models for the header style.

**Keep it under the ceiling.** CONTRIBUTING.md declares a `RECORD-LINES:`
number and `TestDesignRecordsStayUnderTheirCeiling` reads it back; the
median record is about 140 lines. Going over almost always means the
decision is two decisions — split it and take two numbers — or that the
record is restating something an earlier one already settled, which is
what citing it is for. If it genuinely is that large, raise the number in
the same PR and say why. Do not answer the ceiling by compressing the
prose: a record nobody finishes costs more than a long one.

CONTRIBUTING.md also caps the corpus as a whole: `RECORD-CORPUS-LINES`
for how many lines its records total, counting every record, Superseded
ones included, since they still ship in the tree and a reader following a
`Status:` trail still opens them.
`TestDesignRecordCorpusStaysUnderItsCeiling` reads it back.

**Usually you will not touch it.** The ceiling sits on a grid the width
of `RECORD-CORPUS-LINES-SLACK`, so a record that fits inside the current
boundary moves no number — which is deliberate, so that concurrent PRs do
not all collide on one line. When your record *does* cross the boundary,
raise the ceiling to the next multiple in the same PR and say why: the
same two escapes apply, just visible only once the whole corpus is
counted — this decision restates one already on file (cite it instead),
or two subjects have been sharing one number and would read better split.

**4. Update the older docs' `Status:` headers.** Every doc the new one
supersedes or amends gets a line pointing at the new number. See 0011 or
0018 for the style. This is the step that is easiest to forget and the
one the whole scheme rests on.

**5. Update the index.** Add the doc to its area with a one-line summary
in the surrounding style (bold status, then what it decides), and adjust
the status notes of the docs it amends. If this doc becomes the one to
read for an area, update that area's row in the index's opening table —
that table is what a reader is meant to reach the current state from
(0048 §2.4).

**6. Summarize it in English.** `docs/design/README.en.md` is a table, one
row per record (record link, status, decision in a sentence or two); a
missing row fails `TestEnglishDesignIndexCoversEveryRecord`. A doc this
one supersedes **drops to a bare pointer row** — record and status only,
no decision cell — since full rows are only kept for what is current
(0048 §2.5), and `TestEnglishIndexSummarizesOnlyWhatIsCurrent` fails if
the old row is left standing. Cutting it is part of superseding, not
tidying to do later: the row of a replaced record describes the old world
in the present tense and nobody rereads it.

**7. Avoid amendment chains.** If this would be the *second* partial
amendment stacked on the same doc, do not add another diff. Write a full
replacement that states the area's whole current picture, and mark the
older docs **Superseded**.

## What the checks hold, and what they cannot

`cmd/ochakai/designdocs_test.go` fails when steps 3-6 are half done: a
record over the ceiling, a record with no entry in either index, an index
that still lists a superseded record as current (or retires one that is
still standing), a supersession written at only one end. Run
`go test ./cmd/ochakai/` before opening the PR and those come back as
sentences, not review comments.

No test reads step 5's judgment — whether the opening table's row points
at the doc somebody should actually read now — or whether the summary in
step 6 says what the record decided. Spend the attention there.

## Surface consistency

Start from no. [docs/surface.md](../../../docs/surface.md) opens with the
eight conditions ochakai exists to satisfy — a change that serves none of
them is a no before anything else — and counts every REST operation, MCP
tool, CLI command and environment variable. A change that widens one
names the condition it serves and answers the three questions in the PR:
who actually got stuck, whether an existing surface already covers it,
and what can be folded away in exchange. `cmd/ochakai/surface_test.go` fails until the document matches
what the build offers — the failure prints the section as it should read,
so the bookkeeping is free and the judgment is the part to spend on.

If the change adds or changes a feature, [0067](../../../docs/design/0067-four-faces-and-what-they-decline.md)
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

Reread the index and ask the question it exists to answer: can someone
learn an area's current state from the opening table alone, and then from
that area's docs by following the Status notes? If not, the index change
is not done.
