# ochakai — team knowledge base for data work

<!--
Copy this section into your project's CLAUDE.md. Point the CLI at your
server once per machine — authentication is your gcloud login or
service-account ADC; there are no tokens to configure:

    ochakai use https://ochakai-<hash>.run.app

(Or set OCHAKAI_URL, which takes precedence — handy for CI.)
`ochakai whoami` shows which server you target, as whom, and whether
it is reachable.
-->

ochakai holds metric definitions, verified golden queries, interpretation
knowledge (how to read a metric), glossary terms, and table catalog
entries. Search it before writing analytics SQL; write learnings back.

- `ochakai context "<question>"` — the one call to make before answering
  a data question: prints the full entries behind the top hits (verified
  entries rank higher), expanded one hop through links so the insight
  explaining a metric arrives with it. Start here; use search/get below
  for precise lookups.
- `ochakai search "<question or keyword>" [--type metric|query|insight|term|table] [--status verified]`
  — one hit per line: score, uri, status, title. Trust `verified` entries;
  judge `draft` entries by their provenance (`--json` shows `created_by`).
- `ochakai get <id>` — full entry as markdown (YAML frontmatter +
  body). Follow the `# Links` section to related entries. If stderr lists
  attachments (dashboard screenshots, ER diagrams), fetch them with
  `ochakai get <id> --download <dir>` and Read the saved files when
  the body's image references matter to the question.
- `ochakai attach <id> <file>` — attach a file to an entry
  (png/jpeg/webp, pdf, plain text; reference it from the body so the
  caption is searchable). If you learn something by looking at an
  attachment, write it into the body with `ochakai update` — knowledge
  locked in pixels is invisible to search.
- `ochakai compile --metric <name> [--dimension ds.field] [--grain ds.time_field:month] [--filter "ds.field = value"]`
  — deterministic SQL on stdout. ochakai never executes SQL; run the result
  with your own warehouse access. **Exit 2** means the request is outside
  the supported subset: read the reason on stderr and prefer any suggested
  verified golden queries.
- `ochakai report <id> worked|failed [--note "what went wrong"]`
  — after acting on knowledge (running a golden query, executing compiled
  SQL), report whether the result was actually correct. `failed` reports
  flag verified entries for re-verification, so the next agent doesn't
  trust a stale entry blind. Always report `failed` when a verified entry
  led you to a wrong number.
- `ochakai create -f entry.md` — write a learning back (OKF markdown as
  printed by `get`, or JSON; see `ochakai create -h`). Entries start as
  `draft`; your identity is recorded as provenance automatically.
- `ochakai export <dir>` — snapshot the whole knowledge base as markdown;
  `ochakai import <dir>` loads a bundle back (any OKF bundle works).

Types beyond the five above are welcome (any slug works — e.g.
`runbook/…`), and IDs may be hierarchical (`query/sales/monthly-revenue`)
to group related knowledge.

When a query you compiled or wrote turns out to be correct and useful,
save it: `type: query` with `attrs.question` (the natural-language
question) and `attrs.sql`. A human can promote it to `verified` later.
