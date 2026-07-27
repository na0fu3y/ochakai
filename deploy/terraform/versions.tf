terraform {
  required_version = ">= 1.5.0"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 6.0"
    }
    # Only the webui (var.enable_webui) needs this one: enabling IAP directly
    # on a Cloud Run service is still a beta surface, so `iap_enabled` and the
    # IAP service-agent resource exist in google-beta and not in google.
    # Terraform requires the declaration whether or not the webui is enabled;
    # with it disabled the provider is downloaded and then does nothing.
    google-beta = {
      source  = "hashicorp/google-beta"
      version = "~> 6.0"
    }
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
}

provider "google-beta" {
  project = var.project_id
  region  = var.region
}
