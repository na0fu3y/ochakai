---
name: design-doc
description: Update the current-state spec in docs/spec and, when a choice is likely to be reopened, add a short entry to docs/decisions. Use when a change alters what a user can observe — the wire, the stored form, identity and provenance, a Google Cloud dependency, a surface, or something ochakai refuses to do.
---

# Recording a design change

Two places, two promises (CONTRIBUTING.md, *Design: spec and decisions*):

- **`docs/spec/<area>.md`** — how the area works today and why. Rewritten
  in place, in the present tense. No `Status:` notes, no "amends": git
  holds the history.
- **`docs/decisions/NNNN-<slug>.md`** — a choice somebody will reopen:
  context, decision, alternatives turned down, what would bring it back.
  Short (`DECISION-LINES`), never rewritten, numbered from 0153.
- **`docs/design/`** — records 0001–0152. Frozen history. Do not edit a
  record, its `Status:` header or its index entry.

## When

A change **a user can observe** — the shape of the wire (REST, MCP, CLI,
web UI), the stored form and its round trip, what identity and
provenance mean, a new Google Cloud dependency, or something ochakai
refuses to do — updates the spec. Internal work that leaves the outside
unchanged belongs in the PR description.

A decision entry is extra, and rarer: write one only when somebody three
months from now will ask "why not the other way". A rule added inside an
area, a number moved, a word renamed — the spec and the CHANGELOG carry
those.

Check any proposal against these first:

- **Only a ruling changes what is served, no LLM rules, and the server
  runs no SQL** (0142). A ruling is a person's, or a confirmation job's
  recorded as `process:` and read as `machine-confirmed`. The
  deployment's own agent is off by default, answers and proposes, and
  never rules; a query runs as the person asking.
- **Secret-zero** (0065, 0003). Cloud Run IAM + Cloud SQL IAM on Google
  Cloud, in-process OIDC verification off it (0086); no tokens, no
  passwords.

## Steps

**1. Find the area.** [docs/spec/README.md](../../../docs/spec/README.md)
lists the areas, one document each; that document is what you edit. The
design records and their index are frozen history — do not touch them.

**2. Edit the spec** in the same PR as the change. Say what is true now;
delete what stopped being true rather than annotating it. Cite a
decision entry for the why when there is one.

**3. Add a decision entry only if it earns one.**

```bash
ls docs/decisions | tail -3
```

Take the next number (0153 onward). The shape is in
[docs/decisions/README.md](../../../docs/decisions/README.md): title,
`Date:`, `領域:` linking the spec document, then 背景 / 決定 / 退けた案 /
戻す条件. Keep it under `DECISION-LINES`; going over means two decisions,
or spec prose in the wrong place. If a later change reverses it, add a
new entry and edit the spec — the old entry is not annotated.

**4. Say it in the CHANGELOG**, as for any change a user can observe.

**5. Name the gate in the PR**: which condition (C1–C8) the change
serves; for LLM or automation, which person's ruling it makes cheaper;
for a wider surface, the three questions. Do not write that ochakai will
not do something a ROADMAP stage plans.

What the checks hold: a decision entry's length, title and `Date:`, and a
number not already used (`cmd/ochakai/decisions_test.go`). Nothing reads
whether the spec says what the code does — that is the review.

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

Reread the area's spec document and ask the question it exists to
answer: can someone learn how the area works today, and why, from it
alone? If not, the spec change is not done.
