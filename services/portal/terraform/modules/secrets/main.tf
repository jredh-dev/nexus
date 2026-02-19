# Secret Manager Configuration
# Manages secrets for Firebase credentials and session keys

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

variable "firebase_credentials" {
  description = "Firebase service account JSON credentials"
  type        = string
  sensitive   = true
}

variable "session_secret" {
  description = "Session encryption secret key"
  type        = string
  sensitive   = true
}

# Firebase credentials secret
resource "google_secret_manager_secret" "firebase_credentials" {
  project   = var.project_id
  secret_id = "firebase-credentials-${var.environment}"

  labels = {
    app         = "nexus-portal"
    environment = var.environment
    managed_by  = "terraform"
  }

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "firebase_credentials" {
  secret      = google_secret_manager_secret.firebase_credentials.id
  secret_data = var.firebase_credentials
}

resource "google_secret_manager_secret_iam_member" "firebase_credentials_access" {
  project   = var.project_id
  secret_id = google_secret_manager_secret.firebase_credentials.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${var.service_account_email}"
}

# Session secret
resource "google_secret_manager_secret" "session_secret" {
  project   = var.project_id
  secret_id = "session-secret-${var.environment}"

  labels = {
    app         = "nexus-portal"
    environment = var.environment
    managed_by  = "terraform"
  }

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "session_secret" {
  secret      = google_secret_manager_secret.session_secret.id
  secret_data = var.session_secret
}

resource "google_secret_manager_secret_iam_member" "session_secret_access" {
  project   = var.project_id
  secret_id = google_secret_manager_secret.session_secret.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${var.service_account_email}"
}

# Outputs
output "firebase_credentials_secret_id" {
  description = "Firebase credentials secret ID"
  value       = google_secret_manager_secret.firebase_credentials.secret_id
}

output "session_secret_secret_id" {
  description = "Session secret ID"
  value       = google_secret_manager_secret.session_secret.secret_id
}

output "firebase_credentials_name" {
  description = "Firebase credentials full resource name"
  value       = google_secret_manager_secret.firebase_credentials.name
}

output "session_secret_name" {
  description = "Session secret full resource name"
  value       = google_secret_manager_secret.session_secret.name
}
