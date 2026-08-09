# Required. Everything else has a default that reproduces the ~$10/month
# baseline of deploy/cloudrun/README.md.

variable "project_id" {
  description = "Google Cloud project that will hold every resource in this module."
  type        = string
}

variable "region" {
  description = <<-EOT
    Region for Cloud Run, Cloud SQL, Artifact Registry and (if enabled) the
    files bucket. Deliberately has no default: the choice is a latency
    and cost decision. The guide's cost table is quoted for us-central1;
    Asian regions such as asia-northeast1 cost slightly more.
  EOT
  type        = string
}

variable "image_tag" {
  description = <<-EOT
    Release tag of ghcr.io/na0fu3y/ochakai to run, without the leading "v"
    (for example "0.13.0"). Pinned rather than "latest" so that a redeploy is
    a decision and shows up as a diff. See the releases page:
    https://github.com/na0fu3y/ochakai/releases
  EOT
  type        = string

  validation {
    condition     = var.image_tag != "latest"
    error_message = "Pin a released version instead of \"latest\", so that a redeploy is a decision (deploy/cloudrun/README.md §1)."
  }
}

# --- Naming ---------------------------------------------------------------

variable "name" {
  description = "Base name for the Cloud Run service, the Cloud SQL instance, the database and the runtime service account. The webui service is named \"<name>-webui\"."
  type        = string
  default     = "ochakai"
}

variable "image_repository" {
  description = "Path of the image inside ghcr.io. Change only when running a fork."
  type        = string
  default     = "na0fu3y/ochakai"
}

variable "artifact_registry_repository_id" {
  description = "ID of the Artifact Registry remote repository that proxies ghcr.io."
  type        = string
  default     = "ghcr"
}

# --- Access ---------------------------------------------------------------

variable "invoker_members" {
  description = <<-EOT
    IAM members granted roles/run.invoker on the ochakai service, in the
    "domain:your-org.example" / "user:..." / "serviceAccount:..." form. This
    is the entire access-control surface: whoever can reach ochakai can read
    and write everything, and ochakai itself authorizes nothing (design doc
    0002). Empty by default, so a fresh apply reaches nobody but the deployer.
  EOT
  type        = list(string)
  default     = []

  validation {
    # The identity headers ochakai trusts for provenance are only meaningful
    # behind Cloud Run's IAM check. A public service would let any caller
    # claim to be anyone, so this state is made unrepresentable rather than
    # merely discouraged (deploy/cloudrun/README.md §3).
    #
    # There is exactly one posture where public is intended, and it is not
    # reachable from here: var.public_read_only grants allUsers itself, and
    # only together with the flag that stops the service believing anyone.
    # Keeping the two in one variable is what makes "public and writable"
    # unrepresentable instead of merely checked.
    condition     = !contains(var.invoker_members, "allUsers") && !contains(var.invoker_members, "allAuthenticatedUsers")
    error_message = "ochakai must never be publicly invokable by naming it here: its provenance headers are only trustworthy behind Cloud Run IAM. Grant a domain, group, user or service account instead — or, for the public read-only demo, set public_read_only = true, which grants allUsers itself and only alongside OCHAKAI_MODE=public (deploy/cloudrun/README.md §5d)."
  }
}

# --- Posture: read-only, and the one intended public deployment -----------

variable "read_only" {
  description = <<-EOT
    Serve the knowledge base without changing it (OCHAKAI_MODE=read-only, design
    doc 0040): every write is a 403, MCP does not offer the write tools at
    all, and the web UI stops drawing buttons that would only fail. For a
    reference-only instance, or for freezing a base during a migration or an
    audit. The service stays private and still records who reached it; this
    is not the public demo below.

    It is not authorization — it does not look at the caller, and it refuses
    the operator too. Note the ordering it forces: a read-only deployment
    cannot be seeded, so import first and set this afterwards
    (deploy/cloudrun/README.md §5d).
  EOT
  type        = bool
  default     = false
}

variable "sandbox" {
  description = <<-EOT
    The disposable public sandbox (OCHAKAI_MODE=sandbox, design doc 0087).
    The second posture where a publicly invokable ochakai is intended, and
    the only writable one: the module grants allUsers roles/run.invoker,
    so anyone with the URL can draft a concept, verify it and report an
    outcome — the loop this product is about, which the read-only demo
    could never show.

    What makes it safe is that nothing here is kept. Anyone may write, so
    nobody is identified (the Authorization header is unverifiable without
    Cloud Run IAM in front, and believing it would let any caller name any
    person), and **you restore the base on a schedule** — the module does
    not do that for you; deploy/../docs/guides/operating.md has the job.
    The server says so on its own: GET /api/v1/stats answers
    sandbox: true and the bundled web UI carries a banner, because a
    sandbox that does not announce itself steals the work somebody
    curated into it.

    Never point this at a knowledge base you mind losing, and never
    combine it with public_read_only — the two are different postures and
    the server takes one word.
  EOT
  type        = bool
  default     = false
}

