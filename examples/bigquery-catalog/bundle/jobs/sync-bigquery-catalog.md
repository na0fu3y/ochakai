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
  receipt: [sync_identity, project, datasets, prefix, tables_seen, written, unchanged, lapsed, skipped, missing, failed]
attester:
  resource: /attesters/catalog-sync.py
---

The sanctioned way to get BigQuery table metadata into this knowledge base.
Run [the script](sync-bigquery-catalog/sync-bigquery-catalog.py) with
parameter values; do not write a projection of your own instead. The
computation lives in a file rather than a `# Computation` fence because it
is a few hundred lines of Python (SPEC §10.2), and the rule around it is
the same either way: an agent supplies parameters and **must not author or
edit the computation** (SPEC §10.3).

ochakai has no connectors today — a harvester inside the server would
need warehouse credentials it deliberately does not hold. That is a means
rather than a property (decision 0160): what a sync writes is served at
the trust it has, and a change lapses the verification that stood. This
runs outside it, as an ordinary client of the same REST API the CLI uses,
under your own service account. Nothing in it is privileged; treat it as a starting point
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
    last_modified: "2026-07-26T00:00:00Z"
    usage_count: 412
usage_window: { from: "2026-06-27T00:00:00Z", to: "2026-07-27T00:00:00Z" }
projection: {"text": "sha256:…", "shape": "sha256:…"}   # see "Who owns an entry"
```

with a body holding the columns (nested `RECORD` fields flattened to
`items.sku`), the partitioning and clustering — including whether a
partition filter is mandatory, which is the thing an agent most needs to
know before it writes a query — the row count, the view SQL when it is a
view, and an empty `# Caveats` section for a person to fill in.

A **date-sharded table** — `events_20260101`, `events_20260102`, … —
is one entry, not one per day: two or more tables in a dataset whose
names are one stem and a calendar date land at
`catalog/bigquery/<project>/<dataset>/events_`, titled and
addressed as the wildcard `events_*` they are queried through, with the
latest shard's columns and layout and the shards' query counts added up.
Seven hundred identical entries tie on every search one of them matches
and leave no room on the page for anything else; `ochakai seed` folds by
the same rule. A single dated table stays itself — it may be a snapshot
somebody named — and neither does a stem that is itself a table's name
(`sales` beside `sales20260101`), which would put two tables at one
address.

And one entry per dataset, at `catalog/bigquery/<project>/<dataset>` —
the dataset's own description and a table list linking to the entries
under it, which is the address "what is in shop?" resolves to. The
concept and the directory of its tables share that name by design
(design doc 0075 §2), and the OKF reference bundle keeps a dataset
concept beside its table concepts for the same reason. One discipline
travels with it: the list is a claim about the *whole* dataset, so only
a run that enumerated the whole dataset writes it. If you narrow this
script — one table, one glob — leave the dataset entry alone rather
than have it recite tables the run never saw.

Three of those choices are load-bearing:

- **No `title`.** The id's last segment is the display name, and that
  segment is the table id already — except for a date-sharded table,
  whose segment is the stem (`events_`) and whose title is the wildcard.
- **The entry cites the table as its own source**, so
  `ochakai list --source bigquery://my-project.shop.orders` returns the
  catalog entry *and* every insight or computation written against the same
  table. `list`, not `search`: a reverse lookup is a listing and has no
  query to rank by, which is the whole of design doc 0068 §2. That lookup only
  works if everyone spells the URI the same way — pick the spelling once
  and keep it.
- **Query counts go in `sources[].usage_count`, never into ochakai's usage
  telemetry.** ochakai counts how often *its knowledge* was retrieved; this
  counts how often *a table* was queried. Same word, two subjects, and
  mixing them would corrupt the `--sort usage` review feed with numbers
  that have nothing to do with the knowledge base.

# Who owns an entry

The obvious failure mode of any sync is that it flattens what a person
wrote, the next morning, silently. The opposite failure is quieter: an
entry a person confirmed stops following the table, and a column added
next month never appears in it. This one avoids both by telling apart
the two things a person does to an entry.

- **Editing it** — the person's words are in it now. The sync leaves it
  alone from then on, for good.
- **Verifying it** — "I checked this and it is right." The words are
  still the projection's, so the sync keeps following the table. When
  the table's shape changes — a column, its type, the partitioning, the
  view, the description — the sync writes the new shape, the
  verification lapses (any meaningful change does, decision 0160), the
  entry reads `unverified`, and it waits in the edited queue
  (`ochakai list verified_at --trust unverified`) for a person, or for
  [the confirmation job](/jobs/confirm-bigquery-catalog.md), to confirm
  it again. A published entry whose shape has not changed is not
  rewritten: today's row count is not worth a lapsed verification.

The difference is read off a stamp every projection carries in its
frontmatter:

```yaml
projection: {"text": "sha256:…", "shape": "sha256:…"}
```

