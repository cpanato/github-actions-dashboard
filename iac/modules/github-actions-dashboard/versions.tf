terraform {
  required_version = ">= 1.15.0"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = ">= 8.6.0"
    }
    google-beta = {
      source  = "hashicorp/google-beta"
      version = ">= 8.6.0"
    }
    random = {
      source  = "hashicorp/random"
      version = ">= 3.9.1"
    }
    ko = {
      source  = "ko-build/ko"
      version = ">= 0.0.21"
    }
    cosign = {
      source  = "chainguard-dev/cosign"
      version = ">= 0.5.5"
    }
  }
}
