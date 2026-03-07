# Nexus Deadman - Development Environment
# Provisions secrets and Cloud Run service for the deadman switch.
#
# NOTE: This module does NOT create a Cloud SQL instance — the deadman
# service uses a PostgreSQL connection string injected via DATABASE_URL.
# For dev, this is a Cloud SQL instance you provision separately (or a
# shared PG instance).  Terraform manages the secret; the DB itself is
# out of scope here (like portal's Firebase credentials).

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
  service_name = "nexus-deadman-${local.environment}"
  github_org   = "jredh-dev"
  github_repo  = "nexus"

  common_labels = {
    app         = "nexus-deadman"
    environment = local.environment
    managed_by  = "terraform"
  }
}

# Module: GCP Project APIs — reuse portal's project module
module "project" {
  source     = "../../../portal/terraform/modules/project"
  project_id = var.project_id
}

# Module: IAM and Workload Identity Federation — reuse portal's IAM module
module "iam" {
  source      = "../../../portal/terraform/modules/iam"
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
  twilio_account_sid    = var.twilio_account_sid
  twilio_auth_token     = var.twilio_auth_token
  twilio_from           = var.twilio_from
  deadman_phone         = var.deadman_phone
  deadman_db_password   = var.deadman_db_password

  depends_on = [module.project, module.iam]
}

# Module: Cloud Run
module "cloud_run" {
  source                = "../../modules/cloud-run"
  project_id            = var.project_id
  region                = var.region
  service_name          = local.service_name
  image                 = var.cloud_run_image
  service_account_email = module.iam.service_account_email

  # Non-secret config
  environment_variables = {
    ENVIRONMENT = local.environment
    PORT        = "8080"
  }

  # Secrets injected as env vars from Secret Manager
  secrets = {
    TWILIO_ACCOUNT_SID = "${module.secrets.twilio_account_sid_secret_id}:latest"
    TWILIO_AUTH_TOKEN  = "${module.secrets.twilio_auth_token_secret_id}:latest"
    TWILIO_FROM        = "${module.secrets.twilio_from_secret_id}:latest"
    DEADMAN_PHONE      = "${module.secrets.deadman_phone_secret_id}:latest"
    DATABASE_URL       = "${module.secrets.deadman_db_password_secret_id}:latest"
  }

  memory                = "256Mi"
  cpu                   = "1"
  min_instances         = 0
  max_instances         = 5
  allow_unauthenticated = true # Twilio must be able to POST /sms

  labels = local.common_labels

  depends_on = [module.project, module.iam, module.secrets]
}
