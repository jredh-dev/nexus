# GCP Project APIs and Configuration
# Enables required Google Cloud APIs for the nexus-portal application

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

# Enable required APIs
locals {
  required_apis = [
    "run.googleapis.com",                  # Cloud Run
    "secretmanager.googleapis.com",        # Secret Manager
    "iam.googleapis.com",                  # IAM (for Workload Identity)
    "iamcredentials.googleapis.com",       # IAM Credentials (for WIF)
    "cloudresourcemanager.googleapis.com", # Resource Manager
    "firebase.googleapis.com",             # Firebase
    "firestore.googleapis.com",            # Firestore
    "identitytoolkit.googleapis.com",      # Firebase Authentication
    "storage-api.googleapis.com",          # Cloud Storage (for GCR)
    "cloudapis.googleapis.com",            # Google Cloud APIs
    "sts.googleapis.com",                  # Security Token Service (for WIF)
  ]
}

resource "google_project_service" "apis" {
  for_each = toset(local.required_apis)

  project                    = var.project_id
  service                    = each.value
  disable_on_destroy         = false
  disable_dependent_services = false
}

# Output project information
output "project_id" {
  description = "GCP Project ID"
  value       = var.project_id
}

output "enabled_apis" {
  description = "List of enabled APIs"
  value       = local.required_apis
}
