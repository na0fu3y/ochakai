# The baseline deployment of deploy/cloudrun/README.md, expressed as
# infrastructure as code. Sections below follow that guide's numbering, and
# every resource here traces to a command there.
#
# Two properties are load-bearing and are the reason several obvious-looking
# things are missing:
#
#   * Secret-zero. There is no password in this module, no random_password,
#     no Secret Manager entry, no service-account key. The runtime logs in to
#     Postgres with Cloud SQL IAM database authentication, where the
#     "password" is a short-lived IAM token fetched at connect time. If a
#     change to this module ever needs a generated credential, the change is
#     wrong for this project (design docs 0002, 0003).
#
#   * The service is not publicly invokable, with exactly one exception.
#     ochakai performs no authorization of its own: it reads the caller
#     identity Cloud Run has already verified and records it as provenance.
#     Those headers only mean something behind Cloud Run's IAM check, so
#     allUsers would not merely widen access, it would make provenance
#     forgeable. The exception is var.public_read_only (design doc 0042),
#     which grants allUsers only together with the flag that makes the
#     deployment refuse every write and stop reading identity altogether —
#     there is then no provenance to forge and no author to get wrong. A
#     public binding from anywhere else in this module is a bug.

data "google_project" "this" {
  project_id = var.project_id
}

locals {
  webui_name = "${var.name}-webui"

  # Pulled through the Artifact Registry remote repository below, not from
  # ghcr.io directly.
  image = "${var.region}-docker.pkg.dev/${var.project_id}/${var.artifact_registry_repository_id}/${var.image_repository}:${var.image_tag}"

  # Same image, normally the same tag. var.webui_image_tag exists only to be
  # set ahead of var.image_tag for a staged upgrade — see its description.
  webui_image = "${var.region}-docker.pkg.dev/${var.project_id}/${var.artifact_registry_repository_id}/${var.image_repository}:${coalesce(var.webui_image_tag, var.image_tag)}"

  connection_name = google_sql_database_instance.ochakai.connection_name

  # The database principal for a service account is its email with the
  # .gserviceaccount.com suffix removed — that is the name Postgres knows it
  # by, and it must match the "user=" in the connection string exactly.
  db_service_account_user = trimsuffix(google_service_account.ochakai.email, ".gserviceaccount.com")

  gcs_bucket_name = coalesce(var.gcs_bucket_name, "${var.project_id}-ochakai-blobs")

  # The webui forwards the identity of the person signed in through IAP, so
  # the server has to accept it as a delegator. Granting this before the
  # webui exists is deliberate: ochakai answers 403 rather than silently
  # downgrading to the service account, so the grant must never lag behind.
  delegating_callers = distinct(concat(
    var.delegating_callers,
    var.enable_webui && var.webui_records_browser_user ? [google_service_account.webui[0].email] : [],
  ))

  iap_audience = "/projects/${data.google_project.this.number}/locations/${var.region}/services/${local.webui_name}"

  # Public read-only implies read-only (design doc 0042): the server applies
  # the implication itself and cannot be separated from it, so a publicly
  # readable and writable ochakai is not a configuration it accepts. The
  # module states the implication in the environment rather than leaving it
  # to be read back off the running service, so the deployed configuration
  # reads the same way the posture does.
  # One word for the posture (design doc 0060): the module maps its two
  # booleans onto the single OCHAKAI_MODE the server reads, and public
  # wins because it is the stronger of the two — it is read-only plus
  # believing nobody.
  mode = var.public_read_only ? "public" : (var.read_only ? "read-only" : "")

  server_env = merge(
    {
      # No password appears here, and none exists to appear. This is what
      # makes plain environment variables the right place for the connection
      # string; with password auth it would have to be a Secret Manager
      # reference instead.
      OCHAKAI_DB_IAM_AUTH = "true"
      OCHAKAI_DATABASE_URL = join("", [
        "postgres:///${google_sql_database.ochakai.name}",
        "?host=/cloudsql/${local.connection_name}",
        "&user=${local.db_service_account_user}",
      ])
    },
    local.mode != "" ? { OCHAKAI_MODE = local.mode } : {},
    var.enable_gcs_files ? { OCHAKAI_GCS_BUCKET = local.gcs_bucket_name } : {},
    # One variable says how this deployment embeds (design doc 0080).
    # Nothing is set when embeddings are on and no model is named:
    # ochakai discovers the project it runs in, and a discovered default
    # is the mode that falls back to lexical search instead of refusing
    # to start (design doc 0080 §1.3). That matters here, because the
    # pgvector extension is a documented manual bootstrap step below — an
    # apply that lands before somebody ran it should degrade, not fail to
    # serve. Naming a model is the other side of that trade, and
    # var.embedding_model's description says so.
    var.enable_vertex_embeddings ? {} : { OCHAKAI_EMBEDDINGS = "off" },
    var.enable_vertex_embeddings && var.embedding_model != null ? { OCHAKAI_EMBEDDINGS = var.embedding_model } : {},
    length(local.delegating_callers) > 0 ? { OCHAKAI_DELEGATING_CALLERS = join(",", local.delegating_callers) } : {},
  )

  webui_env = merge(
    { OCHAKAI_URL = google_cloud_run_v2_service.ochakai.uri },
    var.webui_records_browser_user ? { OCHAKAI_IAP_AUDIENCE = local.iap_audience } : {},
  )

  services = var.manage_project_services ? toset(concat(
    [
      "run.googleapis.com",
      "sqladmin.googleapis.com",
      "sql-component.googleapis.com",
      "artifactregistry.googleapis.com",
    ],
    var.enable_private_ip ? ["compute.googleapis.com", "servicenetworking.googleapis.com"] : [],
    var.enable_vertex_embeddings ? ["aiplatform.googleapis.com"] : [],
    var.enable_webui ? ["iap.googleapis.com", "cloudresourcemanager.googleapis.com"] : [],
  )) : toset([])
}

