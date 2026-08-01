# Deploying ochakai on Cloud Run + Cloud SQL

The recommended setup: an **organization-restricted, tokenless** ochakai.
Cloud Run IAM decides who can reach it; ochakai records who did what
(provenance) and performs no authorization of its own. Cloud Run scales to
zero and the smallest Cloud SQL instance carries the whole knowledge base —
one container image plus one database.

**Cost (approximate, us-central1):**

| Component | Configuration | Monthly |
|---|---|---|
| Cloud SQL | `db-f1-micro` (shared core), 10 GB SSD, single zone, no backups | ~$9–10 |
| Cloud Run | request-based billing, `min-instances=0` | ~$0 when idle |
| Vertex AI embeddings (on by default, §4) | `gemini-embedding-001`, pay per token | cents at example scale |

Cloud SQL dominates the bill. Regions in Asia (e.g. `asia-northeast1`) cost
slightly more; pick what matches your latency needs. Teardown commands are
at the bottom.

**Recommended: stand this up with [deploy/terraform](../terraform)**,
which turns §1–§4b (plus the web UI and demo posture) into a thirteen-step
`terraform apply`, reviewable as a diff, reproducible per environment, and
destroyed cleanly. This guide is what stays the reference — it explains
why each resource is shaped the way it is, and is the path to use if you
would rather run the commands by hand. It covers §1–§5, §5d and §9; the
operating guide covers the web UI, the public demo, org-policy guardrails
and upgrade notes, and [docs/guides/rest-integration.md](../../docs/guides/rest-integration.md)
covers §5c and the rest of what an application embedding the REST API
needs.

**Already deployed?** [docs/guides/operating.md](../../docs/guides/operating.md)
covers what happens after: backup and restore, hardening, the team web
UI, monitoring, capacity, and upgrades.

**Embedding ochakai in your own product?**
[docs/guides/rest-integration.md](../../docs/guides/rest-integration.md)
covers authenticating, delegated provenance and safe concurrent writes for
an application calling the REST API directly.

## 1. Prerequisites

