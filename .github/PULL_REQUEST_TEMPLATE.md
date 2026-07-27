<!-- Keep PRs small and focused. Link the issue or design doc, if there is one. -->

## What and why

## Checks

CI runs exactly this; make it pass locally first (CONTRIBUTING.md has the
context, including the store integration test and the fuzz targets).

- [ ] `gofmt -l .` prints nothing; `go vet ./...`; `go test -race ./...`
- [ ] `CGO_ENABLED=0 go build -trimpath ./...`
- [ ] golangci-lint and govulncheck report nothing
- [ ] Behavior changes come with tests

## Surfaces and contract

- [ ] `api/openapi.yaml`, `internal/restapi`, `internal/mcpserver`, and
      `internal/apiclient` say the same thing; a new endpoint has an
      integration test, which is what puts it under the OpenAPI contract check
- [ ] The change lands on every surface it belongs on — REST / MCP / CLI /
      Web UI — and what it stays off is deliberate (design doc 0015)

## Design docs

- [ ] Alters an accepted decision? A new numbered doc under `docs/design` is in
      this PR, the `Status:` header of every doc it supersedes or amends links
      to it, and `docs/design/README.md` is updated — or: it does not, and this
      box is not applicable
- [ ] Commit messages and code comments are in English
