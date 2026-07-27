# BigQuery catalog sync

A daily job that projects BigQuery table metadata into ochakai as
`BigQuery Table` entries, so an agent asking about `shop.orders` finds the
columns, the partition filter it must not forget, and whatever a human has
since written underneath.

**This is an example, not a feature.** ochakai has no connectors and is not
getting any: a harvester living in the server would need warehouse
credentials it deliberately does not hold, and knowledge here is curated
rather than collected (README's refusal table, design doc
[0001](../../docs/design/0001-architecture.md) §2). None of that stops
*you* from filling a catalog — this one runs outside, as an ordinary client
of the same REST API the CLI uses, under your own service account.

## It ships as knowledge, not as a script in a folder

`bundle/` is a two-entry OKF bundle. Import it and the job documents
itself in the knowledge base it writes to:

```sh
ochakai import examples/bigquery-catalog/bundle
```

| Entry | Type | |
|---|---|---|
| [`jobs/sync-bigquery-catalog`](bundle/jobs/sync-bigquery-catalog.md) | Attested Computation | `runtime: python`, the parameters, the receipt, and every decision the projection makes |
| [`skills/run-a-python-job`](bundle/skills/run-a-python-job.md) | Skill | what the `executor` points at: the identity to run as, the IAM roles, the receipt fields |

[`sync-bigquery-catalog.py`](bundle/jobs/sync-bigquery-catalog.py) is the
computation itself. It is named by the entry's `computation` key as a file
rather than pasted into a `# Computation` fence — the form
[OKF SPEC](https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md)
§10.2 gives for a computation too long to inline — and it travels with the
entry as an attachment, so `ochakai get jobs/sync-bigquery-catalog
--download .` brings back the code with the contract.

That shape is the point as much as the sync is. An agent told to refresh
the catalog gets a contract that says *supply these parameters and run this
file*, and SPEC §10.3 says in as many words that it must not author the
computation instead. A knowledge base whose own maintenance job is an entry
inside it is one fewer thing living in a README nobody reads.

The two entries import as **drafts**, on purpose. Verify them once you have
run the job in your own project and the receipt came back clean — which is
also the moment you would want to correct the IAM roles for how you
actually granted them.

## Try it without touching anything

```sh
pip install google-cloud-bigquery google-auth requests

python bundle/jobs/sync-bigquery-catalog.py \
  --project my-project --datasets shop \
  --ochakai-url https://ochakai-<hash>.run.app --dry-run
```

`--dry-run` prints the entries it would write and needs no credentials for
ochakai at all. Everything else — the daily schedule, the IAM grants, the
ownership rule that keeps the job from flattening what a person wrote, and
what is deliberately left out (Looker, a recommended-table flag, deletes) —
is in the two entries.
