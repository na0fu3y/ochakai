---
type: Attested Computation
title: Confirm the BigQuery catalog
description: Re-confirm, as machine-confirmed, the published catalog entries that still say what BigQuery says
tags: [bigquery, catalog, confirmation, python]
generated: { by: human:na0fu3y, at: 2026-10-24T00:00:00Z }
status: draft
runtime: python
parameters:
  - { name: project, type: string, required: true }
  - { name: datasets, type: array, required: true }
  - { name: ochakai_url, type: string, required: true }
  - { name: prefix, type: string }
  - { name: confirm, type: boolean, required: true }
  - { name: dry_run, type: boolean }
computation: /jobs/sync-bigquery-catalog/sync-bigquery-catalog.py
executor:
  resource: /skills/run-a-python-job.md
  receipt: [confirm_identity, project, datasets, prefix, tables_seen, confirmed, standing, drafts, edited, own, drifted, raced, skipped, missing, failed]
---

The confirmation job for [the catalog sync](/jobs/sync-bigquery-catalog.md).
It is the same file run with `--confirm`
([the script](sync-bigquery-catalog/sync-bigquery-catalog.py)): it reads
BigQuery and projects every entry again exactly as the sync would, writes
nothing, and verifies the entries whose stamped shape still matches. Its
verifications are recorded under its own `process:` identity and read as
`machine-confirmed` — never as `human-reviewed` (decision 0160).

When the sync follows a table a person had verified, that verification
lapses and the entry waits in the edited queue. Most of those changes are
a column added at the end of a table; a person who confirmed the entry
once does not need to look at it again for that. This job takes those
off the queue and leaves everything else to a person.

# What a person delegates

Running this job is a delegation, and it is only as wide as the person
who sets it up makes it:

1. **Its own identity.** A service account of its own — not the sync's.
   A writer confirming its own write says nothing, so the job also
   refuses, entry by entry, anything whose last write is under its own
   name, and it will not run without knowing its name.
2. **Its scope.** Where the deployment has an access policy, grant the
   service account write over the catalog's prefix and nothing else
   (`ochakai access`). A verification takes the same grant an edit does,
   so that row is the edge of what it can confirm. Where there is no
   policy, the edge is the `--prefix` and `--datasets` it is run with.

Taking the delegation back is removing that row, or the job.

# What it confirms, and what it leaves

An entry is verified only when all of these hold:

| Check | Otherwise | Receipt |
|---|---|---|
| its standing verification has lapsed | nothing to do | `standing` |
| it is published, not a draft — verifying a draft publishes it (decision 0157), and publishing is a person's call | left for a person | `drafts` |
| its text is still the projection's (the stamp's `text` digest) | a person's words; a person confirms them | `edited` |
| its last write is not under this job's identity | refused | `own` |
| the shape projected from BigQuery now matches the stamp's `shape` | left for the next sync to correct | `drifted` |

A ruling takes no precondition, so after verifying the job reads the
entry's ETag again. If it moved, a write landed in between and the
verification stands on content the job never checked: that is counted as
`raced`, printed, and turns the exit code non-zero for a person to look
at. Run the job after the sync rather than beside it and this does not
happen.

What it deliberately does not do:

- **Confirm prose.** Anything a person wrote — a caveat, a description
  in their words — is checked by a person. A job that checks claims
  against the warehouse with a model is a different delegation, and a
  separate job.
- **Confirm the first time.** It never publishes a draft, so every entry
  it confirms is one a person published.
- **Write.** Not a document, not a draft, not a note. Its only effect is
  ledger rows.

# Running it

The same grants as the sync on the BigQuery side (`metadataViewer`; no
job history — a confirmation compares shapes, which leave the query
counts out), `roles/run.invoker` on the ochakai service, and the access
row above. Schedule it after the sync:

```sh
python sync-bigquery-catalog.py --confirm \
  --project my-project --datasets shop \
  --ochakai-url https://ochakai-<hash>.run.app
```

The receipt names `confirm_identity` instead of `sync_identity`, and
counts one outcome per entry under the fields above.
