---
type: Skill
title: Build the first catalog
description: The day-one procedure for a new knowledge base — choose a scope, project the tables in, verify what a person can vouch for, and only then attach computations
tags: [bigquery, catalog, onboarding, runbook]
generated: { by: human:na0fu3y, at: 2026-08-05T00:00:00Z }
status: draft
---

The order to do things in on the first day, and where to stop.

This exists because nobody is coming to do it for you. A platform that
sells an ontology sends a forward-deployed engineer who has done this
before and knows which step is load-bearing; ochakai does not have one, so
the procedure has to ship as a document you can run. That is the whole
difference between the two things — not the steps, which are nearly the
same steps.

## When to use

A knowledge base with nothing in it, or a new BigQuery project arriving in
one that already has knowledge. Run it once per project.

## Preconditions

- A reachable ochakai instance and `ochakai whoami` returning your identity.
- Read access to the datasets you intend to catalog.
- [sync the BigQuery catalog](/jobs/sync-bigquery-catalog.md) imported, so
  the contract lives in the base rather than in your shell history.

## Steps

### 1. Choose the scope, and make it small

Name the datasets your agents actually query. Not the warehouse — the job
has no "all datasets" flag on purpose. The review feeds this base is built
around assume a person can empty them, and a few thousand unreviewed
machine drafts is how a queue stops being read. Two datasets is a fine
first day; you can add the third next week and nothing about that is a
retreat.

### 2. Dry-run it and read one document end to end

```sh
ochakai get jobs/sync-bigquery-catalog --download .
python sync-bigquery-catalog.py \
  --project my-project --datasets shop \
  --ochakai-url "$OCHAKAI_URL" --dry-run
```

The download brings back two files beside each other — the computation and
the attester you will need in step 4 — because both are the job's, and a
contract you can read without the code it names is half a contract.

**That download is empty on an instance with no `OCHAKAI_GCS_BUCKET`**, and
nothing says so: files are not stored there at all, so the concepts arrived
without them. Take the two `.py` files from the bundle directory or the
release archive instead.

No credentials for ochakai are needed for the run itself. Read **one** of the printed
documents completely — the columns, the partition filter, the empty
`# Caveats` section. What you are checking is whether an agent handed this
document could write a correct query against that table. If it could not,
the fix is upstream in BigQuery's own table and column descriptions, and
doing it there fixes every consumer rather than this one.

### 3. Settle the address before the first real run

The entry id is `<prefix>/<project>/<dataset>/<table>`. This is the primary
key, and it is the one decision on this page that is expensive to revisit:
changing `--prefix` later does not rename anything, it writes a **second**
catalog beside the first and leaves the first to go stale where nobody is
looking. Pick the prefix now, write it down, and pass the same one every
run. The attester's first check exists for exactly this.

### 4. Run it, then attest the run

Run without `--dry-run`, keep the receipt, and hand it to the attester with
the scope **you authorized in step 3** — not with whatever the run reports:

```sh
export OCHAKAI_ID_TOKEN=$(gcloud auth print-identity-token)

python sync-bigquery-catalog.py --project my-project --datasets shop \
  --ochakai-url "$OCHAKAI_URL" > receipt.json

python catalog-sync.py --sync-identity you@example.com --project my-project \
  --datasets shop --prefix catalog/bigquery < receipt.json
```

The first line is what lets day one be run by hand: your own ADC cannot
mint the ID token a private Cloud Run service wants, so you mint one with
gcloud and the script uses it as-is. `--sync-identity` is then your own
email — the identity you authorized the run to use is the one you ran it
as. Progress goes to stderr, so the redirect captures the receipt and
nothing else. The attester exits 0 only if the run counted; keep the
receipt of a run that passed and pass it as `--previous` tomorrow, which
is what turns on the check that the catalog did not collapse overnight.

The job's own exit code is not that check. A run that wrote a whole second
catalog under the wrong prefix exits 0 and looks like a good night. What
the four checks are, and why the first one bites hardest, is in
[the job itself](/jobs/sync-bigquery-catalog.md).

**This step wants the deployment, not a local instance.** `sync_identity`
is the email claim of the ID token the run used, and an instance you can
reach without credentials makes it use none — the field comes back empty
and the attester refuses the receipt rather than attest a run that cannot
say who made it. That is the right answer, and it means the dev instance
you dry-ran against in step 2 cannot carry you past here.

Everything is now a draft written by a machine. Nothing here is knowledge
yet. And nothing here is yours in the ownership rule's eyes, even though
you ran it as yourself: the writes carry the sync's producer stamp, so
when the scheduled service account takes over tomorrow it recognizes your
day-one entries as the projection's and keeps them current, instead of
skipping a catalog that looks like a person wrote it.

### 5. Turn one table into knowledge

Pick the table your agents get wrong most often. The entry is a document,
so editing it is a round trip through one:

```sh
ochakai get catalog/bigquery/my-project/shop/orders > orders.md
$EDITOR orders.md          # fill in the empty `# Caveats` section
ochakai put catalog/bigquery/my-project/shop/orders -f orders.md
ochakai verify catalog/bigquery/my-project/shop/orders
```

Write what an agent has to know before it queries the table — the filter
that is always required, the column that lies, the timezone the timestamps
are in. Leave the rest of the document alone; the sync owns it and will
keep it current. Two things then happen: the sync stops overwriting the
entry, and its trust tier changes, so agents can tell it apart from the 83
projections beside it.

**Do one.** The point of day one is not a verified catalog, it is a base
that has one entry a person will vouch for and a procedure for making the
next. A catalog of 84 machine drafts and one verified table is in better
shape than 84 drafts, and it is in better shape than nothing, which is what
you get from a plan to verify all of them.

### 6. Only now, attach a computation

With the tables in place, write the first `Attested Computation` — the
query somebody keeps asking for — pointing its `sources` at the table
entries from step 4 and its `executor` at the skill that runs it. Then
`ochakai list --links-to catalog/bigquery/my-project/shop/orders` answers
"what do we know about this table", computations included.

The order matters and it is not ochakai's invention: a platform whose
ontology manager creates object types from datasets puts *Actions* after
properties and marks the step optional. Semantics first, because a
computation that cites a table nobody has described is a query with a
comment on it.

## Post-conditions

At the end of the day the base holds the tables an agent will meet, one of
them carries something only a person knew, the daily run is scheduled, and
a failed run is visible. Stop there. Step 6 repeats for as long as the base
is useful, and there is no step 7.

## What this is not

Not an ingestion pipeline. The job runs outside the server, under your
service account, as an ordinary REST client — ochakai holds no warehouse
credentials and gains no connectors by your running this.
