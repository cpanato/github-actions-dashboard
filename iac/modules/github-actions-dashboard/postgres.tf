# Random password for database
resource "random_password" "db_password" {
  length  = 32
  special = true
}

# Store database password in Secret Manager
resource "google_secret_manager_secret" "db_password" {
  project   = var.project_id
  secret_id = "${var.name}-db-password"

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "db_password" {
  secret      = google_secret_manager_secret.db_password.id
  secret_data = random_password.db_password.result
}

# Grant the service account access to the database password secret
resource "google_secret_manager_secret_iam_member" "db_password_access" {
  project   = var.project_id
  secret_id = google_secret_manager_secret.db_password.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.gh_dashboard_sa.email}"
}

# Cloud SQL PostgreSQL instance
resource "google_sql_database_instance" "gh_dashboard" {
  project          = var.project_id
  name             = "${var.name}-db"
  database_version = "POSTGRES_15"
  region           = var.regions[0]

  deletion_protection = var.deletion_protection

  settings {
    tier              = var.database_tier
    availability_type = var.database_availability_type
    disk_size         = var.database_disk_size
    disk_type         = "PD_SSD"

    backup_configuration {
      enabled                        = true
      point_in_time_recovery_enabled = true
      start_time                     = "03:00"
      transaction_log_retention_days = 7
      backup_retention_settings {
        retained_backups = 7
      }
    }

    ip_configuration {
      ipv4_enabled    = false # No public IP
      private_network = module.networking.network_id
    }

    database_flags {
      name  = "log_checkpoints"
      value = "on"
    }

    database_flags {
      name  = "log_connections"
      value = "on"
    }

    database_flags {
      name  = "log_disconnections"
      value = "on"
    }

    insights_config {
      query_insights_enabled  = true
      query_plans_per_minute  = 5
      query_string_length     = 1024
      record_application_tags = true
    }
  }

  depends_on = [google_service_networking_connection.private_vpc_connection]
}

# Private VPC connection for Cloud SQL
resource "google_compute_global_address" "private_ip_address" {
  project       = var.project_id
  name          = "${var.name}-private-ip"
  purpose       = "VPC_PEERING"
  address_type  = "INTERNAL"
  prefix_length = 16
  network       = module.networking.network_id
}

resource "google_service_networking_connection" "private_vpc_connection" {
  network                 = module.networking.network_id
  service                 = "servicenetworking.googleapis.com"
  reserved_peering_ranges = [google_compute_global_address.private_ip_address.name]
}

# Database
resource "google_sql_database" "dashboard" {
  project  = var.project_id
  name     = "dashboard"
  instance = google_sql_database_instance.gh_dashboard.name
}

# Database user
resource "google_sql_user" "dashboard" {
  project  = var.project_id
  name     = "dashboard-user"
  instance = google_sql_database_instance.gh_dashboard.name
  password = random_password.db_password.result
}
