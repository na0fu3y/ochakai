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

export IMAGE=$REGION-docker.pkg.dev/$PROJECT_ID/ghcr/na0fu3y/ochakai:0.8.0
```

(Check [tags](https://github.com/na0fu3y/ochakai/tags) for the latest;
§3 requires 0.3.0 or later, §4b requires 0.8.0 or later.)

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
  care about (`gcloud sql instances patch ochakai --backup`).
- Users created through the Cloud SQL API (like this admin user) are
  members of `cloudsqlsuperuser` and can create extensions. The runtime
  service account deliberately gets neither (§3).
- **About the public IP**: it is not open to the internet. With the
  authorized-networks list empty (the default; §5's fallback adds an
  entry only temporarily — always remove it), direct connections are
  dropped, and the only way in is the Cloud SQL connector — IAM-checked
  (`cloudsql.instances.connect`) and mTLS'd — followed by database
  authentication. Keep the list empty and this posture holds. To remove
  the reachable endpoint entirely, see §2b.

### 2b. Optional hardening: private IP only

For production-like deployments, drop the public IP entirely. Costs
nothing extra (VPC, private services access, and Cloud Run's Direct VPC
egress are free); the trade-off is that local admin access (§5's import)
needs a temporary public IP or a VPC-attached workstation.

One-time per VPC — allocate a peering range and connect it:

```sh
gcloud services enable servicenetworking.googleapis.com compute.googleapis.com
gcloud compute addresses create google-managed-services-default \
  --global --purpose=VPC_PEERING --prefix-length=16 --network=default
gcloud services vpc-peerings connect --service=servicenetworking.googleapis.com \
  --ranges=google-managed-services-default --network=default
```

Then create the instance with `--network=default --no-assign-ip`
(instead of the defaults above), and add Direct VPC egress to the §3
deploy so Cloud Run can reach it:

```sh
gcloud run deploy ochakai ... \
  --network=default --subnet=default
```

For §5's import, temporarily attach a public IP and detach it after:

```sh
gcloud sql instances patch ochakai --assign-ip       # + authorized network, import
gcloud sql instances patch ochakai --no-assign-ip    # afterwards
```

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
user — e.g. through §5's temporary authorized network). Extensions are
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

Tip: run §5's import once **before the first deploy** — it creates the
schema as the admin user, so both identities can work with it.

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

On the next start, ochakai creates the pgvector table and embeds new and
updated knowledge with `gemini-embedding-001`. Search becomes hybrid
(trigram + vector, reciprocal rank fusion). If Vertex AI is ever
unavailable, writes and searches degrade gracefully to trigram-only.

## 4b. Attachments require GCS (design doc 0013)

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

On the next start, ochakai copies any legacy in-Postgres attachment
bytes to the bucket (objects are content-addressed `blob/<sha256>`,
create-only, never deleted), clears the database copies, and drops the
legacy bytea column (migration 0009); the backfill is idempotent, so an
interrupted start resumes on the next one. New attachments go straight
to the bucket.

**Upgrading from ≤0.8.x with attachments and no bucket**: set
`OCHAKAI_GCS_BUCKET` *before* (or together with) deploying this version.
Migration 0009 refuses to run while attachment bytes are still inline
and no bucket is configured — the service fails to start with the
instruction, nothing is lost. Once migrated, the bytea column is gone,
so binaries and configurations without the bucket cannot read
attachments again; keep the var set from then on.

## 5. Load a semantic model and connect Claude Code

Import Apache Ossie semantic models through the API. The CLI resolves
Google ID tokens itself from your gcloud login, so no Cloud SQL proxy
or authorized network is needed — `$OCHAKAI_URL` was exported when the
service was deployed above:

```sh
go run github.com/na0fu3y/ochakai/cmd/ochakai@latest import-ossie examples/semantic-model.yaml
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
  -d '{"metrics":["revenue"],"dimensions":["customers.region"],"dialect":"bigquery"}'