variable "public_read_only" {
  description = <<-EOT
    The public read-only demo (OCHAKAI_MODE=public, design doc 0042).
    This is the one posture where a publicly invokable ochakai is intended:
    the module grants allUsers roles/run.invoker, so anyone with the URL can
    read the base with no Google account and no token.

    It is safe only because of the two things it gives up at once, and they
    are one variable for that reason. Nothing can be written: this implies
    var.read_only and cannot be separated from it, so a publicly readable
    and writable ochakai is not a configuration the server accepts. And no
    identity is believed: the Authorization header is not read at all — its
    signature is unverifiable without Cloud Run IAM in front —
    Ochakai-On-Behalf-Of is ignored, and every caller is human:anonymous. Provenance would be forgeable on a public service, so
    none is read; nothing is written, so there is none to record.

    Everything else in this module stays private. Do not reach for this to
    "share a knowledge base more easily": for anything but a demo or a
    reference-only copy, Cloud Run IAM is the answer.
  EOT
  type        = bool
  default     = false
}

variable "delegating_callers" {
  description = <<-EOT
    Callers allowed to forward an end user's identity with the
    Ochakai-On-Behalf-Of header (OCHAKAI_DELEGATING_CALLERS, design doc
    0027). For applications that embed ochakai and serve many people;
    without it every one of their users collapses into the application's one
    service account. Both identities are always recorded. The webui's own
    service account is added automatically when var.enable_webui and
    var.webui_records_browser_user are both true.
  EOT
  type        = list(string)
  default     = []
}

variable "maintenance_users" {
  description = <<-EOT
    Email addresses of people who need to connect to the database directly
    (cloud-sql-proxy --auto-iam-authn) for maintenance. Each gets a Cloud SQL
    IAM login and roles/cloudsql.instanceUser — no password, which is the
    whole point (deploy/cloudrun/README.md §6, "Retire the last password").
    This module never creates the guide's password-holding admin user.
  EOT
  type        = list(string)
  default     = []
}

# --- Database shape -------------------------------------------------------

variable "database_version" {
  description = "Cloud SQL Postgres version."
  type        = string
  default     = "POSTGRES_17"
}

variable "database_tier" {
  description = "Cloud SQL machine type. db-f1-micro is the cheapest viable instance and carries the whole knowledge base at the scale a curated base reaches."
  type        = string
  default     = "db-f1-micro"
}

variable "database_disk_size_gb" {
  description = "Cloud SQL disk size in GB. Auto-growth is off, so this is a hard ceiling and a hard cost ceiling."
  type        = number
  default     = 10
}

variable "database_backups" {
  description = "Enable daily backups. On by default: the database is the knowledge base, and losing it to a zonal incident is the one cost this module must not minimize. Set to false only for a throwaway evaluation (deploy/cloudrun/README.md §6)."
  type        = bool
  default     = true
}

variable "database_backup_start_time" {
  description = "HH:MM UTC start time for backups. Only used when var.database_backups is true — enabling backups means picking a time."
  type        = string
  default     = "03:00"
}

variable "deletion_protection" {
  description = <<-EOT
    Guard the Cloud SQL instance and the Cloud Run service against
    `terraform destroy`. True by default: the database is the knowledge base.
    Set to false and apply once before tearing the deployment down.
  EOT
  type        = bool
  default     = true
}

# --- Optional: private IP (guide §2b) -------------------------------------

variable "enable_private_ip" {
  description = <<-EOT
    Remove the Cloud SQL public endpoint entirely and reach the instance over
    private services access, with Direct VPC egress on Cloud Run. Costs
    nothing extra. The trade-off is that local admin access (cloud-sql-proxy)
    then needs a VPC-attached workstation or a temporary public IP.
  EOT
  type        = bool
  default     = false
}

variable "network" {
  description = "VPC network used when var.enable_private_ip is true."
  type        = string
  default     = "default"
}

variable "subnetwork" {
  description = "Subnetwork in var.region used for Cloud Run's Direct VPC egress when var.enable_private_ip is true."
  type        = string
  default     = "default"
}

# --- Vertex AI embeddings, on by default (guide §4) -----------------------

variable "enable_vertex_embeddings" {
  description = <<-EOT
    Search is hybrid (trigram + vector, reciprocal rank fusion) using Vertex AI
    embeddings through the service identity — no API keys. On by default: this
    grants roles/aiplatform.user and enables the API, and ochakai finds the
    project it runs in by itself (design doc 0053). It is what makes search work
    on a Japanese knowledge base, where the trigram index degrades to a scan.

    Set it to false to run lexical-only: the role is not granted and
    OCHAKAI_EMBEDDINGS=off is set, so ochakai calls no external API at all.
    To pick a particular model, region or project instead, see
    var.embedding_model — the same one variable carries all three.
    Worth knowing before leaving it on: ochakai performs no authorization, so
    every write by anyone who can reach the service is one Vertex AI call.
  EOT
  type        = bool
  default     = true
}

