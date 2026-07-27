# Terraform module for the Cloud Run + Cloud SQL deployment

The baseline of [deploy/cloudrun/README.md](../cloudrun/README.md) as code,
so that a deployment can be reviewed as a diff, reproduced per environment,
and torn down cleanly.

**The gcloud guide remains the reference and the source of truth.** It
explains why each piece is shaped the way it is, covers everything this
module leaves out, and is the thing to read first. This module is a
convenience for standing up what it describes — where the two disagree, the
guide is right and this module has a bug.

## What it creates

The defaults reproduce the guide's cost-minimized example (~$10/month, almost
entirely the Cloud SQL instance):

| | |
|---|---|
| Artifact Registry | a **remote repository** proxying `ghcr.io` (§1) — Cloud Run cannot pull from GHCR directly |
| Cloud SQL | `db-f1-micro`, Postgres 17, 10 GB SSD, single zone, no backups, `cloudsql.iam_authentication=on` (§2) |
| Service account | `ochakai-run`, with `roles/cloudsql.client` + `roles/cloudsql.instanceUser`, and an IAM database login (§3) |
| Cloud Run | the ochakai service, scaled to zero, **not publicly invokable**, `OCHAKAI_DB_IAM_AUTH=true` (§3) |
| IAM | `roles/run.invoker` for the members you name — the entire access-control surface |

Optional, all off by default: private IP (§2b), Vertex AI embeddings (§4),
GCS attachments (§4b), the IAP-fronted web UI (§5b).

### No password, anywhere

There is no `random_password` in this module, no Secret Manager entry, no
service-account key, and no `password` argument on any resource. The runtime
authenticates to Postgres with Cloud SQL IAM database authentication, where
the connection password is a short-lived IAM token fetched at connect time.
That is why `OCHAKAI_DATABASE_URL` can sit in a plain environment variable,
and it is the project's central design decision, not a detail (design docs
0002, 0003).

Two consequences worth knowing before you read further:

- The module does **not** create the guide's password-holding admin user.
  Direct database access for people is an IAM login instead
  (`maintenance_users`), which is where guide §6 ends up anyway.
- Terraform therefore cannot run the one-time schema bootstrap SQL — doing so
  would mean holding a password. That step stays manual; see below.

### Never publicly invokable, with one named exception

`invoker_members` refuses `allUsers` and `allAuthenticatedUsers`. This is not
just about access: ochakai does no authorization of its own and reads the
caller identity Cloud Run has already verified in order to record provenance.
Without the IAM check in front, those headers would be forgeable, and every
recorded author would be a guess. The same rule keeps the deployment
compatible with the Domain Restricted Sharing org policy.

The exception is `public_read_only` (guide §5d, design doc 0041), which grants
`allUsers` itself. It is one variable rather than two because public is only
safe together with what it comes with: the deployment refuses every write and
reads no identity at all, so there is no provenance to forge and no author to
get wrong. A public *writable* ochakai therefore has no spelling in this
module — not a check to remember, a state with no representation.

## Usage

```sh
cd deploy/terraform
cp terraform.tfvars.example terraform.tfvars   # edit: project, region, image tag, invokers
terraform init
terraform plan
terraform apply    # Cloud SQL instance creation takes 10-15 minutes
```

Then the one manual step, and the service is ready:

```sh
terraform output -raw database_bootstrap_sql
terraform output -raw use_command
```

### The one manual step: schema bootstrap

Extensions have to be created by a `cloudsqlsuperuser` member — Cloud SQL's
pgvector is not a trusted extension — and the runtime's object grants have to
be issued by someone. Pre-creating the extensions is what lets the runtime
identity hold nothing but explicit object grants: its startup migration then
only ever hits the privilege-free `CREATE EXTENSION IF NOT EXISTS` skip path.

An IAM login never qualifies as `cloudsqlsuperuser`, so use the built-in
`postgres` user with a break-glass password you mint and discard rather than
store (guide §6):

```sh
gcloud sql users set-password postgres --instance=ochakai --prompt-for-password
cloud-sql-proxy "$(terraform output -raw sql_connection_name)" --port 55432 &
psql "host=localhost port=55432 dbname=ochakai user=postgres"
# paste: terraform output -raw database_bootstrap_sql
```

