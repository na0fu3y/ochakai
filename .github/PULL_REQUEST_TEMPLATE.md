<!-- Keep PRs small and focused. Link the issue or design doc, if there is one. -->

## What and why

## Checks

CI runs the same script; make it pass locally first (CONTRIBUTING.md has the
context, including the store integration test and the fuzz targets).

- [ ] `scripts/check` is clean — add `--db` when the change touches the store
- [ ] Behavior changes come with tests
- [ ] Behavior changes have a line under `## [Unreleased]` in `CHANGELOG.md`
      — the `changelog` workflow checks for one when a non-test file under
      `internal/` or `cmd/` moves; if this change is not behavior, label the
      PR `no-changelog` and say why above

## Scope

Answer each in a line (CONTRIBUTING.md, "Proposing a feature").

- **Condition** — which of `docs/surface.md`'s C1–C8 this serves, or "none:
  internal / fix":
- **Ruling made cheaper** — for anything an LLM or other automation does,
  which person's ruling it makes cheaper (never one it makes), or "n/a":

## Surfaces and contract

- [ ] `api/openapi.yaml`, `internal/restapi`, `internal/mcpserver`, and
      `internal/apiclient` say the same thing; a new endpoint has an
      integration test, which is what puts it under the OpenAPI contract check
- [ ] The change lands on every surface it belongs on — REST / MCP / CLI /
      Web UI — and what it stays off is deliberate (design doc 0067)
- [ ] `api/openapi.frozen.txt` is untouched. REST is frozen at `/api/v1`
      (design doc 0064), and `cmd/ochakai/frozenwire_test.go` fails on any
      wire change; regenerating that file is a decision, and §11 leaves a
      security defect as the only reason — say which one, here
- [ ] Widens a surface — a REST operation, an MCP tool, a CLI command, an
      environment variable? `docs/surface.md` is updated
      (`cmd/ochakai/surface_test.go` says so), the description names which of
      its eight conditions the addition serves, and the three questions are
      answered under *What and why*: who actually got stuck, why an existing
      surface does not already cover it, and what is folded away in exchange.
      The default answer is no

## Spec and decisions

- [ ] Changes what a user can observe? The area's `docs/spec` document says so
      in the present tense. Or: it does not, and this box is not applicable
- [ ] A choice somebody will reopen? A short entry in `docs/decisions`
      (0153 onward, under `DECISION-LINES`) — or it is not one
- [ ] No document here says ochakai will not do something a ROADMAP stage plans
- [ ] Commit messages and code comments are in English
