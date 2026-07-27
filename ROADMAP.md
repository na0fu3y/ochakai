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

Priorities are open to input. Say what you need in
[Discussions](https://github.com/na0fu3y/ochakai/discussions), or open an issue
if the proposal is concrete.

## Now

- **Get 0.15.0 out.** The latest release is
  [0.14.0](https://github.com/na0fu3y/ochakai/releases/tag/v0.14.0)
  (2026-07-27), which shipped 0035, 0036 and 0037. What is implemented on
  `main` and unreleased is listed in the changelog's
  [Unreleased](CHANGELOG.md) section; the breaking one is
  [0038](docs/design/0038-type-vocabulary-realignment.md), which realigns
  the recommended type vocabulary to what OKF itself spells out — nine
  spellings become eleven, with `Semantic Model` and `Golden Query`
  retired to free types. No migration runs and no stored entry changes.
  Beside it, [0039](docs/design/0039-mcp-stdio-bridge.md) gives
  stdio-only MCP clients a way in (`ochakai mcp-stdio`) and
  [0040](docs/design/0040-read-only-mode.md) adds `OCHAKAI_READ_ONLY`,
  for a deployment that serves knowledge without changing it. The release
  baseline for breaking changes is 0.14.0.
- **Keep the invariant checks growing with the code**
  ([0035](docs/design/0035-verifiability.md)): exhaustiveness linting, the
  OpenAPI contract test that runs every REST integration request and response
  past `api/openapi.yaml`, and fuzzing on the OKF parser. This is maintenance,
  not a feature — but it is where new endpoints acquire an obligation, since an
  endpoint only comes under the contract check once it has an integration test.
- **Make the project legible to somebody who has not read it.** An audit of
  what a newcomer can learn from this repository found the writing good and
  the way in poor: the runtime requirement is stated only in
  `docs/architecture.md`, MCP setup exists for one client out of the several
  the README names, the CLI's help is the best documentation here and is
  invisible until you have a binary, and the design records that the English
  prose calls authoritative are Japanese-only. That is now
  [a set of issues](https://github.com/na0fu3y/ochakai/issues) rather than a
  paragraph here. None of it is a feature, and it is the work most likely to
  decide whether anyone else can use this.

## Next

- **Write down the Git-review workflow.**
  [0009](docs/design/0009-provenance-portability.md) is the only design doc
  still marked *Proposed*. Its world-view has since been settled by 0036 §2.2 —
  bundles carry knowledge, provenance is what this instance observed and is
  never read back from a document — which leaves the recommended
  export → Git → review → import loop to be documented rather than decided.
- **Japanese lexical search does not stay fast forever.** A trigram index
  cannot serve a two-character pattern, so Japanese terms are answered by
  scanning: about 16 ms across 5000 entries, against 0.2 ms for a latin word.
  That is fine at the scale a curated knowledge base reaches, and enabling
  embeddings is the current answer. A better lexical index has not been
  designed, and nothing here promises one.

Beyond that this roadmap is thin, and honestly so. Work has been arriving from
use and from release reviews rather than from a plan; the open issues are the
current exception, and no design doc is proposed but unlanded except 0009. If
something you need is missing from this list, that is a reason to say so, not a
sign it was already considered.

## Considered and deliberately not doing

These are decisions, not backlog. Each has a document behind it, and where a
decision has a condition under which it would be revisited, the document states
it.

- **An LLM inside.** ochakai returns human-verified golden queries verbatim,
  and the definitions and caveats around them. Interpretation is the client
  agent's job, and staying LLM-free is what the trust in human verification
  rests on ([0001](docs/design/0001-architecture.md)). Summarization, chat, and
  automatic verification are out for the same reason.
- **SQL execution.** ochakai holds no warehouse credentials; your agent
  executes. `compile_sql` — deterministic SQL generation from a semantic model,
  which was as close as this ever came — existed until 0.13.0 and was retired
  ([0028](docs/design/0028-retire-compile-sql.md)): what an agent needs is the
  verified query and the caveat around it, and both arrive from `get_context`.
- **Connector ingestion.** Knowledge is curated by humans and agents, not
  harvested by pipelines. Trust density over volume.
- **A chat UI or dashboards.** The bundled web UI is a curation surface, not a
  BI tool: no charts, no query execution, no chat. It feeds your agents rather
  than competing with them.
- **Secrets, and clouds that would require them.** Cloud Run IAM decides who
  reaches a deployment and Cloud SQL authenticates the service account, so
  there is nothing to issue or rotate. Features must not introduce a token or a
  password, and porting the auth model to another cloud is not a goal — the
  secret-zero property is exactly the thing that would be lost
  ([0002](docs/design/0002-authn-authz.md),
  [0003](docs/design/0003-gcp-only.md)).
- **Authorization, and a user database.** Whoever can reach a deployment can
  read and write; identity is recorded as provenance, and trust is judged from
  provenance by whoever reads the entry (0002).
- **A server-side bulk OKF import endpoint.** Loading a bundle is a loop over
  endpoints that already exist, and a second server-side path to the same
  outcome buys convenience rather than capability. Re-examined in July 2026 at
  an embedding host's request and kept; the condition for revisiting is a
  client without the CLI that actually needs it
  ([0015 §3.2](docs/design/0015-surface-consistency.md)). The same reasoning
  closed the `ochakai sync` / diff-only-import-from-CI request (issue #43).
- **A one-to-one MCP mirror of REST.** Tool schemas cost the agent's context,
  so the tool count is a budget: browse, revisions, backlinks, attachment
  writes, bulk export/import, purge, and reembed stay off MCP, as does
  overwriting or deleting an entry a human has curated — an agent that finds a
  verified entry wrong reports the outcome or drafts a replacement
  ([0015 §3.1](docs/design/0015-surface-consistency.md),
  [0025](docs/design/0025-closing-the-loop.md)).
- **A publicly reachable MCP OAuth connector service.** It existed briefly and
  was retired in 0.9.0 ([0012](docs/design/0012-retire-mcp-oauth-connector.md));
  [0010](docs/design/0010-mcp-oauth-connector.md) is kept as the starting point
  if it ever comes back.
