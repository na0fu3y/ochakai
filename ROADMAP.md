# Roadmap

## How this roadmap works

Decisions land as numbered design docs under [docs/design](docs/design), and
[the index](docs/design/README.md) says which of them describe the current
state of an area. That index is the record; this file is only intent — what is
being worked on, and what has been ruled out. Nothing here is a commitment, and
there are no dates. An item stops being roadmap the moment it becomes a design
doc or a release.

The project is maintained by one person and stays small by refusing things, so
the last section is the load-bearing one: knowing what ochakai will not do is
more useful than a list of what it might.

What those refusals are measured against is written down in
[docs/surface.md](docs/surface.md): eight conditions ochakai exists to satisfy,
and every surface that serves them, counted. A request that serves none of the
eight is one this project will decline — it is fairer to say so up front than
to leave it open.

Priorities are open to input. Say what you need in
[Discussions](https://github.com/na0fu3y/ochakai/discussions), or open an issue
if the proposal is concrete.

## Where the data agent is going

Decided 2026-09-25. **A team that turns on ochakai's own agent should not
have to build a data agent of its own.** The agent
([0142](docs/design/0142-ochakai-carries-a-data-agent-that-does-not-rule.md))
is meant to be a finished product that a person with only a browser asks
directly. It is in the same class as Databricks Genie, Snowflake Cortex
Agents and BigQuery Conversational Analytics, and it is not only a reference
implementation behind other people's agents. Those still read the same
knowledge over the same contract
([0144](docs/design/0144-a-turn-is-kept-whoever-answered.md)).

**Why this is worth building.** Every published account of an in-house data
agent finds that accuracy depends on context, not on the model or the agent
code. Anthropic's analytics agent answered 21% of its evals correctly without
curated knowledge and more than 95% with it. Snowflake reports 24% → 86% for
Cortex Sense. Anthropic also found that giving the agent thousands of past
queries moved accuracy by less than 1%, while curated structure moved it a
lot. The large platforms are going the other way, towards context they
harvest automatically and rank by an algorithm (Genie Ontology, Cortex Sense).
ochakai competes on what they gave up:

- an answer rests on knowledge a person confirmed;
- the record says who confirmed it;
- the knowledge leaves whole, in OKF.

**What does not move.** The goal changes what the agent does, not what the
agent is allowed to do:

- only a person's ruling changes what is served;
- the server executes no SQL, and a query runs as the person who asked;
- no secrets;
- the agent is off by default;
- MCP does not carry an agent.

**The stages.** Each stage is aimed at something a team currently builds
itself. Each still answers
[docs/surface.md](docs/surface.md)'s three questions in its own PR. This
section says what the stages are for; it does not approve them in advance.

1. **An agent that corrects itself.** The person agrees once per
   conversation. After that, the page runs the agent's read-only queries on
   its own, as that person, under the byte cap BigQuery enforces before it
   bills anything. The agent reads errors and results and
   tries again, and the answer shows its result as a table and, where one
   helps, a chart. Today every query waits for a click. That is the largest
   gap to the products above, and no property depends on it: the token is
   read-only, and the page can reach only BigQuery and ochakai.
2. **A loop that closes.** A 👎 and its note become a diagnosis: which
   concept misled the agent, or what was missing. The diagnosis becomes a
   draft that waits for a ruling, together with its evidence. Kept questions
   are replayed on the operator's own machine, as the operator, so the
   server still runs nothing. Each replay is scored and stamped with the
   model and the version.
3. **Useful on the first day.** The page reads a dataset's schema as the
   person and writes one draft per table, each with an empty description
   ([0148](docs/design/0148-the-empty-base-fills-from-the-page-too.md)).
   The agent then fills that gap with the person
   ([0149](docs/design/0149-the-agent-proposes-and-the-person-applies.md)):
   it asks what a table is for, counts rows, freshness and NULLs as that
   person, and proposes the description, which the person applies. Asked
   what is used a lot, it reads the query history as that person and
   drafts the recurring aggregates. Nothing is harvested in bulk. This is
   where No FDE (C4) is won or lost.
4. **Where people already ask.** Chat surfaces and assistant connectors, on
   the conditions already written down
   ([0116](docs/design/0116-the-connector-price-changed-not-its-condition.md)).
   A chat surface that needs a secret stays out.

**How it is measured.** Four numbers, and each has to be something
`stats` or a replay can report:

- accuracy on replayed kept questions;
- the share of answers whose grounding a person confirmed;
- the time from a 👎 to a ruling;
- how many clicks one question costs the person asking.

## Now

- **Stage 4 of the data agent** (above): where people already ask. It
  is not designed yet, and the condition it starts from is that a chat
  surface needing a secret stays out. Stages 1 and 2 shipped in 0.29.0:
  automatic runs, answers that show their result, 👍 keeping a question,
  👎 diagnosing, and `ochakai eval`. Stage 3 shipped in 0.29.1: an empty
  base fills from the page (0148), and the agent proposes the meaning a
  person applies (0149). It shipped after a full run against a public
  dataset and a real model, and the fixes that run found.
- **Keep the invariant checks growing with the code**
  ([0035](docs/design/0035-verifiability.md)): exhaustiveness linting, the
  OpenAPI contract test that runs every REST integration request and response
  past `api/openapi.yaml`, and fuzzing on the OKF parser. This is maintenance,
  not a feature — but it is where new endpoints acquire an obligation, since an
  endpoint only comes under the contract check once it has an integration test.
  0.15.0 extended the same habit to the documentation: the CLI reference is
  generated from the commands' own help and the shell completions are read
  from their FlagSets, so both fail CI rather than drifting quietly. Prose the
  checks do not reach is where the drift shows up, so the habit reached the
  prose too: the vocabulary guard reads the whole OpenAPI document rather than
  only its `Type:` schema block (issue #222), a guard fails a tree that teaches
  a retired spelling (issue #275), and another fails a contract that names an
  address its own `paths` does not declare. Each of those was written after a
  release review found the drift by hand, which is where the next one will
  come from as well.
- **Make the project legible to somebody who has not read it.** An audit of
  what a newcomer can learn from this repository found the writing good and
  the way in poor. Most of what it raised has landed: the quick start loads a
  ten-concept knowledge base and says what ochakai requires before you need it,
  every MCP client the README names has setup instructions
  ([docs/guides/mcp-clients.md](docs/guides/mcp-clients.md)) (Japanese), the
  CLI's help — still the best documentation here — is rendered into
  [docs/cli.md](docs/cli.md) before you have a binary, every design record
  carries an English abstract, and the half of the product that is not Cloud
  Run has a [FAQ](docs/faq.md) (Japanese),
  [troubleshooting](docs/guides/troubleshooting.md) (Japanese) and an
  [operating guide](docs/guides/operating.md) (Japanese). What is left is
  [open as issues](https://github.com/na0fu3y/ochakai/issues) rather than a
  paragraph here. The compatibility policy the wire surfaces actually follow
  has since been written down
  ([docs/compatibility.md](docs/compatibility.md), issue #215). The two ways in
  that were missing for somebody with no shell or no map were an installable
  bundle for desktop MCP clients (issue #213) and a guided `ochakai tutorial`
  (issue #212). The bundle shipped: a release now carries
  `ochakai_X.Y.Z.mcpb`, the same `ochakai mcp-stdio` bridge with Claude
  Desktop's own config JSON written for it, for macOS and Windows (issue
  #526). It does not remove `gcloud auth login` — the bundle carries
  ochakai's binary, not your Google identity — only the step this project
  could remove, which was finding and writing the JSON by hand. The tutorial
  stayed closed as not planned: a tutorial command would be a third copy of a
  walkthrough the quick start and [docs/cli.md](docs/cli.md) already carry,
  and issue #212 names the condition for revisiting. None of this is a
  feature, and it is the work most likely to decide whether anyone else can
  use this.

## Next

Beyond the data agent's stages, work has been arriving from use and from
release reviews rather than from a plan. If something you need is missing
from this list, say so; its absence does not mean it was already
considered.

- **Spell the demand feed something other than `usage`, at 1.0.** The CLI reads
  `ochakai usage <id>` for one concept's totals and `ochakai list usage` for
  the feed ordered by them: one word, two commands, and the reader learns
  which is which from the shape of the line rather than from the word. It
  waits for the freeze to lift, and that is the whole of the decision —
  `sort=usage` is a line in [api/openapi.frozen.txt](api/openapi.frozen.txt),
  and [0064](docs/design/0064-rest-stops-at-api-v1.md) §11 leaves a security
  defect as the only reason to move one. Renaming it in the CLI alone would
  set the client against the contract it is a thin client of
  ([0067](docs/design/0067-four-faces-and-what-they-decline.md) §2) and pay for
  the same rename twice.

## Considered and deliberately not doing

These are decisions, not backlog. Each has a document behind it, and where a
decision has a condition under which it would be revisited, the document states
it.

- **An LLM that rules.** ochakai returns human-verified golden queries
  verbatim, and the definitions and caveats around them, and only a
  person's ruling changes what it serves — that is what the trust in human
  verification rests on. Since [0142](docs/design/0142-ochakai-carries-a-data-agent-that-does-not-rule.md)
  a deployment may turn on its own data agent (off by default), and it
  answers and proposes but never verifies, rejects or rewrites a ruled
  concept; search and reads carry no LLM either way. Automatic
  verification, LLM ranking, and summaries served in place of the
  knowledge stay out.
- **SQL execution on the server.** ochakai holds no warehouse
  credentials; your agent executes — and ochakai's own agent, where a
  query is run at all, runs it as the person asking, never as the server
  (0142 §4). `compile_sql` — deterministic SQL generation from a semantic model,
  which was as close as this ever came — existed until 0.13.0 and was retired
  ([0070 §3](docs/design/0070-what-was-retired-and-why.md)): what an agent needs is the
  verified query and the caveat around it — search finds the one, and the
  concept's own `linked_from` names the other
  ([0106](docs/design/0106-a-read-carries-what-points-at-it.md)).
- **Connector ingestion.** Knowledge is curated by humans and agents, not
  harvested by pipelines. Trust density over volume. The agent may read a
  warehouse as the person asking, to answer a question or to write drafts
  for an empty base (stage 3 above). What stays declined is a pipeline, and
  anything harvested being served without a ruling.
- **A second format beside OKF — including Apache Ossie (formerly Open
  Semantic Interchange).** Ossie standardizes the definition layer, and
  ochakai stands on the other side of that boundary: nothing in its core spec
  carries who wrote a definition, who confirmed it, or when it goes stale,
  which is what OKF holds. Reading or writing it would be a second format
  beside OKF, which C8's sibling condition C3 declines. The condition under
  which this is revisited is written down
  ([docs/positioning.md](docs/positioning.md#ウェアハウス-native-の-semantic-layer)):
  Ossie leaving incubation with a place in its core for a human having
  confirmed a definition.
- **Dashboards, or a BI tool.** The bundled web UI has no dashboards, and
  the server runs no query. Where a deployment turns on its own agent, the UI
  asks it. The agent answers from the knowledge, says whether a person
  confirmed each concept, and rules on nothing
  ([0145](docs/design/0145-four-faces-and-an-answer-that-shows-its-result.md) §1).
  An answer may show its own result (stage 1 above). Charts that are saved,
  pinned, scheduled or shared as a board stay out: that is a BI tool, and
  the knowledge is what ochakai competes on.
- **Secrets.** Cloud Run IAM decides who reaches a deployment and Cloud SQL
  authenticates the service account, so there is nothing to issue or rotate by
  default. Features must not introduce a token or a password. This declined
  "a second authentication path" until 2026-08, when
  [0086](docs/design/0086-a-second-way-to-say-who-is-calling.md) found one
  that costs secret-zero nothing: a deployment can name its own OpenID
  Connect (OIDC) issuer and verify bearer tokens itself, against the issuer's
  published public keys, with nothing to issue or rotate there either. The
  goal was always secret-zero
  ([0065](docs/design/0065-identity-and-provenance.md),
  [0003](docs/design/0003-gcp-only.md)) — not Google Cloud, and not one
  authentication path, for their own sake.
- **A user database, roles, and per-concept permissions.** Identity is recorded
  as provenance, and trust is judged from provenance by whoever reads the
  concept (0065 §1). This declined authorization outright until 2026-08, when
  [0109](docs/design/0109-a-directory-has-readers-and-writers.md) took the
  narrowest form of it that 0065 §1's own revision condition asked for: grants
  of read and write under a **directory prefix**, held in the database, off by
  default, and enforced in one place. What stays declined is everything around
  it — deny rules, roles, group registries, per-concept or per-type grants, and
  a policy history. A deployment that writes no grant is the deployment 0065
  describes, unchanged.
- **A server-side bulk OKF import endpoint.** Loading a bundle is a loop over
  endpoints that already exist, and a second server-side path to the same
  outcome buys convenience rather than capability. Re-examined in July 2026 at
  an embedding host's request and kept; the condition for revisiting is a
  client without the CLI that actually needs it
  ([0067 §5.2](docs/design/0067-four-faces-and-what-they-decline.md)). The same reasoning
  closed the `ochakai sync` / diff-only-import-from-CI request (issue #43).
- **A one-to-one MCP mirror of REST.** Tool schemas cost the agent's context,
  so the tool count is a budget: browse, revisions, the reverse-lookup
  filter, file writes, bulk export/import, purge, and reembed stay off MCP
  ([0067 §5.1](docs/design/0067-four-faces-and-what-they-decline.md)), as does
  overwriting or deleting a concept a human has curated — an agent that finds a
  verified concept wrong reports the outcome or drafts a replacement
  ([0067 §6](docs/design/0067-four-faces-and-what-they-decline.md),
  [0069](docs/design/0069-the-loop-and-what-measures-it.md)). A tool is what
  stays off, not the answer: a fetched concept names what links at it in
  `linked_from`, so an agent reaching a metric sees the insight explaining it
  without a lookup of its own
  ([0106](docs/design/0106-a-read-carries-what-points-at-it.md)).
- **A publicly reachable MCP OAuth connector service.** It existed briefly and
  was retired in 0.9.0 ([0070 §2](docs/design/0070-what-was-retired-and-why.md));
  [0070 §5](docs/design/0070-what-was-retired-and-why.md) names revert as the starting point
  if it ever comes back.
- **A tenant column, or a hosted edition.** There is no hosted ochakai,
  and a process that holds several organizations behind a tenant column is
  refused: the boundary between organizations is an address that already
  exists — a deployment (one Cloud Run service and one database) for an
  organization that needs isolation, a directory under
  [0109](docs/design/0109-a-directory-has-readers-and-writers.md)'s grants
  for one that accepts a shared failure domain — and that is also the shape
  in which one operator could run ochakai for dozens of organizations
  without it becoming a different product
  ([0119](docs/design/0119-an-operated-fleet-is-deployments-or-directories.md)).
  The record names the four existing decisions that keep that shape open,
  the price an operator pays today, and the order in which the product may
  move — the public-invoke self-verifying posture first, then the splits
  0109 §3 foresaw; a change that breaks one of the four is the change that
  closes it, and fleet tooling belongs with the operator, not in this
  repository.
- **A per-caller rate limit.** Who may reach a deployment is Cloud Run
  IAM's decision and lives nowhere in this binary
  ([0003](docs/design/0003-gcp-only.md),
  [0065](docs/design/0065-identity-and-provenance.md)); how often they
  reach it is the same layer's job for the same reason. A limiter inside
  would add a 429 to the operations the freeze holds still
  ([0082 §2](docs/design/0082-what-the-freeze-holds-still.md)), key on a
  client IP the API cannot see through its own web UI's proxy, and make
  the number a release to change. The `public` and `sandbox` postures
  ([0066](docs/design/0066-four-postures-one-word.md),
  [0087](docs/design/0087-a-sandbox-says-it-is-one.md)) are capped by
  `--max-instances` and Cloud Run's own 429 at saturation; fairness
  between visitors is bought, when it is needed, with an external load
  balancer and Cloud Armor
  ([operating guide](docs/guides/operating.md#公開デモ)). Revisited if a
  self-verifying public posture ([0119 §5](docs/design/0119-an-operated-fleet-is-deployments-or-directories.md))
  ships and an operator who cannot put a load balancer in front of it
  actually appears.
