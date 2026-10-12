variable "project_id" {
  description = "GCP project where the dashboard resources are created."
  type        = string
}

variable "name" {
  description = "Prefix for resource names; must leave room for the service account suffix."
  type        = string
  default     = "services"

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{0,15}[a-z0-9]$", var.name))
    error_message = "Use 2 to 17 lowercase letters, digits or hyphens, starting with a letter and ending with a letter or digit."
  }
}

variable "regions" {
  description = "Deployment region. One region is supported while sessions and realtime connections are in memory."
  type        = list(string)

  validation {
    condition     = length(var.regions) == 1 && alltrue([for region in var.regions : trimspace(region) != ""])
    error_message = "Provide exactly one nonempty GCP region."
  }
}

variable "github_app_id" {
  description = "GitHub App integration ID; supplied by the caller."
  type        = string

  validation {
    condition     = can(regex("^[1-9][0-9]*$", var.github_app_id))
    error_message = "github_app_id must be a positive integer expressed as a string."
  }
}

variable "public_url" {
  description = "External HTTPS URL used for GitHub OAuth callbacks."
  type        = string

  validation {
    condition     = can(regex("^https://[^/?#[:space:]]+/?$", var.public_url))
    error_message = "Provide an HTTPS origin without a path, query or fragment."
  }
}

variable "source_directory" {
  description = "Absolute path to the dashboard checkout built by ko."
  type        = string
}

variable "secret_version_adder" {
  description = "IAM member authorized to upload GitHub secret versions, for example user:operator@example.com. Values are uploaded outside Terraform."
  type        = string
}

variable "allowed_users" {
  description = "GitHub usernames allowed to view all tracked repositories."
  type        = list(string)
  default     = []
}

variable "allowed_organizations" {
  description = "GitHub organizations whose active members may view all tracked repositories."
  type        = list(string)
  default     = []

  validation {
    condition     = length(var.allowed_organizations) > 0 || length(var.allowed_users) > 0
    error_message = "Set allowed_users and/or allowed_organizations; the dashboard requires an allowlist."
  }
}

variable "team" {
  description = "Team label on resources."
  type        = string
  default     = "github-actions-dashboard"
}

variable "notification_channels" {
  description = "Cloud Monitoring notification channel IDs."
  type        = list(string)
  default     = []
}

variable "deletion_protection" {
  description = "Protect Cloud SQL and Cloud Run from accidental deletion."
  type        = bool
  default     = true
}

variable "database_tier" {
  description = "Cloud SQL machine tier."
  type        = string
  default     = "db-custom-1-3840"
}

variable "database_disk_size" {
  description = "Initial Cloud SQL SSD size in GB."
  type        = number
  default     = 10
}

variable "database_availability_type" {
  description = "Cloud SQL availability: ZONAL or REGIONAL."
  type        = string
  default     = "ZONAL"

  validation {
    condition     = contains(["ZONAL", "REGIONAL"], var.database_availability_type)
    error_message = "Use ZONAL or REGIONAL."
  }
}