# --- 1. Prerequisites -----------------------------------------------------

resource "google_project_service" "this" {
  for_each = local.services

  project = var.project_id
  service = each.value

  # Turning an API off again is a project-wide act, and other things in the
  # project may depend on it. Teardown removes this deployment's resources,
  # not the project's capabilities.
  disable_on_destroy = false
}

# Cloud Run cannot pull from ghcr.io, so pull through Artifact Registry
# instead. A remote repository is a caching proxy, not a copy: the digest
# stays identical to the GHCR one, so the GHCR build attestations still
# describe exactly what runs here (`gh attestation verify
# oci://ghcr.io/na0fu3y/ochakai:<tag> -R na0fu3y/ochakai`). Nothing needs to
# be pushed, and new tags are fetched on demand at deploy time.
resource "google_artifact_registry_repository" "ghcr" {
  project       = var.project_id
  location      = var.region
  repository_id = var.artifact_registry_repository_id
  description   = "Remote repository proxying ghcr.io, so Cloud Run can pull the ochakai image"
  format        = "DOCKER"
  mode          = "REMOTE_REPOSITORY"

  remote_repository_config {
    description = "ghcr.io"

    docker_repository {
      custom_repository {
        uri = "https://ghcr.io"
      }
    }
  }

  depends_on = [google_project_service.this]
}

# --- 2. The database ------------------------------------------------------

resource "google_sql_database_instance" "ochakai" {
  project          = var.project_id
  name             = var.name
  region           = var.region
  database_version = var.database_version

  # Terraform-level and API-level guards. Both have to come off (apply, then
  # destroy) before this instance can be deleted — see the module README.
  deletion_protection = var.deletion_protection

  settings {
    edition                     = "ENTERPRISE"
    tier                        = var.database_tier
    availability_type           = "ZONAL"
    disk_size                   = var.database_disk_size_gb
    disk_type                   = "PD_SSD"
    disk_autoresize             = false
    deletion_protection_enabled = var.deletion_protection

    # The setting that makes the whole deployment passwordless: it lets a
    # Google identity authenticate to Postgres with a short-lived IAM token.
    database_flags {
      name  = "cloudsql.iam_authentication"
      value = "on"
    }

    backup_configuration {
      enabled    = var.database_backups
      start_time = var.database_backups ? var.database_backup_start_time : null
    }

    ip_configuration {
      # With private IP there is no reachable endpoint at all. Without it the
      # public IP is still not open to the internet: the authorized_networks
      # list below is deliberately empty, which drops direct connections. The
      # only way in is the Cloud SQL connector — IAM-checked and mTLS'd —
      # which is what both Cloud Run and cloud-sql-proxy use. Adding an entry
      # here is what would open the database; do not.
      ipv4_enabled    = !var.enable_private_ip
      private_network = var.enable_private_ip ? data.google_compute_network.this[0].id : null
    }
  }

  depends_on = [
    google_project_service.this,
    google_service_networking_connection.this,
  ]
}

