// Create a network with several regional subnets
module "networking" {
  source  = "chainguard-dev/common/infra//modules/networking"
  version = "1.65.0"

  name                        = var.name
  project_id                  = var.project_id
  regions                     = var.regions
  netnum_offset               = 1
  hosted_zone_logging_enabled = false
  team                        = var.team
}

// Create a dedicated service account for the dashboard.
resource "google_service_account" "gh_dashboard_sa" {
  project = var.project_id

  account_id   = "${var.name}-gh-dashboard"
  display_name = "GitHub Dashboard Service Account"
  description  = "Dedicated service account for the GitHub Dashboard."
}

// Grant Cloud SQL client permissions to the service account
resource "google_project_iam_member" "gh_dashboard_cloudsql_client" {
  project = var.project_id
  role    = "roles/cloudsql.client"
  member  = "serviceAccount:${google_service_account.gh_dashboard_sa.email}"
}

module "gh_dashboard_webhook_secret" {
  source  = "chainguard-dev/common/infra//modules/secret"
  version = "1.65.0"

  project_id = var.project_id
  name       = "gh-dashboard-webhook-secret"

  service-accounts = [google_service_account.gh_dashboard_sa.email]

  authorized-adder = var.secret_version_adder

  notification-channels      = var.notification_channels
  team                       = var.team
  create_placeholder_version = false
}

module "gh_dashboard_gh_private_key_secret" {
  source  = "chainguard-dev/common/infra//modules/secret"
  version = "1.65.0"

  project_id       = var.project_id
  name             = "gh-dashboard-gh-private-key-secret"
  service-accounts = [google_service_account.gh_dashboard_sa.email]

  authorized-adder = var.secret_version_adder

  notification-channels      = var.notification_channels
  team                       = var.team
  create_placeholder_version = false
}

module "gh_dashboard_gh_oauth_client_id_secret" {
  source  = "chainguard-dev/common/infra//modules/secret"
  version = "1.65.0"

  project_id = var.project_id
  name       = "gh-dashboard-gh-oauth-client-id-secret"

  service-accounts = [google_service_account.gh_dashboard_sa.email]

  authorized-adder = var.secret_version_adder

  notification-channels      = var.notification_channels
  team                       = var.team
  create_placeholder_version = false
}

module "gh_dashboard_gh_oauth_client_secret" {
  source  = "chainguard-dev/common/infra//modules/secret"
  version = "1.65.0"

  project_id = var.project_id
  name       = "gh-dashboard-gh-oauth-client-secret"

  service-accounts = [google_service_account.gh_dashboard_sa.email]

  authorized-adder = var.secret_version_adder

  notification-channels      = var.notification_channels
  team                       = var.team
  create_placeholder_version = false
}

module "gh_dashboard_service" {
  source  = "chainguard-dev/common/infra//modules/regional-go-service"
  version = "1.65.0"

  deletion_protection = var.deletion_protection
  team                = var.team

  project_id = var.project_id
  name       = "${var.name}-gh-dashboard"
  regions    = module.networking.regional-networks

  ingress = "INGRESS_TRAFFIC_ALL"
  // This needs to egress in order to talk to Github
  egress = "PRIVATE_RANGES_ONLY"

  service_account = google_service_account.gh_dashboard_sa.email