Local tools this guide uses, beyond `gcloud` itself (authenticated —
`gcloud auth login`): the [Go toolchain](https://go.dev/dl/) (`go run` /
`go install`, §5), [`cloud-sql-proxy`](https://cloud.google.com/sql/docs/postgres/sql-proxy)
and `psql` (§3's one-time database setup), `openssl` (§2's admin
password), and optionally the [`gh` CLI](https://cli.github.com) (the
version lookup just below — without it, read the version off the
[releases page](https://github.com/na0fu3y/ochakai/releases) by hand).

```sh
export PROJECT_ID=<your-project>
export REGION=us-central1
gcloud config set project $PROJECT_ID
gcloud services enable run.googleapis.com sqladmin.googleapis.com \
  sql-component.googleapis.com artifactregistry.googleapis.com
```

Cloud Run cannot pull images from ghcr.io directly, so create an Artifact
Registry **remote repository** that proxies GHCR. Layers are cached on
first pull and the image digest stays identical to the GHCR one, so GHCR
attestations (`gh attestation verify`) remain valid for what you run:

```sh
gcloud artifacts repositories create ghcr \
  --repository-format=docker \
  --mode=remote-repository \
  --remote-docker-repo=https://ghcr.io \
  --location=$REGION

export VERSION=$(gh release view --repo na0fu3y/ochakai --json tagName -q .tagName | tr -d v)
export IMAGE=$REGION-docker.pkg.dev/$PROJECT_ID/ghcr/na0fu3y/ochakai:$VERSION
```

Pin a version rather than `:latest`, so a redeploy is a decision. Without
the `gh` CLI, read the number off the
[releases page](https://github.com/na0fu3y/ochakai/releases) and set it by
hand — `export VERSION=0.17.0`.

(This guide assumes 0.9.0 or later; earlier releases are
[retracted](https://go.dev/ref/mod#go-mod-file-retract) and unsupported —
if you run one, see §8 for the upgrade path.)

## 2. Create the database (cheapest viable instance)

```sh
gcloud sql instances create ochakai \
  --database-version=POSTGRES_17 \
  --edition=enterprise \
  --tier=db-f1-micro \
  --region=$REGION \
  --storage-size=10 \
  --storage-type=SSD \
  --no-storage-auto-increase \
  --no-backup \
  --database-flags=cloudsql.iam_authentication=on

gcloud sql databases create ochakai --instance=ochakai

# admin user for local import/maintenance only — its password never
# reaches Cloud Run (the service itself connects passwordless, §3)
export DB_PASSWORD=$(openssl rand -hex 24)
gcloud sql users create ochakai --instance=ochakai --password=$DB_PASSWORD
```

Notes:

- Instance creation takes 10–15 minutes.
- `--no-backup` keeps the example cheap; enable backups for anything you
  care about (`gcloud sql instances patch ochakai --backup-start-time=03:00`
  — there is no bare `--backup` flag on `patch`; enabling means picking a
  start time).
- Users created through the Cloud SQL API (like this admin user) are
  members of `cloudsqlsuperuser` and can create extensions. The runtime
  service account deliberately gets neither (§3).
- **About the public IP**: it is not open to the internet. With the
  authorized-networks list empty (the default), direct connections are
  dropped, and the only way in is the Cloud SQL connector — IAM-checked
  (`cloudsql.instances.connect`) and mTLS'd — followed by database
  authentication. Local admin access (§3's SQL, §6's maintenance) goes
  through [`cloud-sql-proxy`](https://cloud.google.com/sql/docs/postgres/sql-proxy),
  which uses the same connector path, so the list never needs a concept.
  Keep it empty and this posture holds. To remove the reachable endpoint
  entirely, see §2b.

### 2b. Optional hardening: private IP only

For production-like deployments, drop the public IP entirely — free, and
worth doing for anything beyond a quick trial. The commands and the
local-admin-access trade-off are in
[Hardening](../../docs/guides/operating.md#hardening) in the operating
guide.

## 3. Deploy Cloud Run (dedicated identity, passwordless, org-restricted)

Create a dedicated service account for ochakai and let it log in to the
database with **IAM database authentication** — the connection password is
a short-lived IAM token fetched at connect time, so **no database password
exists anywhere in the deployment**:

```sh
gcloud iam service-accounts create ochakai-run --display-name="ochakai service"
export SERVICE_ACCOUNT=ochakai-run@$PROJECT_ID.iam.gserviceaccount.com

gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member=serviceAccount:$SERVICE_ACCOUNT --role=roles/cloudsql.client
gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member=serviceAccount:$SERVICE_ACCOUNT --role=roles/cloudsql.instanceUser

# database principal for the service account (note: no .gserviceaccount.com)
export DB_SA_USER=ochakai-run@$PROJECT_ID.iam
gcloud sql users create $DB_SA_USER --instance=ochakai --type=cloud_iam_service_account
```

Set up the database for the service account (one-time, as the admin
user — connect with
`cloud-sql-proxy $PROJECT_ID:$REGION:ochakai --port 55432` and
`psql "host=localhost port=55432 dbname=ochakai user=ochakai"`, using
`$DB_PASSWORD` from §2). Extensions are
pre-created here so the runtime never needs elevated rights: ochakai's
startup migration only ever hits the privilege-free
`CREATE EXTENSION IF NOT EXISTS` skip path. Everything else is explicit
object grants — **no `cloudsqlsuperuser`, no role membership** for the
runtime identity:

```sql
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS vector;   -- for §4's semantic search, which is on
                                         -- by default; Cloud SQL's pgvector is not
                                         -- a trusted extension, hence admin-created
GRANT USAGE, CREATE ON SCHEMA public TO "ochakai-run@<PROJECT_ID>.iam";
GRANT ALL ON ALL TABLES IN SCHEMA public TO "ochakai-run@<PROJECT_ID>.iam";
GRANT ALL ON ALL SEQUENCES IN SCHEMA public TO "ochakai-run@<PROJECT_ID>.iam";
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO "ochakai-run@<PROJECT_ID>.iam";
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO "ochakai-run@<PROJECT_ID>.iam";
```

Deploy privately with the dedicated identity and `OCHAKAI_DB_IAM_AUTH`
(passwordless database), then allow your organization to invoke it:

```sh
gcloud run deploy ochakai \
  --image=$IMAGE \
  --region=$REGION \
  --service-account=$SERVICE_ACCOUNT \
  --no-allow-unauthenticated \
  --min-instances=0 --max-instances=1 \
  --cpu=1 --memory=512Mi \
  --add-cloudsql-instances=$PROJECT_ID:$REGION:ochakai \
  --set-env-vars="OCHAKAI_DB_IAM_AUTH=true" \
  --set-env-vars="OCHAKAI_DATABASE_URL=postgres:///ochakai?host=/cloudsql/$PROJECT_ID:$REGION:ochakai&user=$DB_SA_USER"

gcloud run services add-iam-policy-binding ochakai --region=$REGION \
  --member=domain:your-org.example --role=roles/run.invoker

export OCHAKAI_URL=$(gcloud run services describe ochakai --region=$REGION --format='value(status.url)')
```

**One more one-time step, now that the first deploy has run its startup
migration:** the migration creates the tables, owned by the **runtime
service account** (imports go through the API, so nothing is ever
created as the admin). For the admin user to work with those tables
directly (maintenance, ad-hoc SQL), give it the runtime's role —
reconnect the same way as above, with `$DB_PASSWORD` from §2:

```sql
GRANT "ochakai-run@<PROJECT_ID>.iam" TO "ochakai";
```

How this works:

- **Cloud Run IAM decides who can reach the service** (org members and
  service accounts you grant `roles/run.invoker`; anonymous requests get
  Google's 401 without hitting the container). ochakai performs no
  authorization: whoever reaches it reads and writes.
- **ochakai reads the Cloud-Run-verified caller identity for provenance**:
  people are recorded as `human:<email>`, service accounts as
  `process:<sa-email>`. Nothing to issue, rotate, or revoke.
- **Never make the service publicly invokable (`allUsers`)** — the
  identity headers ochakai reads are only trustworthy behind Cloud Run's
  IAM check. The single exception is a deployment that reads no identity
  and writes nothing at all (§5d's demo); for anything you can write to,
  this rule has no exception.
- No `allUsers` grant is needed for this deployment, so it is compatible
  with — and a good reason to keep — the Domain Restricted Sharing org
  policy (`iam.allowedPolicyMemberDomains`).
- With `OCHAKAI_DB_IAM_AUTH` the `OCHAKAI_DATABASE_URL` contains no
  password, so there is nothing secret in the environment variables.
  (If you use password auth instead, put the URL in Secret Manager with
  `--set-secrets`.)
- The whole deployment is **secret-zero**: clients bring Google
  identities, ochakai brings its service-account identity to the
  database, and nothing needs to be issued, stored, or rotated.

Verify through the
[Cloud Run proxy](https://cloud.google.com/sdk/gcloud/reference/run/services/proxy)
(direct `curl` is blocked by IAM, which is the point):

```sh
gcloud run services proxy ochakai --region=$REGION --port=8787 &
curl http://localhost:8787/health
```

Note: use `/health`, not `/healthz` — Google Frontends intercept
`/healthz` on `run.app` URLs and return their own 404 without ever
reaching the app.

## 4. Hybrid semantic search (Vertex AI, on by default)

On Cloud Run, ochakai asks the metadata server which project it is
running in and turns semantic search on with it (design doc 0053) —
there is no variable to set, and authentication is the service identity
via ADC, so there are still no API keys.

**What decides whether you actually get it is IAM, not configuration.**
Without `roles/aiplatform.user` the service identity cannot call Vertex
AI: ochakai finds that out with one small embedding at startup, says so
in one log line, and serves lexical-only search. So this section is two
commands, and they are the difference between hybrid and lexical:

```sh
gcloud services enable aiplatform.googleapis.com

gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member=serviceAccount:$SERVICE_ACCOUNT --role=roles/aiplatform.user   # ochakai-run SA from §3
```

Grant them unless you have a reason not to — above all for a knowledge
base written in Japanese, where the trigram index cannot serve a
two-character term and the lexical half is answering by scanning. On the
next start ochakai creates the pgvector tables and search becomes hybrid
(trigram + vector); if Vertex AI is ever unavailable afterwards, it
degrades gracefully back to trigram-only.

**To refuse it**, either do not grant the role (nothing is called, and
nothing is charged), or say so:

```sh
gcloud run services update ochakai --region=$REGION \
  --update-env-vars=OCHAKAI_EMBEDDINGS=off
```

Use `--update-env-vars`, not `--set-env-vars`: the latter **replaces** all
environment variables and would wipe `OCHAKAI_DATABASE_URL`.

Worth knowing before you grant the role: ochakai has no authorization
(design doc 0002), so **anyone who can reach the service can cause a
Vertex AI call by writing** — cents at the scale a curated knowledge base
reaches.

The model, location, multimodal attachment search, `OCHAKAI_EMBEDDING_DIM`
and `ochakai reembed` — the command that backfills a base loaded before
the role was granted, or before a model change — are all in
[Environment variables](../../docs/configuration.md#environment-variables);
what changing the dimension does to an existing database is in
[Upgrades](../../docs/guides/operating.md#upgrades) in the operating
guide.

## 4b. Attachments require GCS

Attachment bytes live only in a GCS bucket — metadata and revisions stay
in Postgres, and auth is ADC via the service identity, no keys. Without
`OCHAKAI_GCS_BUCKET` the service runs markdown-only: attach operations
return 501 and imports report attachments as failed. Skip this section
only if you never attach files:

```sh
gcloud storage buckets create gs://$PROJECT_ID-ochakai-blobs \
  --location=$REGION --uniform-bucket-level-access --public-access-prevention

gcloud storage buckets add-iam-policy-binding gs://$PROJECT_ID-ochakai-blobs \
  --member=serviceAccount:$SERVICE_ACCOUNT --role=roles/storage.objectUser  # ochakai-run SA from §3

gcloud run services update ochakai --region=$REGION \
  --update-env-vars=OCHAKAI_GCS_BUCKET=$PROJECT_ID-ochakai-blobs
```

Attachment bytes go straight to the bucket (objects are
content-addressed `blob/<sha256>`, create-only, never deleted).

**Upgrading from ≤0.8.x with attachments and no bucket**: run a 0.8.x
release with `OCHAKAI_GCS_BUCKET` set once — its startup backfill moves
the in-Postgres attachment bytes to the bucket. From 0.9.0 on, the
backfill is gone and migration 0009 refuses to run while attachment
bytes are still inline — the service fails to start with the
instruction, nothing is lost. Once migrated, the bytea column is gone,
so binaries and configurations without the bucket cannot read
attachments again; keep the var set from then on.

## 5. Load knowledge and connect Claude Code

Register a concept through the API. The CLI resolves Google ID tokens
itself from your gcloud login, so no Cloud SQL proxy or authorized
network is needed — `$OCHAKAI_URL` was exported when the service was
deployed above:

```sh
curl -fsSL "https://raw.githubusercontent.com/na0fu3y/ochakai/v$VERSION/examples/golden-query.md" | \
  go run github.com/na0fu3y/ochakai/cmd/ochakai@latest put queries/monthly-revenue
```

Point Claude Code — or any agent with a shell — at the same URL; the CLI
run above already proved it resolves tokens with no proxy needed. This is
the recommended way in, per the README's
[Connect an agent](../../README.md#connect-an-agent):

```sh
go install github.com/na0fu3y/ochakai/cmd/ochakai@latest
ochakai use $OCHAKAI_URL
```

If you want MCP tools inside Claude Code instead, the bridge needs no
proxy either — `claude mcp add ochakai -- ochakai mcp-stdio` (needs
`gcloud auth login` and `ochakai` on `PATH`; see [connecting an MCP
client](../../docs/guides/mcp-clients.md#what-the-bridge-needs)). The
repository's committed `.mcp.json` defaults to the local compose port
(8080); if using this repo's config file against a Cloud Run deployment,
repoint its `url` at the proxy below.

Smoke test over REST through the [Cloud Run
proxy](https://cloud.google.com/sdk/gcloud/reference/run/services/proxy),
same as §3 (direct `curl` is blocked by IAM):

```sh
gcloud run services proxy ochakai --region=$REGION --port=8787 &
curl "http://localhost:8787/api/v1/search?q=revenue"
```

## 5b. Optional: the team web UI behind IAP (separate service, by design)

The web UI runs as its own service, **not** inside `serve`, behind
[Identity-Aware Proxy](https://cloud.google.com/iap/docs/enabling-cloud-run)
— deploy it when people who cannot run the Go CLI need browser access.
The commands, including recording the browser user instead of the
service account, are in
[The team web UI](../../docs/guides/operating.md#the-team-web-ui) in the
operating guide.

## 5c. Optional: an application that serves many people (delegated provenance)

Embedding ochakai in your own product — a data-analytics chat app, an
internal agent with a web front end — hits a problem the §5 path does not:
the application reaches Cloud Run with *its* service account, so every
concept its users write is recorded as that one identity. Provenance, which
is most of what ochakai sells, collapses.

The fix, the two-command setup, and everything else an application
embedding the REST API needs — authenticating, delegated provenance,
`X-Ochakai-Producer`, and safe concurrent writes — are in
[Embedding the REST API](../../docs/guides/rest-integration.md).

## 5d. Optional: a public read-only demo (the one public posture)

Everything above says never `allUsers`, and that stays true for every
deployment anyone can write to. A demo is the one exception, and only
because of what it gives up: `OCHAKAI_MODE=public` reads no identity and
implies no writes (design docs 0040, 0042) — a service that records
nobody and writes nothing is the one kind safe to open to everyone.

Seeding it, flipping the mode without a window in which a public service
still believes a stranger's `Authorization` header, checking the flip
landed, cost, what you give up, and taking it back down are all in
[Public demo](../../docs/guides/operating.md#public-demo) in the
operating guide.

## 6. Security hardening checklist

The default §1–§5 deployment is already secret-zero and least-privilege.
Org-policy guardrails, TLS enforcement, retiring the last password,
backups, and deploy-time image gating that raise the bar further are in
[Hardening](../../docs/guides/operating.md#hardening) in the operating
guide.

## 7. Troubleshooting in security-hardened organizations

- **`allUsers` binding fails with "do not belong to a permitted customer"**:
  the org enforces Domain Restricted Sharing
  (`iam.allowedPolicyMemberDomains`). No normal deployment needs
  `allUsers` (the webui goes behind IAP, §5b), so keep the policy on — if
  you hit this while setting up §5d's demo, exempt that project rather
  than the organization.
- **`run.app` returns Google's HTML 404 ("That's an error") even though
  the service is Ready**: before suspecting infrastructure, test a real
  application endpoint (e.g. `/api/v1/search?q=x`) and check request
  logs. Two Google Frontend behaviors conspire to make a healthy service
  look dead:
  1. `/healthz` is intercepted by Google Frontends on `run.app` and 404s
     without ever reaching the container (and without request logs). Use
     `/health`.
  2. Application-level 404 responses are dressed up as Google's branded
     404 page, so an unhandled path looks like a routing failure.
  A genuinely unknown service URL returns a much shorter 404 page
  (~272 bytes vs ~1.5 KB) — comparing `content-length` tells them apart.
- **Container exits with `cloudsql.instances.get ... NOT_AUTHORIZED`**:
  the service account is missing `roles/cloudsql.client` (§3, first step).
- **Cloud SQL socket `connection refused` right after creating the
  service account**: IAM grants on a freshly created service account can
  take a minute to propagate. Verify the roles landed
  (`gcloud projects get-iam-policy ... --filter=bindings.members:ochakai-run@`)
  and redeploy.

## 8. Upgrading an existing deployment

Point the service at the new tag; migrations run automatically at
startup. The full upgrade path, including the version-specific breaking-
change notes, is [Upgrades](../../docs/guides/operating.md#upgrades) in
the operating guide.

## 9. Teardown

```sh
gcloud run services delete ochakai --region=$REGION --quiet
gcloud run services delete ochakai-webui --region=$REGION --quiet
gcloud sql instances delete ochakai --quiet
gcloud storage rm -r gs://$PROJECT_ID-ochakai-blobs --quiet  # if §4b was used
# if §2b was used:
gcloud services vpc-peerings delete --service=servicenetworking.googleapis.com --network=default --quiet
gcloud compute addresses delete google-managed-services-default --global --quiet
```
