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

[Quick start](#quick-start) · [Requirements](#requirements) ·
[All docs](docs/README.md) · [Architecture](docs/architecture.md) ·
[Deploy on Cloud Run](deploy/cloudrun/README.md) ·
[REST API](api/openapi.yaml) · [Changelog](CHANGELOG.md) ·
[Roadmap](ROADMAP.md) · [Support](SUPPORT.md) ·
[Contributing](CONTRIBUTING.md)

The whole thing is one Go binary and Postgres, deployable on Cloud Run +
Cloud SQL for [about $10/month](deploy/cloudrun/README.md). The local
stack is Docker and nothing else.

![The draft review queue: entries agents wrote back, waiting for a human
to verify or reject them](docs/images/webui-review.png)

This README describes `main`. For what is in the version you are running,
see the [changelog](CHANGELOG.md) — features documented here may not have
reached a tagged release yet.

## Requirements

**Running it for real means Google Cloud.** Cloud Run's IAM check decides
who reaches a deployment and Cloud SQL authenticates the service account,
which is how ochakai holds no secret of its own — and it is also why
there is no supported way to run it on another cloud or on-prem (design
docs [0002](docs/design/0002-authn-authz.md),
[0003](docs/design/0003-gcp-only.md)). What is portable is the knowledge,
not the runtime: it round-trips through OKF bundles, so leaving does not
depend on what you are leaving. `deploy/compose.yaml` runs the same
binary locally with authentication switched off — a development harness,
not the small end of production.

**There is no authorization.** Whoever can reach a deployment can read
and write everything. If you need per-entry permissions, this is the
wrong tool; see the [refusals](#why-ochakai) for what that buys.

**PostgreSQL, plus `pg_trgm`.** The first migration creates the
extension, so a database whose user may not `CREATE EXTENSION` needs an
admin to create it once — that is what the deploy guide's bootstrap SQL
is for. Turning embeddings on adds `vector` (pgvector) the same way;
without them, plain PostgreSQL is enough. CI and the deploy guide both
exercise Postgres 17.

**The server needs Docker; the client commands need a binary.** The quick
start below runs the CLI with `go run`, which wants the toolchain named
in `go.mod` — Go 1.21 and newer download it for you. To stay
toolchain-free, take a
[release archive](https://github.com/na0fu3y/ochakai/releases) instead,
or talk to the API directly, since that is all the CLI does:

```sh
curl -X PUT http://localhost:8080/api/v1/knowledge/metrics/revenue \
  -H 'content-type: text/markdown' \
  --data-binary $'---\ntype: Metric\n---\n\nCompleted orders only, net of refunds.\n'
```

Knowledge is written as an OKF document — the frontmatter is the
metadata, the markdown is the body, and the path is the address. It is
the same text `ochakai get` prints and an export writes, so reading an
entry, editing it and sending it back is one loop with no translation in
it.

## Quick start

```sh
git clone https://github.com/na0fu3y/ochakai && cd ochakai
docker compose -f deploy/compose.yaml up
```

Load the demo knowledge base — ten entries about one invented retail
domain — and try a search; everything goes through the API, so plain curl
works too:

```sh
export OCHAKAI_URL=http://localhost:8080
go run ./cmd/ochakai import examples/demo
curl 'http://localhost:8080/api/v1/knowledge?q=revenue'
```

[examples/demo](examples/demo) is a knowledge base rather than a sample
file: metrics, attested computations, an insight on how to read the
numbers, a glossary term the others depend on, a table catalog entry.
They link to each other, so `get_context` and backlinks have something to
expand; two are drafts, so the review queue is not empty; one is past the
expiry its author declared, so the stale feed has an occupant. The
numbers and table names are invented — it is there to be explored, not
copied into your warehouse. For a single entry instead, there is
[examples/golden-query.md](examples/golden-query.md).

Every client command needs to know which server it is talking to. The
environment variable is the explicit way and the right one for a trial;
`ochakai use` (below) saves the choice once you install the CLI.

### Connect Claude Code

For Claude Code — and anything else with a shell (headless agents, CI) —
the recommended interface is the bundled CLI, a thin client of the same
REST API: tool schemas don't occupy the agent's context (`--help` is
read on demand), output composes
with pipes, and it resolves Google ID tokens itself, so no proxy process
is needed against Cloud Run.

Install it from the [releases page](https://github.com/na0fu3y/ochakai/releases)
— each release carries archives for linux, macOS and Windows on amd64 and
arm64, a `checksums.txt`, and build provenance you can verify the same way
as the image (see [Supply chain](#supply-chain)). With a Go toolchain,
`go install` works too:

```sh
go install github.com/na0fu3y/ochakai/cmd/ochakai@latest

ochakai use http://localhost:8080  # Cloud Run: ochakai use https://your-service.run.app (auth = gcloud login / ADC, no tokens to configure)
ochakai whoami                     # which server, as whom, reachable?
ochakai context "why is revenue down?"  # the one-call read before a data question: full entries, links expanded
ochakai search "revenue" --type Metric --trust human-reviewed
ochakai search --sort stale_after   # past the expiry their author declared
ochakai search --source https://wiki.example/revenue-policy  # what derives from this
ochakai search "revenue" --prefix queries/sales   # only what lives under one subtree
ochakai get queries/sales/monthly-revenue
ochakai verify metrics/revenue      # promotes a draft — and re-affirms a verified entry, clearing the review feeds
ochakai attach insights/reading-revenue weekly.png   # files travel with the entry
ochakai export ./knowledge   # or: ochakai export - > okf.tar.gz
ochakai import ./knowledge   # the inverse; works with any OKF bundle (a client-side loop — see below)
ochakai ui                   # web UI at http://127.0.0.1:8098, acting as you (no deploy)
```

`ochakai use` saves the selection locally (name more servers with
`--name` and switch by name); `--url` and `OCHAKAI_URL` always override
it, so scripts and CI stay explicit. Tab completion:
`source <(ochakai completion zsh)` (also bash, fish).

Every command carries its own flags and worked examples in
`ochakai <command> -h`. [docs/cli.md](docs/cli.md) is that same text
rendered, for reading before you install anything.

Then copy [examples/claude-code/CLAUDE.md](examples/claude-code/CLAUDE.md)
into your project's CLAUDE.md — it teaches the agent the commands and the
write-learnings-back habit. To make the loop automatic instead of
habitual, install the [hooks](examples/claude-code): a UserPromptSubmit
hook injects relevant knowledge before the agent starts (recall), and a
Stop hook asks it once per data session to save what it learned
(write-back) — both without an LLM.

### MCP

Agents without a shell (Claude Desktop, Gemini Enterprise managed
agents) connect over MCP, which remains the primary interface. Claude Code
can use it too:

```sh
claude mcp add --transport http ochakai http://localhost:8080/mcp
```

Clients that take a JSON config want the same URL:

```json
{
  "mcpServers": {
    "ochakai": {
      "type": "http",
      "url": "http://localhost:8080/mcp"
    }
  }
}
```

The server speaks streamable HTTP. Two kinds of client need something in
front of it: one that can only launch a command and talk over stdio, and
*any* client pointed at Cloud Run, where the request has to carry a Google
ID token the client cannot mint. `ochakai mcp-stdio` is both answers —
it speaks MCP on stdin/stdout and forwards every message to the server,
resolving your identity the way every other client command does, so the
client itself is configured with no credentials:

```sh
claude mcp add ochakai -- ochakai mcp-stdio
```

```json
{
  "mcpServers": {
    "ochakai": { "command": "ochakai", "args": ["mcp-stdio"] }
  }
}
```

It listens on no port and holds no state; it copies JSON-RPC messages, so
it carries whatever the server offers rather than a second copy of the
tool list that could go stale (design doc
[0039](docs/design/0039-mcp-stdio-bridge.md)). Add `--url` to point it at
a specific server. `ochakai ui` exposes the same authenticated `/mcp` over
HTTP for clients that would rather have a URL, and
`gcloud run services proxy` is the third way, documented in the
[deploy guide §5](deploy/cloudrun/README.md).

Opening this repository in Claude Code connects automatically via the
committed [.mcp.json](.mcp.json), which expects ochakai (or the Cloud Run
proxy) on `localhost:8787`.

Which of the two forms a given client wants, where it keeps its config,
and which clients cannot reach an IAM-restricted deployment at all:
[docs/guides/mcp-clients.md](docs/guides/mcp-clients.md).

### Try a prompt

Once an agent is connected, four prompts walk the whole loop. Run them
against the quick start's knowledge base — the demo bundle has enough in
it that each one comes back with something.

**Recall.** Ask something the knowledge base can answer and watch the
agent reach for it rather than guess:

> What do we already know about revenue? Use ochakai before answering.

One `get_context` call comes back with the entries in full, so the agent
starts from your definitions instead of inventing one.

**Write back.** Tell it something worth keeping:

> Revenue in August runs about 15% below a normal month — it's seasonal,
> not a problem. Save that to ochakai so the next session has it.

It lands as a **draft**, attributed to the agent that wrote it. It is not
trusted yet, and search says so.

**Review.** Open `ochakai ui`, find it in the review queue, and press
Verify — or Reject with a reason, which is kept so agents stop
re-proposing it. This is the half no agent does for you.

**Close the loop.** When knowledge turns out to be wrong, say so:

> That golden query returned a number that doesn't match the finance
> close. Report it as failed in ochakai, with why.

`report_outcome` moves the entry into the re-verification feed instead of
letting the next agent trust it blind. Verifying it again empties the
feed.

If nothing comes back on the first prompt, the agent has not actually
connected — `ochakai whoami` says which server it is talking to and as
whom.

### Web UI: the human half of the loop

Agents write drafts; somebody has to read them. The bundled web UI is
where a human reviews what agents learned — search and filter by status,
browse the knowledge as a folder tree (hierarchical IDs are
directories), read an entry with its links and usage counts, then
verify / deprecate / reject (with the reason) in one click. Two feeds
put the re-verification queue in front of that reviewer: a *verification
age* feed (oldest `verified_at` first) so stale golden queries surface,
and a *needs review* feed (`sort=failed`) that lists the entries agents
reported wrong, worst first. Both empty the same way: re-verifying an
entry — "I checked it again and it is still right" — stamps a fresh
`verified_at` and takes it out of either feed, so the queues are
something a reviewer can finish rather than a ledger that only grows.
A third feed, *stale* (`sort=stale_after`), lists entries past the expiry
their own author declared — that one clears by editing the entry to
re-declare the date, since the date is a claim the writer made rather
than something the server observed. And when a cited document changes,
`?source=<uri>` answers the other direction: every entry derived from it,
straight from the source's own line on the entry page. One
self-contained page, no build step; deliberately **not** a BI tool — no
charts, no query execution, no chat.

![The knowledge base as a folder tree: an entry's id is its path, so the
sidebar is the way in](docs/images/webui-tree.png)

![An entry: status, provenance, and the tabs for its attributes, links,
backlinks, usage and revision history](docs/images/webui-entry.png)

![The re-verification feed, filtered to entries agents reported wrong,
with the type and status filters above it](docs/images/webui-wrong.png)

Same page, two identities:
`ochakai ui` serves it on loopback acting as *you* (zero deploy, edits
recorded as `human:<you>`), and `ochakai serve-ui` deploys it as a
team-shared service — same container image as the server, just
`--args=serve-ui` ([deploy guide §5b](deploy/cloudrun/README.md)).

## Why ochakai

In 2026, serving metric definitions to agents over MCP is table stakes —
semantic layers, warehouses, and data catalogs all do it. ochakai exists
for what they still don't do:

- **Interpretation knowledge is a first-class type.** An `Insight` entry
  records the baseline, the seasonality, the caveat, the threshold — the
  tribal knowledge that never fits in a semantic-model YAML and today
  travels by Slack. Your agent gets it in the same search that returns
  the metric definition.
- **A write-back loop with a memory.** Agents are encouraged to write
  learnings back; entries start as drafts and a human promotes
  them to `verified`, with provenance (who wrote it, who verified it,
  when) on every entry and every change kept as a revision. Proposals that
  don't make it are kept as `rejected` with the reason, so agents stop
  re-proposing them — a memory of *no* that verified-answer stores
  elsewhere don't keep. And the loop closes: per-entry usage counts show
  whether it's working, a *verification-age* feed surfaces knowledge that
  has gone too long unchecked (time-based staleness), and outcome reports
  (`report_outcome`: worked / failed) add the evidence-based half — an
  agent that ran a golden query and got a wrong number says so, and the
  entry rises in a *re-verification* feed (`sort=failed`) for a human or
  agent to re-check, instead of the next agent trusting the same entry
  blind. Re-checking is itself recorded (`ochakai verify`, or the web
  UI's Verify), which is what empties the feed (design doc 0025).
- **No forward-deployed engineers, by design.** Encoding what your data
  means is labor, and it doesn't vanish — the only question is *who* does
  it. Palantir's answer is forward-deployed engineers who hand-build an
  ontology on-site; the dbt and warehouse-native answer is self-serve,
  folding curation into the engineering workflow that already ships the
  data. ochakai's bet is a third path: the agent *is* the forward-deployed
  labor — it drafts the breadth every time it learns something — and a
  human verifies the judgment-heavy core, which is what keeps a knowledge
  base from rotting into a stale, confidently-wrong graveyard. No FDE army
  and no dedicated data steward: the smallest team that sustains a living
  knowledge base is one reviewer already in the loop, with the gate built
  into the workflow they already run — the bundled
  [hooks](examples/claude-code) and the CI
  [canary](docs/guides/golden-query-canary.md).
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
  [OKF v0.2](https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf)
  bundles (`ochakai export` / `ochakai import`) — plain markdown + YAML
  frontmatter that lives happily in git, with provenance and trust in the
  spec's own keys (`generated`, `verified`, `status`, `stale_after`), so
  any OKF consumer can read how much to trust an entry. Import accepts any
  producer's OKF bundle, v0.1 or v0.2, not just ochakai's own. MIT-licensed
  and self-hostable per tenant: your knowledge is never a hostage.

And it stays small by refusing things:

| ochakai has no… | because |
|---|---|
| LLM | it returns human-verified golden queries verbatim, and the definitions and caveats around them. Interpretation is the client agent's job |
| SQL execution | it holds no warehouse credentials. Your agent executes |
| connector ingestion | knowledge is curated by humans and agents, not harvested by pipelines. Trust density over volume. A harvester in the server would also need warehouse credentials it does not hold — so when you do want a catalog projected in, it runs as an ordinary client under your own service account: [examples/bigquery-catalog](examples/bigquery-catalog) |
| chat UI or dashboards | it feeds your agents; it doesn't compete with them. The bundled web UI is a curation surface, not a BI tool |
| secrets | Cloud Run IAM decides who reaches it, callers are identified by their Google identity, and Cloud SQL authenticates the service account — nothing to issue or rotate |
| authorization | reachability is the whole access model: **whoever can reach the deployment can read and write everything**. ochakai identifies the caller and records it as provenance, and stops there. Deciding who may reach it is Cloud Run IAM's job, and running it publicly invokable is a misconfiguration, not a deployment mode (design doc [0002](docs/design/0002-authn-authz.md)). If you need per-entry permissions, this is the wrong tool |
| telemetry | nothing is reported anywhere. The only hosts ochakai ever contacts are the Google Cloud APIs you configure — Cloud SQL, and GCS or Vertex AI if you turn them on. Usage counts are rows in your own database |

## MCP tools

| Tool | Description |
|---|---|
| `get_context` | The one call before answering a data question: full entries behind the top hits, links expanded both ways |
| `search_knowledge` | Cross-type search; verified entries rank higher |
| `get_knowledge` | Fetch one entry as an OKF document, with its links and attachment metadata |
| `get_attachment` | Fetch a file attached to an entry (dashboard screenshots, ER diagrams, seeds files) |
| `create_knowledge` | Write learnings back (agents create drafts) |
| `update_knowledge` | Update; every change is kept as a revision |
| `delete_knowledge` | Soft-delete (history retained) |
| `get_knowledge_usage` | Usage totals per entry — draft-promotion evidence, staleness signal |
| `report_outcome` | Report worked/failed after acting on knowledge — failed reports flag verified entries for re-verification |

Every one of these is a knowledge operation: ochakai never executes SQL
and never calls an LLM. `compile_sql` — deterministic SQL generation from
a semantic model — existed until 0.13.0 and was retired (design doc 0028):
what an agent actually needs is the verified query and the caveat around
it, and both arrive from `get_context`.

Every entry is also an **MCP resource** addressable by its canonical URI —
`ochakai://` plus its id (the entry's path), e.g. `ochakai://metrics/revenue`
or `ochakai://queries/sales/top-customers`. Clients that
support resource references (`@`-mentions) can pull an entry in as an OKF
document — frontmatter and body — without a tool call; discovery
stays with `get_context`/`search_knowledge`. Read tools carry `readOnly`
annotations and `delete_knowledge` a `destructive` one, so client
auto-approval policies work without parsing descriptions.

`import` is the one place the CLI is more than a thin client: there is no
bulk import endpoint, because loading a bundle is a loop over endpoints
that already exist, and a server-side second path to the same outcome
buys convenience rather than capability (design doc 0015 §3.2). The cost
is that the bundle semantics — attributing loose files to entries,
skipping what does not belong, ignoring the frontmatter keys the server
owns — live in the client. To load OKF bundles from something other than
this binary, [api/openapi.yaml](api/openapi.yaml) spells out what that
loop does under `/api/v1/export`.

The REST API (`/api/v1`) is a superset of these tools, adding bulk
export, the human-facing browse/revisions/backlinks reads, and
attachment writes — see
[api/openapi.yaml](api/openapi.yaml). To keep golden queries trustworthy
over time, run them as canaries from your CI:
[docs/guides/golden-query-canary.md](docs/guides/golden-query-canary.md).

## Knowledge types

| Type | What it holds |
|---|---|
| `Metric` | Semantic metric definition, synonyms |
| `Attested Computation` | A sanctioned computation and the means to check a run of it: the computation in a `# Computation` body fence, the contract in the `runtime` / `parameters` / `executor` / `attester` fields. ochakai records it and never runs it. A golden query is one of these: `runtime` says where the SQL runs, and a producer key such as `question` carries the question it answers |
| `Skill` | A procedure an entry's `executor.resource` points at — how to actually run a computation on a given runtime |
| `Playbook` | An operational procedure people and agents follow, bound to no resource: incident triage, an on-call runbook |
| `Insight` | How to read a metric: baselines, seasonality, caveats, thresholds |
| `Policy` | The rule that decides a number — revenue recognition, cost allocation. What an entry's `sources[].resource` cites |
| `Glossary Term` | Glossary term |
| `BigQuery Dataset` | BigQuery dataset catalog entry: a container grouping tables |
| `BigQuery Table` | BigQuery table catalog entry: source, column notes, known issues |
| `API Endpoint` | Catalog entry for an asset that is not a BigQuery one |
| `Reference` | Mirror of external material: enum definitions, licenses, schema docs |

These are recommendations, not a closed set — any single-line value works as a type for your
own document kinds, and IDs may be hierarchical
(`queries/sales/monthly-revenue`) to organize knowledge into directories.
The type values are the [OKF knowledge-catalog](https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf/bundles)
vocabulary verbatim: OKF registers no taxonomy, so the spelling is the
meaning, and a bundle's types survive an import/export round-trip unchanged
with no translation layer (design docs 0023, 0038). Matching is case-insensitive;
the spelling you write is the one stored. A type is not an address — the
directory layout of an exported bundle comes from entry IDs alone (design
doc 0017), so you are free to organize files however you like.

Because an ID is an address, it is also something to search within.
`--prefix` (REST `?prefix=`, MCP `prefixes`) narrows a search or a
`context` call to a subtree, and repeating it ORs the scopes, so one call
can cover two parts of the tree and each hit's ID says which it came from
(design doc 0041). Matching is on segment boundaries: `--prefix metrics`
does not reach `metrics-legacy`. Whether that is worth using depends on
your layout — `examples/demo` groups by kind (`metrics/`, `glossary/`,
`queries/sales/`), where `--type` already does most of this; it earns its
keep when directories mean something `--type` cannot say, such as one
team's own vocabulary beside a shared one. ochakai reads no meaning into
any path either way. Note that this is a filter, not a permission —
anyone who can reach the server can pass any prefix, or none.

Entries relate to each other through **the markdown links in their body**
— there is no links field to fill in. Write `[revenue](/metrics/revenue.md)`
in the prose and the entry links to `metrics/revenue`; the target gains a
backlink, and `get_context` expands the edge in both directions. Relative
paths (`./gross.md`) work too — those two are the forms SPEC §6 defines,
and they are the only ones, so an exported bundle's links resolve for
whoever reads it. This is OKF SPEC §5 taken at its word: a link
asserts a relationship, and what kind of relationship it is comes from the
surrounding prose, so ochakai stores no relationship type of its own
(design doc 0024). Links inside fenced code blocks (``` or ~~~) and
inline `` `code spans` `` are examples, not edges, and are skipped —
indented code is not detected, so a link four spaces deep is read as
prose. Renaming an entry rewrites the links pointing at it, prose
included.

Entries can carry file attachments — the dashboard screenshot behind an
insight, the ER diagram behind a table entry, the seeds.txt or spec PDF
behind a dataset. Accepted formats are the intersection of what Claude
reads and what Gemini embeds (png/jpeg/webp, pdf, plain text —
sniffed from the bytes). Attachments are searchable: filenames match in
every search, and contents join hybrid search when embeddings are
enabled — text with any embedding model, images and PDFs with
`gemini-embedding-2` — and a hit is always the owning entry (design
doc 0020).
Attachment bytes live in GCS and are fetched on demand, and attachments
round-trip through OKF bundles as plain files next to their entry.

**Trust travels with the knowledge.** OKF v0.2's schema is ochakai's
schema: every key the spec defines is a first-class field, so an exported
entry carries its provenance, trust and lifecycle (SPEC §5) where a
consumer that has never heard of ochakai will look for them.

```yaml
type: Attested Computation
runtime: bigquery
title: Monthly revenue by channel
sources:
  - id: rev-policy
    resource: https://wiki.example/finance/revenue-recognition
    title: Revenue Recognition Policy (FY2026)
    author: human:jsmith@example.co.jp
    last_modified: "2026-06-15"
usage_window: { from: "2026-06-01", to: "2026-06-30" }
generated: { by: process:analyst@example.iam.gserviceaccount.com, at: 2026-07-26T04:12:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-26T09:30:00Z }
status: stable                 # draft | stable | deprecated
stale_after: "2026-12-31"      # advisory: re-check on and after this day
```

`sources` is the material an entry derives from — give one an `id` and a
markdown footnote in the body can attribute a single claim to it.
`generated` is who the content stands by and when it last changed;
`verified` is who confirmed it — absent means unverified, and a `human:`
entry is what makes it human-reviewed (SPEC §5.3). ochakai holds these
the way the spec defines them: `status` is the lifecycle value alone
(`draft`, `stable`, `deprecated`) and `verified` is a ledger of every
confirmation, so the two signals stay independent. A draft somebody
checked and a stable entry nobody has are both states the model can hold,
and a foreign bundle's lifecycle value survives the trip unaltered.

A rejection — reviewed and *not* accepted — is this instance's ruling
rather than a stage of the concept, so it is not a status either. It
exports as `rejected_by` / `rejected_at` beside the entry's real status,
and import never reads it back: a bundle carries knowledge, not one
instance's judgments.

ochakai **records** these; it never acts on them. It does not fetch a
source's `resource`, score its credibility signals, or run an Attested
Computation's `executor` and `attester` — weighing the signals and
running the computation belong to whoever consumes the entry (SPEC §5.1,
§10.5). Keys OKF does not define pass through untouched and in place —
at the top level, and inside a `sources` entry, a `parameter`, an
`executor` or an `attester` too. Provenance itself is never read back
from a bundle: it is what this instance observed, not what a document
claims (design docs 0009, 0043).

**Japanese knowledge bases should turn embeddings on.** PostgreSQL's
full-text search does not tokenize Japanese, so the lexical half of search
matches a query by its fragments against a stored haystack — id, title,
description, tags, body, attachment filenames. Latin words stay whole;
Japanese, which has no spaces, is cut into two-character windows, and only
the windows carrying a kanji or katakana are kept, because an all-hiragana
window is grammar and matches nearly everything. Entries are then ranked
by how much of the *rare* part of the query they contain.

That finds the right entries for a keyword and for a Japanese question,
but it is still a bag of words: ask an English question and an entry
sharing three of its function words can outrank the one that names the
subject. It is also not fully indexed. A trigram index cannot serve a
two-character pattern — there is no whole trigram in one — so Japanese
terms like 売上 or 原価 are answered by scanning the table: about 16 ms
per search across 5000 entries, against 0.2 ms for a latin word. That
is fine at the scale a curated knowledge base reaches and does not stay
fine forever. `gemini-embedding-001` handles Japanese well, and with
`OCHAKAI_VERTEX_PROJECT` set, ranking comes from rank fusion across both
halves rather than from term overlap alone.

Scores are not comparable across the two modes and are not calibrated:
matched-fragment weight plus boosts in the lexical-only mode, RRF rank
fusion (~0.02 scale) in the hybrid one. Treat them as an ordering, not a
measure — `min_score` is for callers who have calibrated against their own
corpus, which is why the MCP surface does not offer it. To bound a
response, use `budget` instead: it caps the whole payload — the entries
delivered in full and the `outline` rows naming the rest — so what comes
back fits what you asked for.

## Configuration

| Env var | Description |
|---|---|
| `OCHAKAI_DATABASE_URL` | Cloud SQL connection string (required) |
| `OCHAKAI_DB_IAM_AUTH` | `true` enables Cloud SQL IAM database authentication: the connection password is a short-lived IAM token, so the connection string carries no secret |
| `OCHAKAI_GCS_BUCKET` | Bucket for attachment bytes (auth is ADC — no keys). Default: unset — the instance stores markdown entries only and attach operations return an error |
| `OCHAKAI_VERTEX_PROJECT` | Set to enable hybrid semantic search via Vertex AI embeddings (default: off, lexical-only — ochakai calls no external API unless you opt in). Auth is ADC — no API keys. **Recommended for Japanese knowledge bases** (see below) |
| `OCHAKAI_VERTEX_LOCATION` / `OCHAKAI_VERTEX_MODEL` / `OCHAKAI_EMBEDDING_DIM` | Embedding details (defaults: `us-central1`, `gemini-embedding-001`, 768). For image/PDF attachment search set model `gemini-embedding-2` with location `global` (or `us`/`eu`). Vectors are written when an entry is written, so enabling this on an existing base — or changing the model — leaves entries unembedded until you run `ochakai reembed` (design doc 0020). Dimensions above 2000 exceed pgvector's indexing limit, so those deployments fall back to an exact scan. Changing `OCHAKAI_EMBEDDING_DIM` on a base that already holds vectors is refused at startup, with the two ways out: put it back, or drop the vector tables and re-embed |
| `OCHAKAI_DELEGATING_CALLERS` | Comma-separated caller identities allowed to forward an end user's identity with `X-Ochakai-On-Behalf-Of: human:tanaka@example.co.jp` (`*` for any authenticated caller). For applications that embed ochakai and serve many people — without it, every one of their users collapses into the application's one service account. Both identities are recorded (`human:tanaka@… via process:app-sa@…`), never just the forwarded one. Default: empty, delegation off; a header from an unlisted caller is a 403, not a silent downgrade (design doc 0027) |
| `OCHAKAI_IAP_AUDIENCE` | `ochakai serve-ui` only: the IAP JWT audience to verify, which turns browser edits from `process:<webui-sa>` into the person signed in (`human:tanaka@… via process:<webui-sa>`). Requires the webui's service account in the server's `OCHAKAI_DELEGATING_CALLERS`. Once set, a request IAP did not sign is refused rather than recorded as the service account. Default: empty, per-user provenance off (design doc 0032) |
| `OCHAKAI_READ_ONLY` | `true` makes the deployment serve knowledge without changing it: every write is a 403, MCP does not offer the write tools at all, and the web UI stops drawing buttons that would only fail. For a reference-only instance or freezing a base during a migration; a *public* demo needs `OCHAKAI_PUBLIC_READ_ONLY` below, because read-only alone still refuses anonymous callers. It is not authorization — it does not look at the caller, and it refuses the operator too (design doc [0040](docs/design/0040-read-only-mode.md)). Usage telemetry still records, being the server's own observation. Default: off |
| `OCHAKAI_PUBLIC_READ_ONLY` | `true` is the posture for a deployment anyone may reach — a demo, or a reference-only copy handed out. It **reads no identity at all**: the `Authorization` header is ignored (without Cloud Run IAM in front nothing verified its signature, so believing it would let any caller name any person), delegation is ignored, every caller is `human:anonymous`, and nobody is refused. It **implies** `OCHAKAI_READ_ONLY` and cannot be separated from it — not recording who asked is only defensible because nothing is written, so a publicly readable *and writable* ochakai is not a configuration this program accepts (design doc [0042](docs/design/0042-public-read-only.md)). Setting it together with `OCHAKAI_INSECURE_DEV` is refused at startup. Default: off |
| `OCHAKAI_INSECURE_DEV` | Local development only: disables auth, everything acts as human:anonymous |
| `PORT` | Listen port (default `8080`) |

Authentication has no configuration: ochakai reads the caller identity
that Cloud Run forwards after its IAM check (`human:<email>` for people,
`process:<sa-email>` for service accounts) and records it as provenance.
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

[docs/architecture.md](docs/architecture.md) is the English overview: how
the pieces fit, what the data model is, and why there is no authorization
layer.

Underneath it, the decisions themselves live in
[docs/design](docs/design) as numbered, immutable decision records — the
[index](docs/design/README.md) groups them by area and marks which ones
still describe the current state. They are **mostly Japanese**, and they
are authoritative where they and the prose here disagree — which is why
[README.en.md](docs/design/README.en.md) sits beside the index and
summarizes every record in English: what was decided, and what it means
if you are using ochakai rather than building it.
[0001](docs/design/0001-architecture.md) carries the survey of prior art:
OpenAI's and Meta's in-house data agents, Airbnb Minerva, Uber uMetric,
Snowflake's Verified Query Repository, dbt-mcp, Vanna, WrenAI, and the
2026 context-layer landscape (OpenMetadata, DataHub, Atlan,
warehouse-native semantic layers).

## Contributing

Issues and PRs welcome — see [CONTRIBUTING.md](CONTRIBUTING.md) for the
local setup and the checks CI runs. A design doc in Japanese is the
project's own habit, not a requirement: propose in whichever language you
think in, and say so in the PR.

- [docs/faq.md](docs/faq.md) — the questions this shape invites, and
  [troubleshooting](docs/guides/troubleshooting.md) for the symptoms
- [SUPPORT.md](SUPPORT.md) — where to ask a question
- [ROADMAP.md](ROADMAP.md) — what is being worked on, and what is
  deliberately refused
- [CHANGELOG.md](CHANGELOG.md) — what changed between releases
- [SECURITY.md](SECURITY.md) — reporting vulnerabilities

## License

[MIT](LICENSE)
