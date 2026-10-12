output "service_account_email" {
  description = "Runtime service account email."
  value       = google_service_account.gh_dashboard_sa.email
}

output "service_account_unique_id" {
  description = "Runtime service account unique ID."
  value       = google_service_account.gh_dashboard_sa.unique_id
}

output "service_urls" {
  description = "Cloud Run URLs by region."
  value       = module.gh_dashboard_service.uris
}

output "database_connection_name" {
  description = "Cloud SQL connection name used by the proxy."
  value       = google_sql_database_instance.gh_dashboard.connection_name
}

output "github_secret_ids" {
  description = "Secret IDs to populate outside Terraform; no secret payloads are exported."
  value = {
    webhook_secret      = module.gh_dashboard_webhook_secret.secret_id
    private_key         = module.gh_dashboard_gh_private_key_secret.secret_id
    oauth_client_id     = module.gh_dashboard_gh_oauth_client_id_secret.secret_id
    oauth_client_secret = module.gh_dashboard_gh_oauth_client_secret.secret_id
  }
}
