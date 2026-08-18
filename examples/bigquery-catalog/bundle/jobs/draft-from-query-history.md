---
type: Attested Computation
title: Draft golden queries from job history
description: Group the queries people keep running and write each one back as a draft for review
tags: [bigquery, query-history, drafting, python]
generated: { by: human:na0fu3y, at: 2026-08-10T09:00:00Z }
status: draft
runtime: python
parameters:
  - { name: prefix, type: string }
  - { name: min_runs, type: integer }
  - { name: min_users, type: integer }
  - { name: limit, type: integer }
computation: /jobs/draft-from-query-history/draft-from-query-history.py
executor:
  resource: /skills/run-a-python-job.md
  receipt: [jobs_read, distinct_queries, drafted]
---

The other half of the cold start. [The catalog
sync](sync-bigquery-catalog.md) answers *what tables exist*; this answers
*what people keep asking of them* — and it answers it from the warehouse's
own record rather than from anybody's memory.

It reads `INFORMATION_SCHEMA.JOBS` rows as JSON on stdin and writes an OKF
bundle of drafts on stdout, one per query somebody keeps running:

```sh
bq query --max_rows=100000 --format=json --nouse_legacy_sql \
  'SELECT query, user_email, creation_time, referenced_tables
     FROM `region-us`.INFORMATION_SCHEMA.JOBS
    WHERE creation_time >= TIMESTAMP_SUB(CURRENT_TIMESTAMP(), INTERVAL 90 DAY)
      AND job_type = "QUERY" AND state = "DONE" AND error_result IS NULL' \
  | python3 draft-from-query-history/draft-from-query-history.py \
  | ochakai import -
```

# It connects to nothing

You run the query, with your own client and your own identity, and pipe
the answer in. That is the same shape `ochakai seed` takes and it is the
same reason: ochakai holds no warehouse credential, and neither does this
([0081](../../../../docs/design/0081-what-ochakai-is-and-what-it-refuses-to-hold.md) §1).

It also makes the permission yours to have or not. Seeing anybody else's
jobs needs `bigquery.jobs.listAll`; without it the history is your own
queries, and the counts still look plausible — which is worse than no
counts. Read the first line of the summary before the drafts.

Your client's row limit does the same thing more quietly. `bq query`
prints the first 100 rows unless `--max_rows` says otherwise and never
mentions the ones it dropped, so 90 days of history arrives as the last
100 jobs and almost nothing reaches `min_runs`. `jobs_read` in the
receipt is the number to disbelieve when it is exactly 100.

# There is no LLM in it

Grouping is a fingerprint: comments and literals stripped, whitespace
collapsed, space around punctuation dropped. Two runs of the same question
against different months land together, and nothing is invented.

**What a query means is the one thing this cannot produce.** So every
draft carries an empty heading:

```markdown
## The question this answers

<!-- Nothing here yet, and this script cannot fill it: the history says
what was run, never why. -->
```

That is the seam. Fill it yourself, or hand the drafts to your own agent
— which is where an LLM belongs, on your side of the wire, with your
knowledge base as the thing it writes *back* to. The server stays
deterministic either way.

# What lands

One `Attested Computation` per surviving query at
`queries/from-history/<table>-<hash>`, `status: draft`, carrying the SQL,
the tables it touched, how often it ran, by how many accounts, and over
what window. The id is a placeholder built to be unique and readable —
whoever promotes the draft renames it to whatever the question turns out
to be about.

Nothing declares its own `generated:`. A document that does imports as a
*claim*, kept under `received` and answering to no trust tier
([0075](../../../../docs/design/0075-the-bundle-is-the-address-space.md) §3.1),
and it would say that on every concept. The instance's own record — it
observed this import, as you — is truer and quieter; what made them is in
the tag and the section above.

# What it deliberately drops

- **Anything that is not a `SELECT`.** DDL, DML and scripts are not
  questions. Judged on the fingerprint, so a query opening with a comment
  is still a query.
- **Queries run fewer than `--min-runs` times** (5 by default). A query
  run once is a question somebody had, not one the team keeps having.
- **Everything past `--limit`** (50, most-run first). A review queue
  nobody can finish is one nobody starts — and the counts say which end
  of the list to raise the limit for.

Every one of those is a threshold, which means every one of them is
wrong for somebody. They are flags for that reason.
