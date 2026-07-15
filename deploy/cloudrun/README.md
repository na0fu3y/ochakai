# Deploying ochakai on Cloud Run + Cloud SQL

The recommended initial setup: Cloud Run scales to zero and the smallest
Cloud SQL instance carries the whole knowledge base. ochakai's only hard
dependency is PostgreSQL, so everything below is one container image plus
one database.

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

export IMAGE=$REGION-docker.pkg.dev/$PROJECT_ID/ghcr/na0fu3y/ochakai:0.1.0
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
  --no-backup

gcloud sql databases create ochakai --instance=ochakai

export DB_PASSWORD=$(openssl rand -hex 24)
gcloud sql users create ochakai --instance=ochakai --password=$DB_PASSWORD
```

Notes:

- `--no-backup` keeps the example cheap; enable backups for anything you
  care about (`gcloud sql instances patch ochakai --backup`).
- Users created through Cloud SQL get `cloudsqlsuperuser`, so ochakai can
  run `CREATE EXTENSION` (pg_trgm, and vector if you enable embeddings)
  during its automatic migration.

## 3. Deploy Cloud Run

Grant the Cloud Run service account access to Cloud SQL. In projects
created since 2024 the default compute service account has no roles, so
this step is required (the container exits at startup with
`cloudsql.instances.get ... NOT_AUTHORIZED` without it):

```sh
export SERVICE_ACCOUNT=$(gcloud projects describe $PROJECT_ID \
  --format='value(projectNumber)')-compute@developer.gserviceaccount.com
gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member=serviceAccount:$SERVICE_ACCOUNT --role=roles/cloudsql.client
```

ochakai authenticates clients with static bearer tokens that map to an
actor (`human` or `agent`) for provenance. Generate one per client:

```sh
export AGENT_TOKEN=$(openssl rand -hex 24)
export HUMAN_TOKEN=$(openssl rand -hex 24)

gcloud run deploy ochakai \
  --image=$IMAGE \
  --region=$REGION \
  --allow-unauthenticated \
  --min-instances=0 --max-instances=1 \
  --cpu=1 --memory=512Mi \
  --add-cloudsql-instances=$PROJECT_ID:$REGION:ochakai \
  --set-env-vars="OCHAKAI_DATABASE_URL=postgres:///ochakai?host=/cloudsql/$PROJECT_ID:$REGION:ochakai&user=ochakai&password=$DB_PASSWORD" \
  --set-env-vars="^@^OCHAKAI_CLIENTS=$AGENT_TOKEN=agent:claude-code,$HUMAN_TOKEN=human:$(whoami)"

export OCHAKAI_URL=$(gcloud run services describe ochakai --region=$REGION --format='value(status.url)')
curl $OCHAKAI_URL/health
```

Notes:

- `^@^` changes the env-var delimiter so `OCHAKAI_CLIENTS` may contain commas.
- `--allow-unauthenticated` exposes the service publicly, protected by
  ochakai's bearer tokens; every endpoint except `/health` requires one.
  Use `/health`, not `/healthz`: Google Frontends intercept `/healthz` on
  `run.app` URLs and return their own 404 without ever reaching the app.
  For private networking, drop the flag and front it with IAM/IAP instead.
- Container port defaults are fine: ochakai honors Cloud Run's `PORT`.
- Environment variables are visible to project viewers in the console.
  For production, store `OCHAKAI_DATABASE_URL` and `OCHAKAI_CLIENTS` in
  Secret Manager and use `--set-secrets` instead of `--set-env-vars`.

## 3b. Recommended: restrict access to your organization

`--allow-unauthenticated` leaves the URL reachable by anyone (protected
only by ochakai's bearer tokens). To make the service unreachable from
outside your Google Workspace / Cloud Identity organization, use Cloud Run
IAM as a second, network-level layer:

```sh
gcloud run services remove-iam-policy-binding ochakai --region=$REGION \
  --member=allUsers --role=roles/run.invoker
gcloud run services add-iam-policy-binding ochakai --region=$REGION \
  --member=domain:your-org.example --role=roles/run.invoker
```

Auth becomes two-layer: Cloud Run IAM decides **who can reach** the
service (org members only; anonymous requests get Google's 401 without
ever hitting the container), and `OCHAKAI_CLIENTS` decides **which actor**
(human/agent) they act as for provenance.

Clients with static headers (Claude Code MCP, curl) go through the
[Cloud Run proxy](https://cloud.google.com/sdk/gcloud/reference/run/services/proxy),
which handles the Google identity layer transparently and passes your
`Authorization` header (the ochakai token) through untouched:

```sh
gcloud run services proxy ochakai --region=$REGION --port=8787
claude mcp add --transport http ochakai http://localhost:8787/mcp \
  --header "Authorization: Bearer $AGENT_TOKEN"
```

Server-to-server callers instead send the Google ID token in
`X-Serverless-Authorization` (Cloud Run strips it before your app sees
it), keeping `Authorization` free for the ochakai token.

This setup never needs an `allUsers` grant, so it is compatible with —
and a good reason to keep — the Domain Restricted Sharing org policy
(`iam.allowedPolicyMemberDomains`).

## 4. Optional: enable hybrid semantic search (Vertex AI)

Embeddings are off by default (trigram-only search, no external calls).
Enabling them uses the Cloud Run service identity via ADC — no API keys.

```sh
gcloud services enable aiplatform.googleapis.com

