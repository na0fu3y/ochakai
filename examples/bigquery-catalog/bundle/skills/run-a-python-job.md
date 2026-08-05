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
3. Run the *scheduled* job as a service account: provenance should say a
   job wrote these entries, because a job did. Running it by hand — day
   one, a backfill, the dataset you need refreshed this morning — is done
   as yourself. A person's own ADC cannot mint the ID token a private
   Cloud Run service wants, so bring one in:

   ```sh
   export OCHAKAI_ID_TOKEN=$(gcloud auth print-identity-token)
   ```

   Provenance then names you, which is true; the `Ochakai-Producer` stamp
   on every write is what lets the next run — anyone's — recognize your
   run's entries as the projection's rather than skip them as a person's.
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
shows red without anyone reading the log. A table that vanished between
the listing and the read is `missing`, not `failed`, and does not turn
the night red — deletion is a warehouse's ordinary day, and a schedule
that cries wolf over it stops being believed.

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

Printed to stdout as one JSON object. Progress goes to stderr, so a
scheduler can capture the receipt on its own. Return **exactly** the fields
the entry's `executor.receipt` declares — the attester refuses a receipt
that is missing one rather than guessing what it meant.

| Field | Where it comes from |
|---|---|
| `sync_identity` | the email claim of the ID token the run used — who the entries will say wrote them. Empty when there was no token to mint or bring, and the attester refuses a receipt that cannot name its run |
| `project` | the `--project` the run used |
| `datasets` | the `--datasets` it was given, sorted |
| `prefix` | the `--prefix` the ids were built under |
| `tables_seen` | tables and views considered, before any ownership check |
| `written` | entries created or updated, dataset entries included |
| `unchanged` | entries whose bytes already matched, so no revision was made |
| `skipped` | entries left alone because a person had verified, edited or rejected them |
| `missing` | tables dropped between the listing and the read — the world changed, the run did not break, and the exit code stays 0 |
| `failed` | tables that errored; the run's exit code is non-zero when this is not 0 |

The four scope fields are there because the entry id is derived from them.
Ownership itself no longer hangs on `sync_identity` — the producer stamp
is what a run recognizes its predecessors by, whoever ran them — but the
receipt still has to name who ran, because the attester's provenance
check is about whether the run was the one sanctioned, and entries from
before the stamp existed are still recognized by account.

## Post-conditions

Hand the receipt to the entry's `attester.resource` together with the
values the run was authorized with. Do not report the catalog as refreshed
until the attester returns `ok: true`; a verdict that fails names which
check it was, and the run's own exit code cannot tell you — a run that
wrote a whole second catalog under the wrong prefix exits 0.

## What this is not

Running the computation and checking the receipt belong to whoever runs the
job. ochakai stores this procedure and the contract that points at it, and
executes neither. No Python runs inside the knowledge base.
