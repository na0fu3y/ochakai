<!-- Keep PRs small and focused. Link the issue or design doc, if there is one. -->

## What and why

## Checks

CI runs the same script; make it pass locally first (CONTRIBUTING.md has the
context, including the store integration test and the fuzz targets).

- [ ] `scripts/check` is clean — add `--db` when the change touches the store
- [ ] Behavior changes come with tests

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

## Design docs

- [ ] Alters an accepted decision? A new numbered doc under `docs/design` is in
      this PR — or: it does not, and this box is not applicable. The index
      entries, the `Status:` headers at both ends and the two indexes agreeing
      about them are checked by `cmd/ochakai/designdocs_test.go`; what is left
      for a human is whether the index's opening table still points at the doc
      to read now, and whether the English summary says what was decided
- [ ] Commit messages and code comments are in English