export SERVICE_ACCOUNT=$(gcloud run services describe ochakai --region=$REGION \
  --format='value(spec.template.spec.serviceAccountName)')
gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member=serviceAccount:$SERVICE_ACCOUNT --role=roles/aiplatform.user

gcloud run services update ochakai --region=$REGION \
  --update-env-vars=OCHAKAI_EMBEDDING_PROVIDER=vertex,OCHAKAI_VERTEX_PROJECT=$PROJECT_ID
```

Use `--update-env-vars`, not `--set-env-vars`: the latter **replaces** all
environment variables and would wipe `OCHAKAI_DATABASE_URL`.

On the next start, ochakai creates the pgvector table and embeds new and
updated knowledge with `gemini-embedding-001`. Search becomes hybrid
(trigram + vector, reciprocal rank fusion). If Vertex AI is ever
unavailable, writes and searches degrade gracefully to trigram-only.

## 5. Load a semantic model and connect an agent

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
tools — remember to remove it afterwards:

```sh
gcloud sql instances patch ochakai --authorized-networks=$(curl -s ifconfig.me)/32
export DB_IP=$(gcloud sql instances describe ochakai \
  --format='value(ipAddresses[0].ipAddress)')
OCHAKAI_DATABASE_URL="postgres://ochakai:$DB_PASSWORD@$DB_IP:5432/ochakai?sslmode=require" \
  go run github.com/na0fu3y/ochakai/cmd/ochakai@latest import-ossie examples/semantic-model.yaml
gcloud sql instances patch ochakai --clear-authorized-networks
```

Connect Claude Code:

```sh
claude mcp add --transport http ochakai $OCHAKAI_URL/mcp \
  --header "Authorization: Bearer $AGENT_TOKEN"
```

Smoke test over REST:

```sh
curl -H "Authorization: Bearer $AGENT_TOKEN" "$OCHAKAI_URL/api/v1/knowledge?q=revenue"
curl -H "Authorization: Bearer $AGENT_TOKEN" -X POST "$OCHAKAI_URL/api/v1/compile" \
  -d '{"metrics":["revenue"],"dimensions":["customers.region"],"dialect":"bigquery"}'
```

## 5b. Deploy the sample web UI (separate service, by design)

The sample UI ([examples/webui](../../examples/webui)) is **not** part of
the ochakai image — the core keeps its serving surface minimal. It ships
as its own tiny service: a static page plus a reverse proxy that attaches
this service's identity token (`X-Serverless-Authorization`) to API
calls, so **ochakai stays organization-restricted (§3b)** while the UI is
reachable from any machine. The client's `Authorization` header (the
ochakai token choosing the actor) passes through untouched.

```sh
# build & push (from the repository root)
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

Open the webui service URL from any machine, paste a client token, and
search. The UI and API are same-origin through the proxy, so no CORS
setup is needed. Note the trade-off: the webui itself is public here
(token-gated only) — anyone reaching it can attempt API calls, so treat
it like the public-endpoint variant of §3. To require Google login in
the browser too, put IAP in front of the webui service, or wait for
first-class MCP OAuth (issue #5).

Alternatively, host `index.html` on any static host (no container) and
allow its origin to call the REST API directly from the browser:

```sh
gcloud run services update ochakai --region=$REGION \
  --update-env-vars=OCHAKAI_CORS_ORIGINS=https://your-ui.example
```

CORS is off unless `OCHAKAI_CORS_ORIGINS` is set, origins match exactly
(no wildcards), and this path requires the browser to reach ochakai
itself — combine with §3b's proxy on restricted deployments.

## 6. Troubleshooting in security-hardened organizations

- **`allUsers` binding fails with "do not belong to a permitted customer"**:
  the org enforces Domain Restricted Sharing
  (`iam.allowedPolicyMemberDomains`). Either request a project-level
  exception, or skip `--allow-unauthenticated` and call the service with
  Google ID tokens (Cloud Run accepts the app's bearer token in
  `Authorization` and the Google ID token in `X-Serverless-Authorization`
  simultaneously).
- **`run.app` returns Google's HTML 404 ("That's an error") even though
  the service is Ready**: before suspecting infrastructure, test a real
  application endpoint (e.g. `/api/v1/knowledge?q=x` with a token) and
  check request logs. Two Google Frontend behaviors conspire to make a
  healthy service look dead:
  1. `/healthz` is intercepted by Google Frontends on `run.app` and 404s
     without ever reaching the container (and without request logs). Use
     `/health`.
  2. Application-level 404 responses are dressed up as Google's branded
     404 page, so an unhandled path looks like a routing failure.
  A genuinely unknown service URL returns a much shorter 404 page
  (~272 bytes vs ~1.5 KB) — comparing `content-length` tells them apart.

## 7. Teardown

```sh
gcloud run services delete ochakai --region=$REGION --quiet
gcloud sql instances delete ochakai --quiet
```