```

## 5b. Optional: the sample web UI behind IAP (separate service, by design)

The sample UI ([examples/webui](../../examples/webui)) is **not** part of
the ochakai image — the core keeps its serving surface minimal. For
personal use it needs no deployment at all: `ochakai ui` serves the same
page on loopback with your own identity. Deploy this service when people
who cannot run the Go CLI need browser access.

It ships as its own tiny service: a static page plus a reverse proxy that
attaches its service identity (`X-Serverless-Authorization`) to API
calls, so ochakai stays organization-restricted. The webui itself is
non-public too: [Identity-Aware Proxy](https://cloud.google.com/iap/docs/enabling-cloud-run)
sits in front, so browsers sign in with their Google account and only
your organization gets through — no `allUsers` grant anywhere. Note that
writes through the UI are still recorded as the webui's service account
(`agent:ochakai-webui@…`), not the browser user; per-user provenance
would need IAP JWT verification (design doc 0002 §4). MCP and CLI
clients get per-user identity via the §5 proxy path.

```sh
# build & push (from the repository root)
gcloud artifacts repositories create images --repository-format=docker --location=$REGION
docker build --platform linux/amd64 -f examples/webui/Dockerfile \
  -t $REGION-docker.pkg.dev/$PROJECT_ID/images/ochakai-webui:0.1 .
docker push $REGION-docker.pkg.dev/$PROJECT_ID/images/ochakai-webui:0.1

# dedicated identity, allowed to invoke ochakai only
gcloud iam service-accounts create ochakai-webui
gcloud run services add-iam-policy-binding ochakai --region=$REGION \
  --member=serviceAccount:ochakai-webui@$PROJECT_ID.iam.gserviceaccount.com \
  --role=roles/run.invoker

# deploy non-public, with IAP in front
gcloud services enable iap.googleapis.com
gcloud beta run deploy ochakai-webui \
  --image=$REGION-docker.pkg.dev/$PROJECT_ID/images/ochakai-webui:0.1 \
  --region=$REGION --no-allow-unauthenticated --iap \
  --service-account=ochakai-webui@$PROJECT_ID.iam.gserviceaccount.com \
  --min-instances=0 --max-instances=1 --cpu=1 --memory=256Mi \
  --set-env-vars=OCHAKAI_URL=$OCHAKAI_URL

# let IAP invoke the service, and your organization through IAP
export PROJECT_NUMBER=$(gcloud projects describe $PROJECT_ID --format='value(projectNumber)')
gcloud run services add-iam-policy-binding ochakai-webui --region=$REGION \
  --member=serviceAccount:service-$PROJECT_NUMBER@gcp-sa-iap.iam.gserviceaccount.com \
  --role=roles/run.invoker
gcloud beta iap web add-iam-policy-binding \
  --resource-type=cloud-run --service=ochakai-webui --region=$REGION \
  --member=domain:your-org.example --role=roles/iap.httpsResourceAccessor
```

Open the webui service URL: IAP presents the Google sign-in, then the
page loads. The UI and API are same-origin through the proxy, so nothing
else to configure.

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
# forbid broad authorized networks (e.g. 0.0.0.0/0) on Cloud SQL
gcloud resource-manager org-policies enable-enforce \
  sql.restrictAuthorizedNetworks --project=$PROJECT_ID
# forbid public IPs on Cloud SQL entirely — pair with §2b
gcloud resource-manager org-policies enable-enforce \
  sql.restrictPublicIp --project=$PROJECT_ID
```

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
secret in the whole system, and it too can go: create a personal IAM
login for maintenance, hand it the same object privileges, and delete
the password user. Future imports go through
`cloud-sql-proxy --auto-iam-authn` with your own identity:

```sh
gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member=user:you@your-org.example --role=roles/cloudsql.instanceUser
gcloud sql users create you@your-org.example --instance=ochakai --type=cloud_iam_user
# as the admin user, repeat §3's GRANT block for "you@your-org.example", then:
gcloud sql users delete ochakai --instance=ochakai
```

(URL-encode the `@` as `%40` in connection strings:
`postgres://you%40your-org.example@localhost:55432/ochakai`.)

**Backups and point-in-time recovery** — the example skips them for
cost; real knowledge deserves them:

