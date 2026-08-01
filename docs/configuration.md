# Requirements and configuration

## Requirements

**Running it for real means Google Cloud.** Cloud Run's IAM check decides who
reaches a deployment and Cloud SQL authenticates the service account, which
is how ochakai holds no secret of its own — and it is also why there is no
supported way to run it on another cloud or on-prem (design docs
[0002](design/0002-authn-authz.md), [0003](design/0003-gcp-only.md)). What is
portable is the knowledge, not the runtime: it round-trips through OKF
bundles, so leaving does not depend on what you are leaving.
`deploy/compose.yaml` runs the same binary locally with authentication
switched off — a development harness, not the small end of production.

**There is no authorization.** Reachability is the whole access model — see
[Authentication has no configuration](#authentication-has-no-configuration)
below.

**PostgreSQL, plus `pg_trgm`.** The first migration creates the extension, so
a database whose user may not `CREATE EXTENSION` needs an admin to create it
once — that is what the deploy guide's bootstrap SQL is for. Semantic search
adds `vector` (pgvector) the same way — it is on by default on Google Cloud,
and a database that cannot hold vectors makes a discovered deployment fall
back to lexical search rather than fail. Without it, plain PostgreSQL is
enough. CI and the deploy guide both exercise Postgres 17.

**The server needs Docker; the client commands need a binary.** The
README's quick start runs its one CLI command, `import`, inside the
compose container (`docker compose exec ochakai /ochakai import ...`), so
nothing beyond Docker is needed to follow it. Past that — `ochakai use`,
`ochakai context`, the web UI — take a
[release archive](https://github.com/na0fu3y/ochakai/releases), or
`go run ./cmd/ochakai`, which wants the toolchain named in `go.mod` (Go
1.21 and newer download it for you). Or talk to the API directly, since
that is all the CLI does — see [Writing knowledge](knowledge.md) for the
one-line `curl` version of a write.

## Environment variables

| Env var | Description |
|---|---|
| `OCHAKAI_DATABASE_URL` | Cloud SQL connection string (required) |
| `OCHAKAI_DB_IAM_AUTH` | `true` enables Cloud SQL IAM database authentication: the connection password is a short-lived IAM token, so the connection string carries no secret |
| `OCHAKAI_GCS_BUCKET` | Bucket for attachment bytes (auth is ADC — no keys). Default: unset — the instance stores markdown concepts only and attach operations return an error |
| `OCHAKAI_EMBEDDINGS` | `off` runs lexical-only on a deployment that would otherwise get hybrid semantic search. Running on Google Cloud, ochakai reads its own project from the metadata server and turns embeddings on with it — there is nothing to set, and whether it can actually call Vertex AI is decided by IAM (`roles/aiplatform.user`) rather than by configuration, asked once at startup (design doc [0053](design/0053-embeddings-by-default.md)). Off Google Cloud there is no metadata server and nothing is enabled. Takes `on` or `off`; any other spelling is a startup error rather than a guess. Default: on where it is discovered |
| `OCHAKAI_VERTEX_PROJECT` | Names the Vertex AI project explicitly — for a project other than the one ochakai runs in, or for running it outside Google Cloud. A deployment that names one has asked for semantic search: if Vertex AI or pgvector is not there it refuses to start, where a discovered one falls back to lexical search. Auth is ADC — no API keys |
| `OCHAKAI_VERTEX_LOCATION` / `OCHAKAI_VERTEX_MODEL` / `OCHAKAI_EMBEDDING_DIM` | Embedding details (defaults: `us-central1`, `gemini-embedding-001`, 768). For image/PDF attachment search set model `gemini-embedding-2` with location `global` (or `us`/`eu`). Vectors are written when a concept is written, so a base loaded before embeddings were reachable — or a changed model — leaves concepts unembedded until you run `ochakai reembed` (design doc [0020](design/0020-attachment-search.md)). Dimensions above 2000 exceed pgvector's indexing limit, so those deployments fall back to an exact scan. Changing `OCHAKAI_EMBEDDING_DIM` on a base that already holds vectors rebuilds the vector tables at the new width on the next start: a vector is derived from the concept it describes, so nothing curated is lost, and `ochakai reembed` refills them |
| `OCHAKAI_DELEGATING_CALLERS` | Comma-separated caller identities allowed to forward an end user's identity with `X-Ochakai-On-Behalf-Of: human:tanaka@example.co.jp` (`*` for any authenticated caller). For applications that embed ochakai and serve many people — without it, every one of their users collapses into the application's one service account. Both identities are recorded (`human:tanaka@… via process:app-sa@…`), never just the forwarded one. Default: empty, delegation off; a header from an unlisted caller is a 403, not a silent downgrade (design doc [0027](design/0027-delegated-provenance.md)) |
| `OCHAKAI_PRODUCER` | `ochakai` CLI only: the software running the CLI, as `<producer>/<version>` — e.g. `claude-code/2026.07`. It goes out as `X-Ochakai-Producer` and is recorded **beside** the actor (`human:tanaka@… using claude-code/2026.07`), never in its place, so an agent shelling out to the CLI stops writing under the operator's name alone. Any authenticated caller may send the header — it names the caller's own build, not somebody else's identity, so no allowlist gates it — and a malformed value is a 400, not a silent drop. On the MCP surface the same value is taken from the client's `initialize` info when the header is absent. Default: unset, nothing declared (design doc [0052](design/0052-producer-beside-the-actor.md)) |
| `OCHAKAI_IAP_AUDIENCE` | `ochakai serve-ui` only: the IAP JWT audience to verify, which turns browser edits from `process:<webui-sa>` into the person signed in (`human:tanaka@… via process:<webui-sa>`). Requires the webui's service account in the server's `OCHAKAI_DELEGATING_CALLERS`. Once set, a request IAP did not sign is refused rather than recorded as the service account. Default: empty, per-user provenance off (design doc [0032](design/0032-webui-iap-identity.md)) |
| `OCHAKAI_MODE` | What this deployment **is**, as one word — the postures are exclusive, so only one can be spelled (design doc [0060](design/0060-one-word-for-the-posture.md)). Unset is the ordinary posture: Cloud Run IAM decides who reaches it, and whoever reaches it reads and writes. `read-only` serves knowledge without changing it — every write is a 403, MCP does not offer the write tools at all, and the web UI stops drawing buttons that would only fail; for a reference-only instance or for freezing a base during a migration. It is not authorization: it does not look at the caller, and it refuses the operator too (design doc [0040](design/0040-read-only-mode.md)). `public` is the posture for a deployment anyone may reach — a demo, or a reference-only copy handed out. It **reads no identity at all**: the `Authorization` header is ignored (without Cloud Run IAM in front nothing verified its signature, so believing it would let any caller name any person), delegation is ignored, every caller is `human:anonymous`, and nobody is refused. It **includes** read-only, because not recording who asked is only defensible when nothing is written (design doc [0042](design/0042-public-read-only.md)). `dev` is local development only: authentication is off and everything acts as `human:anonymous` — never on a deployment. Usage telemetry records under every posture, being the server's own observation. A spelling that is none of these is a startup error rather than a guess. Default: unset |
| `OCHAKAI_RECORD_MISSES` | `false` stops ochakai keeping the searches that found nothing. A miss is the one thing here that a caller typed rather than curated, so it has a switch: with it off, `ochakai stats` reports `misses -` rather than zero, and the rest of the loop is measured as before. A public deployment (`OCHAKAI_MODE=public`) keeps none either way — it reads no identity, so it does not collect what strangers asked for. Kept 180 days, like the raw usage events (design doc [0051](design/0051-instance-metrics-and-search-misses.md)). Default: on |
| `PORT` | Listen port (default `8080`) |

Client commands read `OCHAKAI_URL` — the server to talk to, overriding the
`ochakai use` selection. [CLI reference](cli.md) has the rest.

## Authentication has no configuration

Whoever can reach a deployment can read and write everything. If you need
per-concept permissions, this is the wrong tool — the
[README's refusal table](../README.md#what-it-refuses) says what that buys.

ochakai reads the caller identity that Cloud Run forwards after its IAM check
(`human:<email>` for people, `process:<sa-email>` for service accounts) and
records it as provenance; nothing else consults it. Reachability — deciding
who may reach a deployment at all — is entirely Cloud Run IAM's job.
`OCHAKAI_MODE` above narrows what a reachable caller can do (`read-only`) or
stops recording who they are at all (`public`); neither is authorization,
because neither looks at who is asking.

The complete, cost-minimized deployment walkthrough (~$10/month) lives in
[deploy/cloudrun/README.md](../deploy/cloudrun/README.md), including a
security hardening checklist (org-policy guardrails, private IP, retiring the
last password, and more).