resource "google_sql_database" "ochakai" {
  project  = var.project_id
  name     = var.name
  instance = google_sql_database_instance.ochakai.name
}

# Note what is absent: the guide's password-holding admin user. This module
# does not create it, because a generated password would be the only stored
# secret in an otherwise secret-zero system (guide §6 exists to retire it).
# Direct database access for people goes through IAM logins instead, and the
# one-time schema bootstrap uses a break-glass password minted by hand and
# discarded — see the module README.

resource "google_sql_user" "maintenance" {
  for_each = toset(var.maintenance_users)

  project  = var.project_id
  instance = google_sql_database_instance.ochakai.name
  name     = each.value
  type     = "CLOUD_IAM_USER"
}

resource "google_project_iam_member" "maintenance_instance_user" {
  for_each = toset(var.maintenance_users)

  project = var.project_id
  role    = "roles/cloudsql.instanceUser"
  member  = "user:${each.value}"
}

# --- 2b. Optional hardening: private IP only ------------------------------

data "google_compute_network" "this" {
  count = var.enable_private_ip ? 1 : 0

  project = var.project_id
  name    = var.network
}

data "google_compute_subnetwork" "this" {
  count = var.enable_private_ip ? 1 : 0

  project = var.project_id
  region  = var.region
  name    = var.subnetwork
}

# One-time per VPC: a range Google's managed services get peered into. Free.
resource "google_compute_global_address" "private_services" {
  count = var.enable_private_ip ? 1 : 0

  project       = var.project_id
  name          = "google-managed-services-${var.network}"
  purpose       = "VPC_PEERING"
  address_type  = "INTERNAL"
  prefix_length = 16
  network       = data.google_compute_network.this[0].id

  depends_on = [google_project_service.this]
}

resource "google_service_networking_connection" "this" {
  count = var.enable_private_ip ? 1 : 0

  network                 = data.google_compute_network.this[0].id
  service                 = "servicenetworking.googleapis.com"
  reserved_peering_ranges = [google_compute_global_address.private_services[0].name]

  depends_on = [google_project_service.this]
}

# --- 3. The runtime identity ----------------------------------------------

resource "google_service_account" "ochakai" {
  project      = var.project_id
  account_id   = "${var.name}-run"
  display_name = "ochakai service"
}

resource "google_project_iam_member" "ochakai_cloudsql_client" {
  project = var.project_id
  role    = "roles/cloudsql.client"
  member  = "serviceAccount:${google_service_account.ochakai.email}"
}

# Distinct from cloudsql.client: client permits opening the connector,
# instanceUser permits logging in as an IAM principal once through it.
resource "google_project_iam_member" "ochakai_cloudsql_instance_user" {
  project = var.project_id
  role    = "roles/cloudsql.instanceUser"
  member  = "serviceAccount:${google_service_account.ochakai.email}"
}

resource "google_sql_user" "ochakai_run" {
  project  = var.project_id
  instance = google_sql_database_instance.ochakai.name
  name     = local.db_service_account_user
  type     = "CLOUD_IAM_SERVICE_ACCOUNT"
}

# The Postgres-side setup for this principal — the extensions and the object
# grants — is not expressible here without a database password, so it stays a
# documented one-time manual step. `terraform output database_bootstrap_sql`
# prints the statements with the right principal already filled in. The
# runtime deliberately gets neither cloudsqlsuperuser nor any role
# membership: only explicit object grants.

# --- 4. Vertex AI embeddings (on by default) ------------------------------
#
# This grant is the switch. ochakai asks for embeddings by itself where it
# runs on Google Cloud; whether it gets them is IAM's answer (0053 §2.2).