Afterwards set the password to another value you throw away. Nothing is
stored, and admin access stays IAM-gated.

If you named `maintenance_users`, let them see the tables the runtime will
own (startup migrations create them as the service account, so ownership
follows the writer, not the deployer), in the same session:

```sql
GRANT "<database_user output>" TO "you@your-org.example";
```

They connect with `cloud-sql-proxy --auto-iam-authn` and their own identity
from then on — no password on that path either.

## Options

| Variable | Guide | Effect |
|---|---|---|
| `enable_private_ip` | §2b | Drops the Cloud SQL public endpoint; peering range + Direct VPC egress on Cloud Run. Free. Local `cloud-sql-proxy` access then needs a VPC-attached workstation or a temporary public IP. |
| `enable_vertex_embeddings` | §4 | `roles/aiplatform.user` + `OCHAKAI_VERTEX_PROJECT`. Search becomes hybrid. Entries written before this was on stay unembedded until `ochakai reembed`. |
| `enable_gcs_attachments` | §4b | Bucket + `roles/storage.objectUser` + `OCHAKAI_GCS_BUCKET`. Without it, attach operations return 501. |
| `enable_webui` | §5b | `serve-ui` as a second Cloud Run service behind IAP, same image, dedicated identity. `webui_records_browser_user` (default on) records the person in the browser rather than the UI's service account. |
| `read_only` | §5d | `OCHAKAI_READ_ONLY`. Serves the base without changing it; still private. Seed the base *before* turning it on — a read-only deployment cannot be imported into. |
| `public_read_only` | §5d | `OCHAKAI_PUBLIC_READ_ONLY` + the module's only `allUsers` grant. The public demo: no account needed, no identity read, nothing writable (implies `read_only`). Apply writable, `ochakai import examples/demo`, then re-apply with this on. |
| `maintenance_users` | §6 | Cloud SQL IAM logins for people, no passwords. |
| `database_backups` | §6 | Daily backups. Off by default for cost; turn it on for anything you care about. |

Enabling the web UI pulls in the `google-beta` provider: IAP on Cloud Run
(`iap_enabled`) is still a beta surface. The provider is declared either way,
so it is downloaded even when the UI is off, where it does nothing.

## What this module does not cover

Deliberately. Each is in the guide; reach for it there.

- **The schema bootstrap SQL** (§3) — needs a database password, see above.
- **Loading knowledge and connecting Claude Code** (§5) — client-side, and
  the CLI needs no configuration beyond `ochakai use`.
- **Delegated provenance for your own application** (§5c) — the variable is
  here (`delegating_callers`), but the application and its service account
  are yours to deploy.
- **Org-policy guardrails** (§6: `sql.restrictAuthorizedNetworks`,
  `sql.restrictPublicIp`, `iam.allowedPolicyMemberDomains`) — they belong at
  the organization level, above any one deployment, and need the Org Policy
  Administrator role.
- **Retiring the password admin user** (§6) — nothing to retire: this module
  never creates one.
- **`--ssl-mode=ENCRYPTED_ONLY`, point-in-time recovery, Binary
  Authorization, Cloud SQL data-access audit logs** (§6) — hardening beyond
  the baseline, one `gcloud` command or console setting each.
- **Upgrading an existing deployment** (§8) — change `image_tag` and apply;
  migrations run at startup. The version notes in §8 still apply, and
  importing an existing gcloud-built deployment into this module's state is
  not something this module tries to make easy.

## Teardown

`deletion_protection` defaults to true and guards both the Cloud SQL instance
and the Cloud Run services at the Terraform and the API level, so it has to
come off first:

```sh
terraform apply -var deletion_protection=false
terraform destroy
```

If attachments are enabled, the bucket refuses to be destroyed while it holds
objects — those are the only copy of every file anyone attached. Add
`-var gcs_force_destroy=true` to the same `apply` when you mean it.

Enabled APIs are left enabled: turning one off is a project-wide act and
other things may depend on it.
