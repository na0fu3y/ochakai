# The manual, for English speakers

**The manual is in Japanese, and that is a decision** rather than a
backlog item: condition C8 in [surface.md](surface.md) says ochakai
should be one of the best choices available to a Japanese-speaking
reader, and a manual in their language is most of what that means.
Seven pages stay in English — the front door and the contract — and
[docs/README.md](README.md) lists them.

No page has a translated twin. Two texts saying the same thing go out of
step, and nobody can tell which half is stale
([CONTRIBUTING.md](../CONTRIBUTING.md), *Translating a manual page*).

**So this page is not a translation.** It is the one page an English
speaker needs in order to use the Japanese ones: what each contains, in
what order to read them, and the handful of names you would otherwise
have to find inside a page you cannot skim. Machine translation handles
the prose from there, and it handles it well — what it cannot give you is
which page to open.

## Read in this order

**Deciding whether to adopt it.** [README](../README.md) (English) says
what ochakai is and what it refuses. Then
[docs/positioning.md](positioning.md): the answer to "why not use X?",
one neighbour at a time — warehouse-native semantic layers, catalogs,
agent memory layers, RAG over your own documents, the verified-query
store inside an AI-analyst product, Palantir-style ontologies, graph-DB
ontology platforms, and a markdown vault with an MCP server. It ends with
**where ochakai loses** and who should pick something else, which is the
section worth reading first if you are evaluating.

**Running it.** [docs/configuration.md](configuration.md) is every
setting and what it does. [deploy/terraform](../deploy/terraform) is the
recommended path — thirteen steps to a Cloud Run + Cloud SQL deployment
for about $10 a month; [deploy/cloudrun](../deploy/cloudrun) is the
gcloud reference it was built from, and stays the source of truth where
the two disagree. [docs/guides/operating.md](guides/operating.md) covers
backups, monitoring, hardening, the team web UI and upgrades.

**Using it.** [docs/loop.md](loop.md) walks recall → write-back → ruling
→ outcome in four prompts. [docs/cli.md](cli.md) (English, generated from
the binary's own help) is the command reference.
[docs/guides/mcp-clients.md](guides/mcp-clients.md) is how each MCP
client is pointed at a deployment.
[docs/architecture.md](architecture.md) is the reference side: what a
concept is, types, ids as addresses, links derived from the body.

**Building on it.** [api/openapi.yaml](../api/openapi.yaml) (English) is
the contract, frozen at `/api/v1`.
[docs/guides/rest-integration.md](guides/rest-integration.md) is what an
application embedding it needs.

## The names you would otherwise have to hunt for

**Environment variables.** Meanings are in
[configuration.md](configuration.md); this is the list, so you know what
to look up.

- `OCHAKAI_DATABASE_URL`
- `OCHAKAI_DB_IAM_AUTH`
- `OCHAKAI_DELEGATING_CALLERS`
- `OCHAKAI_EMBEDDINGS`
- `OCHAKAI_GCS_BUCKET`
- `OCHAKAI_IAP_AUDIENCE`
- `OCHAKAI_MODE`
- `OCHAKAI_PRODUCER`
- `OCHAKAI_RECORD_MISSES`
- `OCHAKAI_URL`
- `PORT`

**`OCHAKAI_MODE` is the access posture, and there are four.** Unset is
the normal one: private, writable, Cloud Run IAM decides who reaches it.
`read-only` serves the base and refuses every write. `public` is the
posture the demo runs in — reachable without a Google account, read-only,
and reading no identity at all. `dev` turns authentication off and makes
every caller `human:anonymous`; it is for a laptop, never for a
deployment.

**There is no authorization.** Whoever can reach a deployment can read
and write everything; identity is recorded as provenance, and trust is
judged from provenance by whoever reads the concept. If you need
per-concept permissions, this is the wrong tool — said plainly here
because it is a disqualifying requirement for some teams and it is not
going to change.

**Connecting an MCP client** takes one of two forms, whichever client it
is: a URL for clients that speak HTTP MCP, or the `ochakai mcp-stdio`
bridge for those that cannot. The guide's table names the config file and
the key each client spells the URL with.

## Contributing in English

Design records are Japanese, and every one carries an English abstract in
[docs/design/README.en.md](design/README.en.md) — a reading aid, not the
authority. Propose in whichever language you think in and say so in the
pull request; [CONTRIBUTING.md](../CONTRIBUTING.md) is in English and
describes the checks.
