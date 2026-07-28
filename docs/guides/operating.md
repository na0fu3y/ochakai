# Operating a deployment

Backup and restore, what to watch, and what is known about scale. The
Cloud Run guide builds the deployment and covers the Google-side
troubleshooting; this page is about running the thing afterwards.

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

The deploy guide's [§6](../../deploy/cloudrun/README.md) has the commands
— the example instance ships with `--no-backup` to stay at ~$10/month,
so this is a deliberate step, not a default:

```sh
gcloud sql instances patch ochakai --backup-start-time=03:00 --enable-point-in-time-recovery
```

Restoring is Cloud SQL's own procedure, and it restores the database
only. **Attachment bytes are not in it**: they live in GCS as
content-addressed objects, so an instance restored from a database backup
finds attachment metadata whose bytes are whatever the bucket holds
today. Turn on object versioning, or a retention policy, if that matters
to you — and note that `purge` deliberately does not reclaim GCS blobs
(design doc 0031), so the bucket only grows.

### Exporting

```sh
ochakai export ./knowledge          # a directory
ochakai export - > okf.tar.gz       # or a stream
ochakai export --no-attachments -   # entries only; bytes are already in GCS
```

The bundle is an OKF v0.2 directory: one markdown file per entry with
YAML frontmatter, attachments as plain files beside their entry, and an
`index.md` per directory. Exporting the ten-entry demo base writes 20
files — ten entries and ten indexes.

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
database, by exporting a ten-entry base and importing it into an empty
instance:

- **Timestamps become the restore.** `created_at`, `updated_at` and
  `verified_at` on the restored entry are all the moment of the import,
  not the moment the work happened.
- **History collapses.** An entry with two revisions came back with one.
  Revisions are the audit trail of what this instance did, and a bundle
  is not that trail.
- **Provenance is re-recorded as the importer.** A bundle that *claims*
  someone else wrote and verified an entry does not get believed: editing
  the exported frontmatter to say `generated.by: human:tanaka@…` and
  `verified.by: human:sato@…`, then importing, stored the body change and
  kept both actors as the importing identity. Provenance is what an
  instance observed, never what a document asserts (design doc 0009).
- **Usage counts and outcome reports do not travel** at all.

What *does* survive is the knowledge and its trust: the verifications
came back verified, through the OKF `verified` key rather than an ochakai
alias, along with every envelope field.

So: **run the import under an identity you are willing to see on every
entry**, and if provenance matters to you, keep a database backup as
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
`GET /api/v1/knowledge?sort=verified_at&limit=1` succeeds only when the
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
| `query embedding failed; falling back to lexical-only` | Vertex AI did not answer a search; results are still returned, ranked by the lexical half alone |
| `document embedding failed` / `storing embedding failed` | an entry was written but is not in the vector index. It stays findable lexically; `ochakai reembed` repairs it |
| `attachment embedding failed` | same, for an attachment: still findable by filename |
| `backlink lookup failed` | one entry's backlinks are missing from a response |
| `export truncated after the response began` | an export failed midway. **The bytes the client received are not a complete backup** — this is the one in the list that can quietly cost you |
| `knowledge verified` / `knowledge purged` | not errors: the audit line for a curation event, with the actor |

At startup the server states its own posture, which is the fastest way to
confirm what a running instance actually has enabled:

```
"ochakai listening" addr=:8080 version=… insecure_dev=false endpoints=[/mcp /api/v1 /health]
"semantic search disabled; using lexical search only"
"attachments disabled (no OCHAKAI_GCS_BUCKET); markdown entries only"
```

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

## Capacity

### What is known

- **Search**: about 16 ms per Japanese search across 5000 entries, against
  0.2 ms for a latin word. Japanese terms are answered by scanning,
  because a trigram index cannot serve a two-character pattern.
- **Attachments**: 5 MiB per file, 20 files per entry, enforced by the
  server.
- **Usage buffer**: 20,000 events in memory, flushed every 5 seconds.
  Past the cap, events are dropped rather than queued (design doc 0029).
- **Raw usage events** are pruned after 180 days; the per-entry totals
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

Honestly: most of it. There is no measured entry-count ceiling, no
throughput figure, no latency distribution under concurrency, and no
guidance on when `db-f1-micro` stops being enough. The project is one
person's, and these numbers would be invented rather than observed.

If you are running ochakai at a scale where this matters, the useful
thing is to say so in
[Discussions](https://github.com/na0fu3y/ochakai/discussions) with what
you actually see. That is how this section gets numbers.

## Upgrades

Migrations run automatically at startup, so an upgrade is a new image
tag. The two things to read first are the
[changelog](../../CHANGELOG.md) — which marks breaking changes and says
what an operator has to do about each one — and the deploy guide's
[§8](../../deploy/cloudrun/README.md), which has the upgrade path and the
version-specific notes.

Pin a version rather than `:latest`, so a redeploy is a decision.

Two upgrade-adjacent traps worth knowing here:

- **Changing `OCHAKAI_EMBEDDING_DIM` on a database that already holds
  vectors is refused at startup.** Put it back, or drop the vector tables
  and re-embed.
- **Enabling embeddings, or changing the model, does not backfill.**
  Existing entries stay unembedded until `ochakai reembed`, which costs
  Vertex AI tokens proportional to the base.
