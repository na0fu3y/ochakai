terraform {
  required_version = ">= 1.5.0"

  required_providers {
    google = {
      source = "hashicorp/google"
      # 7.21 is where `iap_enabled` on a Cloud Run service arrived in the
      # GA provider, and that is the field the webui is built on. IAP on
      # Cloud Run itself went GA in March 2026, so the beta surface it
      # used to need is gone rather than merely available elsewhere.
      version = "~> 7.21"
    }
    # Only the webui (var.enable_webui) needs this one, and only for one
    # resource: `google_project_service_identity`, which is still beta.
    # Terraform requires the declaration whether or not the webui is
    # enabled; with it disabled the provider is downloaded and then does
    # nothing.
    google-beta = {
      source  = "hashicorp/google-beta"
      version = "~> 7.21"
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
