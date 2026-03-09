# Nexus Deadman - Development Environment
# Provisions Cloud SQL, secrets, DNS, and Cloud Run service for the deadman switch.

terraform {
  required_version = ">= 1.6.0"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 5.0"
    }
    google-beta = {
      source  = "hashicorp/google-beta"
      version = "~> 5.0"
    }
  }
}

# Provider configuration
provider "google" {
  project = var.project_id
  region  = var.region
}

provider "google-beta" {
  project = var.project_id
  region  = var.region
}

# Local variables
locals {
  environment  = "dev"
  service_name = "nexus-deadman-${local.environment}"

  common_labels = {
    app         = "nexus-deadman"
    environment = local.environment
    managed_by  = "terraform"
  }
}

# Data source: reference the existing github-actions-ci service account.
# The portal IAM module creates "github-actions-{env}" = "github-actions-dev", which does
# NOT match the real SA name "github-actions-ci". Using a data source avoids any replacement.
data "google_service_account" "github_actions" {
  project    = var.project_id
  account_id = "github-actions-ci"
}

# Module: Cloud SQL (PostgreSQL 16, us-west1, shared instance nexus-dev-west1)
module "cloud_sql" {
  source                = "../../modules/cloud-sql"
  project_id            = var.project_id
  region                = var.region
  instance_name         = "nexus-dev-west1"
  database_name         = "deadman"
  db_user               = "deadman"
  service_account_email = data.google_service_account.github_actions.email
}

# Module: Secret Manager
module "secrets" {
  source                = "../../modules/secrets"
  project_id            = var.project_id
  environment           = local.environment
  service_account_email = data.google_service_account.github_actions.email
  twilio_account_sid    = var.twilio_account_sid
  twilio_auth_token     = var.twilio_auth_token
  twilio_from           = var.twilio_from
  deadman_phone         = var.deadman_phone
  deadman_db_password   = var.deadman_db_password
}

# Module: Cloud DNS — CNAME for deadman.jredh.com
module "dns" {
  source        = "../../modules/dns"
  project_id    = var.project_id
  dns_zone_name = var.dns_zone_name
  custom_domain = var.custom_domain
}

# Module: Cloud Run
module "cloud_run" {
  source                = "../../modules/cloud-run"
  project_id            = var.project_id
  region                = var.region
  service_name          = local.service_name
  image                 = var.cloud_run_image
  service_account_email = data.google_service_account.github_actions.email

  # Non-secret config.
  # NOTE: do NOT include PORT here — Cloud Run reserves it.
  environment_variables = {
    ENVIRONMENT               = local.environment
    CLOUD_SQL_CONNECTION_NAME = module.cloud_sql.connection_name
    DEADMAN_PUBLIC_URL        = var.deadman_public_url
  }

  # Secrets injected as env vars from Secret Manager.
  # DEADMAN_DB_PASSWORD is the raw password; entrypoint.sh builds the full DSN.
  secrets = {
    TWILIO_ACCOUNT_SID  = "${module.secrets.twilio_account_sid_secret_id}:latest"
    TWILIO_AUTH_TOKEN   = "${module.secrets.twilio_auth_token_secret_id}:latest"
    TWILIO_FROM         = "${module.secrets.twilio_from_secret_id}:latest"
    DEADMAN_PHONE       = "${module.secrets.deadman_phone_secret_id}:latest"
    DEADMAN_DB_PASSWORD = "${module.secrets.deadman_db_password_secret_id}:latest"
  }

  cloud_sql_connection_name = module.cloud_sql.connection_name
  custom_domain             = var.custom_domain

  memory                = "256Mi"
  cpu                   = "1"
  min_instances         = 0
  max_instances         = 5
  allow_unauthenticated = true # Twilio must be able to POST /sms

  labels = local.common_labels

  depends_on = [module.secrets, module.cloud_sql]
}
