# ochakai

[![ci](https://github.com/na0fu3y/ochakai/actions/workflows/ci.yaml/badge.svg)](https://github.com/na0fu3y/ochakai/actions/workflows/ci.yaml)
[![release](https://img.shields.io/github/v/release/na0fu3y/ochakai?sort=semver)](https://github.com/na0fu3y/ochakai/releases)
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

One Go binary and Postgres: [Terraform stands up Cloud Run + Cloud
SQL](deploy/terraform/README.md) for about $10/month, or Docker and
nothing else locally.

[Quick start](#quick-start) · [Why ochakai](#why-ochakai) ·
[Requirements](#requirements) · [All docs](docs/README.md) ·
[REST API](api/openapi.yaml) · [Changelog](CHANGELOG.md)

![The draft review queue: concepts agents wrote back, waiting for a human
to verify or reject them](docs/images/webui-review.png)

## Quick start

```sh
git clone https://github.com/na0fu3y/ochakai && cd ochakai
docker compose -f deploy/compose.yaml up -d
```

This runs with `OCHAKAI_MODE=dev`: authentication is off and every
request acts as `human:anonymous` — never do this on a deployment
([every mode](docs/configuration.md#environment-variables)).

Load the demo knowledge base — [ten concepts](examples/demo) about one
invented retail domain, linked to each other, some of them drafts — and
search it. Everything goes through the API, so plain curl works too:

```sh
docker compose -f deploy/compose.yaml exec ochakai /ochakai import /examples/demo
curl 'http://localhost:8080/api/v1/search?q=revenue'
```

## Connect an agent

For Claude Code — and anything else with a shell (headless agents, CI) —
the recommended interface is the bundled CLI, a thin client of the same
REST API: tool schemas don't occupy the agent's context, output composes
with pipes, and it resolves Google ID tokens itself, so no proxy process
is needed against Cloud Run. Take a
[release archive](https://github.com/na0fu3y/ochakai/releases), or:

```sh
go install github.com/na0fu3y/ochakai/cmd/ochakai@latest

ochakai use http://localhost:8080   # Cloud Run: ochakai use https://your-service.run.app
ochakai whoami                      # which server, as whom, reachable?
ochakai context "why is revenue down?"  # the one-call read before a data question
ochakai search "revenue" --type Metric --trust human-reviewed
ochakai verify metrics/revenue      # promotes a draft; re-affirms a verified concept
ochakai ui                          # web UI at http://127.0.0.1:8098, acting as you
```

Auth is `gcloud auth login` — there are no tokens to configure. (A
service account's ADC works too; your own `gcloud auth
application-default login` does not, because Cloud Run needs an
audience-bound ID token that only those two can mint — [why, and what to
run instead](docs/guides/mcp-clients.md#what-the-bridge-needs).) Every
command carries its flags and worked examples in `ochakai <command> -h`;
[docs/cli.md](docs/cli.md) is that same text rendered, for reading before
you install anything.

Agents without a shell — Claude Desktop, hosted agents — connect over
MCP instead, and it's the primary interface for that case. Which form
each client wants (a URL, or the `ochakai mcp-stdio` bridge against
Cloud Run), and the eight tools it will see:
[connecting an MCP client](docs/guides/mcp-clients.md).

Then copy [examples/claude-code/CLAUDE.md](examples/claude-code/CLAUDE.md)
into your project's CLAUDE.md, or install the
[hooks](examples/claude-code) to make recall and write-back automatic
instead of habitual — both without an LLM.
**[Walk the whole loop in four prompts →](docs/loop.md)**

## Why ochakai

In 2026, serving metric definitions to agents over MCP is table stakes —
semantic layers, warehouses, and data catalogs all do it. ochakai exists
for what they still don't do:

- **Interpretation knowledge is a first-class type.** An `Insight` concept
  records the baseline, the seasonality, the caveat, the threshold — the
  tribal knowledge that never fits in a semantic-model YAML and today
  travels by Slack. Your agent gets it in the same search that returns
  the metric definition.
- **A write-back loop with a memory.** Agents write learnings back as
  drafts and a human promotes them, with provenance on every concept and
  every change kept as a revision. Proposals that don't make it are kept
  as `rejected` with the reason, so agents stop re-proposing them — a
  memory of *no*. And the loop is measured: usage counts, a
  verification-age feed, and outcome reports from agents that acted on a
  concept and found it wrong ([the loop](docs/loop.md)).
- **No forward-deployed engineers, by design.** Encoding what your data
  means is labor, and it doesn't vanish — the only question is *who* does
  it. Palantir's answer is engineers on-site; the warehouse-native answer
  folds it into the data team's workflow. ochakai's bet is a third path:
  the agent *is* the forward-deployed labor, drafting breadth every time
  it learns something, while a human verifies the judgment-heavy core.
  The smallest team that sustains a living knowledge base is one reviewer
  already in the loop.
- **Not a memory layer — the other half of one.** Memory layers (mem0,
  Zep, Letta) auto-extract per-user memories with an LLM and inject them
  back unaudited. ochakai is the opposite trade: team-shared knowledge
  that passes through human review. *Memory layers remember what
  happened; ochakai curates what's true.* They compose — preferences
  there, verified data knowledge here.
- **Verified answers for any client, and an exit.** Verified-query stores
  exist inside Snowflake Cortex Analyst, inside Databricks Genie, inside
  each AI-analyst SaaS — each feeding only its own chat. The same
  knowledge here serves Claude Code, hosted MCP agents and CI jobs alike,
  and the whole base round-trips through
  [OKF v0.2](https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf)
  bundles — plain markdown + YAML frontmatter that lives happily in git,
  with trust in the spec's own keys. MIT-licensed and self-hosted per
  tenant: your knowledge is never a hostage.

Each of those has an alternative you may already run — a catalog, a
memory layer, a markdown vault with an MCP server.
[Positioning](docs/positioning.md) takes them one at a time, says where
ochakai loses, and says who should pick something else.

### What it refuses

| ochakai has no… | because |
|---|---|
| LLM | it returns human-verified golden queries verbatim, and the definitions and caveats around them. Interpretation is the client agent's job |
| SQL execution | it holds no warehouse credentials. Your agent executes |
| connector ingestion | knowledge is curated, not harvested. Trust density over volume — and a harvester would need warehouse credentials the server does not hold, so a catalog projection runs as an ordinary client under your own service account ([example](examples/bigquery-catalog)) |
| chat UI or dashboards | it feeds your agents; it doesn't compete with them. The bundled web UI is a curation surface, not a BI tool |
| secrets | Cloud Run IAM decides who reaches it and Cloud SQL authenticates the service account — nothing to issue or rotate |
| authorization | reachability is the access model — see [requirements and configuration](docs/configuration.md#authentication-has-no-configuration) |
| telemetry | nothing is reported anywhere. The only hosts ochakai contacts are Google Cloud APIs in your own project |

## Requirements

- **Google Cloud**, for a real deployment: Cloud Run IAM and Cloud SQL
  IAM are how ochakai holds no secret of its own, and there is no
  supported way to run it elsewhere. What is portable is the knowledge,
  not the runtime.
- **PostgreSQL 17 with `pg_trgm`**; `vector` (pgvector) as well where
  semantic search is on, which on Google Cloud is the default.
- **No authorization**, as above. Deciding who may reach a deployment is
  Cloud Run IAM's job.

Details, and every environment variable:
[requirements and configuration](docs/configuration.md).

## Documentation

This README describes `main`; the [changelog](CHANGELOG.md) says what is
in the version you are running.

- **Evaluating** — [FAQ](docs/faq.md) ·
  [Positioning](docs/positioning.md) ·
  [Architecture](docs/architecture.md) ·
  [Compatibility and support](docs/compatibility.md) ·
  [Roadmap](ROADMAP.md)
- **Using** — [The improvement loop](docs/loop.md) ·
  [CLI reference](docs/cli.md) ·
  [MCP clients](docs/guides/mcp-clients.md) ·
  [Troubleshooting](docs/guides/troubleshooting.md)
- **Running** — [Deploy with Terraform](deploy/terraform/README.md) ·
  [the gcloud reference it runs](deploy/cloudrun/README.md) ·
  [Configuration](docs/configuration.md) ·
  [Operating](docs/guides/operating.md) ·
  [Golden query canary](docs/guides/golden-query-canary.md)
- **Building on it** — [REST API](api/openapi.yaml) ·
  [Examples](examples) · [All docs](docs/README.md)
- **Deciding** — [The surface](docs/surface.md), the eight conditions
  ochakai exists to satisfy and everything counted against them, and
  [docs/design](docs/design/README.md), the numbered decision records
  behind it (mostly Japanese, [summarized in
  English](docs/design/README.en.md)).

## Contributing

Issues and PRs welcome — see [CONTRIBUTING.md](CONTRIBUTING.md) for the
local setup and the checks CI runs. A design doc in Japanese is the
project's own habit, not a requirement: propose in whichever language you
think in, and say so in the PR. [SUPPORT.md](SUPPORT.md) is where to ask
a question, [SECURITY.md](SECURITY.md) where to report a vulnerability.

## License

[MIT](LICENSE)
