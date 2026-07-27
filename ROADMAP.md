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

- **Get 0.14.0 out.** The latest release is 0.13.0. Two accepted design docs
  are implemented on `main` and unreleased:
  [0036](docs/design/0036-okf-schema-first.md), which takes OKF v0.2's schema
  as ochakai's own and moves the keys the spec defines (`sources`,
  `usage_window`, and the Attested Computation contract) out of `attrs` and
  into first-class fields, and
  [0037](docs/design/0037-stale-and-source-lookup.md), which makes those keys
  something you can ask for: the `sort=stale_after` feed of entries past the
  expiry their author declared, and the `?source=<uri>` reverse lookup for
  everything derived from a document that changed. The release baseline for
  breaking changes is 0.13.0, not 0.12.
- **Keep the invariant checks growing with the code**
  ([0035](docs/design/0035-verifiability.md)): exhaustiveness linting, the
  OpenAPI contract test that runs every REST integration request and response
  past `api/openapi.yaml`, and fuzzing on the OKF parser. This is maintenance,
  not a feature — but it is where new endpoints acquire an obligation, since an
  endpoint only comes under the contract check once it has an integration test.

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

Beyond that this roadmap is thin, and honestly so. There are no open issues and
no other proposed design docs at the time of writing; work has been arriving
from use and from release reviews rather than from a plan. If something you
need is missing from this list, that is a reason to say so, not a sign it was
already considered.

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
