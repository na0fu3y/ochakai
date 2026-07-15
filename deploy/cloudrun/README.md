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
gcloud services enable run.googleapis.com sqladmin.googleapis.com artifactregistry.googleapis.com
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

export IMAGE=$REGION-docker.pkg.dev/$PROJECT_ID/ghcr/na0fu3y/ochakai:0.2.0
```

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
- Users created through Cloud SQL get `cloudsqlsuperuser`, so ochakai can
  run `CREATE EXTENSION` (pg_trgm, and vector if you enable embeddings)
  during its automatic migration.

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

Grant the service account database privileges (one-time, as the admin
user — e.g. through §5's temporary authorized network):

```sql
GRANT cloudsqlsuperuser TO "ochakai-run@<PROJECT_ID>.iam";  -- CREATE EXTENSION in migrations
GRANT ochakai TO "ochakai-run@<PROJECT_ID>.iam";            -- share admin-owned objects
```

Tip: run §5's import once **before the first deploy** — it creates the
schema as the admin user, so both identities can work with it.

Deploy privately with the dedicated identity, `OCHAKAI_AUTH=cloudrun-iam`
(tokenless clients), and `OCHAKAI_DB_IAM_AUTH` (passwordless database),
then allow your organization to invoke it:

```sh
gcloud run deploy ochakai \
  --image=$IMAGE \
  --region=$REGION \
  --service-account=$SERVICE_ACCOUNT \
  --no-allow-unauthenticated \
  --min-instances=0 --max-instances=1 \
  --cpu=1 --memory=512Mi \
  --add-cloudsql-instances=$PROJECT_ID:$REGION:ochakai \
  --set-env-vars="OCHAKAI_AUTH=cloudrun-iam,OCHAKAI_DB_IAM_AUTH=true" \
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
- **Never combine `cloudrun-iam` with a public (`allUsers`) service** —
  there the identity headers are unverified. For a public deployment use
  the token variant (§3-alt).
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

### 3-alt. Public endpoint with bearer tokens

If you cannot use Google identities (mixed IdPs, external partners) or
must expose a public URL, deploy with `--allow-unauthenticated` and static
tokens instead. Each token maps to an actor for provenance; whoever holds
a token acts as that actor, so issue one per client and don't share them:

```sh
export AGENT_TOKEN=$(openssl rand -hex 24)
gcloud run deploy ochakai ... --allow-unauthenticated \
  --set-env-vars="^@^OCHAKAI_CLIENTS=$AGENT_TOKEN=agent:claude-code,$(openssl rand -hex 24)=human:$(whoami)"
```

(`^@^` changes the env-var delimiter so the value may contain commas.
Clients then send `Authorization: Bearer <token>`; every endpoint except
`/health` requires it.)

## 4. Optional: enable hybrid semantic search (Vertex AI)

Embeddings are off by default (trigram-only search, no external calls).
Enabling them uses the Cloud Run service identity via ADC — no API keys.

```sh
gcloud services enable aiplatform.googleapis.com

gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member=serviceAccount:$SERVICE_ACCOUNT --role=roles/aiplatform.user   # ochakai-run SA from §3

gcloud run services update ochakai --region=$REGION \
  --update-env-vars=OCHAKAI_EMBEDDING_PROVIDER=vertex,OCHAKAI_VERTEX_PROJECT=$PROJECT_ID
```

Use `--update-env-vars`, not `--set-env-vars`: the latter **replaces** all
environment variables and would wipe `OCHAKAI_DATABASE_URL`.

On the next start, ochakai creates the pgvector table and embeds new and
updated knowledge with `gemini-embedding-001`. Search becomes hybrid
(trigram + vector, reciprocal rank fusion). If Vertex AI is ever
unavailable, writes and searches degrade gracefully to trigram-only.

## 5. Load a semantic model and connect Claude Code

Import Apache Ossie semantic models through the
[Cloud SQL Auth Proxy](https://cloud.google.com/sql/docs/postgres/sql-proxy)
(uses your ADC; run `gcloud auth application-default login` once):

```sh
# terminal 1
cloud-sql-proxy $PROJECT_ID:$REGION:ochakai --port 55432

# terminal 2
OCHAKAI_DATABASE_URL="postgres://ochakai:$DB_PASSWORD@localhost:55432/ochakai?sslmode=disable" \
  go run github.com/na0fu3y/ochakai/cmd/ochakai@latest import-ossie examples/semantic-model.yaml
```

No proxy installed? A temporary authorized network works with no extra
tools — remember to remove it afterwards (IPv4 only):

```sh
gcloud sql instances patch ochakai --authorized-networks=$(curl -4 -s ifconfig.me)/32
export DB_IP=$(gcloud sql instances describe ochakai \
  --format='value(ipAddresses[0].ipAddress)')
OCHAKAI_DATABASE_URL="postgres://ochakai:$DB_PASSWORD@$DB_IP:5432/ochakai?sslmode=require" \
  go run github.com/na0fu3y/ochakai/cmd/ochakai@latest import-ossie examples/semantic-model.yaml
gcloud sql instances patch ochakai --clear-authorized-networks
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

## 5b. Deploy the sample web UI (separate service, by design)

The sample UI ([examples/webui](../../examples/webui)) is **not** part of
the ochakai image — the core keeps its serving surface minimal. It ships
as its own tiny service: a static page plus a reverse proxy that attaches
its service identity (`X-Serverless-Authorization`) to API calls, so
ochakai stays organization-restricted while the UI is reachable from any
machine. Browser users are recorded as the webui's service account
(`agent:ochakai-webui@…`); per-user browser identity needs IAP or MCP
OAuth (issue #5).

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

gcloud run deploy ochakai-webui \
  --image=$REGION-docker.pkg.dev/$PROJECT_ID/images/ochakai-webui:0.1 \
  --region=$REGION --allow-unauthenticated \
  --service-account=ochakai-webui@$PROJECT_ID.iam.gserviceaccount.com \
  --min-instances=0 --max-instances=1 --cpu=1 --memory=256Mi \
  --set-env-vars=OCHAKAI_URL=$OCHAKAI_URL
```

Open the webui service URL from any machine and search — no token needed
on `cloudrun-iam` deployments (leave the token field empty). The UI and
API are same-origin through the proxy, so no CORS setup is needed. Note
the trade-off: the webui itself is public, so anyone who finds its URL
can read and write through it as the webui's identity. Put IAP in front
of it if that is not acceptable.

Alternatively, host `index.html` on any static host (no container) and
allow its origin to call the REST API directly from the browser:

```sh
gcloud run services update ochakai --region=$REGION \
  --update-env-vars=OCHAKAI_CORS_ORIGINS=https://your-ui.example
```

CORS is off unless `OCHAKAI_CORS_ORIGINS` is set, origins match exactly
(no wildcards), and this path requires the browser to reach ochakai
itself — on restricted deployments that means going through the Cloud Run
proxy.

## 6. Troubleshooting in security-hardened organizations

- **`allUsers` binding fails with "do not belong to a permitted customer"**:
  the org enforces Domain Restricted Sharing
  (`iam.allowedPolicyMemberDomains`). The recommended §3 setup never needs
  `allUsers`, so keep the policy on; only §3-alt and the sample webui
  require an exception.
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

## 7. Teardown

```sh
gcloud run services delete ochakai --region=$REGION --quiet
gcloud run services delete ochakai-webui --region=$REGION --quiet
gcloud sql instances delete ochakai --quiet
```
