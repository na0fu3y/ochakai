# Operating a deployment

Backup and restore, hardening beyond the default, the team web UI,
monitoring, capacity, supply chain, and upgrades — the Cloud Run guide's
[§1–§5](../../deploy/cloudrun/README.md) build the deployment and cover
the Google-side troubleshooting; this page is about running the thing
afterwards.

Where a number here is not measured, it says so. That is deliberate:
ochakai is a small project, and an invented capacity figure would be
worth less than an admission.

## Backup and restore

**There are two backups, and they recover different things.** Neither is
a superset of the other, which is the one fact to take away.

| | Cloud SQL backup | `ochakai export` |
|---|---|---|
| The knowledge | yes | yes |
| Provenance — who wrote it, who verified it, when | yes | **no** |
| Revision history | yes | **no** |
| Usage counts and outcome reports | yes | **no** |
| Attachment bytes | no — they are in GCS | yes, as files |
| Readable without ochakai | no | yes: markdown and YAML |
| Restores to | the same database | any instance, any version, any OKF consumer |

A Cloud SQL backup is the operational one: it restores the deployment as
it was. An OKF export is the portability one: it is the copy that
survives ochakai, and the reason the README can promise that the
knowledge is never a hostage.

### Cloud SQL backups

The example instance ships with `--no-backup` to stay at ~$10/month, so
enabling backups is a deliberate step, not a default — see
[Hardening](#hardening) below for the command.

Restoring is Cloud SQL's own procedure, and it restores the database
only. **File bytes are not in it**: they live in GCS as
content-addressed objects, so an instance restored from a database backup
finds file metadata whose bytes are whatever the bucket holds
today. Turn on object versioning, or a retention policy, if that matters
to you — and note that `purge` deliberately does not reclaim GCS blobs
(design doc 0031), so the bucket only grows.

### Exporting

```sh
ochakai export ./knowledge          # a directory
ochakai export - > okf.tar.gz       # or a stream
ochakai export --no-files -   # concepts only; bytes are already in GCS
```

The bundle is an OKF v0.2 directory: one markdown file per concept with
YAML frontmatter, files beside their concept, and an
`index.md` per directory. Exporting the ten-concept demo base writes 20
files — ten concepts and ten indexes.

This is the backup to run on a schedule if you want one that a human can
read, diff and commit. It is also the migration path between instances,
and the only way to move knowledge into a project that has never heard of
ochakai.

### Restoring from a bundle

```sh
ochakai import ./knowledge
```

Import is a client-side loop over ordinary endpoints — there is no
server-side bulk import, by decision (design doc 0015 §3.2) — so a
restore needs the CLI, or a client that reproduces the loop. Plan for
that: a disaster-recovery runbook that assumes a web console will not
work here.

**What a bundle restore does not bring back.** Measured, on a fresh
database, by exporting a ten-concept base and importing it into an empty
instance:

- **Timestamps become the restore.** `created_at`, `updated_at` and
  `verified_at` on the restored concept are all the moment of the import,
  not the moment the work happened.
- **History collapses.** A concept with two revisions came back with one.
  Revisions are the audit trail of what this instance did, and a bundle
  is not that trail.
- **Provenance is re-recorded as the importer.** A bundle that *claims*
  someone else wrote and verified a concept does not get believed: editing
  the exported frontmatter to say `generated.by: human:tanaka@…` and
  `verified.by: human:sato@…`, then importing, stored the body change and
  kept both actors as the importing identity. Provenance is what an
  instance observed, never what a document asserts (design doc 0009).
- **Usage counts and outcome reports do not travel** at all.

What *does* survive is the knowledge and its trust: the verifications
came back verified, through the OKF `verified` key rather than an ochakai
alias, along with every envelope field.

So: **run the import under an identity you are willing to see on every
concept**, and if provenance matters to you, keep a database backup as
well. Neither of these is a bug — a bundle carries knowledge, and
provenance belongs to the instance that watched it happen — but a runbook
that expects a full-fidelity restore from an export will be wrong at the
worst moment.

## Monitoring

### `/health` is a liveness check, and nothing more

`GET /health` returns `ok` unconditionally
([cmd/ochakai/main.go](../../cmd/ochakai/main.go)). It does not touch the
database. An uptime check on it answers "is the process serving?", not
"is the deployment working" — a server whose database has gone away
answers `ok` right up until a request needs a row.

It is `/health` and not `/healthz` because Google Frontends intercept
`/healthz` on `run.app` URLs and return their own 404.

A stricter check, if you want one, is any read that touches the store:
`GET /api/v1/search?sort=verified_at&limit=1` succeeds only when the
database answers. `ochakai whoami` does the same thing from a shell and
also prints which identity the server sees.

### Logs

Everything is JSON through `slog`, so Cloud Logging parses it without
help. The messages worth building an alert or a saved query on, all of
them warnings that describe **degraded but running**:

| Message | What it means |
|---|---|
| `usage flush failed` | a batch of usage events was lost. Best-effort by design (0029); a *stream* of these means the database is unhappy |
| `usage recording failed` | the in-memory buffer was full — 20,000 events — and events were dropped |
| `search miss recording failed` | same buffer, same bargain (0051): a question that found no answer was not kept. The knowledge is unaffected; `ochakai stats` undercounts |
| `query embedding failed; falling back to lexical-only` | Vertex AI did not answer a search; results are still returned, ranked by the lexical half alone |
| `document embedding failed` / `storing embedding failed` | a concept was written but is not in the vector index. It stays findable lexically; `ochakai reembed` repairs it |
| `attachment embedding failed` | same, for an attachment: still findable by filename |
| `backlink lookup failed` | one concept's backlinks are missing from a response |
| `export truncated after the response began` | an export failed midway. **The bytes the client received are not a complete backup** — this is the one in the list that can quietly cost you |
| `knowledge verified` / `knowledge purged` | not errors: the audit line for a curation event, with the actor |

At startup the server states its own posture, which is the fastest way to
confirm what a running instance actually has enabled:

```
"ochakai listening" addr=:8080 version=… insecure_dev=false endpoints=[/mcp /api/v1 /health]
"semantic search enabled" model=gemini-embedding-001 dim=768 project=… discovered=true
"files disabled (no OCHAKAI_GCS_BUCKET); markdown entries only"
```

`discovered=true` means the project came from the metadata server rather
than `OCHAKAI_VERTEX_PROJECT` — semantic search is the default on Google
Cloud (design doc [0053](../design/0053-embeddings-by-default.md)). The
line that replaces it says which way it went instead:

| Line | What it means |
|---|---|
| `semantic search off: Vertex AI did not answer for this deployment` | the startup probe was refused, almost always a missing `roles/aiplatform.user`. Search is lexical-only; grant the role and restart |
| `semantic search is off: this database cannot hold vectors` | pgvector is not there and this role may not create it. Create the extension as the admin user (deploy guide §3) |
| `semantic search off by configuration (OCHAKAI_EMBEDDINGS=off)` | asked for — the one line here that is not worth investigating |
| `semantic search disabled; using lexical search only` | no project was configured and none was discovered — ochakai is not running on Google Cloud |

If `insecure_dev=true` appears anywhere but a laptop, stop and fix that
first: every caller is `human:anonymous` and nothing is authenticated.

### What is worth alerting on

Nothing here is prescriptive — this is a knowledge base, not a payment
system — but in rough order of what actually costs you something:

1. **Cloud Run 5xx rate**, which is where a broken deployment shows up
   first.
2. **`export truncated`**, because a truncated backup looks like a
   backup.
3. **Cloud SQL disk usage**, since the example instance is 10 GB with
   `--no-storage-auto-increase`: it will stop rather than grow.
4. **A sustained stream of `usage flush failed`**, as a database-health
   signal rather than for the statistics themselves.

### Knowing there is reviewing to do

The list above is about the deployment. The other thing worth watching is
the knowledge, and it fails silently: a review queue nobody opens looks
exactly like a review queue with nothing in it.

`ochakai stats` is the whole answer (design doc
[0049](../design/0049-queue-counts.md)). Among its lines are the three
queues a curator empties, each carrying the command that lists it:

```console
$ ochakai stats
…
drafts	12	ochakai list usage --status draft
failed	1	ochakai list failed
stale_after	0	ochakai list stale_after
…
```

Each queue is named after the `sort` that lists it, so the count and the
way to see what it counts are one word (design doc
[0059](../design/0059-a-queue-is-named-by-its-listing.md)). `drafts` is
the exception, because no single sort lists it.

`--exit-code` turns that into something a scheduler watches: **2 while
any of the three is non-empty, 0 when all are** — and 1 stays what it
always was, an error, so an unreachable server cannot be read as "nothing
to do". `--prefix teams/growth` scopes it, which is how one team on a
shared deployment asks about its own queue.

That is enough for a weekly nudge through whatever channel your team
already watches for red builds — no address list, no secret, nothing new
to run:

```yaml
name: ochakai-review-queue
on:
  schedule:
    - cron: "0 0 * * 1" # Mondays, 09:00 JST
jobs:
  queues:
    runs-on: ubuntu-latest
    permissions:
      id-token: write # Workload Identity Federation (keyless)
    steps:
      - uses: google-github-actions/auth@v3
        with:
          workload_identity_provider: ${{ vars.WIF_PROVIDER }}
          service_account: ${{ vars.REVIEWER_SA }}
      - run: |
          TOKEN=$(gcloud auth print-identity-token --audiences="$OCHAKAI_URL")
          waiting=$(curl -sf -H "Authorization: Bearer $TOKEN" \
            "$OCHAKAI_URL/api/v1/stats" | jq '.queues | add')
          echo "review queue: $waiting waiting"
          [ "$waiting" -eq 0 ] || { echo "::error::$waiting waiting for review"; exit 1; }
```

ochakai delivers nothing itself — no mail, no chat, no webhooks (design
doc 0049 §4). The job above is the delivery, and it is yours.

The same command answers the question one step further out — not "is
anything waiting" but "is this working": how much of the base is
confirmed, how much moved through review this month, and what people
searched for and did not find (design doc
[0051](../design/0051-instance-metrics-and-search-misses.md)). One number
per line, so the same cron that watches the queues can keep a history of
it with `>>` and a date:

```console
$ ochakai stats --days 7
entries	128
draft	31
stable	92
…
misses	11
gap	5	解約率の定義
gap	3	arr by segment
```

The `gap` lines are the questions that came back empty, most-asked first
— the one list that says what to write next. A deployment that keeps none
prints `misses -` rather than `misses 0`, because those are not the same
answer (`OCHAKAI_RECORD_MISSES`, and a public deployment keeps none at
all).

## Capacity

### What is known

- **Search**: about 16 ms per Japanese search across 5000 concepts, against
  0.2 ms for a latin word. Japanese terms are answered by scanning,
  because a trigram index cannot serve a two-character pattern.
- **Attachments**: 5 MiB per file, 20 files per concept, enforced by the
  server.
- **Usage buffer**: 20,000 events in memory, flushed every 5 seconds.
  Past the cap, events are dropped rather than queued (design doc 0029).
- **Raw usage events** are pruned after 180 days; the per-concept totals
  are not.
- **Shutdown**: in-flight requests get 5 seconds after SIGTERM, then the
  final usage flush runs. Cloud Run allows 10 seconds before SIGKILL.

### Raising `--max-instances`

The deploy guide sets `--max-instances=1`, and that is a cost decision
rather than a correctness one: nothing in the server is single-instance.
The usage buffer is per-instance and best-effort either way, and the
database is the only shared state.

The ceiling to watch when raising it is **database connections**. Each
instance opens its own pool, and `pgxpool`'s default is four connections
(or one per CPU, whichever is larger), so *N* instances is roughly *4N*
connections. `db-f1-micro` is a shared-core instance with a small
connection limit; if you plan to run several instances, check
`SHOW max_connections` before assuming it fits, and set `pool_max_conns`
in `OCHAKAI_DATABASE_URL` if you need to bound it.

### What has not been measured

Honestly: most of it. There is no measured concept-count ceiling, no
throughput figure, no latency distribution under concurrency, and no
guidance on when `db-f1-micro` stops being enough. The project is one
person's, and these numbers would be invented rather than observed.

If you are running ochakai at a scale where this matters, the useful
thing is to say so in
[Discussions](https://github.com/na0fu3y/ochakai/discussions) with what
you actually see. That is how this section gets numbers.

## Supply chain

Images are published to `ghcr.io/na0fu3y/ochakai` by GitHub Actions with
SBOM and SLSA provenance; workflow actions are pinned by commit SHA. The
binary is a static, trimmed-path Go build on `distroless/static`. Verify
what you are about to deploy:

```sh
gh attestation verify oci://ghcr.io/na0fu3y/ochakai:<tag> -R na0fu3y/ochakai
```

Release archives for the CLI carry the same attestations, plus a
`checksums.txt`. Binary Authorization can enforce the check at deploy time
— see [Hardening](#hardening) below.

## Hardening

The default deploy guide walkthrough
([§1–§5](../../deploy/cloudrun/README.md)) is already secret-zero and
least-privilege — Cloud Run IAM, Google identities, explicit grants only,
an empty authorized-networks list, provenance-attested images. The steps
below raise the bar further; pick what matches your risk profile.

### Private IP only

For production-like deployments, drop the Cloud SQL public IP entirely.
Costs nothing extra (VPC, private services access, and Cloud Run's Direct
VPC egress are free); the trade-off is that local admin access (the
deploy guide's §3 SQL, this section's password-retirement work below —
§5's import is unaffected, it goes over the API) needs a temporary public
IP or a VPC-attached workstation.

One-time per VPC — allocate a peering range and connect it (if the
Compute API was just enabled, the `addresses create` may need a minute
before it stops returning `SERVICE_DISABLED`):

```sh
gcloud services enable servicenetworking.googleapis.com compute.googleapis.com
gcloud compute addresses create google-managed-services-default \
  --global --purpose=VPC_PEERING --prefix-length=16 --network=default
gcloud services vpc-peerings connect --service=servicenetworking.googleapis.com \
  --ranges=google-managed-services-default --network=default
```

Then create the instance with `--network=default --no-assign-ip`
(instead of the deploy guide's §2 defaults) — or convert an existing
one, which is the same patch and takes 15–20 minutes:

```sh
gcloud sql instances patch ochakai --network=default --no-assign-ip
```

Add Direct VPC egress to the Cloud Run service so it can reach the
private IP (the `/cloudsql` connector socket follows automatically; the
`OCHAKAI_DATABASE_URL` doesn't change):

```sh
gcloud run services update ochakai --region=$REGION \
  --network=default --subnet=default
```

Local admin access no longer routes from outside the VPC. Temporarily
attach a public IP and detach it after:

```sh
gcloud sql instances patch ochakai --assign-ip       # + cloud-sql-proxy, do the work
gcloud sql instances patch ochakai --no-assign-ip    # afterwards
```

(If the `sql.restrictPublicIp` org policy below is enforced, the
temporary `--assign-ip` is blocked too — lift the policy for the window
or use a VPC-attached workstation.)

### Guardrails against misconfiguration

Org-policy; needs the Org Policy Administrator role. These make the risky
states unrepresentable rather than merely avoided:

```sh
# forbid adding ANY authorized network on Cloud SQL (not just broad
# ranges — the empty-list posture becomes unrepresentable to leave)
gcloud resource-manager org-policies enable-enforce \
  sql.restrictAuthorizedNetworks --project=$PROJECT_ID
# forbid public IPs on Cloud SQL — pair with private IP only, above.
# Existing public IPs are grandfathered: the instance keeps running and
# unrelated patches still work; only changes to the IP configuration are
# blocked
gcloud resource-manager org-policies enable-enforce \
  sql.restrictPublicIp --project=$PROJECT_ID
```

Enforcement takes a minute or two to propagate — a violating patch
issued immediately after can still succeed. Verify the guardrail is
live by trying one: `gcloud sql instances patch ochakai
--authorized-networks=203.0.113.1/32` should fail with an
`Organization Policy check failure` (and if it succeeded, you were too
fast — `--clear-authorized-networks` and try again).

Keep Domain Restricted Sharing (`iam.allowedPolicyMemberDomains`) on at
the org level; nothing in a working deployment needs `allUsers` — every
service, [the team web UI](#the-team-web-ui) included, stays
`--no-allow-unauthenticated`. The deploy guide's §5d public demo is the
one thing this policy blocks, and it is a fair trade: exempt a dedicated
demo project if you want one, and leave the org policy alone.

**Enforce TLS on direct connections** (Cloud SQL connector traffic is
always mTLS; this covers the authorized-network path):

```sh
gcloud sql instances patch ochakai --ssl-mode=ENCRYPTED_ONLY
```

**Retire the last password.** The admin user's password is the only
stored secret in the whole system, and it too can go: create a personal
IAM login for maintenance, transfer the admin user's objects to it, and
delete the password user. Future maintenance goes through
`cloud-sql-proxy --auto-iam-authn` with your own identity.

Two Postgres facts shape the sequence. Tables created by startup
migrations are owned by the **runtime service account**, so the admin
cannot `GRANT` on them — you inherit them via role membership instead.
And a role that still owns objects or appears in grants cannot be
dropped, so the admin's footprint must be reassigned and dropped first —
which also **revokes every grant the admin ever made, breaking the
running service until the re-grant below**. Do this in one sitting:

```sh
gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member=user:you@your-org.example --role=roles/cloudsql.instanceUser
gcloud sql users create you@your-org.example --instance=ochakai --type=cloud_iam_user
```

As the admin user (`cloud-sql-proxy` + `psql`, deploy guide §3), in one
session:

```sql
GRANT "ochakai-run@<PROJECT_ID>.iam" TO "you@your-org.example";  -- inherit runtime-owned tables, present and future
REASSIGN OWNED BY "ochakai" TO "you@your-org.example";
DROP OWNED BY "ochakai";  -- clears its grants; the service is degraded from here until re-granted
GRANT USAGE, CREATE ON SCHEMA public
  TO "ochakai-run@<PROJECT_ID>.iam", "you@your-org.example";
```

(The schema grant must come from a `cloudsqlsuperuser` member — the
admin still qualifies; your IAM login never will.) Then as yourself
(`cloud-sql-proxy --auto-iam-authn`, connect as `you@your-org.example`),
re-grant the runtime on the tables you now own:

```sql
GRANT ALL ON ALL TABLES IN SCHEMA public TO "ochakai-run@<PROJECT_ID>.iam";
GRANT ALL ON ALL SEQUENCES IN SCHEMA public TO "ochakai-run@<PROJECT_ID>.iam";
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO "ochakai-run@<PROJECT_ID>.iam";
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO "ochakai-run@<PROJECT_ID>.iam";
```

Finally delete the password user and verify a cold start (the startup
migration needs the schema grant — a warm instance can mask a mistake):

```sh
gcloud sql users delete ochakai --instance=ochakai
gcloud run services update ochakai --region=$REGION --update-labels=grants=rotated  # forces a new instance
# then deploy guide §3's proxy + curl /health
```

(URL-encode the `@` as `%40` in connection strings:
`postgres://you%40your-org.example@localhost:55432/ochakai`.)

What "secret-zero" means afterwards: the built-in `postgres` user still
exists, and anyone with Cloud SQL admin IAM can mint it a throwaway
password (`gcloud sql users set-password postgres ...`) for break-glass
schema-level work — no *stored* secret remains, and admin access stays
IAM-gated.

**Backups and point-in-time recovery** — the example skips them for
cost; real knowledge deserves them:

```sh
gcloud sql instances patch ochakai --backup-start-time=03:00 --enable-point-in-time-recovery
```

(Enabling backups means picking a start time — there is no bare
`--backup` flag on `patch`. To turn both off again, disable PITR first:
`--no-backup` refuses to combine with `--no-enable-point-in-time-recovery`.)

**Deploy-time image gating**: releases ship SLSA provenance ([Supply
chain](#supply-chain) above); Binary Authorization can enforce that check
automatically on every deploy instead of relying on operator diligence.

**Audit trails**: knowledge changes are fully recorded by ochakai itself
(`knowledge_revision`, actor per change). For infrastructure-level
trails, enable Cloud SQL Data Access audit logs in the Console — admin
activity is logged by default.

Everything above — private IP, [the team web UI](#the-team-web-ui)'s IAP
commands, the org-policy guardrails, TLS enforcement, backups, and the
password-retirement sequence — has been exercised end-to-end on the
deploy guide's example deployment (2026-07, Postgres 17, gcloud 575). The
one exception is Binary Authorization, which remains a pointer; report
anything that doesn't work as written.

## The team web UI

The web UI runs as its own service, **not** inside `serve` — the core
keeps its serving surface minimal. For personal use it needs no
deployment at all: `ochakai ui` serves the same page on loopback with
your own identity. Deploy this service when people who cannot run the Go
CLI need browser access.

It is the **same container image** as ochakai itself — `$IMAGE` from the
deploy guide's §1 — started with `--args=serve-ui` instead of the default
`serve`: a static page plus a reverse proxy that attaches its service
identity (`X-Serverless-Authorization`) to API calls, so ochakai stays
organization-restricted — no second image to build, and the UI is always
the exact version of the server it fronts. The webui itself is
non-public too: [Identity-Aware Proxy](https://cloud.google.com/iap/docs/enabling-cloud-run)
sits in front, so browsers sign in with their Google account and only
your organization gets through — no `allUsers` grant anywhere. By default
writes through the UI are recorded as the webui's service account
(`process:ochakai-webui@…`); set `OCHAKAI_IAP_AUDIENCE` (below) to record
the person in the browser instead. MCP and CLI clients get per-user
identity via the deploy guide's §5 proxy path.

```sh
# dedicated identity, allowed to invoke ochakai only
gcloud iam service-accounts create ochakai-webui
gcloud run services add-iam-policy-binding ochakai --region=$REGION \
  --member=serviceAccount:ochakai-webui@$PROJECT_ID.iam.gserviceaccount.com \
  --role=roles/run.invoker

# deploy non-public, with IAP in front — same $IMAGE as the deploy guide's §3.
# (--iap needs the gcloud beta component; the iap web command below
#  needs the Resource Manager API)
gcloud services enable iap.googleapis.com cloudresourcemanager.googleapis.com
gcloud beta run deploy ochakai-webui \
  --image=$IMAGE --args=serve-ui \
  --region=$REGION --no-allow-unauthenticated --iap \
  --service-account=ochakai-webui@$PROJECT_ID.iam.gserviceaccount.com \
  --min-instances=0 --max-instances=1 --cpu=1 --memory=256Mi \
  --set-env-vars=OCHAKAI_URL=$OCHAKAI_URL

# let your organization through IAP (the deploy already granted IAP's
# service agent run.invoker on the service — "Setting IAP service agent"
# in its output)
gcloud beta iap web add-iam-policy-binding \
  --resource-type=cloud-run --service=ochakai-webui --region=$REGION \
  --member=domain:your-org.example --role=roles/iap.httpsResourceAccessor
```

Open the webui service URL: IAP presents the Google sign-in, then the
page loads. The UI and API are same-origin through the proxy, so nothing
else to configure. To check IAP is actually fronting the service, request
it unauthenticated — the response is a 302 to `accounts.google.com`
with an `x-goog-iap-generated-response: true` header, not the page.

### Recording the browser user instead of the service account

Out of the box every edit from the UI reads `process:ochakai-webui@…`,
which is one author for the whole team. To record who actually made it,
tell serve-ui which IAP audience to trust and let ochakai accept the
webui as a delegator (design docs 0027, 0032):

**Set them in this order.** The moment serve-ui starts forwarding
identities, ochakai answers 403 unless it already accepts this caller
(it never downgrades silently) — so grant the delegation first, and the
UI never sees the window in between.

```sh
# 1. ochakai must accept this caller's forwarded identities.
gcloud run services update ochakai --region=$REGION \
  --update-env-vars="OCHAKAI_DELEGATING_CALLERS=ochakai-webui@$PROJECT_ID.iam.gserviceaccount.com"

# 2. Tell serve-ui which IAP audience to trust. It refuses to guess:
# a wrong value would mean verifying nothing. For IAP enabled directly
# on Cloud Run — what this section deploys — the audience is
# /projects/PROJECT_NUMBER/locations/REGION/services/SERVICE_NAME.
# (Behind a load balancer it is the backend service instead, and the
# IAP console gives it: ⋮ next to the resource → "Get JWT audience code".)
IAP_AUDIENCE="/projects/$(gcloud projects describe $PROJECT_ID --format='value(projectNumber)')/locations/$REGION/services/ochakai-webui"

gcloud run services update ochakai-webui --region=$REGION \
  --update-env-vars="OCHAKAI_IAP_AUDIENCE=$IAP_AUDIENCE"
```

The startup log says which way it went: `recording browser users by
their IAP identity` with the audience it will require, or `no
OCHAKAI_IAP_AUDIENCE; writes are recorded as this service account`.

Writes then read `human:tanaka@example.co.jp via process:ochakai-webui@…`
— both identities, never just one. serve-ui verifies IAP's signature,
audience, expiry and issuer on every request, and **refuses (403) any
request IAP did not sign** once the audience is set: if IAP is not
really in front of the service, you find out immediately instead of
discovering months of writes attributed to the wrong author. The
audience it received is logged next to the one you configured, so a
mismatch is a one-line fix.

Whether or not you set this, both proxies discard any
`Ochakai-On-Behalf-Of` the browser sends — a page cannot name its own
author (design doc 0032).

Notes from exercising this end-to-end:

- **Upgrading a pre-0.9.0 webui deployment** (a separately built image,
  or one exposed with `allUsers`): the same `gcloud beta run deploy`
  converts it in place — enabling IAP replaces the service's invoker
  policy, so a leftover `allUsers` binding is removed automatically.
- **Programmatic access** (curl, scripts) is limited with the
  Google-managed OAuth client that `--iap` uses: ordinary ID tokens are
  rejected. A service account granted `roles/iap.httpsResourceAccessor`
  can get through with a self-signed JWT whose `aud` is the service URL
  plus `/*` (`gcloud iam service-accounts sign-jwt`); for anything more,
  configure IAP with a custom OAuth client. Browsers are the intended
  clients here — MCP and CLI use the deploy guide's §5 path.

## Public demo

Everything in the deploy guide says never `allUsers`, and that stays true
for every deployment anyone can write to. A demo is the exception, and it
is an exception only because of what it gives up. Two settings, and the
second implies the first:

- **`OCHAKAI_MODE=read-only`** — the deployment changes no knowledge (design
  doc 0040). Writes are 403, MCP does not offer the write tools at all, and
  the web UI stops drawing buttons that would only fail. Useful on its own,
  private, for a reference-only copy or for freezing a base during a
  migration.
- **`OCHAKAI_MODE=public`** — the deployment reads no identity
  (design doc 0042). The `Authorization` header is ignored: without Cloud Run
  IAM in front its signature is unverifiable, and a forged unsigned token
  would otherwise be believed. `Ochakai-On-Behalf-Of` is ignored. Every
  caller is `human:anonymous`, and nothing returns 401. It **implies
  read-only** and cannot be separated from it — a publicly readable *and*
  writable ochakai is not a configuration this program accepts, so the
  dangerous half of "public" has no configuration.

That is the whole trade: a service that believes nobody is safe to open,
because it records nobody and writes nothing.

### The ordering problem

**A read-only deployment cannot be seeded.** `ochakai import` is a
client-side loop over the ordinary write endpoints (deploy guide §5) —
there is no bulk import path that bypasses the refusal. So the sequence
is always: deploy **writable and private**, import, then flip. Getting
this backwards is the one way to end up with an empty demo and no way to
fill it.

**Terraform** — [deploy/terraform](../../deploy/terraform) has both
variables:

```sh
cd deploy/terraform
# 1. Normal private deployment: public_read_only stays false.
#    (invoker_members = ["user:you@your-org.example"] is enough for a demo.)
terraform apply
export OCHAKAI_URL=$(terraform output -raw service_url)
# ... the one-time schema bootstrap, as always (module README) ...

# 2. Seed it while it is still private and writable — see below.

# 3. Flip. This sets OCHAKAI_MODE=public and grants allUsers in the
#    same apply; the module has no way to do one without the other.
terraform apply -var public_read_only=true
terraform output -raw demo_url
```

**gcloud** — deploy per the deploy guide's §3 (private,
`--no-allow-unauthenticated`), seed, then flip in this order:

```sh
# 1. Stop believing identities and stop writing, while still private.
gcloud run services update ochakai --region=$REGION \
  --update-env-vars=OCHAKAI_MODE=public

# 2. Only then open it.
gcloud run services add-iam-policy-binding ochakai --region=$REGION \
  --member=allUsers --role=roles/run.invoker
```

The order is the point: between the two commands the service is harmless, and
the reverse order leaves a window in which a public ochakai still believes
whatever `Authorization` header a stranger sends. As everywhere in the
deploy guide, `--update-env-vars` — `--set-env-vars` would wipe
`OCHAKAI_DATABASE_URL`.

### Seeding

[examples/demo](../../examples/demo) is a ten-concept knowledge base built to be
read: linked concepts, mixed types, and enough usage for `sort=usage` to mean
something. Import it while the service is still private, where you
authenticate exactly as in the deploy guide's §5 — the CLI resolves a
Google ID token from your gcloud login, nothing special:

```sh
git clone --depth 1 https://github.com/na0fu3y/ochakai && cd ochakai
go run ./cmd/ochakai import examples/demo    # $OCHAKAI_URL from above
```

Those ten concepts are recorded as `human:you@your-org.example` — the last
provenance this base will ever receive, and after the flip the only
`created_by` any visitor sees. (The sequence has been rehearsed against a
local server: import writable — 10 created, 0 skipped — then restart with
`OCHAKAI_MODE=public`, after which reads serve anonymously and
every write is refused. Cloud Run adds only the IAM binding.)

### Checking the flip landed

The demo is the one deployment where a plain `curl` is supposed to work, so
it is also the one you can verify without a proxy:

```sh
curl -s -o /dev/null -w '%{http_code}\n' "$OCHAKAI_URL/api/v1/search?q=revenue"  # 200, no token
curl -s -o /dev/null -w '%{http_code}\n' -X DELETE "$OCHAKAI_URL/api/v1/bundle/queries/monthly-revenue.md"  # 403
```

200 without a token proves the public grant and `OCHAKAI_MODE=public`
both landed (before the flip this is Google's 401); 403 on the write proves
the implied read-only. Sending a bogus `Authorization` header changes
neither answer — that is the property, not a side effect.

If you want the demo to show **hybrid search** (deploy guide §4), grant
the Vertex AI role *before* the import: vectors are written when a
concept is written, and `ochakai reembed` is itself a write, so it is
refused once read-only is on. The demo bundle has no files, so the
deploy guide's §4b bucket is not needed for it.

### Cost

The same shape as the table at the top of the deploy guide — a demo is
one Cloud Run service and one `db-f1-micro`, with nothing extra to pay
for. One difference in kind, not in the numbers: Cloud Run is
request-billed and the requests no longer come only from your
organization. `--max-instances=1` (deploy guide §3) is what keeps that
bounded; leave it in place.

### What you give up, and why that is acceptable

- **No identity, at all.** Nothing about a visitor is known or recorded.
  `created_by` / `updated_by` on anything written through a public service
  would be a value the server invented — which is precisely why nothing can
  be written. Provenance is most of what ochakai sells; a public demo does
  not weaken it, it declines to pretend.
- **Everything in the base is public.** There is no 401 anywhere, so the
  concepts, their revisions, and their authors' email addresses are on the
  open internet. Put a demo bundle in it. Never point this posture at a real
  knowledge base because "it is only read-only".
- **Delegation is gone** (deploy guide §5c) — an embedding application
  cannot forward its users' identities here, because none of it would be
  believed.
- **Domain Restricted Sharing rejects the `allUsers` binding** — see
  [Guardrails against misconfiguration](#guardrails-against-misconfiguration)
  above; the deploy guide's §7 has the exact error message. It should be
  rejected — lift it for a dedicated demo project if you want one, not for
  the organization.

**The demo database still grows.** Usage telemetry — search hits and fetch
counts (design doc 0029) — keeps recording under read-only on purpose: it is
the server's own observation rather than a caller's write, and switching it
off would freeze the `sort=usage` feed that a demo most wants to show (design
doc 0040 §2.2). Two consequences: the 10 GB disk is a hard ceiling with
auto-growth off (deploy guide §2), so watch it on a demo that gets
attention; and those counts are driven by anonymous strangers, so treat
them as a public popularity signal, never as evidence about the knowledge.

### Taking it back down

Full teardown is the deploy guide's §9. To retract just the posture and
keep the deployment, reverse the order it was set in — the public grant
goes first, so the service is never open while it is anything but
read-only:

```sh
gcloud run services remove-iam-policy-binding ochakai --region=$REGION \
  --member=allUsers --role=roles/run.invoker
gcloud run services update ochakai --region=$REGION \
  --remove-env-vars=OCHAKAI_MODE
```

With Terraform, `terraform apply -var public_read_only=false` does both, in
that order.

## Upgrades

Migrations run automatically at startup, so an upgrade is a new image
tag. The two things to read first are the
[changelog](../../CHANGELOG.md) — which marks breaking changes and says
what an operator has to do about each one — and the version notes below.

```sh
gcloud run services update ochakai --region=$REGION \
  --image=$REGION-docker.pkg.dev/$PROJECT_ID/ghcr/na0fu3y/ochakai:<new-tag>
```

The Artifact Registry remote repository set up in the deploy guide's §1
fetches new tags from GHCR on demand; verify what you got with the
[Supply chain](#supply-chain) command above. Rolling back Cloud Run
traffic to a previous revision does **not** roll back database
migrations; migrations are additive, so older binaries keep working
against a newer schema.

Pin a version rather than `:latest`, so a redeploy is a decision.

Three upgrade-adjacent traps worth knowing here:

- **Upgrade the web UI before or together with the API, never after.**
  Design doc [0064](../design/0064-rest-stops-at-api-v1.md) renamed the
  delegation header with no dual-accept window; the web UI proxy only
  strips the spelling its own build knows. An old web UI in front of an
  API that has already adopted 0064 forwards the current spelling
  untouched — silently misattributing a write, not a 400 (issue
  [#418](https://github.com/na0fu3y/ochakai/issues/418)).
- **Changing `OCHAKAI_EMBEDDING_DIM` on a database that already holds
  vectors rebuilds the vector tables at the new width**, because a vector
  is derived from the concept it describes and nothing curated is involved
  (design doc [0053](../design/0053-embeddings-by-default.md) §3). The
  startup log says it happened. Search is lexical-only until
  `ochakai reembed` refills them — that part is deliberate, since
  refilling spends money.
- **Semantic search becoming reachable, or a changed model, does not
  backfill.** Existing concepts stay unembedded until `ochakai reembed`,
  which costs Vertex AI tokens proportional to the base.

### Version notes

- **→ 0.9.0 (breaking)**: the MCP OAuth connector service is retired.
  `OCHAKAI_CONNECTOR_PUBLIC_URL` is now silently ignored — **never point
  a connector deployment at this image**: that service was publicly
  invokable, and this image would serve the trust-the-headers private
  surface on it. Delete the connector service instead of upgrading it
  (`gcloud run services delete ochakai-connector --region=$REGION`), and
  clean up its Google OAuth client, the Secret Manager client secret,
  and any Domain Restricted Sharing exemption. The private service is
  unaffected; its startup migration drops the now-unused `oauth_*`
  tables (which the private service never read, so rolling it back
  afterwards remains safe).
  Also removed in 0.9.0: the `DATABASE_URL` alias (use
  `OCHAKAI_DATABASE_URL`), `OCHAKAI_ADDR` (use `PORT`), the `/healthz`
  alias (use `/health`), the startup bytea→GCS backfill (upgrade through
  0.8.x with `OCHAKAI_GCS_BUCKET` set if attachment bytes are still in
  Postgres, deploy guide §4b), and OKF import of the pre-0.4 nested
  `attrs:` frontmatter form (re-export old bundles, or lift the keys to
  the top level — SPEC §4.1).
- **From anything older than 0.8.0**: all pre-0.9.0 releases are
  [retracted](https://go.dev/ref/mod#go-mod-file-retract) and their
  per-version upgrade notes have been removed from here — recover them
  from git history if needed
  (`git log -- docs/guides/operating.md deploy/cloudrun/README.md`). The
  short of it: remove pre-0.3 configuration (`OCHAKAI_CLIENTS`,
  `OCHAKAI_AUTH`, `OCHAKAI_CORS_ORIGINS`, `OCHAKAI_EMBEDDING_PROVIDER` —
  stale variables are silently ignored, so check they are unset), adopt
  the deploy guide's §3 posture (`--no-allow-unauthenticated` + IAM
  invoker grants, identity headers, passwordless database), step through
  **0.8.x** for the attachment backfill if applicable (deploy guide
  §4b), then land on 0.9.0 with the note above.
