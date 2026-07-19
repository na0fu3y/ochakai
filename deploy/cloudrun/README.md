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
| Vertex AI embeddings (optional) | `gemini-embedding-001`, pay per token | cents at example scale |

Cloud SQL dominates the bill. Regions in Asia (e.g. `asia-northeast1`) cost
slightly more; pick what matches your latency needs. Teardown commands are
at the bottom.

## 1. Prerequisites

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

export IMAGE=$REGION-docker.pkg.dev/$PROJECT_ID/ghcr/na0fu3y/ochakai:0.9.0
```

(Check [tags](https://github.com/na0fu3y/ochakai/tags) for the latest.
This guide assumes 0.9.0 or later; earlier releases are
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
  which uses the same connector path, so the list never needs an entry.
  Keep it empty and this posture holds. To remove the reachable endpoint
  entirely, see §2b.

### 2b. Optional hardening: private IP only

For production-like deployments, drop the public IP entirely. Costs
nothing extra (VPC, private services access, and Cloud Run's Direct VPC
egress are free); the trade-off is that local admin access (§3's SQL,
§6's maintenance — §5's import is unaffected, it goes over the API)
needs a temporary public IP or a VPC-attached workstation.

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
(instead of the defaults above) — or convert an existing one, which is
the same patch and takes 15–20 minutes:

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

Local admin access (§3's SQL, §6's maintenance) no longer works from
outside the VPC — `cloud-sql-proxy` connects only to instances it can
route to. Temporarily attach a public IP and detach it after:

```sh
gcloud sql instances patch ochakai --assign-ip       # + cloud-sql-proxy, do the work
gcloud sql instances patch ochakai --no-assign-ip    # afterwards
```

(If §6's `sql.restrictPublicIp` org policy is enforced, the temporary
`--assign-ip` is blocked too — lift the policy for the window or use a
VPC-attached workstation.)

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
CREATE EXTENSION IF NOT EXISTS vector;   -- for §4; Cloud SQL's pgvector is not
                                         -- a trusted extension, hence admin-created
GRANT USAGE, CREATE ON SCHEMA public TO "ochakai-run@<PROJECT_ID>.iam";
GRANT ALL ON ALL TABLES IN SCHEMA public TO "ochakai-run@<PROJECT_ID>.iam";
GRANT ALL ON ALL SEQUENCES IN SCHEMA public TO "ochakai-run@<PROJECT_ID>.iam";
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO "ochakai-run@<PROJECT_ID>.iam";
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO "ochakai-run@<PROJECT_ID>.iam";
```

Note on ownership: the first deploy's startup migration creates the
tables, owned by the **runtime service account** (imports go through the
API, so nothing is ever created as the admin). For the admin user to
work with those tables directly (maintenance, ad-hoc SQL), give it the
runtime's role in the same session:

```sql
GRANT "ochakai-run@<PROJECT_ID>.iam" TO "ochakai";
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

How this works:

- **Cloud Run IAM decides who can reach the service** (org members and
  service accounts you grant `roles/run.invoker`; anonymous requests get
  Google's 401 without hitting the container). ochakai performs no
  authorization: whoever reaches it reads and writes.
- **ochakai reads the Cloud-Run-verified caller identity for provenance**:
  people are recorded as `human:<email>`, service accounts as
  `agent:<sa-email>`. Nothing to issue, rotate, or revoke.
- **Never make the service publicly invokable (`allUsers`)** — the
  identity headers ochakai reads are only trustworthy behind Cloud Run's
  IAM check.
- No `allUsers` grant is needed anywhere, so this is compatible with —
  and a good reason to keep — the Domain Restricted Sharing org policy
  (`iam.allowedPolicyMemberDomains`).
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

## 4. Optional: enable hybrid semantic search (Vertex AI)

Embeddings are off by default (trigram-only search, no external calls).
Enabling them uses the Cloud Run service identity via ADC — no API keys.

```sh
gcloud services enable aiplatform.googleapis.com

gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member=serviceAccount:$SERVICE_ACCOUNT --role=roles/aiplatform.user   # ochakai-run SA from §3

