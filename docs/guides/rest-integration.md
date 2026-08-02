# Embedding the REST API in your own service

ochakai's REST API is a small surface meant to be called directly by your
own web service — a data-analytics chat app, an internal agent with a
browser front end — rather than only from the CLI or an MCP client. One
[OpenAPI file](../../api/openapi.yaml) is the whole contract; this page is
everything else that file's schemas alone do not say: how to authenticate,
how not to collapse every user of your product into one identity, and how
to write without racing another writer.

If you are deploying ochakai itself rather than building against one that
is already running, start with the
[deploy guide](../../deploy/cloudrun/README.md) instead.

**REST is frozen at `/api/v1`** (design doc
[0064](../design/0064-rest-stops-at-api-v1.md)): the contract this page
walks through does not move except for a security fix, unlike MCP, the
CLI and the stored shape, which can still change in a minor release —
[docs/compatibility.md](../compatibility.md) has the full policy.

## Authenticating

Cloud Run IAM decides who reaches ochakai (design docs
[0002](../design/0002-authn-authz.md), [0003](../design/0003-gcp-only.md))
— there is no API key or bearer token ochakai itself issues or checks.
Every request carries a Google-signed **ID token**, audience-bound to your
ochakai service's URL, as `Authorization: Bearer <token>`; Cloud Run
verifies it before the request ever reaches the container, and ochakai
reads the identity it forwards only to record provenance.

Your service account needs `roles/run.invoker` on the ochakai service:

```sh
gcloud run services add-iam-policy-binding ochakai \
  --region="$REGION" \
  --member="serviceAccount:$YOUR_SERVICE_ACCOUNT" \
  --role=roles/run.invoker
```

From a shell, mint a token with the CLI you already have:

```sh
TOKEN=$(gcloud auth print-identity-token --audiences="$OCHAKAI_URL")
curl -H "Authorization: Bearer $TOKEN" "$OCHAKAI_URL/api/v1/search?q=revenue"
```

From your own service, mint one with a client library instead of shelling
out — every major Google Cloud client library has an ID-token helper.
Python's:

```python
import google.auth.transport.requests
import google.oauth2.id_token

request = google.auth.transport.requests.Request()
token = google.oauth2.id_token.fetch_id_token(request, audience=OCHAKAI_URL)
```

[`examples/bigquery-catalog`](../../examples/bigquery-catalog/bundle/jobs/sync-bigquery-catalog/sync-bigquery-catalog.py)
is a complete, working example of this — reading BigQuery with Application
Default Credentials and writing to ochakai with a minted ID token, holding
no secret of its own.

## Delegated provenance: forwarding who used your product

Without this, every concept your users write is recorded under your
application's one service-account identity, and provenance — most of what
ochakai sells — collapses into a single name.

Let ochakai know your application forwards identities on behalf of its
users (design doc [0027](../design/0027-delegated-provenance.md)). Both
identities are recorded — `human:tanaka@… via process:app-sa@…` — never
just the forwarded one, so a write made through your application stays
distinguishable from one the person made directly.

```sh
# In addition to roles/run.invoker (Authenticating, above): allow your
# application's identity, and only it, to speak for its users.
gcloud run services update ochakai --region="$REGION" \
  --update-env-vars="OCHAKAI_DELEGATING_CALLERS=$YOUR_SERVICE_ACCOUNT"
```

Send the header with each request:

```
Ochakai-On-Behalf-Of: human:tanaka@example.co.jp
```

The kind (`human:` / `process:`) is required and never guessed — your
application knows whether it is forwarding a person or another piece of
software.

Notes:

- **Delegation is off by default.** With `OCHAKAI_DELEGATING_CALLERS`
  unset, the header is a **403**, not a silent downgrade: an application
  that believes it writes as its users must not discover months later
  that every concept says otherwise.
- **A 403 mentioning the header means the caller is not on the list.**
  Compare the service account you granted `run.invoker` to (Authenticating,
  above) with the value here — they are the same email, and a mismatch is
  the usual cause. A **400** means the header itself is malformed (missing
  kind, whitespace in the identity).
- `OCHAKAI_DELEGATING_CALLERS` takes a comma-separated list; `*` trusts
  every authenticated caller, sensible only when IAM already admits
  nothing but your own applications.
- **This is not authorization.** It decides whose identity is recorded,
  not what anyone may do — every caller that reaches ochakai can already
  read and write everything (design doc
  [0002](../design/0002-authn-authz.md)). Reachability stays IAM's job.

## Declaring what wrote it: Ochakai-Producer

Name your software beside the identity that vouches for it (design doc
[0052](../design/0052-producer-beside-the-actor.md)):

```
Ochakai-Producer: insightflow/1.4.0
```

One slash, both halves non-empty — `<producer>/<version>`. It is recorded
as `Actor.producer`, beside the authenticated actor and never in place of
it (`human:tanaka@… using insightflow/1.4.0`), so a concept written through
your product still says who wrote it. No allowlist gates it: it names your
own build, not somebody else's identity. A malformed value is a 400.

## Concurrent writes: ETag and If-Match

Last write wins, unless you ask for better. Every read and write against
`/api/v1/bundle/{path}` returns an `ETag` — the hash of the concept's
canonical OKF document, quoted, and also in the body as
`summary.content_hash` — and a `PUT` carrying `If-Match` with a stale value
gets `412` and writes nothing (design docs
[0030](../design/0030-optimistic-locking.md), 0043 §3.4):

```sh
etag=$(curl -si "$OCHAKAI_URL/api/v1/bundle/metrics/revenue.md" \
  -H "Authorization: Bearer $TOKEN" | grep -i '^etag:' | cut -d' ' -f2 | tr -d '\r')
curl -X PUT "$OCHAKAI_URL/api/v1/bundle/metrics/revenue.md" \
  -H "Authorization: Bearer $TOKEN" -H "If-Match: $etag" \
  -H "Content-Type: text/markdown" --data-binary @revenue.md
```

It is a hash of the content alone, so verifying, rejecting or attaching a
file to a concept leaves a held precondition valid — only an edit
invalidates it.

## Local development

`deploy/compose.yaml` and `OCHAKAI_MODE=dev` both turn authentication off:
every caller is `human:anonymous`, so a malformed `Ochakai-On-Behalf-Of`
or `Ochakai-Producer` header fails on your machine instead of on first
deploy against Cloud Run. See
[Requirements and configuration](../configuration.md#environment-variables)
(Japanese) for `OCHAKAI_MODE`'s other postures.

## See also

- [api/openapi.yaml](../../api/openapi.yaml) — the checked contract: every
  request and response is validated against it in CI.
- [Requirements and configuration](../configuration.md) (Japanese) — every
  environment variable named above.
- [examples/bigquery-catalog](../../examples/bigquery-catalog) — a
  complete application-shaped integration: reads a warehouse, writes
  ochakai, holds no secret.