resource "google_project_iam_member" "ochakai_vertex" {
  count = var.enable_vertex_embeddings ? 1 : 0

  project = var.project_id
  role    = "roles/aiplatform.user"
  member  = "serviceAccount:${google_service_account.ochakai.email}"
}

# --- 4b. Optional: files in GCS --------------------------------------------

resource "google_storage_bucket" "files" {
  count = var.enable_gcs_files ? 1 : 0

  project                     = var.project_id
  name                        = local.gcs_bucket_name
  location                    = var.region
  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"

  # File objects are content-addressed (blob/<sha256>), create-only and
  # never deleted by ochakai, so the bucket holds the only copy of every file
  # anyone added.
  force_destroy = var.gcs_force_destroy
}

resource "google_storage_bucket_iam_member" "ochakai" {
  count = var.enable_gcs_files ? 1 : 0

  bucket = google_storage_bucket.files[0].name
  role   = "roles/storage.objectUser"
  member = "serviceAccount:${google_service_account.ochakai.email}"
}

# --- 3. The service -------------------------------------------------------

resource "google_cloud_run_v2_service" "ochakai" {
  project  = var.project_id
  name     = var.name
  location = var.region

  deletion_protection = var.deletion_protection

  template {
    service_account = google_service_account.ochakai.email

    # Scale to zero when idle, and never more than one instance: the cost
    # table's "~$0 when idle" depends on the first, and a single small
    # Postgres instance is sized for the second.
    scaling {
      min_instance_count = 0
      max_instance_count = 1
    }

    containers {
      image = local.image

      resources {
        limits = {
          cpu    = "1"
          memory = "512Mi"
        }
        # Request-based billing: CPU is only charged while a request is in
        # flight, which is the other half of "~$0 when idle".
        cpu_idle = true
      }

      dynamic "env" {
        for_each = local.server_env
        content {
          name  = env.key
          value = env.value
        }
      }

      volume_mounts {
        name       = "cloudsql"
        mount_path = "/cloudsql"
      }
    }

    # The Cloud SQL connector socket, the /cloudsql/<connection-name> path the
    # OCHAKAI_DATABASE_URL points at. This works unchanged whether the
    # instance has a public or a private IP.
    volumes {
      name = "cloudsql"
      cloud_sql_instance {
        instances = [local.connection_name]
      }
    }

    # Direct VPC egress, so the container can route to the instance's private
    # address when there is no public one.
    dynamic "vpc_access" {
      for_each = var.enable_private_ip ? [1] : []
      content {
        egress = "PRIVATE_RANGES_ONLY"
        network_interfaces {
          network    = data.google_compute_network.this[0].id
          subnetwork = data.google_compute_subnetwork.this[0].id
        }
      }
    }
  }

  depends_on = [
    google_project_service.this,
    google_artifact_registry_repository.ghcr,
    google_sql_user.ochakai_run,
    google_project_iam_member.ochakai_cloudsql_client,
    google_project_iam_member.ochakai_cloudsql_instance_user,
  ]
}

# The only thing that decides who can reach ochakai. allUsers cannot be named
# here — the variable refuses it — which is also what keeps the default
# deployment compatible with the Domain Restricted Sharing org policy.
resource "google_cloud_run_v2_service_iam_member" "invokers" {
  for_each = toset(var.invoker_members)

  project  = var.project_id
  location = var.region
  name     = google_cloud_run_v2_service.ochakai.name
  role     = "roles/run.invoker"
  member   = each.value
}

