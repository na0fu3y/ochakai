# ochakai

[![ci](https://github.com/na0fu3y/ochakai/actions/workflows/ci.yaml/badge.svg)](https://github.com/na0fu3y/ochakai/actions/workflows/ci.yaml)
[![Go Reference](https://pkg.go.dev/badge/github.com/na0fu3y/ochakai.svg)](https://pkg.go.dev/github.com/na0fu3y/ochakai)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**ochakai is a context provider for data agents.** Semantic layers tell you
that revenue = `SUM(price)` — they don't tell you whether 100 is good or bad.
ochakai keeps metric definitions, verified golden queries, interpretation
knowledge (how to read a metric), glossary terms, and table catalog entries
in one knowledge base, shared with Claude Code and other data agents over
[MCP](https://modelcontextprotocol.io), REST, and a CLI. It is built for
Google Cloud — Cloud Run + Cloud SQL, secret-zero — and runs locally with
nothing but Docker.

Principles:

- **No LLM inside.** ochakai returns deterministically compiled SQL from
  semantic definitions ([Apache Ossie](https://github.com/apache/ossie)) or
  human-verified golden queries, verbatim. Interpretation is the client
  agent's job.
- **No SQL execution.** ochakai holds no warehouse credentials. It compiles;
  your agent executes.
- **Knowledge is co-owned by humans and agents.** Agents are encouraged to
  write learnings back (`create_knowledge`); entries start as drafts and a
  human promotes them to `verified`. Every change is kept as a revision.
  Proposals that don't make it are kept as `rejected` with the reason
  (`status_note`) so agents stop re-proposing them, and per-entry usage
  counts (`/usage`) show whether the write-back loop is actually working.
- **Your knowledge is yours.** Self-hostable per tenant; export the whole
  knowledge base as an
  [OKF](https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf)
  bundle (`ochakai export-okf <dir>` or `GET /api/v1/export`) — plain
  markdown + YAML frontmatter that lives happily in git.
- **Google Cloud native, secret-zero.** ochakai targets Cloud Run +
  Cloud SQL (optionally Vertex AI) exclusively: Cloud Run IAM decides who
  reaches it, callers are identified by their Google identity, and the
  database authenticates the service account — no tokens, no passwords,
  nothing to issue or rotate.

## Quick start

```sh
git clone https://github.com/na0fu3y/ochakai && cd ochakai
docker compose -f deploy/compose.yaml up
```

Import a semantic model and try a search:

```sh
go run ./cmd/ochakai import-ossie examples/semantic-model.yaml
curl 'http://localhost:8080/api/v1/knowledge?q=revenue'
```

### Connect Claude Code

For Claude Code — and anything else with a shell (headless agents, CI) —
the recommended interface is the bundled CLI, a thin client of the same
REST API ([design doc 0004](docs/design/0004-cli.md)): tool schemas don't
occupy the agent's context (`--help` is read on demand), output composes
with pipes, and it resolves Google ID tokens itself, so no proxy process
is needed against Cloud Run.

```sh
go install github.com/na0fu3y/ochakai/cmd/ochakai@latest

ochakai use http://localhost:8080  # Cloud Run: ochakai use https://your-service.run.app (auth = gcloud login / ADC, no tokens to configure)
ochakai whoami                     # which server, as whom, reachable?
ochakai search "revenue" --type metric --status verified
ochakai get metric/revenue
ochakai compile --metric revenue --grain orders.created_at:month
ochakai export ./knowledge   # or: ochakai export - > okf.tar.gz
```

`ochakai use` saves the selection locally (name more servers with
`--name` and switch by name); `--url` and `OCHAKAI_URL` always override
it, so scripts and CI stay explicit. Tab completion:
`source <(ochakai completion zsh)` (also bash, fish).

Then copy [examples/claude-code/CLAUDE.md](examples/claude-code/CLAUDE.md)
into your project's CLAUDE.md — it teaches the agent the commands and the
write-learnings-back habit.

### MCP

Hosted agents without a shell (claude.ai connectors, Claude Desktop,
Gemini Enterprise managed agents) connect over MCP, which remains the
primary interface. Claude Code can use it too:

```sh
claude mcp add --transport http ochakai http://localhost:8080/mcp
```

Opening this repository in Claude Code connects automatically via the
committed [.mcp.json](.mcp.json), which expects ochakai (or the Cloud Run
proxy — see the [deploy guide](deploy/cloudrun/README.md)) on
`localhost:8787`.

## MCP tools

| Tool | Description |
|---|---|
| `search_knowledge` | Cross-type search; verified entries rank higher |
| `get_knowledge` | Fetch one entry with body, attrs, and links |
| `create_knowledge` | Write learnings back (agents create drafts) |
| `update_knowledge` | Update; every change is kept as a revision |
| `delete_knowledge` | Soft-delete (history retained) |
| `compile_sql` | Metrics + dimensions + filters + time grain → SQL. Never executes, never guesses |

The REST API (`/api/v1`) mirrors these tools for building your own web UI —
see [api/openapi.yaml](api/openapi.yaml) and the sample in
[examples/webui](examples/webui). To keep golden queries trustworthy over
time, run them as canaries from your CI:
[docs/guides/golden-query-canary.md](docs/guides/golden-query-canary.md).

## Knowledge types

| Type | What it holds |
|---|---|
| `metric` | Semantic metric definition (Apache Ossie), synonyms |
| `query` | Golden query: natural-language question + verified SQL |
| `insight` | How to read a metric: baselines, seasonality, caveats, thresholds |
| `term` | Glossary term |
| `table` | Table catalog entry: source, column notes, known issues |

## Configuration

| Env var | Description |
|---|---|
| `OCHAKAI_DATABASE_URL` | Cloud SQL connection string (required; `DATABASE_URL` also works) |
| `OCHAKAI_DB_IAM_AUTH` | `true` enables Cloud SQL IAM database authentication: the connection password is a short-lived IAM token, so the `DATABASE_URL` carries no secret |
| `OCHAKAI_VERTEX_PROJECT` | Set to enable hybrid semantic search via Vertex AI embeddings (default: off, trigram-only). Auth is ADC — no API keys |
| `OCHAKAI_VERTEX_LOCATION` / `OCHAKAI_VERTEX_MODEL` / `OCHAKAI_EMBEDDING_DIM` | Embedding details (defaults: `us-central1`, `gemini-embedding-001`, 768) |
| `OCHAKAI_INSECURE_DEV` | Local development only: disables auth, everything acts as human:anonymous |
| `PORT` / `OCHAKAI_ADDR` | Listen address (default `:8080`) |

Authentication has no configuration: ochakai reads the caller identity
that Cloud Run forwards after its IAM check (`human:<email>` for people,
`agent:<sa-email>` for service accounts) and records it as provenance.
Reachability is Cloud Run IAM's job; ochakai does no authorization.

The complete, cost-minimized deployment walkthrough (~$10/month) lives in
[deploy/cloudrun/README.md](deploy/cloudrun/README.md), including a
security hardening checklist (org-policy guardrails, private IP,
retiring the last password, and more).

## Supply chain

Images are published to `ghcr.io/na0fu3y/ochakai` by GitHub Actions with
SBOM and SLSA provenance; workflow actions are pinned by commit SHA. The
binary is a static, trimmed-path Go build on `distroless/static`. Verify:

```sh
gh attestation verify oci://ghcr.io/na0fu3y/ochakai:<tag> -R na0fu3y/ochakai
```

## Design

Architecture decisions live in
[docs/design/0001-architecture.md](docs/design/0001-architecture.md)
(Japanese), including the survey of prior art: OpenAI's and Meta's in-house
data agents, Airbnb Minerva, Uber uMetric, Snowflake's Verified Query
Repository, dbt-mcp, Vanna, WrenAI, and the 2026 context-layer landscape
(OpenMetadata, DataHub, Atlan, warehouse-native semantic layers).

## Contributing

Issues and PRs welcome — see [CONTRIBUTING.md](CONTRIBUTING.md) for the
local setup and the checks CI runs, and [SECURITY.md](SECURITY.md) for
reporting vulnerabilities.

## License

[MIT](LICENSE)
