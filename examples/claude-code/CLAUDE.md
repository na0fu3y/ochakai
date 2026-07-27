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

ochakai holds metric definitions, attested computations (sanctioned SQL,
including verified golden queries), interpretation knowledge (how to read
a metric), glossary terms, and table catalog entries. Search it before writing analytics SQL; write learnings back.

- `ochakai context "<question>"` — the one call to make before answering
  a data question: prints the full entries behind the top hits (verified
  entries rank higher), expanded one hop through links so the insight
  explaining a metric arrives with it. Start here; use search/get below
  for precise lookups.
- `ochakai search "<question or keyword>" [--type Metric|'Attested Computation'|Skill|Playbook|Insight|Policy|'Glossary Term'|'BigQuery Dataset'|'BigQuery Table'|'API Endpoint'|Reference] [--status verified]`
  — one hit per line: score, uri, status, title. Trust `verified` entries;
  judge `draft` entries by their provenance (`--json` shows `created_by`).
- `ochakai get <id>` — full entry as markdown (YAML frontmatter +
  body). Follow the body's markdown links to related entries — a link to
  another entry's path, `[revenue](/metrics/revenue.md)`, is how entries
  relate, and writing one is how you create a relationship. If stderr lists
  attachments (dashboard screenshots, ER diagrams), fetch them with
  `ochakai get <id> --download <dir>` and Read the saved files when
  the body's image references matter to the question.
- `ochakai attach <id> <file>` — attach a file to an entry
  (png/jpeg/webp, pdf, plain text; reference it from the body so the
  caption is searchable). If you learn something by looking at an
  attachment, write it into the body with `ochakai update` — knowledge
  locked in pixels is invisible to search.
- `ochakai report <id> worked|failed [--note "what went wrong"]`
  — after acting on knowledge (running an attested computation, running SQL you
  wrote from a metric definition), report whether the result was actually correct. `failed` reports
  flag verified entries for re-verification, so the next agent doesn't
  trust a stale entry blind. Always report `failed` when a verified entry
  led you to a wrong number.
- `ochakai create <id> -f entry.md` — write a learning back (OKF markdown
  as printed by `get`, or JSON; see `ochakai create -h`). The id is the
  entry's path (`queries/sales/monthly-revenue`) and it is an argument:
  an OKF document does not carry one. Entries start as `draft`; your
  identity is recorded as provenance automatically.
- `ochakai export <dir>` — snapshot the whole knowledge base as markdown;
  `ochakai import <dir>` loads a bundle back (any OKF bundle works).
Types beyond these are welcome (any slug works — e.g. `runbook/…`), and
IDs may be hierarchical (`queries/sales/monthly-revenue`) to group
related knowledge.

When a query you wrote turns out to be correct and useful,
save it: `type: Attested Computation` with `runtime` (where the SQL runs),
the SQL in a `# Computation` fence in the body, and `attrs.question` (the
natural-language question). A human can promote it to `verified` later.