# The public read-only demo, and the only allUsers grant in this module
# (design doc 0042). It is deliberately not something an operator can compose
# out of parts: the same variable that opens the service is the one that makes
# it refuse every write and stop reading identity, so "public" and "believes
# nobody" cannot drift apart. A public writable ochakai would record authors
# it has no way to know, which is worse than recording none.
#
# Domain Restricted Sharing (guide §6) rejects this binding, as it should —
# a demo project is the place to lift it, not the org.
resource "google_cloud_run_v2_service_iam_member" "public_demo" {
  count = var.public_read_only ? 1 : 0

  project  = var.project_id
  location = var.region
  name     = google_cloud_run_v2_service.ochakai.name
  role     = "roles/run.invoker"
  member   = "allUsers"

  lifecycle {
    # Reads the environment the service actually carries rather than the
    # variable that set it. Today the two cannot disagree; this exists for
    # the edit that separates them, and it fails at plan time — an allUsers
    # binding must never outlive the posture that justifies it, not even for
    # the length of an apply.
    precondition {
      condition     = lookup(local.server_env, "OCHAKAI_MODE", "") == "public"
      error_message = "Refusing to grant allUsers: the service is not running with OCHAKAI_MODE=public. Public is only safe while ochakai writes nothing and reads no identity (design doc 0042)."
    }
  }
}

# --- 5b. Optional: the team web UI behind IAP -----------------------------

resource "google_service_account" "webui" {
  count = var.enable_webui ? 1 : 0

  project      = var.project_id
  account_id   = "${var.name}-webui"
  display_name = "ochakai web UI"
}

# The webui reaches ochakai as itself, attaching this identity to every API
# call, which is how the server stays organization-restricted behind a
# browser-facing page.
resource "google_cloud_run_v2_service_iam_member" "webui_invokes_ochakai" {
  count = var.enable_webui ? 1 : 0

  project  = var.project_id
  location = var.region
  name     = google_cloud_run_v2_service.ochakai.name
  role     = "roles/run.invoker"
  member   = "serviceAccount:${google_service_account.webui[0].email}"
}

# The same image as the server, started with serve-ui instead of the default
# serve. There is no second image to build, and by default (local.webui_image)
# the UI is the exact version of the server it fronts.
resource "google_cloud_run_v2_service" "webui" {
  count    = var.enable_webui ? 1 : 0
  provider = google-beta

  project  = var.project_id
  name     = local.webui_name
  location = var.region

  deletion_protection = var.deletion_protection

  # Browsers sign in with their Google account and only the members granted
  # below get through. Note that this is IAP, not an allUsers grant: the
  # service itself stays non-public.
  iap_enabled = true

  template {
    service_account = google_service_account.webui[0].email

    scaling {
      min_instance_count = 0
      max_instance_count = 1
    }

    containers {
      image = local.webui_image
      args  = ["serve-ui"]

      resources {
        limits = {
          cpu    = "1"
          memory = "256Mi"
        }
        cpu_idle = true
      }

      dynamic "env" {
        for_each = local.webui_env
        content {
          name  = env.key
          value = env.value
        }
      }
    }
  }

  # Ordering matters and Terraform gets it right for free within one apply:
  # the webui's OCHAKAI_URL reads the server's uri, so the server — carrying
  # the delegation grant this UI depends on — is always settled first. That
  # is also the wrong order for the one upgrade docs/guides/operating.md
  # warns about (a header rename): the webui must land before or with the
  # server, never after. var.webui_image_tag exists so that case can be
  # driven as two applies — webui first, server second — without disturbing
  # this resource's ordering within either one.
  depends_on = [google_project_service.this]
}

# IAP fronts the service by invoking it as its own service agent, so that
# agent needs run.invoker. gcloud's `--iap` does this as a side effect of
# deploying; here it is explicit.
resource "google_project_service_identity" "iap" {
  count    = var.enable_webui ? 1 : 0
  provider = google-beta

  project = var.project_id
  service = "iap.googleapis.com"

  depends_on = [google_project_service.this]
}

resource "google_cloud_run_v2_service_iam_member" "iap_agent" {
  count = var.enable_webui ? 1 : 0

  project  = var.project_id
  location = var.region
  name     = google_cloud_run_v2_service.webui[0].name
  role     = "roles/run.invoker"
  member   = "serviceAccount:${google_project_service_identity.iap[0].email}"
}

# Who may sign in through IAP.
resource "google_iap_web_cloud_run_service_iam_member" "members" {
  for_each = var.enable_webui ? toset(var.webui_iap_members) : toset([])

  project                = var.project_id
  location               = var.region
  cloud_run_service_name = google_cloud_run_v2_service.webui[0].name
  role                   = "roles/iap.httpsResourceAccessor"
  member                 = each.value
}
