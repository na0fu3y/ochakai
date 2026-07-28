# CLAUDE.md

ochakai is a knowledge store for data-analysis agents. Two decisions
frame everything: no LLM inside and no SQL execution (0001), and
Google Cloud only with zero secrets — Cloud Run IAM + Cloud SQL IAM,
never tokens or passwords (0002, 0003).

## Design docs

Architecture decisions live in [docs/design](docs/design) as numbered,
immutable decision records (mostly Japanese).
[docs/design/README.md](docs/design/README.md) is the index — its
opening table says which doc describes each area today; the prose below
it carries the history, and a superseded or amended doc says so in its
`Status:` header.

A numbered doc is for a decision **a user can observe**
([0048](docs/design/0048-decision-records-for-wire-contracts.md)): the
shape of the wire, the stored form and its round trip, what identity and
provenance mean, a new Google Cloud dependency, or something ochakai
refuses to do. Internal changes that leave the outside unchanged belong
in the PR description instead. **A doc that has not reached a release is
revised by replacing it, not by taking a new number** — immutability is a
promise about decisions somebody could be depending on.

When one is needed, it lands in the same PR as the change and the index
stays truthful — the `design-doc` skill has the full procedure (new
number, older docs' `Status:` headers, the index entry, and why amendment
chains get a replacement instead).

## Checks and conventions

Run the checks CI runs:

```sh
scripts/check          # everything; `scripts/check core` is CI's test job
scripts/check --db     # plus a throwaway PostgreSQL for the store tests
```

CI calls the same script, so the two cannot drift. Linters are
configured in [.golangci.yml](.golangci.yml) and chosen so a clean tree
reports nothing ([docs/design/0035](docs/design/0035-verifiability.md));
[CONTRIBUTING.md](CONTRIBUTING.md) explains the store test setup and
fuzzing.

- The public wire surface is [api/openapi.yaml](api/openapi.yaml); keep
  it, `internal/restapi`, `internal/mcpserver`, and `internal/apiclient`
  in sync.
- New features must land consistently across surfaces per
  [docs/design/0015](docs/design/0015-surface-consistency.md)
  (REST / MCP / CLI / Web UI — including deliberate omissions).
- Write commit messages and code comments in English.
- Cutting a release is a reviewed PR, then a tag, then verification —
  use the `release` skill rather than working from memory. A pushed tag
  is permanent.
