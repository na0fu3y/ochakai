# Contributing to ochakai

Thanks for your interest! Issues and pull requests are welcome.

## Development setup

You need Go (the version pinned in [go.mod](go.mod)) and Docker.

Run the full local stack (server + PostgreSQL with pgvector):

```sh
docker compose -f deploy/compose.yaml up
```

Seed it and poke around:

```sh
go run ./cmd/ochakai import-ossie examples/semantic-model.yaml
go run ./cmd/ochakai use http://localhost:8080
go run ./cmd/ochakai search "revenue"
```

## Tests and checks

CI runs exactly this — make it pass locally before opening a PR:

```sh
gofmt -l .          # must print nothing
go vet ./...
go test -race ./...
CGO_ENABLED=0 go build -trimpath ./...
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

The store integration test is skipped unless a real PostgreSQL is
available (CI runs one as a service container):

```sh
docker run -d --rm -p 55433:5432 -e POSTGRES_PASSWORD=t -e POSTGRES_USER=t -e POSTGRES_DB=t pgvector/pgvector:pg17
OCHAKAI_TEST_DATABASE_URL='postgres://t:t@localhost:55433/t?sslmode=disable' go test ./internal/store/
```

## Design docs

Architecture decisions live in [docs/design](docs/design) as numbered
documents (mostly Japanese). A change that alters an accepted decision —
new interface, new dependency on a Google Cloud service, a change to the
auth model — should update the relevant doc or add a new one in the same
PR. Small fixes and additions within existing decisions don't need one.

Two decisions worth knowing before proposing features:

- **No LLM inside, no SQL execution.** ochakai compiles and serves
  knowledge deterministically; interpretation and execution belong to the
  client agent (0001).
- **Google Cloud only, secret-zero.** Auth is Cloud Run IAM + Cloud SQL
  IAM; features must not introduce tokens or passwords (0002, 0003).

## Pull requests

- Keep PRs small and focused; include tests for behavior changes.
- The public wire surface is [api/openapi.yaml](api/openapi.yaml) — keep
  it, `internal/restapi`, `internal/mcpserver`, and `internal/apiclient`
  in sync (wire compatibility is pinned by tests).
- Write commit messages and code comments in English.
