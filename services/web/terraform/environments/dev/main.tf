# Nexus Web Frontend - Development Environment

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
  service_name = "nexus-web-${local.environment}"
  github_org   = "jredh-dev"
  github_repo  = "nexus"

  common_labels = {
    app         = "nexus-web"
    environment = local.environment
    managed_by  = "terraform"
  }
}

# Reuse portal's IAM module — same WIF setup, same service account
# The web frontend shares the GitHub Actions service account with portal
# since they're in the same repo and use the same WIF pool.

# Module: Cloud Run
module "cloud_run" {
  source                = "../../../portal/terraform/modules/cloud-run"
  project_id            = var.project_id
  region                = var.region
  service_name          = local.service_name
  image                 = var.cloud_run_image
  service_account_email = var.service_account_email

  environment_variables = {
    ENVIRONMENT = local.environment
    PORTAL_URL  = var.portal_url
    HOST        = "0.0.0.0"
    PORT        = "8080"
  }

  secrets = {}

  memory                = "256Mi"
  cpu                   = "1"
  min_instances         = 0
  max_instances         = 5
  allow_unauthenticated = true

  labels = local.common_labels
}
