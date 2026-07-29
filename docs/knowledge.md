# Writing knowledge

An entry is an [OKF](https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf)
v0.2 document — YAML frontmatter is the metadata, the markdown below it is
the body — and its **id is its address**: the entry lives in the bundle at
its id plus `.md`, which is the same place an export writes it and the same
place `ochakai get` reads it from. Reading an entry, editing it, and sending
it back is one loop with no translation in it.

```sh
curl -X PUT http://localhost:8080/api/v1/bundle/metrics/revenue.md \
  -H 'content-type: text/markdown' \
  --data-binary $'---\ntype: Metric\n---\n\nCompleted orders only, net of refunds.\n'
```

This page is the reference for what goes in one.
[Architecture](architecture.md) explains why the model has this shape; the
decision records it links are authoritative.

## Types

| Type | What it holds |
|---|---|
| `Metric` | Semantic metric definition, synonyms |
| `Attested Computation` | A sanctioned computation and the means to check a run of it: the computation in a `# Computation` body fence, the contract in the `runtime` / `parameters` / `executor` / `attester` fields. ochakai records it and never runs it. A golden query is one of these: `runtime` says where the SQL runs, and a producer key such as `question` carries the question it answers |
| `Skill` | A procedure an entry's `executor.resource` points at — how to actually run a computation on a given runtime |
| `Playbook` | An operational procedure people and agents follow, bound to no resource: incident triage, an on-call runbook |
| `Insight` | How to read a metric: baselines, seasonality, caveats, thresholds |
| `Policy` | The rule that decides a number — revenue recognition, cost allocation. What an entry's `sources[].resource` cites |
| `Glossary Term` | Glossary term |
| `BigQuery Dataset` | BigQuery dataset catalog entry: a container grouping tables |
| `BigQuery Table` | BigQuery table catalog entry: source, column notes, known issues |
| `API Endpoint` | Catalog entry for an asset that is not a BigQuery one |
| `Reference` | Mirror of external material: enum definitions, licenses, schema docs |

These are recommendations, not a closed set — any single-line value works as
a type for your own document kinds. The spellings are the
[OKF knowledge-catalog](https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf/bundles)
vocabulary verbatim: OKF registers no taxonomy, so the spelling is the
meaning, and a bundle's types survive an import/export round-trip unchanged
with no translation layer (design docs
[0023](design/0023-okf-type-vocabulary.md),
[0038](design/0038-type-vocabulary-realignment.md)). Matching is
case-insensitive; the spelling you write is the one stored.

## IDs are paths, and paths are searchable

IDs may be hierarchical (`queries/sales/monthly-revenue`) to organize
knowledge into directories. A type is not an address — the directory layout
of an exported bundle comes from entry IDs alone (design doc
[0017](design/0017-path-addressing.md)) — so you are free to organize files
however you like.

Because an ID is an address, it is also something to search within.
`--prefix` (REST `?prefix=`, MCP `prefixes`) narrows a search or a `context`
call to a subtree, and repeating it ORs the scopes, so one call can cover two
parts of the tree and each hit's ID says which it came from (design doc
[0041](design/0041-path-scoped-search.md)). Matching is on segment
boundaries: `--prefix metrics` does not reach `metrics-legacy`. Whether that
is worth using depends on your layout — [examples/demo](../examples/demo)
groups by kind (`metrics/`, `glossary/`, `queries/sales/`), where `--type`
already does most of this; it earns its keep when directories mean something
`--type` cannot say, such as one team's own vocabulary beside a shared one.
ochakai reads no meaning into any path either way. Note that this is a
filter, not a permission — anyone who can reach the server can pass any
prefix, or none.

## Links come from the prose

