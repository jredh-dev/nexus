# Secret Manager Configuration
# Manages secrets for the deadman switch service: Twilio credentials and DB password.

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

variable "environment" {
  description = "Environment name (dev or prod)"
  type        = string
}

variable "service_account_email" {
  description = "Service account email that needs access to secrets"
  type        = string
}

variable "twilio_account_sid" {
  description = "Twilio Account SID"
  type        = string
  sensitive   = true
}

variable "twilio_auth_token" {
  description = "Twilio Auth Token"
  type        = string
  sensitive   = true
}

variable "twilio_from" {
  description = "Twilio phone number in E.164 format (e.g. +15706006135)"
  type        = string
}

variable "deadman_phone" {
  description = "Owner phone number in E.164 format (e.g. +6016914667)"
  type        = string
}

variable "deadman_db_password" {
  description = "PostgreSQL password for the deadman Cloud SQL instance"
  type        = string
  sensitive   = true
}

# -----------------------------------------------------------------------
# Twilio Account SID
# -----------------------------------------------------------------------

resource "google_secret_manager_secret" "twilio_account_sid" {
  project   = var.project_id
  secret_id = "twilio-account-sid-${var.environment}"

  labels = {
    app         = "nexus-deadman"
    environment = var.environment
    managed_by  = "terraform"
  }

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "twilio_account_sid" {
  secret      = google_secret_manager_secret.twilio_account_sid.id
  secret_data = var.twilio_account_sid
}

resource "google_secret_manager_secret_iam_member" "twilio_account_sid_access" {
  project   = var.project_id
  secret_id = google_secret_manager_secret.twilio_account_sid.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${var.service_account_email}"
}

# -----------------------------------------------------------------------
# Twilio Auth Token
# -----------------------------------------------------------------------

resource "google_secret_manager_secret" "twilio_auth_token" {
  project   = var.project_id
  secret_id = "twilio-auth-token-${var.environment}"

  labels = {
    app         = "nexus-deadman"
    environment = var.environment
    managed_by  = "terraform"
  }

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "twilio_auth_token" {
  secret      = google_secret_manager_secret.twilio_auth_token.id
  secret_data = var.twilio_auth_token
}

resource "google_secret_manager_secret_iam_member" "twilio_auth_token_access" {
  project   = var.project_id
  secret_id = google_secret_manager_secret.twilio_auth_token.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${var.service_account_email}"
}

# -----------------------------------------------------------------------
# Twilio From (phone number) — not sensitive but kept in Secret Manager
# for consistency and easy rotation.
# -----------------------------------------------------------------------

resource "google_secret_manager_secret" "twilio_from" {
  project   = var.project_id
  secret_id = "twilio-from-${var.environment}"

  labels = {
    app         = "nexus-deadman"
    environment = var.environment
    managed_by  = "terraform"
  }

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "twilio_from" {
  secret      = google_secret_manager_secret.twilio_from.id
  secret_data = var.twilio_from
}

resource "google_secret_manager_secret_iam_member" "twilio_from_access" {
  project   = var.project_id
  secret_id = google_secret_manager_secret.twilio_from.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${var.service_account_email}"
}

# -----------------------------------------------------------------------
# Deadman owner phone number
# -----------------------------------------------------------------------

resource "google_secret_manager_secret" "deadman_phone" {
  project   = var.project_id
  secret_id = "deadman-phone-${var.environment}"

  labels = {
    app         = "nexus-deadman"
    environment = var.environment
    managed_by  = "terraform"
  }

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "deadman_phone" {
  secret      = google_secret_manager_secret.deadman_phone.id
  secret_data = var.deadman_phone
}

resource "google_secret_manager_secret_iam_member" "deadman_phone_access" {
  project   = var.project_id
  secret_id = google_secret_manager_secret.deadman_phone.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${var.service_account_email}"
}

# -----------------------------------------------------------------------
# Database password
# -----------------------------------------------------------------------

resource "google_secret_manager_secret" "deadman_db_password" {
  project   = var.project_id
  secret_id = "deadman-db-password-${var.environment}"

  labels = {
    app         = "nexus-deadman"
    environment = var.environment
    managed_by  = "terraform"
  }

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "deadman_db_password" {
  secret      = google_secret_manager_secret.deadman_db_password.id
  secret_data = var.deadman_db_password
}

resource "google_secret_manager_secret_iam_member" "deadman_db_password_access" {
  project   = var.project_id
  secret_id = google_secret_manager_secret.deadman_db_password.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${var.service_account_email}"
}

# -----------------------------------------------------------------------
# Outputs
# -----------------------------------------------------------------------

output "twilio_account_sid_secret_id" {
  description = "Twilio Account SID secret ID"
  value       = google_secret_manager_secret.twilio_account_sid.secret_id
}

output "twilio_auth_token_secret_id" {
  description = "Twilio Auth Token secret ID"
  value       = google_secret_manager_secret.twilio_auth_token.secret_id
}

output "twilio_from_secret_id" {
  description = "Twilio From phone secret ID"
  value       = google_secret_manager_secret.twilio_from.secret_id
}

output "deadman_phone_secret_id" {
  description = "Deadman owner phone secret ID"
  value       = google_secret_manager_secret.deadman_phone.secret_id
}

output "deadman_db_password_secret_id" {
  description = "Deadman DB password secret ID"
  value       = google_secret_manager_secret.deadman_db_password.secret_id
}
