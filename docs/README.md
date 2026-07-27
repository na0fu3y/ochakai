# Documentation

Start with the [README](../README.md) — it is the manual as much as the
pitch, and it carries the quick start, the MCP tool list, the knowledge
types, and every environment variable.

## For someone evaluating ochakai

- [Architecture](architecture.md) — how the pieces fit, what the data
  model is, and why there is no authorization layer. English summary of
  the decision records.
- [Roadmap](../ROADMAP.md) — what is being worked on, and what is
  deliberately refused.
- [Changelog](../CHANGELOG.md) — what changed between releases.

## For someone running it

- [Deploy on Cloud Run](../deploy/cloudrun/README.md) — the complete
  walkthrough, ~$10/month, including private IP, the IAP-fronted web UI,
  a security hardening checklist, and the upgrade path.
- [deploy/compose.yaml](../deploy/compose.yaml) — the local stack. Docker
  and nothing else; no Google account needed.
- [Golden query canary](guides/golden-query-canary.md) — running verified
  queries from CI so a knowledge base that has quietly gone wrong says so.

## For someone building against it

- [api/openapi.yaml](../api/openapi.yaml) — the REST contract. It is
  checked rather than described: the integration tests validate every
  request and response against it, so an endpoint that drifts fails CI.
- [examples/demo](../examples/demo) — a ten-entry knowledge base to import
  and poke at (`ochakai import examples/demo`). Linked entries, mixed
  statuses, and an entry past its declared expiry, so the feeds and
  `get_context` have something to show.
- [examples/claude-code](../examples/claude-code) — drop-in agent
  instructions and the recall / write-back hooks.
- [Connecting an MCP client](guides/mcp-clients.md) — the URL or the
  `mcp-stdio` bridge, per client, with the config file each one reads and
  the ones that cannot reach an IAM-restricted deployment at all.
- `ochakai <command> -h` — every command carries its flags and worked
  examples, generated from the domain model where the values come from it.

## For someone changing it

- [CONTRIBUTING.md](../CONTRIBUTING.md) — local setup, the exact checks CI
  runs, and how design docs work.
- [docs/design](design) — the numbered, immutable decision records, and
  the [index](design/README.md) that says which ones still describe the
  current state. Mostly Japanese; authoritative where they and the English
  prose disagree.
- [SECURITY.md](../SECURITY.md) — reporting a vulnerability, and what
  counts as one.
