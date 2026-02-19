# Firebase Configuration
# Note: Some Firebase resources (like Authentication providers) must be configured
# manually via Firebase Console as they're not fully supported by Terraform

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
  description = "Firestore database region"
  type        = string
  default     = "us-central1"
}

# Note: Firebase project initialization is typically done manually or via Firebase CLI
# The google_firebase_project resource can be used if the project isn't already a Firebase project

# Firestore Database
resource "google_firestore_database" "database" {
  project     = var.project_id
  name        = "(default)"
  location_id = var.region
  type        = "FIRESTORE_NATIVE"

  # Prevent accidental deletion
  lifecycle {
    prevent_destroy = true
  }
}

# Outputs
output "database_name" {
  description = "Firestore database name"
  value       = google_firestore_database.database.name
}

output "database_location" {
  description = "Firestore database location"
  value       = google_firestore_database.database.location_id
}

output "console_link" {
  description = "Firebase Console link"
  value       = "https://console.firebase.google.com/project/${var.project_id}"
}

# Note: Firebase Authentication providers (Email/Password, Google, etc.)
# must be configured manually via Firebase Console:
# https://console.firebase.google.com/project/${var.project_id}/authentication/providers
