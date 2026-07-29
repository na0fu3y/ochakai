# Documentation

Start with the [README](../README.md) — what ochakai is, the quick start,
and what it refuses to do. It is deliberately short; the manual is here.

## For someone evaluating ochakai

- [FAQ](faq.md) — what an agent may do to verified knowledge, what
  happens when two people edit one concept, who can read and write, and
  what leaving looks like. Questions another page owns get a short
  answer and a link there rather than a second copy.
- [Positioning](positioning.md) — every "why not just use X?" in one
  place: semantic layers, catalogs, memory layers, RAG, the verified-query
  store inside an AI-analyst product, and a markdown vault with an MCP
  server. Includes where ochakai loses, and who should pick something
  else.
- [Architecture](architecture.md) — how the pieces fit, what the data
  model is, and why there is no authorization layer. English summary of
  the decision records.
- [Compatibility and support](compatibility.md) — every interface is
  unstable at 0.x, there is no deprecation window, and only the latest
  release is supported. Read this before building on ochakai.
- [Roadmap](../ROADMAP.md) — what is being worked on, and what is
  deliberately refused.
- [Changelog](../CHANGELOG.md) — what changed between releases.

## For someone using it

- [Writing knowledge](knowledge.md) — what goes in a concept: the type
  vocabulary, ids as addresses, links that come from the prose,
  attachments, the OKF frontmatter that carries trust, and what search
  does with all of it.
- [The improvement loop](loop.md) — recall, write-back, review, outcome
  reports: four prompts that walk it end to end, the web UI's three
  feeds, and what gets measured.
- [CLI reference](cli.md) — every command's synopsis, flags and worked
  examples. It is `ochakai <command> -h` rendered, and a test fails when
  the two disagree, so it is readable before you have a binary and cannot
  go stale after you do.
- [Connecting an MCP client](guides/mcp-clients.md) — the eight tools an
  agent sees, and the URL or the `mcp-stdio` bridge per client, with the
  config file each one reads.

## For someone running it

- [Requirements and configuration](configuration.md) — what ochakai needs
  before it starts, and every environment variable it reads.
- [Deploy on Cloud Run](../deploy/cloudrun/README.md) — the complete
  walkthrough, ~$10/month, including private IP, the IAP-fronted web UI,
  a security hardening checklist, and the upgrade path.
- [deploy/compose.yaml](../deploy/compose.yaml) — the local stack. Docker
  and nothing else; no Google account needed.
- [Operating a deployment](guides/operating.md) — the two backups and what
  each one recovers, what a restore from an OKF bundle does *not* bring
  back, what the logs say when the deployment is degraded but running, and
  what is known — and not known — about scale.
- [Golden query canary](guides/golden-query-canary.md) — running verified
  queries from CI so a knowledge base that has quietly gone wrong says so.
- [Troubleshooting](guides/troubleshooting.md) — the local and client-side
  half: an empty search, a skipped import, a 409 or 412, an empty web UI,
  a server that refuses to start. Google Cloud symptoms stay in the deploy
  guide's §7.

## For someone building against it

- [api/openapi.yaml](../api/openapi.yaml) — the REST contract. It is
  checked rather than described: the integration tests validate every
  request and response against it, so an endpoint that drifts fails CI.
- [examples/demo](../examples/demo) — a ten-concept knowledge base to import
  and poke at (`ochakai import examples/demo`). Linked concepts, mixed
  statuses, and a concept past its declared expiry, so the feeds and
  `get_context` have something to show.
- [examples/claude-code](../examples/claude-code) — drop-in agent
  instructions and the recall / write-back hooks.

## For someone changing it

- [CONTRIBUTING.md](../CONTRIBUTING.md) — local setup, the exact checks CI
  runs, and how design docs work.
- [The surface](surface.md) — the seven conditions ochakai exists to
  satisfy, and every REST operation, query parameter, header, MCP tool,
  CLI command and environment variable counted in one place, with the
  three questions a change that adds one has to answer. The default
  answer is no. A test reads the six lists back out of the build, so the
  count cannot drift and a feature cannot arrive without the diff saying
  so.
- [docs/design](design) — the numbered, immutable decision records, and
  the [index](design/README.md) that says which ones still describe the
  current state. Mostly Japanese; authoritative where they and the English
  prose disagree. [README.en.md](design/README.en.md) summarizes every one
  of them in English — what was decided, and what it means for somebody
  using ochakai.
- [SECURITY.md](../SECURITY.md) — reporting a vulnerability, and what
  counts as one.
