---
type: Attested Computation
title: Sync the BigQuery catalog
description: Project BigQuery table metadata into ochakai as BigQuery Table entries, daily
tags: [bigquery, catalog, sync, python]
generated: { by: human:na0fu3y, at: 2026-07-27T09:00:00Z }
status: draft
runtime: python
parameters:
  - { name: project, type: string, required: true }
  - { name: datasets, type: array, required: true }
  - { name: ochakai_url, type: string, required: true }
  - { name: jobs_region, type: string }
  - { name: usage_days, type: integer }
  - { name: frequent_threshold, type: integer }
  - { name: prefix, type: string }
  - { name: dry_run, type: boolean }
computation: /jobs/sync-bigquery-catalog/sync-bigquery-catalog.py
executor:
  resource: /skills/run-a-python-job.md
  receipt: [sync_identity, tables_seen, written, skipped, failed]
---

The sanctioned way to get BigQuery table metadata into this knowledge base.
Run [the script](sync-bigquery-catalog/sync-bigquery-catalog.py) with
parameter values; do not write a projection of your own instead. The
computation lives in a file rather than a `# Computation` fence because it
is a few hundred lines of Python (SPEC §10.2), and the rule around it is
the same either way: an agent supplies parameters and **must not author or
edit the computation** (SPEC §10.3).

ochakai has no connectors and is not getting any — a harvester inside the
server would need warehouse credentials it deliberately does not hold, and
knowledge here is curated rather than collected. This runs outside it, as
an ordinary client of the same REST API the CLI uses, under your own
service account. Nothing in it is privileged; treat it as a starting point
and change it.

# What lands

One entry per table and view, at
`catalog/bigquery/<project>/<dataset>/<table>`:

```yaml
type: BigQuery Table
resource: bigquery://my-project.shop.orders
description: Raw orders            # BigQuery's own table description
tags: [bigquery, shop, frequently-queried]
sources:
  - resource: bigquery://my-project.shop.orders
    last_modified: "2026-07-26"
    usage_count: 412
usage_window: { from: "2026-06-27", to: "2026-07-27" }
```

with a body holding the columns (nested `RECORD` fields flattened to
`items.sku`), the partitioning and clustering — including whether a
partition filter is mandatory, which is the thing an agent most needs to
know before it writes a query — the row count, the view SQL when it is a
view, and an empty `# Caveats` section for a person to fill in.

Three of those choices are load-bearing:

- **No `title`.** The id's last segment is the display name, and that
  segment is the table id already.
- **The entry cites the table as its own source**, so
  `ochakai search --source bigquery://my-project.shop.orders` returns the
  catalog entry *and* every insight or computation written against the same
  table. That reverse lookup only works if everyone spells the URI the same
  way — pick the spelling once and keep it.
- **Query counts go in `sources[].usage_count`, never into ochakai's usage
  telemetry.** ochakai counts how often *its knowledge* was retrieved; this
  counts how often *a table* was queried. Same word, two subjects, and
  mixing them would corrupt the `--sort usage` review feed with numbers
  that have nothing to do with the knowledge base.

# Who owns an entry

The obvious failure mode of any sync is that it flattens what a person
wrote, the next morning, silently. This one writes an entry only while
**nobody has ruled on it and the last writer was the sync account**, and
the write is conditional on the version it read, so an edit landing
between the read and the write loses the race instead of being erased by
it.

"Nobody has ruled on it" is three separate questions, because ochakai
keeps the three apart: the **lifecycle** is what the writer declared
(`draft`), the **trust tier** is what the verification ledger derives, and
a **rejection** is a live ruling beside both. Verifying does not move the
status — a ruling and a publication are different acts — so a sync
watching `status` alone would go on overwriting an entry somebody had just
confirmed. The job reads all three.

A human takes an entry out of the sync by touching it, two ways that mean
different things:

- **`ochakai verify`** — "I checked this and it is right." The entry stops
  being a projection and becomes curated knowledge. Its status does not
  change; its trust tier does, and that is what the sync sees.
- **Editing the body** — enough on its own; the sync sees a different
  writer and leaves it alone from then on.

The cost is real: a frozen entry stops receiving schema changes, so a
column added next month will not appear in it. If what you wrote is *about*
the table rather than *of* it — a caveat, a baseline, the filter everyone
forgets — the better home is a separate `Insight` that links to the catalog
entry and cites the same source. Then the projection stays machine-owned
and current, your knowledge stays yours, and one `get_context` call returns
both, because links expand in each direction.

This is also the answer to "can an entry be locked to its owner?" — it can,
with no owner field and no authorization anywhere in the server. Status and
provenance carry the whole rule.

# The receipt, and what would attest it

`executor.resource` points at
[run a Python computation as a scheduled job](/skills/run-a-python-job.md),
which says what identity a run needs and what it has to bring back. A run
prints its receipt to stdout as JSON and its progress to stderr:

```json
{"sync_identity": "catalog-sync@my-project.iam.gserviceaccount.com",
 "tables_seen": 84, "written": 3, "skipped": 12, "failed": 0}
```

There is no `attester` on this entry, which is the honest state of things:
nothing checks these runs today. One would be easy and worth writing —
`sync_identity` has to be the account you scheduled and no other, `failed`
has to be zero, and `tables_seen` should not fall off a cliff between two
nights, which is what a dataset silently losing read access looks like.
Until that exists, a non-zero exit code and a red Cloud Scheduler run are
the whole alarm.

ochakai stores this contract and executes none of it: running the
computation and checking the receipt belong to whoever runs the job
(SPEC §10.5).

# What this deliberately does not do

- **No "recommended table" flag.** The script tags `frequently-queried`,
  which is an observation, and stops there. Recommending a table is a
  judgment; it belongs to the person who verifies the entry, in the words
  they choose. A threshold would put the knowledge base's name on a claim
  nobody made.
- **No Looker, and no other API that needs a client secret.** A whole
  deployment being secret-free is a property worth more than one more
  connector. A Looker sync is perfectly writable as a sibling of this
  script — but the secret is then yours to hold, in Secret Manager, and
  that should be a decision rather than a side effect.
- **No "sync everything".** `datasets` is required and has no default. A
  warehouse has thousands of tables and a curated knowledge base does not:
  the review feeds assume a person can empty them, and a few thousand
  unreviewed machine drafts is how a queue stops being read. Sync the
  datasets your agents actually query, and add more when that stops being
  true.
- **No deletes.** A table that disappears from BigQuery leaves its entry
  behind. Removing knowledge is a human decision, and the entry may be the
  only remaining record of what the table meant.
