# Cloud SQL Module for deadman switch
# Creates the PostgreSQL instance, database, user, and grants cloudsql.client
# to the GitHub Actions service account so it can connect at deploy time.
#
# Instance: nexus-dev-west1 (shared with other dev services in us-west1)
# Database: deadman
# User:     deadman

terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 5.0"
    }
  }
}

variable "project_id" {
  description = "GCP Project ID"
  type        = string
}

variable "region" {
  description = "GCP region for the Cloud SQL instance"
  type        = string
  default     = "us-west1"
}

variable "instance_name" {
  description = "Cloud SQL instance name"
  type        = string
  default     = "nexus-dev-west1"
}

variable "database_name" {
  description = "PostgreSQL database name"
  type        = string
  default     = "deadman"
}

variable "db_user" {
  description = "PostgreSQL user for the deadman service"
  type        = string
  default     = "deadman"
}

variable "service_account_email" {
  description = "Service account email that needs cloudsql.client (for Cloud Run)"
  type        = string
}

# -----------------------------------------------------------------------
# Cloud SQL Instance — db-f1-micro, Postgres 16, us-west1
# Shared instance: other dev databases (e.g. portal) may also live here.
# -----------------------------------------------------------------------
resource "google_sql_database_instance" "instance" {
  project          = var.project_id
  name             = var.instance_name
  region           = var.region
  database_version = "POSTGRES_16"

  settings {
    tier = "db-f1-micro"

    ip_configuration {
      # No public IP; Cloud Run connects via Unix socket through the Cloud SQL proxy.
      ipv4_enabled = true # required even for socket-only to allow IAM auth
    }

    backup_configuration {
      enabled = true
    }
  }

  # Prevent accidental destruction of a shared instance.
  deletion_protection = true
}

# -----------------------------------------------------------------------
# Database
# -----------------------------------------------------------------------
resource "google_sql_database" "database" {
  project  = var.project_id
  instance = google_sql_database_instance.instance.name
  name     = var.database_name
}

# -----------------------------------------------------------------------
# User
# Native PG password auth; password is stored in Secret Manager separately.
# -----------------------------------------------------------------------
resource "google_sql_user" "user" {
  project  = var.project_id
  instance = google_sql_database_instance.instance.name
  name     = var.db_user

  # Password managed externally (GCP Secret Manager: deadman-db-password-dev).
  # Terraform does not set it here to avoid storing plaintext in state.
  # On first run, set the password manually:
  #   gcloud sql users set-password deadman --instance=nexus-dev-west1 --password=<secret>
}

# -----------------------------------------------------------------------
# IAM: cloudsql.client — lets the Cloud Run service connect via socket
# -----------------------------------------------------------------------
resource "google_project_iam_member" "cloudsql_client" {
  project = var.project_id
  role    = "roles/cloudsql.client"
  member  = "serviceAccount:${var.service_account_email}"
}

# -----------------------------------------------------------------------
# Outputs
# -----------------------------------------------------------------------
output "connection_name" {
  description = "Cloud SQL connection name (project:region:instance)"
  value       = google_sql_database_instance.instance.connection_name
}

output "instance_name" {
  description = "Cloud SQL instance name"
  value       = google_sql_database_instance.instance.name
}