gcloud run services update ochakai --region=$REGION \
  --update-env-vars=OCHAKAI_VERTEX_PROJECT=$PROJECT_ID
```

Use `--update-env-vars`, not `--set-env-vars`: the latter **replaces** all
environment variables and would wipe `OCHAKAI_DATABASE_URL`.

On the next start, ochakai creates the pgvector tables and embeds new
and updated knowledge — and newly attached plain-text files (design doc
0020) — with `gemini-embedding-001`. Search becomes hybrid
(trigram + vector, reciprocal rank fusion). If Vertex AI is ever
unavailable, writes and searches degrade gracefully to trigram-only.

To also search image and PDF attachments by content, run the base on
the multimodal model instead:

```sh
gcloud run services update ochakai --region=$REGION \
  --update-env-vars=OCHAKAI_VERTEX_MODEL=gemini-embedding-2,OCHAKAI_VERTEX_LOCATION=global
```

`gemini-embedding-2` lives in the `global`/`us`/`eu` locations only, and
all vectors must share one model's space: on an existing base, entries
and attachments keep their old-model vectors (and stay out of the new
space) until they are written again (design doc 0020 §2.3).

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

## 5. Load a semantic model and connect Claude Code

Register a semantic model as a `models` knowledge entry (design doc
0018) through the API. The CLI resolves Google ID tokens itself from
your gcloud login, so no Cloud SQL proxy or authorized network is
needed — `$OCHAKAI_URL` was exported when the service was deployed
above:

```sh
go run github.com/na0fu3y/ochakai/cmd/ochakai@latest create models/sales-analytics -f examples/semantic-model.md
```

Connect Claude Code — with the Cloud Run proxy running, no headers, no
tokens (this repository's committed `.mcp.json` does the same
automatically when you open the repo in Claude Code):

```sh
gcloud run services proxy ochakai --region=$REGION --port=8787 &
claude mcp add --transport http ochakai http://localhost:8787/mcp
```

Smoke test over REST (through the proxy, also tokenless):

```sh
curl "http://localhost:8787/api/v1/knowledge?q=revenue"
curl -X POST "http://localhost:8787/api/v1/compile" \
  -d '{"metrics":["revenue"],"dimensions":["customers.region"]}'
```

## 5b. Optional: the team web UI behind IAP (separate service, by design)

The web UI runs as its own service, **not** inside `serve` — the core
keeps its serving surface minimal. For personal use it
needs no deployment at all: `ochakai ui` serves the same page on loopback
with your own identity. Deploy this service when people who cannot run
the Go CLI need browser access.

It is the **same container image** as ochakai itself, started with
`--args=serve-ui` instead of the default `serve`: a static page plus a
reverse proxy that attaches its service identity
(`X-Serverless-Authorization`) to API calls, so ochakai stays
organization-restricted — no second image to build, and the UI is always
the exact version of the server it fronts. The webui itself is
non-public too: [Identity-Aware Proxy](https://cloud.google.com/iap/docs/enabling-cloud-run)
sits in front, so browsers sign in with their Google account and only
your organization gets through — no `allUsers` grant anywhere. Note that
writes through the UI are still recorded as the webui's service account
(`agent:ochakai-webui@…`), not the browser user; per-user provenance
would need IAP JWT verification, which ochakai does not do. MCP and CLI
clients get per-user identity via the §5 proxy path.

```sh
# dedicated identity, allowed to invoke ochakai only
gcloud iam service-accounts create ochakai-webui
gcloud run services add-iam-policy-binding ochakai --region=$REGION \
  --member=serviceAccount:ochakai-webui@$PROJECT_ID.iam.gserviceaccount.com \
  --role=roles/run.invoker

# deploy non-public, with IAP in front — same $IMAGE as §3.
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
  clients here — MCP and CLI use the §5 path.

## 6. Security hardening checklist

The default §1–§5 deployment is already secret-zero and least-privilege:
Cloud Run IAM gates reachability, callers and the database authenticate
with Google identities (no tokens, no passwords), the runtime service
account holds only explicit grants, the Cloud SQL authorized-networks
list stays empty, and images are provenance-attested. The steps below
raise the bar further; pick what matches your risk profile.

