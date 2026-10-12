# Copy this caller into the ignored iac/gcp directory and supply values locally.
terraform {
  required_version = ">= 1.15.0"
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "8.6.0"
    }
    google-beta = {
      source  = "hashicorp/google-beta"
      version = "8.6.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "3.9.1"
    }
    ko = {
      source  = "ko-build/ko"
      version = "0.0.21"
    }
    cosign = {
      source  = "chainguard-dev/cosign"
      version = "0.5.5"
    }
  }
}

provider "google" {
  project = var.project_id
}

provider "google-beta" {
  project = var.project_id
}

provider "ko" {
  repo = var.image_repository
}

module "dashboard" {
  # This path is relative to this example; in iac/gcp use ../modules/...
  source = "../../modules/github-actions-dashboard"

  project_id            = var.project_id
  name                  = var.name
  regions               = [var.region]
  github_app_id         = var.github_app_id
  public_url            = var.public_url
  source_directory      = abspath("${path.root}/../../..")
  secret_version_adder  = var.secret_version_adder
  allowed_users         = var.allowed_users
  allowed_organizations = var.allowed_organizations
}

output "service_urls" {
  value = module.dashboard.service_urls
}

output "github_secret_ids" {
  value = module.dashboard.github_secret_ids
}
