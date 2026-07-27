# Contributing to ochakai

Thanks for your interest! Issues and pull requests are welcome. This
project follows a short [code of conduct](CODE_OF_CONDUCT.md).

## Development setup

You need Go (the version pinned in [go.mod](go.mod)) and Docker.

Run the full local stack (server + PostgreSQL with pgvector):

```sh
docker compose -f deploy/compose.yaml up
```

Seed it and poke around:

```sh
export OCHAKAI_URL=http://localhost:8080
go run ./cmd/ochakai create queries/monthly-revenue -f examples/golden-query.md
go run ./cmd/ochakai search "revenue"
```

`OCHAKAI_URL` has to come first: there is no default server, and a client
command without one fails rather than guessing. `ochakai use` saves the
choice instead, but an exported variable keeps a checkout from editing the
config your everyday CLI reads.

## Tests and checks

CI runs exactly this — make it pass locally before opening a PR:

```sh
gofmt -l .          # must print nothing
go vet ./...
go test -race ./...
CGO_ENABLED=0 go build -trimpath ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 run ./...
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

The linters are configured in [.golangci.yml](.golangci.yml) and chosen so
a clean tree reports nothing — a finding means new code, not a linter's
taste ([design doc 0035](docs/design/0035-verifiability.md)). The most
load-bearing one is `exhaustive`: a `switch` over `domain.Status` or
`domain.Type` that names every case and no `default` must keep naming
every case when a value is added.

`go test` runs the fuzz targets' seed corpora like ordinary tests, which
is what CI does. Fuzzing proper is a local tool — reach for it when
touching the OKF parser, id validity, or link derivation:

```sh
go test ./internal/okf/ -run XXX -fuzz FuzzDocumentRoundTrip -fuzztime 60s
```

A failing input lands in `testdata/fuzz/` and becomes a permanent seed;
commit it with the fix.

The store integration test is skipped unless a real PostgreSQL is
available (CI runs one as a service container):

```sh
docker run -d --rm -p 55433:5432 -e POSTGRES_PASSWORD=t -e POSTGRES_USER=t -e POSTGRES_DB=t pgvector/pgvector:pg17
OCHAKAI_TEST_DATABASE_URL='postgres://t:t@localhost:55433/t?sslmode=disable' go test ./internal/store/
```

## Design docs

Architecture decisions live in [docs/design](docs/design) as numbered
documents (mostly Japanese). Start from the
[index](docs/design/README.md), which groups them by area and marks
which ones describe the current state.
[docs/architecture.md](docs/architecture.md) summarizes the accepted ones
in English if you would rather read the shape of the system first.

The Japanese is the maintainer's habit, not a rule — write your design
doc in English if that is what you think in, and say so in the PR. What
the review cares about is that the decision and its rejected
alternatives are legible, not which language they are legible in. A change that alters an accepted
decision — new interface, new dependency on a Google Cloud service, a
change to the auth model — should add a new numbered doc in the same
PR. Small fixes and additions within existing decisions don't need one.

Docs are immutable decision records; the incremental history lives in
them and in PRs. What the repo must keep readable is the *current*
state per area, so when a design doc lands:

- Take the next free number. Gaps are fine — proposals that never land
  stay in their PRs.
- Update the `Status:` header of every older doc the new one supersedes
  or amends, linking to the new number (see 0011 or 0018 for the style).
- Update [docs/design/README.md](docs/design/README.md): add the new
  doc to its area with a one-line summary, and adjust the status notes
  of the docs it amends.
- Avoid amendment chains. If the new decision would be the second
  partial amendment stacked on the same doc, don't add another diff:
  write it as a full replacement that states the area's whole current
  picture and mark the older docs Superseded.

Two decisions worth knowing before proposing features:

- **No LLM inside, no SQL execution.** ochakai stores and serves
  knowledge; interpretation and execution belong to the client agent
  (0001).
- **Google Cloud only, secret-zero.** Auth is Cloud Run IAM + Cloud SQL
  IAM; features must not introduce tokens or passwords (0002, 0003).

## Pull requests

- Keep PRs small and focused; include tests for behavior changes.
- The public wire surface is [api/openapi.yaml](api/openapi.yaml) — keep
  it, `internal/restapi`, `internal/mcpserver`, and `internal/apiclient`
  in sync (wire compatibility is pinned by tests). The REST half is
  checked, not just reviewed: the integration tests run every request and
  response past the spec (`internal/restapi/openapi_test.go`), so an
  endpoint whose shape drifts from `openapi.yaml` fails CI. A new
  endpoint needs an integration test to come under that check.
- Write commit messages and code comments in English.
