# Examples

- **[demo/](demo)** — a whole knowledge base, ten entries, importable in one
  command. Described below.
- **[bigquery-catalog/](bigquery-catalog)** — a daily job that projects BigQuery
  table metadata in, shipped as an Attested Computation with its Python beside
  it. What connector ingestion looks like when it stays outside the server.
- **[claude-code/](claude-code)** — the recall and write-back loop as Claude Code
  instructions and hooks.
- **[golden-query.md](golden-query.md)** — one entry on its own, for
  `ochakai put -f`.

## demo/: a knowledge base you can import

Ten entries about revenue at an invented online shop, written as an
[OKF](https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf)
v0.2 bundle. Enough of a knowledge base to see the whole shape of one: a metric
and the attested computation behind it, the glossary term and the table catalog
entry they both rest on, the policy that decides the number, the skill that says
how to run the query, and an `Insight` that says how to *read* the series — the
type that carries the knowledge no semantic layer holds.

Load it into a running server:

```sh
OCHAKAI_URL=http://localhost:8080 ochakai import examples/demo
```

Then try `ochakai context "why is revenue down?"`,
`ochakai backlinks glossary/completed-order`, and `ochakai search --sort
stale_after`. The entries link to each other with ordinary markdown links, so
the graph is already there — nothing declares it.

Deliberately not uniform: two entries are **drafts** so the review queue is not
empty, one `Reference` is **deprecated**, and one draft's `stale_after` has
already passed so the stale feed has an occupant. That is what a real knowledge
base looks like a month in.

This file is not inside `demo/`, and that is deliberate. OKF conformance asks
that *every* non-reserved `.md` file in a bundle carry frontmatter with a `type`
(SPEC §11) — only `index.md` and `log.md` are exempt. A README sitting in the
bundle would break that rule and make `ochakai import` skip a file on every run.
Every `.md` under `demo/` is a concept document, so the import reports ten
entries and skips nothing.

### The numbers are made up

Every figure there is invented: the baselines, the seasonal percentages, the
thresholds, the table and project names, `example.co.jp`, both people. Nothing
was measured. This is a teaching example — read it for the *shape* of an entry
and copy that, not the contents. Importing it into a knowledge base your agents
actually use would teach them confident nonsense about a shop that does not
exist.