**Guardrails against misconfiguration** (org-policy; needs the Org Policy
Administrator role). These make the risky states unrepresentable rather
than merely avoided:

```sh
# forbid adding ANY authorized network on Cloud SQL (not just broad
# ranges — the empty-list posture becomes unrepresentable to leave)
gcloud resource-manager org-policies enable-enforce \
  sql.restrictAuthorizedNetworks --project=$PROJECT_ID
# forbid public IPs on Cloud SQL — pair with §2b. Existing public IPs
# are grandfathered: the instance keeps running and unrelated patches
# still work; only changes to the IP configuration are blocked
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
the org level; nothing in this guide needs `allUsers` — every service,
the IAP-fronted webui included, stays `--no-allow-unauthenticated`.

**Remove the reachable database endpoint**: §2b (private IP only).

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

As the admin user (`cloud-sql-proxy` + `psql`, §3), in one session:

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
# then §3's proxy + curl /health
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

**Deploy-time image gating**: releases ship SLSA provenance (§8's
`gh attestation verify`); Binary Authorization can enforce that check
automatically on every deploy instead of relying on operator diligence.

**Audit trails**: knowledge changes are fully recorded by ochakai itself
(`knowledge_revision`, actor per change). For infrastructure-level
trails, enable Cloud SQL Data Access audit logs in the Console — admin
activity is logged by default.

Everything above — §2b's private IP, §5b's IAP commands, the org-policy
guardrails, TLS enforcement, backups, and the password-retirement
sequence — has been exercised end-to-end on this guide's example
deployment (2026-07, Postgres 17, gcloud 575). The one exception is
Binary Authorization, which remains a pointer; report anything that
doesn't work as written.

## 7. Troubleshooting in security-hardened organizations

- **`allUsers` binding fails with "do not belong to a permitted customer"**:
  the org enforces Domain Restricted Sharing
  (`iam.allowedPolicyMemberDomains`). Nothing in this guide needs
  `allUsers` (the webui goes behind IAP, §5b), so keep the policy on.
- **`run.app` returns Google's HTML 404 ("That's an error") even though
  the service is Ready**: before suspecting infrastructure, test a real
  application endpoint (e.g. `/api/v1/knowledge?q=x`) and check request
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

Point the service at the new tag — that's normally everything. Database
migrations are embedded in the binary, tracked in `schema_migrations`,
and run automatically at startup:

```sh
gcloud run services update ochakai --region=$REGION \
  --image=$REGION-docker.pkg.dev/$PROJECT_ID/ghcr/na0fu3y/ochakai:<new-tag>
```

The Artifact Registry remote repository fetches new tags from GHCR on
demand; verify what you got with
`gh attestation verify oci://ghcr.io/na0fu3y/ochakai:<new-tag> -R na0fu3y/ochakai`.
Rolling back Cloud Run traffic to a previous revision does **not** roll
back database migrations; migrations are additive, so older binaries keep
working against a newer schema.

Version notes:

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
  Postgres, §4b), and OKF import of the pre-0.4 nested `attrs:`
  frontmatter form (re-export old bundles, or lift the keys to the top
  level — SPEC §4.1).
- **From anything older than 0.8.0**: all pre-0.9.0 releases are
  [retracted](https://go.dev/ref/mod#go-mod-file-retract) and their
  per-version upgrade notes have been removed from this guide — recover
  them from git history if needed
  (`git log -- deploy/cloudrun/README.md`). The short of it: remove
  pre-0.3 configuration (`OCHAKAI_CLIENTS`, `OCHAKAI_AUTH`,
  `OCHAKAI_CORS_ORIGINS`, `OCHAKAI_EMBEDDING_PROVIDER` — stale variables
  are silently ignored, so check they are unset), adopt §3's posture
  (`--no-allow-unauthenticated` + IAM invoker grants, identity headers,
  passwordless database), step through **0.8.x** for the attachment
  backfill if applicable (§4b), then land on 0.9.0 with the note above.

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
