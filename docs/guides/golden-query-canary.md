# Running golden queries as canaries

A verified golden query breaks quietly: the warehouse renames a column,
a table is partitioned differently, the data underneath changes shape.
Nothing in the knowledge base notices. This guide describes running
verified queries on a schedule as **evaluation canaries** and writing the
result back — the practice OpenAI's in-house data agent runs continuously
against its golden queries, and the execution-accuracy metric text-to-SQL
evaluation uses.

**Golden queries are stored as `Attested Computation` entries.** ochakai
has no type of its own for them (design doc
[0038](../design/0038-type-vocabulary-realignment.md)): OKF SPEC §10 already
defines the concept, so a golden query is an Attested Computation whose
`runtime` says where the SQL runs, whose `# Computation` body fence holds
the SQL, and whose `attrs.question` carries the question it answers. If
your base predates this, entries typed `Golden Query` keep working as free
types — substitute that spelling in the `--type` filters below.

**ochakai does not execute SQL.** The canary runs on your side — a CI job
or a scheduled agent — and the warehouse credentials stay there. ochakai
supplies the material and records what you found.

## The cycle

### 1. List — least recently verified first

```sh
ochakai search --sort verified_at --type 'Attested Computation' --verified true --limit 100 --json \
  | jq -r '.hits[] | [.id, .verified_at, .attrs.question] | @tsv'
```

Straight REST is
`GET /api/v1/knowledge?type=Attested%20Computation&verified=true&sort=verified_at&limit=100`;
over MCP, `search_knowledge` with `sort: "verified_at"` returns the same
feed. `sort=verified_at` orders by verification time, oldest first, with
unverified entries last. "Verified queries not re-checked in 90 days" is
where a canary run starts. Checking out an OKF export
(`ochakai export` / `GET /api/v1/export`) in CI works just as well.

### 2. Run — with your own credentials

Execute each entry's `# Computation` fence against the warehouse
(`bq query` for BigQuery), reading `runtime` to know which warehouse that
is. The fence is the computation — SPEC §10.2 puts it there rather than in
`attrs`, so there is exactly one copy for an attester to canonicalize a
run against. ochakai takes no part in this step.

### 3. Judge

| Result | Verdict |
|---|---|
| Execution error (table or column gone) | **Failure** — a schema change broke the query |
| Row count or a headline aggregate moved more than your threshold | **Warning** — the data changed, or the definition drifted |
| Normal | Record it as a re-verification (→ 4) |

### 4. Write back — by recording, not by authorizing

- **It ran clean**: record the re-verification —
  `ochakai verify queries/<id>` (REST:
  `POST /api/v1/verify/queries/<id>`). The caller becomes `verified_by`
  and the current time `verified_at`, so the entry moves to the back of
  the next `sort=verified_at` feed. An `update` cannot say this: an
  update that changes nothing writes nothing, and `verified_at` carries
  over from the stored value, so "I checked it again and it is still
  right" lands nowhere. That is what verify exists for (design doc
  0025 §6). MCP has no such tool — verification is a human judgment, and
  a canary running in CI is recorded under CI's own identity.
- **Failure or warning**: create a draft `Insight` (`kind: caveat`)
  against the affected entry, or propose `status: deprecated` with a
  `status_note` giving the reason. A human decides, looking at the
  provenance. Once the entry is established to be wrong, it becomes
  `ochakai reject <id> --note "…"`. Rulings are made from
  the human surfaces (web UI / CLI): if an agent is running your canary,
  overwriting a verified entry and changing its status are both refused
  over MCP (design doc 0015 §3.1), so the agent's exits are the
  `report_outcome failed` below and a draft under a different id.
- **Either way, record the outcome**: `ochakai report queries/<id> worked`
  / `ochakai report queries/<id> failed --note "what happened"` (REST:
  `POST /api/v1/usage/queries/<id>`, MCP: `report_outcome`). The worked
  and failed totals show up under `/usage`, so verified entries that have
  accumulated failures can be picked up for re-verification first. **The
  `sort=failed` re-verification feed** (`ochakai search --sort failed
  --verified true`, REST: `GET /api/v1/knowledge?sort=failed`, MCP:
  `search_knowledge` with `sort: "failed"`) lists them in order of how
  often they were reported wrong. It is the evidence-based entrance,
  complementing the time-based `sort=verified_at` (design doc 0025).

## A CI snippet (GitHub Actions + BigQuery)

```yaml
name: golden-query-canary
on:
  schedule:
    - cron: "0 21 * * 1" # Mondays, 06:00 JST
jobs:
  canary:
    runs-on: ubuntu-latest
    permissions:
      id-token: write # Workload Identity Federation (keyless)
    steps:
      - uses: google-github-actions/auth@v3
        with:
          workload_identity_provider: ${{ vars.WIF_PROVIDER }}
          service_account: ${{ vars.CANARY_SA }}
      - name: run canaries
        run: |
          TOKEN=$(gcloud auth print-identity-token --audiences="$OCHAKAI_URL")
          curl -s -H "Authorization: Bearer $TOKEN" \
            "$OCHAKAI_URL/api/v1/knowledge?type=Attested%20Computation&verified=true&sort=verified_at&limit=50" \
          | jq -c '.hits[]' | while read -r hit; do
              id=$(jq -r .id <<<"$hit")
              sql=$(jq -r '.body | split("```sql\n")[1] | split("\n```")[0]' <<<"$hit")
              if ! bq query --nouse_legacy_sql --dry_run "$sql" >/dev/null 2>&1; then
                echo "::error::computation $id no longer compiles against the warehouse"
                curl -s -X POST -H "Authorization: Bearer $TOKEN" \
                  -H 'Content-Type: application/json' \
                  -d '{"outcome":"failed","note":"canary: dry-run failed"}' \
                  "$OCHAKAI_URL/api/v1/usage/$id" >/dev/null
              else
                # record the re-verification, moving verified_at to now
                # (which takes the entry out of both feeds)
                curl -s -X POST -H "Authorization: Bearer $TOKEN" \
                  "$OCHAKAI_URL/api/v1/verify/$id" >/dev/null
              fi
            done
```

Whether a clean run should call `verify` depends on what the canary
actually checked. A `--dry_run` like the one above establishes only that
the query still compiles; moving `verified_at` forward is honest when you
ran the query for real and compared the results. If dry-run is all you
do, drop the verify and write back only the failures.

`--dry_run` catches breakage from schema changes — the "failure" row of
step 3 — at zero cost. To see result drift as well, run the query and
compare against the previous run; saving the row count and a few headline
aggregates as an artifact and diffing those is the easy version.

## A supporting signal: usage telemetry

`ochakai usage queries/<id>` (REST: `GET /api/v1/usage/queries/<id>`,
MCP: `get_knowledge_usage`) returns how often that query was actually
returned by a search or fetched, the worked and failed report counts, and
when it was last used. **A verified entry nobody has used in a long time**
is a reason to lower its canary priority — or to consider deprecating it.
**A verified entry that has collected failures** is the reverse: a reason
to re-verify it first.