variable "embedding_model" {
  description = <<-EOT
    OCHAKAI_EMBEDDINGS as a Vertex AI model resource name, for a deployment that
    needs a particular model, region or project:
    "projects/<project>/locations/<location>/publishers/google/models/<model>".
    Leave null for the product's default — the project ochakai runs in,
    gemini-embedding-001 in the region this service runs in (design doc 0080
    §1.2), so text is embedded where you deployed it rather than in a region
    the product picked. Use
    .../locations/global/publishers/google/models/gemini-embedding-2 to also
    search image and PDF files by content; that model wants a location of
    global, us or eu — which means naming it moves the embedding call out of
    this service's own region.

    Naming a model asks for semantic search by name, so the service refuses to
    start where Vertex AI or pgvector is unavailable, where the default would
    have degraded to lexical search (design doc 0080 §1.3) — the pgvector
    bootstrap below is manual, so run it first. Changing the model leaves the
    vectors already stored invisible to the new one: `ochakai reembed` refills
    them, and search is lexical-only meanwhile. The vector width is not
    settable — it belongs to the model, not to the deployment.
  EOT
  type        = string
  default     = null

  validation {
    condition     = var.embedding_model == null || can(regex("^projects/[^/]+/locations/[^/]+/publishers/google/models/[^/]+$", coalesce(var.embedding_model, "")))
    error_message = "embedding_model must be projects/<project>/locations/<location>/publishers/google/models/<model>, or null for the default."
  }
}

# --- Optional: files in GCS (guide §4b) -----------------------------------

variable "enable_gcs_files" {
  description = <<-EOT
    Create a bucket for file bytes and point ochakai at it. Without it
    the service runs markdown-only: add_file operations return 501 and
    imports report files as failed. Auth is ADC via the service identity.
  EOT
  type        = bool
  default     = false
}

variable "gcs_bucket_name" {
  description = "Name of the files bucket. Null means \"<project_id>-ochakai-blobs\"."
  type        = string
  default     = null
}

variable "gcs_force_destroy" {
  description = "Allow `terraform destroy` to delete a bucket that still holds file bytes. False by default — those objects are the only copy."
  type        = bool
  default     = false
}

# --- Optional: the team web UI behind IAP (guide §5b) ---------------------

variable "enable_webui" {
  description = <<-EOT
    Deploy `serve-ui` as a second Cloud Run service behind Identity-Aware
    Proxy, for people who need browser access and cannot run the Go CLI. It
    is the same image as the server, so there is no second thing to build and,
    by default (see `webui_image_tag`), the UI is always the exact version of
    the server it fronts. Personal use needs none of this: `ochakai ui` serves
    the same page on loopback.
  EOT
  type        = bool
  default     = false
}

variable "webui_image_tag" {
  description = <<-EOT
    Override the webui's image tag independently of `image_tag`. Defaults to
    `image_tag`, so a plain tag bump upgrades both services together.

    A single `terraform apply` always settles the server first regardless of
    either tag, because the webui's `OCHAKAI_URL` reads the server's `uri`
    (main.tf's webui resource). That is the wrong order for an upgrade that
    renames the delegation header, per the operating guide's Upgrades
    section: the webui must be upgraded before or together with the server,
    never after. Set this variable ahead of `image_tag` and apply, then
    apply again with `image_tag` caught up, to upgrade the webui first
    without touching the server in between.
  EOT
  type        = string
  default     = null

  validation {
    condition     = var.webui_image_tag != "latest"
    error_message = "Pin a released version instead of \"latest\", so that a redeploy is a decision (deploy/cloudrun/README.md §1)."
  }
}

variable "webui_iap_members" {
  description = "IAM members granted roles/iap.httpsResourceAccessor on the webui, i.e. who may sign in through IAP. Same \"domain:...\" / \"user:...\" form as var.invoker_members."
  type        = list(string)
  default     = []

  validation {
    condition     = !contains(var.webui_iap_members, "allUsers") && !contains(var.webui_iap_members, "allAuthenticatedUsers")
    error_message = "The webui stays organization-restricted; nothing in this deployment needs an allUsers grant."
  }
}

variable "webui_records_browser_user" {
  description = <<-EOT
    Record the person signed in to the browser as the author of their edits
    (`human:tanaka@… via process:<webui-sa>`) instead of the webui's own service
    account. Sets OCHAKAI_IAP_AUDIENCE on the webui and adds the webui's
    service account to the server's delegating callers (design docs 0027,
    0032). Once set, serve-ui refuses any request IAP did not sign, so a
    misconfiguration surfaces immediately rather than as months of writes
    attributed to the wrong author.
  EOT
  type        = bool
  default     = true
}

# --- Project services -----------------------------------------------------

variable "manage_project_services" {
  description = "Let this module enable the Google Cloud APIs it needs. Set to false if API enablement is handled elsewhere (an org-level module, or a project factory)."
  type        = bool
  default     = true
}
