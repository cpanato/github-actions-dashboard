variable "project_id" {
  type = string
}

variable "name" {
  type    = string
  default = "services"
}

variable "region" {
  type = string
}

variable "image_repository" {
  description = "Writable registry repository used by ko."
  type        = string
}

variable "github_app_id" {
  type = string
}

variable "public_url" {
  type = string
}

variable "secret_version_adder" {
  type = string
}

variable "allowed_users" {
  type    = list(string)
  default = []
}

variable "allowed_organizations" {
  type    = list(string)
  default = []
}