  containers = {
    "gh-dashboard" = {
      source = {
        working_dir = var.source_directory
        importpath  = "github.com/cpanato/github-actions-dashboard"
      }

      ports = [{ container_port = 8080 }]

      volume_mounts = [{
        name       = "cloudsql-socket"
        mount_path = "/cloudsql"
      }]

      // Webhook events are processed asynchronously after the response and the
      // app retries DB initialization in the background, so keep CPU allocated.
      resources = {
        cpu_idle = false
      }

      env = concat([
        {
          name  = "DASHBOARD_GITHUB_APP_INTEGRATION_ID"
          value = var.github_app_id
        },
        {
          name  = "DASHBOARD_DATABASE_DRIVER"
          value = "postgres"
        },
        {
          name  = "DASHBOARD_DATABASE_CLOUD_SQL_INSTANCE"
          value = google_sql_database_instance.gh_dashboard.connection_name
        },
        {
          name  = "DASHBOARD_DATABASE_NAME"
          value = google_sql_database.dashboard.name
        },
        {
          name  = "DASHBOARD_DATABASE_USER"
          value = google_sql_user.dashboard.name
        },
        {
          name = "DASHBOARD_DATABASE_PASSWORD"
          value_source = {
            secret_key_ref = {
              secret  = google_secret_manager_secret.db_password.secret_id
              version = "latest"
            }
          }
        },
        {
          name  = "DASHBOARD_PUBLIC_URL"
          value = var.public_url
        },
        {
          name = "DASHBOARD_GITHUB_APP_WEBHOOK_SECRET"
          value_source = {
            secret_key_ref = {
              secret  = module.gh_dashboard_webhook_secret.secret_id
              version = "latest"
            }
          }
        },
        {
          name = "DASHBOARD_GITHUB_APP_PRIVATE_KEY"
          value_source = {
            secret_key_ref = {
              secret  = module.gh_dashboard_gh_private_key_secret.secret_id
              version = "latest"
            }
          }
        },
        {
          name = "DASHBOARD_GITHUB_OAUTH_CLIENT_ID"
          value_source = {
            secret_key_ref = {
              secret  = module.gh_dashboard_gh_oauth_client_id_secret.secret_id
              version = "latest"
            }
          }
        },
        {
          name = "DASHBOARD_GITHUB_OAUTH_CLIENT_SECRET"
          value_source = {
            secret_key_ref = {
              secret  = module.gh_dashboard_gh_oauth_client_secret.secret_id
              version = "latest"
            }
          }
        }
        ],
        // At least one allowlist is required, otherwise the app refuses to start.
        length(var.allowed_users) > 0 ? [{
          name  = "DASHBOARD_ALLOWED_USERS"
          value = join(",", var.allowed_users)
        }] : [],
        length(var.allowed_organizations) > 0 ? [{
          name  = "DASHBOARD_ALLOWED_ORGANIZATIONS"
          value = join(",", var.allowed_organizations)
        }] : [],
      )
    }
  }

  // The app connects to Cloud SQL through a unix socket at
  // /cloudsql/<connection name>, which the proxy sidecar provides.
  volumes = [{
    name      = "cloudsql-socket"
    empty_dir = {}
  }]

  raw_containers = {
    "cloud-sql-proxy" = {
      image = "gcr.io/cloud-sql-connectors/cloud-sql-proxy:2.26.0@sha256:86e4f3cc266020e7ee07740df78d7511e2c340c274b56f84d80b7251913af9ad"
      args = [
        "--private-ip",
        "--unix-socket=/cloudsql",
        "--exit-zero-on-sigterm",
        google_sql_database_instance.gh_dashboard.connection_name,
      ]
      resources = {
        cpu_idle = false
      }
      volume_mounts = [{
        name       = "cloudsql-socket"
        mount_path = "/cloudsql"
      }]
    }
  }

  // Sessions and the realtime hub are in memory, so run a
  // single always-on instance.
  enable_otel_sidecar = var.enable_otel_sidecar

  scaling = {
    min_instances = 1
    max_instances = 1
  }

  notification_channels = var.notification_channels

  depends_on = [
    google_service_account.gh_dashboard_sa,
    module.gh_dashboard_webhook_secret,
    module.gh_dashboard_gh_private_key_secret,
    module.gh_dashboard_gh_oauth_client_id_secret,
    module.gh_dashboard_gh_oauth_client_secret,
    google_sql_user.dashboard,
    google_secret_manager_secret_version.db_password,
    google_secret_manager_secret_iam_member.db_password_access,
    google_project_iam_member.gh_dashboard_cloudsql_client,
  ]
}
