output "service_url" {
  description = "URL of the ochakai service. Not reachable without an identity: a plain curl gets Google's 401, which is the point."
  value       = google_cloud_run_v2_service.ochakai.uri
}

output "use_command" {
  description = "The next thing to run. The CLI resolves Google ID tokens from your gcloud login, so there is nothing else to configure."
  value       = "ochakai use ${google_cloud_run_v2_service.ochakai.uri}"
}

output "service_account_email" {
  description = "The runtime identity. It authenticates to Postgres as itself and holds only explicit grants."
  value       = google_service_account.ochakai.email
}

output "database_user" {
  description = "The Postgres principal for the runtime identity — the service account email without the .gserviceaccount.com suffix."
  value       = local.db_service_account_user
}

output "sql_connection_name" {
  description = "Cloud SQL connection name, as used by cloud-sql-proxy and by the /cloudsql socket path."
  value       = google_sql_database_instance.ochakai.connection_name
}

output "image" {
  description = "The image the service runs, pulled through the Artifact Registry remote repository. Its digest matches the GHCR one, so `gh attestation verify oci://ghcr.io/na0fu3y/ochakai:<tag> -R na0fu3y/ochakai` still describes what is running."
  value       = local.image
}

output "webui_url" {
  description = "URL of the IAP-fronted web UI, or null when var.enable_webui is false."
  value       = var.enable_webui ? google_cloud_run_v2_service.webui[0].uri : null
}

output "webui_service_account_email" {
  description = "The web UI's identity, or null when var.enable_webui is false. Edits made through the UI are recorded as this account unless var.webui_records_browser_user is on."
  value       = var.enable_webui ? google_service_account.webui[0].email : null
}

output "attachments_bucket" {
  description = "Bucket holding attachment bytes, or null when var.enable_gcs_attachments is false."
  value       = var.enable_gcs_attachments ? google_storage_bucket.attachments[0].name : null
}

output "database_bootstrap_sql" {
  description = <<-EOT
    The one step this module cannot take: Terraform would need a database
    password to run SQL, and there is none. Run these once, as a
    cloudsqlsuperuser member, before the first request. Extensions are
    pre-created here so the runtime never needs elevated rights — its startup
    migration only ever hits the privilege-free CREATE EXTENSION IF NOT
    EXISTS skip path. See the module README for how to connect without
    storing a password.
  EOT
  value       = <<-EOT
    CREATE EXTENSION IF NOT EXISTS pg_trgm;
    CREATE EXTENSION IF NOT EXISTS vector;
    GRANT USAGE, CREATE ON SCHEMA public TO "${local.db_service_account_user}";
    GRANT ALL ON ALL TABLES IN SCHEMA public TO "${local.db_service_account_user}";
    GRANT ALL ON ALL SEQUENCES IN SCHEMA public TO "${local.db_service_account_user}";
    ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO "${local.db_service_account_user}";
    ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO "${local.db_service_account_user}";
  EOT
}
