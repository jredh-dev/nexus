# Nexus Cal - Development Environment

terraform {
  required_version = ">= 1.6.0"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 5.0"
    }
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
}

locals {
  environment  = "dev"
  service_name = "nexus-cal-${local.environment}"
  bucket_name  = "nexus-cal-${local.environment}-data"

  common_labels = {
    app         = "nexus-cal"
    environment = local.environment
    managed_by  = "terraform"
  }
}

# GCS bucket for SQLite persistence
resource "google_storage_bucket" "cal_data" {
  project                     = var.project_id
  name                        = local.bucket_name
  location                    = var.region
  force_destroy               = false
  uniform_bucket_level_access = true

  labels = local.common_labels
}

# Grant the existing service account access to the bucket
resource "google_storage_bucket_iam_member" "cal_data_access" {
  bucket = google_storage_bucket.cal_data.name
  role   = "roles/storage.objectAdmin"
  member = "serviceAccount:${var.service_account_email}"
}

# Cloud Run service with GCS volume mount
module "cloud_run" {
  source = "../../modules/cloud-run-gcs"

  project_id            = var.project_id
  region                = var.region
  service_name          = local.service_name
  image                 = var.cloud_run_image
  service_account_email = var.service_account_email
  port                  = 8085

  environment_variables = {
    ENVIRONMENT = local.environment
    CAL_PORT    = "8085"
    CAL_DB_PATH = "/data/cal.db"
  }

  gcs_bucket = google_storage_bucket.cal_data.name
  mount_path = "/data"

  memory        = "512Mi"
  cpu           = "1"
  min_instances = 0
  max_instances = 3

  allow_unauthenticated = true
  labels                = local.common_labels
}
