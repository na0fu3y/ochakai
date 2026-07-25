# CLAUDE.md

ochakai is a knowledge store for data-analysis agents. Two decisions
frame everything: no LLM inside and no SQL execution (0001), and
Google Cloud only with zero secrets — Cloud Run IAM + Cloud SQL IAM,
never tokens or passwords (0002, 0003).

## Design docs

Architecture decisions live in [docs/design](docs/design) as numbered,
immutable decision records (mostly Japanese).
[docs/design/README.md](docs/design/README.md) is the index — read it
first to find which docs describe the current state of an area; a
superseded or amended doc says so in its `Status:` header.

When a change alters an accepted decision, add a new numbered design
doc in the same PR and keep the index truthful:

1. Update the `Status:` header of every older doc the new one
   supersedes or amends, linking to the new number.
2. Update the index: add the new doc to its area with a one-line
   summary, adjust the status notes of amended docs.
3. Avoid amendment chains — if this would be the second partial
   amendment stacked on the same doc, write a full replacement for the
   area and mark the older docs Superseded instead.

## Checks and conventions

CI runs `gofmt -l .`, `go vet`, `go test -race`, a `CGO_ENABLED=0`
build, and govulncheck — see [CONTRIBUTING.md](CONTRIBUTING.md) for the
exact commands and the store integration test setup.

- The public wire surface is [api/openapi.yaml](api/openapi.yaml); keep
  it, `internal/restapi`, `internal/mcpserver`, and `internal/apiclient`
  in sync.
- New features must land consistently across surfaces per
  [docs/design/0015](docs/design/0015-surface-consistency.md)
  (REST / MCP / CLI / Web UI — including deliberate omissions).
- Write commit messages and code comments in English.