`text` is a digest of the document as the projection wrote it, leaving
out `status` (verifying a draft rewrites that one line and keeps every
other byte, decision 0157), the stamp itself, and the keys the instance
adds on a read (`generated`, `verified`, `created_by`). If the standing
document still digests to `text`, nobody has written in it. `shape` is
the digest of the same document without the day's observations — row
count, query counts, last modification — and is what "the table
changed" means. ochakai stores a document byte for byte and reads
neither value; they are a contract between runs of this file, not a
lock. The point was never to stop a person, only to notice one.

The status a person gave an entry stays: the sync writes a published
entry back as published, because putting it back to `draft` would be the
job overruling the person who published it. Every write is conditional
on the version it read, so an edit landing between the read and the
write loses the race instead of being erased by it.

**Entries from before the stamp** (written by `sync-bigquery-catalog/2`
or earlier) keep the old rule, because without the stamp a verified
entry cannot be told from an edited one: the sync writes one only while
it is still an unverified draft whose last write carries this job's
producer family (`sync-bigquery-catalog/`), and that write brings the
stamp in, so the rule converges. An unstamped entry a person already
verified stays frozen; to bring it back under the sync, delete it and
let the next run project it again, then verify the new one.

If what you know is *about* the table rather than *of* it — a caveat, a
baseline, the filter everyone forgets — the better home is a separate
`Insight` that links to the catalog entry and cites the same source.
Then the projection keeps following the table, your knowledge stays
yours, and a read of the catalog entry names the insight in its
`linked_from` rows, because links are one edge read from either end.

A rejection is not a third case. It is a deletion carrying a reason
(design doc 0135), so a rejected entry is gone rather than marked, and the
next run projects it again.

# The receipt, and what would attest it

`executor.resource` points at
[run a Python computation as a scheduled job](/skills/run-a-python-job.md),
which says what identity a run needs and what it has to bring back. A run
prints its receipt to stdout as JSON and its progress to stderr:

```json
{"sync_identity": "catalog-sync@my-project.iam.gserviceaccount.com",
 "project": "my-project", "datasets": ["marketing", "shop"],
 "prefix": "catalog/bigquery",
 "tables_seen": 84, "written": 3, "unchanged": 70, "skipped": 12,
 "missing": 1, "failed": 0}
```

`attester.resource` points at
[catalog-sync.py](/attesters/catalog-sync.py), which decides whether a run
counts. It asks four things, and the first two are the two questions the
reference bundle's `sql_equality.py` asks of a SQL run:

1. **Provenance — did the executor run what was sanctioned?** For a query
   that is about the SQL text. Here it is about **scope**: the four fields
   above the counts are exactly what an entry id is derived from, so a run
   with a different `prefix` did not refresh this catalog. It wrote a
   second one at addresses nobody is watching and left the first to rot.
   This is why the scope is in the receipt at all.
2. **Fidelity — does the outcome hold together?** `written + unchanged +
   skipped + missing + failed` must equal `tables_seen` plus one dataset
   entry per dataset in scope, and `failed` must be zero. The count is
   taken before a write is attempted and the outcomes after, so a
   disagreement means a table left the loop uncounted. `missing` — a
   table dropped between the listing and the read — is an outcome, not a
   failure: deletion is a warehouse's ordinary day, and a nightly job
   that turns red for the world changing normally is a red nobody reads.
   What failed asks is whether the *run* broke; whether the *catalog* is
   draining away is check 4's question, asked across nights.
3. **Ownership** — the run projected something. `written` and `unchanged`
   leave an entry the job's; `skipped` says a person took it. A night where
   every table was skipped refreshed nothing, and the two checks above call
   it clean: the counts conserve, nothing failed, the catalog is the same
   size. **This check exists because that state shipped.** Writing no
   `status` at all does not mean draft — SPEC §5.4 defaults it to `stable`
   — so every entry this job created came back reading as ruled on by a
   person, and from the second night on it skipped all of them. One line
   in `build_document` was the bug; this is the check that would have said
   so on night two.
4. **Continuity** — given the previous passing receipt, `tables_seen` has
   not halved. A dataset silently losing read access looks exactly like a
   warehouse that got smaller. `tables_seen` counts entries, so a sharded
   table is one: the first run after shards began folding reports far
   fewer than the run before it, and is checked without the previous
   receipt. The per-shard entries earlier runs wrote stay where they are
   (the job deletes nothing); `ochakai delete` them once the folded entry
   is in.

Check 1 is the one worth dwelling on, because it is the check a platform
with an ontology manager runs before it saves an object type: **a primary
key must be unique and must not change between builds.** A catalog whose
primary key is its bundle path has the same failure mode, and nothing in
the knowledge base can notice it — every entry the bad run wrote is a
perfectly valid entry.

A failed verdict does not undo anything, deliberately. The entries that
were written stay written, because a bad run you can see beats a bad run
that cleaned up after itself.

ochakai stores this contract and executes none of it: running the
computation and checking the receipt belong to whoever runs the job
(SPEC §10.5). The attester never calls it either — no network, no LLM, no
read of the knowledge base, just the receipt and the values the run was
authorized with.

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
  behind — the run counts it `missing` and moves on. Removing knowledge is
  a human decision, and the entry may be the only remaining record of what
  the table meant.
