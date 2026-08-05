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

## Now

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
  Run has a [FAQ](docs/faq.md),
  [troubleshooting](docs/guides/troubleshooting.md) (Japanese) and an
  [operating guide](docs/guides/operating.md) (Japanese). What is left is
  [open as issues](https://github.com/na0fu3y/ochakai/issues) rather than a
  paragraph here. The compatibility policy the wire surfaces actually follow
  has since been written down
  ([docs/compatibility.md](docs/compatibility.md), issue #215). The two ways in
  that were missing for somebody with no shell or no map — an installable
  bundle for desktop MCP clients (issue #213) and a guided `ochakai tutorial`
  (issue #212) — were both closed as not planned, and for the same reason:
  the surface each would add is not what keeps its reader out. A bundle
  removes the cheapest of the three prerequisites a desktop client needs and
  leaves `gcloud auth login`; a tutorial command would be a third copy of a
  walkthrough the quick start and [docs/cli.md](docs/cli.md) already carry.
  Both issues name the condition for revisiting. None of this is a feature,
  and it is the work most likely to decide whether anyone else can use this.

## Next

- **Japanese lexical search does not stay fast forever.** A trigram index
  cannot serve a two-character pattern, so Japanese terms are answered by
  scanning: about 16 ms across 5000 concepts, against 0.2 ms for a latin word.
  That is fine at the scale a curated knowledge base reaches, and embeddings
  are the answer — which is why they stopped being opt-in: running on Google
  Cloud, ochakai turns them on by itself
  ([0073](docs/design/0073-search-and-when-embeddings-apply.md)). A better lexical index
  has not been designed, and nothing here promises one.

Beyond that this roadmap is thin, and honestly so. Work has been arriving from
use and from release reviews rather than from a plan; the open issues are the
current exception, and no design doc is proposed but unlanded. If something you
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
  ([0070 §3](docs/design/0070-what-was-retired-and-why.md)): what an agent needs is the
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
  ([0065](docs/design/0065-identity-and-provenance.md),
  [0003](docs/design/0003-gcp-only.md)).
- **Authorization, and a user database.** Whoever can reach a deployment can
  read and write; identity is recorded as provenance, and trust is judged from
  provenance by whoever reads the concept (0065 §1).
- **A server-side bulk OKF import endpoint.** Loading a bundle is a loop over
  endpoints that already exist, and a second server-side path to the same
  outcome buys convenience rather than capability. Re-examined in July 2026 at
  an embedding host's request and kept; the condition for revisiting is a
  client without the CLI that actually needs it
  ([0067 §5.2](docs/design/0067-four-faces-and-what-they-decline.md)). The same reasoning
  closed the `ochakai sync` / diff-only-import-from-CI request (issue #43).
- **A one-to-one MCP mirror of REST.** Tool schemas cost the agent's context,
  so the tool count is a budget: browse, revisions, backlinks, file
  writes, bulk export/import, purge, and reembed stay off MCP
  ([0067 §1](docs/design/0067-four-faces-and-what-they-decline.md)), as does
  overwriting or deleting a concept a human has curated — an agent that finds a
  verified concept wrong reports the outcome or drafts a replacement
  ([0067 §6](docs/design/0067-four-faces-and-what-they-decline.md),
  [0069](docs/design/0069-the-loop-and-what-measures-it.md)).
- **A publicly reachable MCP OAuth connector service.** It existed briefly and
  was retired in 0.9.0 ([0070 §2](docs/design/0070-what-was-retired-and-why.md));
  [0070 §5](docs/design/0070-what-was-retired-and-why.md) names revert as the starting point
  if it ever comes back.
