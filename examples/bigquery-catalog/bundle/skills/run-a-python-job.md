---
type: Skill
title: Run a Python computation as a scheduled job
description: How to execute an Attested Computation with runtime python here — the identity it runs as, the roles it needs, and what a run has to bring back
tags: [python, executor, cloud-run, runbook]
generated: { by: human:na0fu3y, at: 2026-07-27T09:00:00Z }
status: draft
---

The procedure the `runtime: python` entries in this bundle name as their
`executor`. There is one so far:
[sync the BigQuery catalog](/jobs/sync-bigquery-catalog.md).

## Run it

1. Take the file the entry's `computation` names — for the one above,
   `sync-bigquery-catalog.py`, which travels with it as an attachment —
   **verbatim**. Do not add a filter, a dataset, or a field. A rewritten
   script is a different computation.
2. Pass every entry in `parameters` as the matching `--flag`. `datasets`
   takes several values; `dry_run` prints the documents instead of writing
   them, which is how you check a change before it touches the base.
3. Run it as a service account, not as yourself. A person's own credentials
   cannot mint the ID token a private Cloud Run service wants, and — more
   to the point — provenance should say a job wrote these entries, because
   a job did.
4. Return the receipt (below).

```sh
pip install google-cloud-bigquery google-auth requests

python sync-bigquery-catalog.py \
  --project my-project \
  --datasets shop marketing \
  --ochakai-url https://ochakai-<hash>.run.app \
  --jobs-region asia-northeast1
```

A Cloud Run job on a Cloud Scheduler trigger is the obvious shape for the
daily run; the script exits non-zero if any table failed, so a bad night
shows red without anyone reading the log.

## What it needs, and what it must not have

Everything is IAM. There is no token, no password, and nothing to rotate —
which is the property to protect when you extend this.

| Grant | Why |
|---|---|
| `roles/bigquery.metadataViewer` on the datasets | list the tables and read their schemas |
| `roles/bigquery.jobUser` | only with `jobs_region` — the job history is a query |
| `roles/bigquery.resourceViewer` | only with `jobs_region` — without `bigquery.jobs.listAll` the query sees *only this account's own jobs*, and every count comes out wrong but plausible |
| `roles/run.invoker` on the ochakai service | to write |

Two things about the job history: the region has to match where the
datasets live (`INFORMATION_SCHEMA.JOBS` is regional), and each run is a
billed query — small, not free.

## The receipt

`receipt: [sync_identity, tables_seen, written, skipped, failed]`, printed
to stdout as one JSON object. Progress goes to stderr, so a scheduler can
capture the receipt on its own.

| Field | Where it comes from |
|---|---|
| `sync_identity` | the email claim of the ID token the run used — who the entries will say wrote them |
| `tables_seen` | tables and views considered, before any ownership check |
| `written` | entries created or updated |
| `skipped` | entries left alone because a person had verified or edited them |
| `failed` | tables that errored; the run's exit code is non-zero when this is not 0 |

`sync_identity` is the field worth checking hardest. The whole ownership
rule rests on the job recognizing its own past writes, so a run under an
unexpected account does not merely mislabel provenance — it stops seeing
the entries as its own and skips the entire catalog, quietly.

## What this is not

Running the computation and checking the receipt belong to whoever runs the
job. ochakai stores this procedure and the contract that points at it, and
executes neither. No Python runs inside the knowledge base.
