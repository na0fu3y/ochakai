# ochakai

[![ci](https://github.com/na0fu3y/ochakai/actions/workflows/ci.yaml/badge.svg)](https://github.com/na0fu3y/ochakai/actions/workflows/ci.yaml)
[![Go Reference](https://pkg.go.dev/badge/github.com/na0fu3y/ochakai.svg)](https://pkg.go.dev/github.com/na0fu3y/ochakai)
[![Go Report Card](https://goreportcard.com/badge/github.com/na0fu3y/ochakai)](https://goreportcard.com/report/github.com/na0fu3y/ochakai)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**ochakai is a context provider for data agents.** Semantic layers tell
you that revenue = `SUM(price)` — they don't tell you whether 100 is good
or bad. ochakai keeps that missing half in one knowledge base: metric
definitions, verified golden queries, interpretation knowledge (how to
*read* a metric), glossary terms, and table catalog entries — curated by
humans and agents together, and served to Claude Code and every other
data agent over [MCP](https://modelcontextprotocol.io), REST, a CLI, and
a bundled web UI. More at [ochak.ai](https://ochak.ai).

The whole thing is one Go binary and Postgres, deployable on Cloud Run +
Cloud SQL for [about $10/month](deploy/cloudrun/README.md). Local
development needs only Docker.

## Quick start

```sh
git clone https://github.com/na0fu3y/ochakai && cd ochakai
docker compose -f deploy/compose.yaml up
```

Import a semantic model and try a search — everything goes through the
API, so plain curl works:

```sh
curl -X POST --data-binary @examples/semantic-model.yaml http://localhost:8080/api/v1/import/ossie
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
ochakai context "why is revenue down?"  # the one-call read before a data question: full entries, links expanded
ochakai search "revenue" --type metric --status verified
ochakai get metric/revenue
ochakai attach insight/revenue-reading weekly.png   # files travel with the entry
ochakai compile --metric revenue --grain orders.ordered_at:month
ochakai export ./knowledge   # or: ochakai export - > okf.tar.gz
ochakai import ./knowledge   # the inverse; works with any OKF bundle
ochakai ui                   # web UI at http://127.0.0.1:8098, acting as you (no deploy)
```

`ochakai use` saves the selection locally (name more servers with
`--name` and switch by name); `--url` and `OCHAKAI_URL` always override
it, so scripts and CI stay explicit. Tab completion:
`source <(ochakai completion zsh)` (also bash, fish).

Then copy [examples/claude-code/CLAUDE.md](examples/claude-code/CLAUDE.md)
into your project's CLAUDE.md — it teaches the agent the commands and the
write-learnings-back habit. To make the loop automatic instead of
habitual, install the [hooks](examples/claude-code): a UserPromptSubmit
hook injects relevant knowledge before the agent starts (recall), and a
Stop hook asks it once per data session to save what it learned
(write-back) — both without an LLM.

### MCP

Agents without a shell (Claude Desktop, Gemini Enterprise managed
agents) connect over MCP, which remains the primary interface. Claude Code can use it too:

```sh
claude mcp add --transport http ochakai http://localhost:8080/mcp
```

Opening this repository in Claude Code connects automatically via the
committed [.mcp.json](.mcp.json), which expects ochakai (or the Cloud Run
proxy — see the [deploy guide](deploy/cloudrun/README.md)) on
`localhost:8787`.

### Web UI: the human half of the loop

Agents write drafts; somebody has to read them. The bundled web UI is
where a human reviews what agents learned — search and filter by status,
browse the knowledge as a folder tree (hierarchical IDs are directories,
[design doc 0014](docs/design/0014-folder-browse.md)),
read an entry with its links and usage counts, then verify / deprecate /
reject (with the reason) in one click. It also shows the *verification
age* feed (oldest `verified_at` first) so stale golden queries surface,
and compiles metrics interactively for debugging semantic models. One
self-contained page, no build step; deliberately **not** a BI tool — no
charts, no query execution, no chat.

Same page, two identities ([design doc 0006](docs/design/0006-web-ui-serving.md)):
`ochakai ui` serves it on loopback acting as *you* (zero deploy, edits
recorded as `human:<you>`), and [examples/webui](examples/webui) deploys
it on Cloud Run as a team-shared service.

## Why ochakai

In 2026, serving metric definitions to agents over MCP is table stakes —
semantic layers, warehouses, and data catalogs all do it. ochakai exists
for what they still don't do:

- **Interpretation knowledge is a first-class type.** An `insight` entry
  records the baseline, the seasonality, the caveat, the threshold — the
  tribal knowledge that never fits in a semantic-model YAML and today
  travels by Slack. Your agent gets it in the same search that returns
  the metric definition.
- **A write-back loop with a memory.** Agents are encouraged to write
  learnings back; entries start as drafts and a human promotes them to
  `verified`, with provenance (who wrote it, who verified it, when) on
  every entry and every change kept as a revision. Proposals that don't
  make it are kept as `rejected` with the reason, so agents stop
  re-proposing them — a memory of *no* that verified-answer stores
  elsewhere don't keep. Per-entry usage counts show whether the loop is
  actually working, and outcome reports (`report_outcome`: worked /
  failed) close its last edge — an agent that ran a golden query and got
  a wrong number says so, instead of the next agent trusting the same
  entry blind.
- **Not a memory layer — the other half of one.** Memory layers (mem0,
  Zep, Letta) auto-extract per-user memories with an LLM and inject them
  back, unaudited: nobody reviews what got remembered, and a wrong
  memory persists silently. ochakai is the opposite trade: team-shared
  knowledge that passes through human review, with provenance on every
  entry. *Memory layers remember what happened; ochakai curates what's
  true.* They compose — preferences in your memory layer, verified data
  knowledge here. The delivery trick memory layers got right (no agent
  judgment needed) ochakai keeps: one `get_context` call returns the
  relevant entries in full, and the bundled
  [Claude Code hooks](examples/claude-code) make recall and write-back
  automatic, no LLM involved.
- **Verified answers for any client.** Verified-query stores exist — inside
  Snowflake Cortex Analyst, inside Databricks Genie, inside each
  AI-analyst SaaS — and each one feeds only its own chat. ochakai is
  client-agnostic by construction: the same verified knowledge serves
  Claude Code, hosted MCP agents, CI jobs, and whatever you build next.
- **An exit, guaranteed.** The whole knowledge base round-trips through
  [OKF](https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf)
  bundles (`ochakai export` / `ochakai import`) — plain markdown + YAML
  frontmatter that lives happily in git. Import accepts any producer's
  OKF bundle, not just ochakai's own
  ([design doc 0005](docs/design/0005-okf-compatibility.md)). MIT-licensed
  and self-hostable per tenant: your knowledge is never a hostage.

And it stays small by refusing things:

| ochakai has no… | because |
|---|---|
| LLM | it returns deterministically compiled SQL from semantic definitions ([Apache Ossie](https://github.com/apache/ossie)) or human-verified golden queries, verbatim. Interpretation is the client agent's job |
| SQL execution | it holds no warehouse credentials. It compiles; your agent executes |
| connector ingestion | knowledge is curated by humans and agents, not harvested by pipelines. Trust density over volume |
| chat UI or dashboards | it feeds your agents; it doesn't compete with them. The bundled web UI is a curation surface, not a BI tool |
| secrets | Cloud Run IAM decides who reaches it, callers are identified by their Google identity, and Cloud SQL authenticates the service account — nothing to issue or rotate |

## MCP tools

| Tool | Description |
|---|---|
| `get_context` | The one call before answering a data question: full entries behind the top hits, links expanded both ways |
| `search_knowledge` | Cross-type search; verified entries rank higher |
| `get_knowledge` | Fetch one entry with body, attrs, links, and attachment metadata |
| `get_attachment` | Fetch a file attached to an entry (dashboard screenshots, ER diagrams, seeds files) |
| `create_knowledge` | Write learnings back (agents create drafts) |
| `update_knowledge` | Update; every change is kept as a revision |
| `delete_knowledge` | Soft-delete (history retained) |
| `get_knowledge_usage` | Usage totals per entry — draft-promotion evidence, staleness signal |
| `report_outcome` | Report worked/failed after acting on knowledge — failed reports flag verified entries for re-verification |
| `compile_sql` | Metrics + dimensions + filters + time grain → SQL. Never executes, never guesses |

Every entry is also an **MCP resource** addressable by its canonical URI —
`ochakai://{type}/{id}`, e.g. `ochakai://metric/revenue` (IDs may be
hierarchical, like `ochakai://query/sales/top-customers`). Clients that
support resource references (`@`-mentions) can pull an entry in as an OKF
document — frontmatter, body, and links — without a tool call; discovery
stays with `get_context`/`search_knowledge`. Read tools carry `readOnly`
annotations and `delete_knowledge` a `destructive` one, so client
auto-approval policies work without parsing descriptions.

The REST API (`/api/v1`) is a superset of these tools, adding bulk
export/import — see
[api/openapi.yaml](api/openapi.yaml). To keep golden queries trustworthy
over time, run them as canaries from your CI:
[docs/guides/golden-query-canary.md](docs/guides/golden-query-canary.md).

## Knowledge types

| Type | What it holds |
|---|---|
| `metric` | Semantic metric definition (Apache Ossie), synonyms |
| `query` | Golden query: natural-language question + verified SQL |
| `insight` | How to read a metric: baselines, seasonality, caveats, thresholds |
| `term` | Glossary term |
| `table` | Table catalog entry: source, column notes, known issues |

These five are recommendations with server behavior attached (compile,
canaries), not a closed set — any slug works as a type for your own document
kinds, and IDs may be hierarchical (`query/sales/monthly-revenue`) to
organize knowledge into directories
([design doc 0005](docs/design/0005-okf-compatibility.md)).

Entries can carry file attachments — the dashboard screenshot behind an
insight, the ER diagram behind a table entry, the seeds.txt or spec PDF
behind a dataset. Accepted formats are the intersection of what Claude
reads and what Gemini embeds (png/jpeg/webp, pdf, plain text —
sniffed from the bytes). Search stays text-first (captions in the body),
attachment bytes live in GCS and are fetched on demand, and attachments
round-trip through OKF bundles as plain files next to their entry
([design docs 0008](docs/design/0008-image-attachments.md) and
[0013](docs/design/0013-attachment-files-gcs-only.md)).

## Configuration

| Env var | Description |
|---|---|
| `OCHAKAI_DATABASE_URL` | Cloud SQL connection string (required; `DATABASE_URL` also works) |
| `OCHAKAI_DB_IAM_AUTH` | `true` enables Cloud SQL IAM database authentication: the connection password is a short-lived IAM token, so the `DATABASE_URL` carries no secret |
| `OCHAKAI_GCS_BUCKET` | Bucket for attachment bytes (auth is ADC — no keys); legacy in-Postgres bytes are migrated out at startup. Default: unset — the instance stores markdown entries only and attach operations return an error ([design doc 0013](docs/design/0013-attachment-files-gcs-only.md)) |
| `OCHAKAI_VERTEX_PROJECT` | Set to enable hybrid semantic search via Vertex AI embeddings (default: off, trigram-only — ochakai calls no external API unless you opt in). Auth is ADC — no API keys |
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