```sh
gcloud sql instances patch ochakai --backup --enable-point-in-time-recovery
```

**Deploy-time image gating**: releases ship SLSA provenance (§8's
`gh attestation verify`); Binary Authorization can enforce that check
automatically on every deploy instead of relying on operator diligence.

**Audit trails**: knowledge changes are fully recorded by ochakai itself
(`knowledge_revision`, actor per change). For infrastructure-level
trails, enable Cloud SQL Data Access audit logs in the Console — admin
activity is logged by default.

Of these, the org-policy guardrails, TLS enforcement, and
password-retirement steps — and §5b's IAP commands — follow standard
Google Cloud procedures but, like §2b, have not yet been exercised
end-to-end by this guide's maintainers; report anything that doesn't
work as written.

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

- **→ 0.9.0 (breaking)**: the MCP OAuth connector service is retired
  ([design doc 0012](../../docs/design/0012-retire-mcp-oauth-connector.md)).
  A deployment with `OCHAKAI_CONNECTOR_PUBLIC_URL` set **refuses to
  start** — deliberately, because that service was publicly invokable
  and this image would otherwise serve the trust-the-headers private
  surface on it. Delete the connector service instead of upgrading it
  (`gcloud run services delete ochakai-connector --region=$REGION`), and
  clean up its Google OAuth client, the Secret Manager client secret,
  and any Domain Restricted Sharing exemption. The private service is
  unaffected; its startup migration drops the now-unused `oauth_*`
  tables (which the private service never read, so rolling it back
  afterwards remains safe).
- **→ 0.8.0 (breaking)**: the v0.3 migration shims are gone. The legacy
  `GET /api/v1/knowledge/{type}/{id}/usage` alias is removed — use
  `GET /api/v1/usage/{type}/{id}`. The startup guard that refused to run
  with removed v0.2 variables (`OCHAKAI_CLIENTS`, `OCHAKAI_AUTH`,
  `OCHAKAI_CORS_ORIGINS`, `OCHAKAI_EMBEDDING_PROVIDER`) is also gone:
  stale variables are now silently ignored, so double-check they are
  unset before upgrading straight from ≤0.2 (see the 0.3.0 note).
  Also in 0.8.0: attachment images can move to GCS (`OCHAKAI_GCS_BUCKET`,
  §4b). Opt-in; enabling it migrates image bytes out of Postgres at
  startup — read §4b's rollback caveat before setting the variable.
- **→ 0.3.0 (breaking)**: ochakai is Google Cloud only (design doc 0003).
  Bearer-token auth (`OCHAKAI_CLIENTS`), `OCHAKAI_AUTH`, and
  `OCHAKAI_CORS_ORIGINS` are removed — remove them from the deployment
  and follow §3 (IAM-restricted + identity headers). Deployments that
  were public + tokens must switch to `--no-allow-unauthenticated` with
  IAM invoker grants. `OCHAKAI_EMBEDDING_PROVIDER=vertex` is tolerated
  but unnecessary: `OCHAKAI_VERTEX_PROJECT` alone enables embeddings.
- **→ 0.2.1**: to adopt passwordless database auth, run §3's identity
  steps (dedicated SA, IAM database user, grants) against your existing
  instance — `cloudsql.iam_authentication=on` can be enabled with
  `gcloud sql instances patch` (brief restart) — then update the service
  with `--service-account`, `OCHAKAI_DB_IAM_AUTH=true`, and the
  password-free `OCHAKAI_DATABASE_URL`.
- **→ 0.2.0**: the verified-promotion restriction is gone
  (`OCHAKAI_VERIFY_ACTORS` is ignored) — anyone who can reach ochakai may
  set `status=verified`, recorded in `verified_by`.

## 9. Teardown

```sh
gcloud run services delete ochakai --region=$REGION --quiet
gcloud run services delete ochakai-webui --region=$REGION --quiet
gcloud sql instances delete ochakai --quiet
gcloud storage rm -r gs://$PROJECT_ID-ochakai-blobs --quiet  # if §4b was used
```
