# Positioning

Every "why not just use X?" answered in one place. The honest answer is
usually **"you can, and they compose"** — ochakai is one layer of a data
agent's context, not a replacement for the warehouse, the catalog, the
memory layer or your notes app. What it claims is a slot none of them
fill: *knowledge a human verified, written by the agents that use it,
served to every client over one contract.*

This page is for somebody evaluating. What ochakai refuses is in the
[README](../README.md#what-it-refuses) and the
[roadmap](../ROADMAP.md#considered-and-deliberately-not-doing); why the
surface stays small is [docs/surface.md](surface.md), whose **seven
conditions** are the vocabulary used below.

## The one line

A semantic layer tells you revenue = `SUM(price)`. It does not tell you
whether 100 is good or bad, that Q3 is always soft, that the `web` and
`web_direct` channel codes were never two things, or that the query
somebody sanctioned for this question is *that* one. ochakai holds that
second half, keeps a human's ruling on each piece of it, and lets the
agents that consume it write the next piece back.

## The neighbors

| Comparing against | It holds | It does not hold | Verdict |
|---|---|---|---|
| Warehouse-native semantic layers (dbt MCP, Cube, Lightdash, Snowflake Semantic Views, Databricks Metric Views) | Metric definitions, dimensions, compiled SQL | Interpretation, glossary, write-back, anything cross-warehouse | Compose |
| Catalogs that became "context layers" (OpenMetadata, DataHub, Atlan) | Technical metadata, lineage, ownership, harvested at scale | The curated half as OSS — interpretation and the review loop tend to sit in the commercial tier | Compose, or replaces ochakai if you already run one |
| Agent memory layers (mem0, Zep, Letta) | Per-user, per-agent memories, auto-extracted and auto-injected | Team ownership, human review, a record of *no* | Compose |
| RAG over your documents | Passages from documents written for other reasons | A reviewed claim as the unit; provenance; direction of authorship | Compose |
| Verified-query stores inside an AI-analyst product (Cortex Analyst's VQR, Genie) | Question + verified SQL, inside one vendor's chat | Any other client; an exit | Overlaps, and locks in |
| **A markdown vault with an MCP server (Obsidian, Logseq, a git repo of notes)** | markdown + frontmatter, links, local ownership, agent-readable | Verification as a live state, the loop, multi-writer identity, reach beyond one machine | Compose — see below |

### Warehouse-native semantic layers

Closest on metric definitions, and by 2026 the warehouses hold them
natively as schema objects. In a single-warehouse world that half is
theirs and ochakai's `Metric` entries are redundant. What stays is
everything that does not fit a semantic-model YAML: the baseline, the
caveat, the threshold, the query somebody sanctioned, and knowledge that
spans warehouses. ochakai stopped generating SQL deliberately
([0028](design/0028-retire-compile-sql.md)) — what an agent needs is the
verified query and the caveat around it.

### Catalogs as context layers

The nearest in intent, and the honest risk: an organization already
running a catalog with an MCP server will reasonably find it sufficient.
ochakai's structural differences are that interpretation knowledge and
the review loop are the *open-source core* rather than the commercial
tier, and that it is curated rather than harvested — no connectors, no
ingestion, trust density over volume. It stands alone for a team with no
catalog, or beside one as the verified layer for agents. A catalog
projection is an ordinary client under your own service account
([example](../examples/bigquery-catalog)), never a credential ochakai
holds.

### Agent memory layers

*Memory layers remember what happened; ochakai curates what's true.*
They extract with an LLM and inject unaudited: nobody reviews what got
remembered, and a wrong memory persists quietly. In data analysis a
wrong memory is a wrong decision, so the quality ceiling of unaudited
recall is low. ochakai is team-shared knowledge behind a human's ruling,
with rejections keeping their reason so agents stop re-proposing them.

The lesson worth taking from them is not about memory quality but about
ergonomics: their real strength is that the agent does not have to
*decide* to remember. ochakai answers that with `get_context` and the
Claude Code [hooks](../examples/claude-code) — recall and write-back as
mechanism rather than habit, and still without an LLM.

### RAG over your documents

Answered in full in the [FAQ](faq.md#how-is-this-different-from-rag-over-our-documents).
Short version: the unit is a reviewed claim, not a chunk, and the
direction is reversed — a RAG index is built *from* documents, a
knowledge base here is written *by the agents using it*.

### Verified-query stores inside an AI-analyst product

Every AI-analyst product has one, and each feeds only its own chat. The
same knowledge in ochakai serves Claude Code, hosted MCP agents and CI
jobs alike, and the whole base round-trips through OKF v0.2 bundles
([0036](design/0036-okf-schema-first.md)) — plain markdown and YAML that
lives in git. MIT and self-hosted per tenant: your knowledge is never a
hostage.

### A markdown vault with an MCP server

The cheapest alternative, and in 2026 the most common one to have
already: a vault of markdown notes with YAML frontmatter, links in the
body, reachable from Claude Code over a community MCP server in about ten
minutes with no infrastructure at all. Obsidian even reads frontmatter as
typed properties and builds table, card, list and map views over them
with Bases, while the data stays in the notes.

The overlap is not superficial — it is the same physical form. An
`ochakai export` bundle is a directory of markdown files with YAML
frontmatter whose relationships are ordinary body links
([0024](design/0024-links-from-body.md)), which is to say **it opens as a
vault**. If what you want is to browse your knowledge base with a good
editor and a graph view, that is the tool, and nothing here competes with
it. (Whether every root-relative link in an export resolves in Obsidian's
graph is untested.)

What the vault does not have is everything that is not the file format:

- **The unit.** A note is a document somebody wrote for their own
  reasons. An ochakai entry is a claim carrying its lifecycle, its
  `verified` ledger and its provenance in
  [OKF](design/0036-okf-schema-first.md)'s own keys. Those keys are
  ordinary YAML, so a vault can hold them — which is exactly where the
  difference shows. In a vault, a `verified` entry naming a human is a
  line somebody typed. Here it is what the instance observed of an
  authenticated caller, and it is never read back from a document
  ([0009](design/0009-provenance-portability.md),
  [0043](design/0043-document-first.md)) — a file has no observer.
  Nothing in a vault appends to that ledger, derives a trust tier from
  it, refuses a write against it, or resurfaces an entry when its last
  confirmation has aged out.
- **The loop (C7).** There is no analogue at all — no review queue, no
  memory of *no*, no usage counts, no verification-age feed, no outcome
  reports from agents that acted on an entry and found it wrong
  ([the loop](loop.md), [0025](design/0025-closing-the-loop.md)). This is
  the sharpest difference, and it is the product.
- **Direction.** A vault is written by a human for a human, with the
  agent as a reader — or lately a writer, unaudited and unmeasured.
  ochakai's bet is the opposite: the agent drafts breadth, a human
  verifies the judgment-heavy core.
- **Multiple writers.** A vault is a folder; its concurrency story is
  file sync, and shared vaults produce conflicts. ochakai is a server
  with ETag / `If-Match` optimistic locking
  ([0030](design/0030-optimistic-locking.md)) and a surface rule that an
  agent cannot overwrite what a human has ruled on
  ([FAQ](faq.md#can-an-agent-overwrite-or-delete-knowledge-a-human-verified)).
- **Identity, and no secret (C2).** A vault MCP server typically runs
  behind a local REST API plugin with an API key in the client config —
  exactly the token ochakai's design refuses
  ([0002](design/0002-authn-authz.md),
  [0003](design/0003-gcp-only.md)). And the caller has no identity to
  record: "who wrote this" is whoever's laptop it was.
- **Reach (C5, C6).** The vault is on one machine and so is its MCP
  server. Hosted agents, CI jobs and a REST API you can embed in your own
  web service are not there.
- **Retrieval.** Hybrid search with embeddings on by default
  ([0053](design/0053-embeddings-by-default.md)), path-scoped search,
  attachment search over image and PDF content, and `get_context` — a
  one-call read shaped for an agent's question rather than a human's
  browse.
- **Types.** A vault is type-agnostic, which is right for notes and
  costly here: nothing tells an agent that this file is a verified golden
  query and that one is a meeting note.

So the composition is real and unusually literal. A vault is where a
person thinks and writes; ochakai is where a team's verified data
knowledge lives and is served to agents — and because the stored form is
the same markdown, the export → Git → review → import loop can pass
through a vault on the way.

## Where ochakai loses

Stated plainly, because an evaluation that only finds advantages is not
an evaluation.

- **Authoring experience.** Obsidian and its ecosystem are far better
  places to write and to browse. The bundled web UI is a curation
  surface by policy, not a writing environment or a BI tool
  ([0015 §3.1](design/0015-surface-consistency.md)).
- **Infrastructure.** A vault costs nothing to stand up. ochakai wants a
  Google Cloud project and a Postgres — about $10/month, but not zero,
  and not five minutes.
- **Google Cloud only.** C2's secret-zero property is bought with Cloud
  Run IAM and Cloud SQL IAM, and there is no supported way to run
  production elsewhere ([0003](design/0003-gcp-only.md)). For many teams
  that is disqualifying, and it is a decision rather than a gap.
- **No authorization.** Whoever can reach a deployment can read and write
  everything ([0002](design/0002-authn-authz.md)). If you need per-entry
  permissions, this is the wrong tool.
- **Scale.** Built for the size a *curated* base reaches — thousands of
  entries, not millions, because every one passed a human.
- **Ecosystem.** One maintainer, one binary, no plugins, no marketplace.

## Choosing

- Already run a catalog with an MCP server, and the questions your agents
  miss are lineage and ownership → the catalog is enough.
- One warehouse, and what is missing is metric definitions → the
  warehouse's own semantic layer.
- What is missing is the agent remembering *you* — preferences, ongoing
  context → a memory layer.
- One person, one machine, notes you write yourself → a vault, and an
  MCP server over it.
- **Several agents and people, and what is missing is knowledge somebody
  checked** — which query is sanctioned, what the number means, what was
  already tried and rejected — and you can run it on Google Cloud →
  that is the slot this fills.

## Where this research lives

This page is the living answer and is expected to change as the
alternatives do. The dated snapshots of the research behind it stay where
they were taken, in
[0001 §2](design/0001-architecture.md) — the 2026-07-16 note on catalogs
turning into context layers and warehouse-native semantic layers, and the
2026-07-17 note on memory layers. Those are decision records and are not
rewritten; a landscape is not a decision, so it is tracked here instead.
