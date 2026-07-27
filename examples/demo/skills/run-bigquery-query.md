---
type: Skill
title: Run a BigQuery computation
description: How to execute an Attested Computation with runtime bigquery here, and what a run has to bring back
tags: [bigquery, executor, runbook]
generated: { by: human:sato@example.co.jp, at: 2026-07-17T07:40:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-21T01:40:00Z }
status: stable
---

The procedure every `runtime: bigquery` entry in this bundle names as its
`executor`. There are two of them:
[monthly revenue](/queries/sales/monthly-revenue.md) and the draft
[revenue by channel](/queries/sales/revenue-by-channel.md).

## Run it

1. Read the SQL out of the entry's `# Computation` fence. **Verbatim** — do not
   add a filter, a limit, or a column. A rewritten query is a different
   computation and the attester will say so.
2. Bind every entry in `parameters` as a query parameter of the declared type.
   `revenue-by-channel` needs `from_date` and `to_date`; `monthly-revenue` takes
   none and derives its own window.
3. Run it in the `demo-shop` project as the `analytics-reader` service account,
   which has BigQuery Job User there and read access to nothing but the
   `shop` dataset. Location `asia-northeast1`.
4. Return the receipt (below) with the result.

## The receipt

Both entries declare `receipt: [job_id, executed_sql, row_count]`, and all three
fields have to come back:

| Field | Where it comes from |
|---|---|
| `job_id` | the BigQuery job — the audit trail, and how a run is re-read later |
| `executed_sql` | the query as it was sent, not as it was meant |
| `row_count` | rows returned; a zero row count is a result, not a failure |

`executed_sql` is the one the attester actually reads. Sending the SQL and
reporting something else is the failure this whole arrangement exists to catch.

## What this is not

Running the computation and checking the receipt belong to whoever runs the
query. ochakai stores this procedure and the contract that points at it, and
executes neither — no SQL runs inside the knowledge base.
