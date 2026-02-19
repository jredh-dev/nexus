# Nexus Portal - Development Environment
# Terraform configuration for the dev environment

terraform {
  required_version = ">= 1.6.0"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 5.0"
    }
  }
}

# Provider configuration
provider "google" {
  project = var.project_id
  region  = var.region
}

# Local variables
locals {
  environment  = "dev"
  service_name = "nexus-portal-${local.environment}"
  github_org   = "jredh-dev"
  github_repo  = "nexus"

  common_labels = {
    app         = "nexus-portal"
    environment = local.environment
    managed_by  = "terraform"
  }
}

# Module: GCP Project APIs
module "project" {
  source     = "../../modules/project"
  project_id = var.project_id
}

# Module: IAM and Workload Identity Federation
module "iam" {
  source      = "../../modules/iam"
  project_id  = var.project_id
  environment = local.environment
  github_org  = local.github_org
  github_repo = local.github_repo

  depends_on = [module.project]
}

# Module: Secret Manager
module "secrets" {
  source                = "../../modules/secrets"
  project_id            = var.project_id
  environment           = local.environment
  service_account_email = module.iam.service_account_email
  firebase_credentials  = var.firebase_credentials
  session_secret        = var.session_secret

  depends_on = [module.project, module.iam]
}

# Module: Firebase
module "firebase" {
  source     = "../../modules/firebase"
  project_id = var.project_id
  region     = var.region

  depends_on = [module.project]
}

# Module: Cloud Run
module "cloud_run" {
  source                = "../../modules/cloud-run"
  project_id            = var.project_id
  region                = var.region
  service_name          = local.service_name
  image                 = var.cloud_run_image
  service_account_email = module.iam.service_account_email

  environment_variables = {
    ENVIRONMENT    = local.environment
    GCP_PROJECT_ID = var.project_id
  }

  secrets = {
    FIREBASE_CREDENTIALS = "${module.secrets.firebase_credentials_secret_id}:latest"
    SESSION_SECRET       = "${module.secrets.session_secret_secret_id}:latest"
  }

  memory                = "512Mi"
  cpu                   = "1"
  min_instances         = 0
  max_instances         = 10
  allow_unauthenticated = true

  labels = local.common_labels

  depends_on = [module.project, module.iam, module.secrets]
}
