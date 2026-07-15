# ochakai

**ochakai is a context provider for data agents.** Semantic layers tell you
that revenue = `SUM(price)` — they don't tell you whether 100 is good or bad.
ochakai keeps metric definitions, verified golden queries, interpretation
knowledge (how to read a metric), glossary terms, and table catalog entries
in one knowledge base, shared with Claude Code and other data agents over
[MCP](https://modelcontextprotocol.io).

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
- **Your knowledge is yours.** Self-hostable per tenant; export the whole
  knowledge base as an
  [OKF](https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf)
  bundle (`ochakai export-okf <dir>` or `GET /api/v1/export`) — plain
  markdown + YAML frontmatter that lives happily in git.
- **Minimal to run.** One container image + PostgreSQL. Nothing else.

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

Connect Claude Code:

```sh
claude mcp add --transport http ochakai http://localhost:8080/mcp \
  --header "Authorization: Bearer <token>"
```

Opening this repository in Claude Code connects automatically via the
committed [.mcp.json](.mcp.json), which expects ochakai (or the Cloud Run
proxy — see the [deploy guide](deploy/cloudrun/README.md)) on
`localhost:8787`. On tokenless `cloudrun-iam` deployments no header is
needed at all.

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
[examples/webui](examples/webui).

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
| `OCHAKAI_DATABASE_URL` | PostgreSQL connection string (required; `DATABASE_URL` also works) |
| `OCHAKAI_CLIENTS` | Bearer tokens: `token=agent:claude-code,token2=human:alice`. Empty disables auth (dev only) |
| `OCHAKAI_AUTH` | `clients` (default: bearer tokens) or `cloudrun-iam` (tokenless: actor from the Cloud-Run-verified caller identity; requires a non-public service) |
| `OCHAKAI_CORS_ORIGINS` | Exact-match origin allowlist for browser access to the REST API (for separately hosted web UIs). Default: no CORS headers |
| `OCHAKAI_EMBEDDING_PROVIDER` | `vertex` enables hybrid semantic search (default: off, trigram-only) |
| `OCHAKAI_VERTEX_PROJECT` / `OCHAKAI_VERTEX_LOCATION` / `OCHAKAI_VERTEX_MODEL` | Vertex AI settings (defaults: `us-central1`, `gemini-embedding-001`). Auth is ADC — no API keys |
| `OCHAKAI_EMBEDDING_DIM` | Embedding dimensionality (default 768) |
| `PORT` / `OCHAKAI_ADDR` | Listen address (default `:8080`) |

On Cloud Run + Cloud SQL (the recommended initial setup), grant the service
account `roles/aiplatform.user` to enable embeddings — no keys to manage.
pgvector is required only when embeddings are enabled. A complete,
cost-minimized walkthrough (~$10/month) lives in
[deploy/cloudrun/README.md](deploy/cloudrun/README.md).

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
Repository, dbt-mcp, Vanna, and WrenAI.

## License

MIT