Entries relate to each other through **the markdown links in their body** —
there is no links field to fill in. Write `[revenue](/metrics/revenue.md)` in
the prose and the entry links to `metrics/revenue`; the target gains a
backlink, and `get_context` expands the edge in both directions. Relative
paths (`./gross.md`) work too — those two are the forms SPEC §6 defines, and
they are the only ones, so an exported bundle's links resolve for whoever
reads it. This is OKF SPEC §5 taken at its word: a link asserts a
relationship, and what kind of relationship it is comes from the surrounding
prose, so ochakai stores no relationship type of its own (design doc
[0024](design/0024-links-from-body.md)). Links inside fenced code blocks
(``` or ~~~) and inline `` `code spans` `` are examples, not edges, and are
skipped — indented code is not detected, so a link four spaces deep is read
as prose. Renaming an entry rewrites the links pointing at it, prose
included.

## Attachments

Entries can carry file attachments — the dashboard screenshot behind an
insight, the ER diagram behind a table entry, the seeds.txt or spec PDF
behind a dataset. Accepted formats are the intersection of what Claude reads
and what Gemini embeds (png/jpeg/webp, pdf, plain text — sniffed from the
bytes). Attachments are searchable: filenames match in every search, and
contents join hybrid search wherever embeddings are enabled — text with any
embedding model, images and PDFs with `gemini-embedding-2` — and a hit is
always the owning entry (design doc
[0020](design/0020-attachment-search.md)). Attachment bytes live in GCS and
are fetched on demand, and attachments round-trip through OKF bundles as
plain files next to their entry.

## Trust travels with the knowledge

OKF v0.2's schema is ochakai's schema: every key the spec defines is a
first-class field, so an exported entry carries its provenance, trust and
lifecycle (SPEC §5) where a consumer that has never heard of ochakai will
look for them.

```yaml
type: Attested Computation
runtime: bigquery
title: Monthly revenue by channel
sources:
  - id: rev-policy
    resource: https://wiki.example/finance/revenue-recognition
    title: Revenue Recognition Policy (FY2026)
    author: human:jsmith@example.co.jp
    last_modified: "2026-06-15"
usage_window: { from: "2026-06-01", to: "2026-06-30" }
generated: { by: process:analyst@example.iam.gserviceaccount.com, producer: insightflow/1.4.0, at: 2026-07-26T04:12:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-26T09:30:00Z }
status: stable                 # draft | stable | deprecated
stale_after: "2026-12-31"      # advisory: re-check on and after this day
```

`sources` is the material an entry derives from — give one an `id` and a
markdown footnote in the body can attribute a single claim to it.
`generated` is who the content stands by and when it last changed; `verified`
is who confirmed it — absent means unverified, and a `human:` entry is what
makes it human-reviewed (SPEC §5.3). The `producer` beside `by` is the
software that made the write, as the caller declared it
(`<producer>/<version>`, SPEC §7): it says *what* wrote, never *who*, so it
sits next to the authenticated identity rather than in it (design doc
[0052](design/0052-producer-beside-the-actor.md)). ochakai holds these the
way the spec defines them: `status` is the lifecycle value alone (`draft`,
`stable`, `deprecated`) and `verified` is a ledger of every confirmation, so
the two signals stay independent. A draft somebody checked and a stable entry
nobody has are both states the model can hold, and a foreign bundle's
lifecycle value survives the trip unaltered.

A rejection — reviewed and *not* accepted — is this instance's ruling rather
than a stage of the concept, so it is not a status either. It exports as
`rejected_by` / `rejected_at` beside the entry's real status, and import
never reads it back: a bundle carries knowledge, not one instance's
judgments.

ochakai **records** these; it never acts on them. It does not fetch a
source's `resource`, score its credibility signals, or run an Attested
Computation's `executor` and `attester` — weighing the signals and running
the computation belong to whoever consumes the entry (SPEC §5.1, §10.5).
Keys OKF does not define pass through untouched and in place — at the top
level, and inside a `sources` entry, a `parameter`, an `executor` or an
`attester` too. Provenance itself is never read back from a bundle: it is
what this instance observed, not what a document claims (design docs
[0009](design/0009-provenance-portability.md),
[0043](design/0043-document-first.md)).

## What search does with it

**Japanese knowledge bases need embeddings, which is why they are the
default.** PostgreSQL's full-text search does not tokenize Japanese, so the
lexical half of search matches a query by its fragments against a stored
haystack — id, title, description, tags, body, attachment filenames. Latin
words stay whole; Japanese, which has no spaces, is cut into two-character
windows, and only the windows carrying a kanji or katakana are kept, because
an all-hiragana window is grammar and matches nearly everything. Entries are
then ranked by how much of the *rare* part of the query they contain.

That finds the right entries for a keyword and for a Japanese question, but
it is still a bag of words: ask an English question and an entry sharing
three of its function words can outrank the one that names the subject. It is
also not fully indexed. A trigram index cannot serve a two-character pattern
— there is no whole trigram in one — so Japanese terms like 売上 or 原価 are
answered by scanning the table: about 16 ms per search across 5000 entries,
against 0.2 ms for a latin word. That is fine at the scale a curated
knowledge base reaches and does not stay fine forever.
`gemini-embedding-001` handles Japanese well, and where embeddings are
reachable — on Google Cloud, that is the default and takes no configuration
(design doc [0053](design/0053-embeddings-by-default.md)) — ranking comes
from rank fusion across both halves rather than from term overlap alone.

Scores are not comparable across the two modes and are not calibrated:
matched-fragment weight plus boosts in the lexical-only mode, RRF rank fusion
(~0.02 scale) in the hybrid one. Treat them as an ordering, not a measure —
`min_score` is for callers who have calibrated against their own corpus,
which is why the MCP surface does not offer it. To bound a response, use
`budget` instead: it caps the whole payload — the entries delivered in full
and the `outline` rows naming the rest — so what comes back fits what you
asked for.
