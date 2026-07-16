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
- **Google Cloud native, secret-zero.** ochakai targets Cloud Run +
  Cloud SQL (optionally Vertex AI) exclusively: Cloud Run IAM decides who
  reaches it, callers are identified by their Google identity, and the
  database authenticates the service account — no tokens, no passwords,
  nothing to issue or rotate. Local development needs only Docker.

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

Connect Claude Code — no headers, no tokens:

```sh
claude mcp add --transport http ochakai http://localhost:8080/mcp
```

Opening this repository in Claude Code connects automatically via the
committed [.mcp.json](.mcp.json), which expects ochakai (or the Cloud Run
proxy — see the [deploy guide](deploy/cloudrun/README.md)) on
`localhost:8787`.

### CLI

Anything with a shell (Claude Code, headless agents, CI) can also skip MCP
and use the bundled CLI — a thin client of the same REST API
([design doc 0004](docs/design/0004-cli.md)). It resolves Google ID tokens
itself, so no proxy process is needed:

```sh
go install github.com/na0fu3y/ochakai/cmd/ochakai@latest

export OCHAKAI_URL=https://your-service.run.app  # auth = gcloud login / ADC
ochakai search "revenue" --type metric --status verified
ochakai get metric/revenue
ochakai compile --metric revenue --grain orders.created_at:month
ochakai export ./knowledge   # or: ochakai export - > okf.tar.gz
```

Hosted agents without a shell (claude.ai connectors, Gemini Enterprise
managed agents, Claude Desktop) keep using MCP. A CLAUDE.md snippet for
Claude Code lives in [examples/claude-code](examples/claude-code/CLAUDE.md).

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
Repository, dbt-mcp, Vanna, and WrenAI.

## License

MIT
