# Development Environment Outputs

# Project Outputs
output "project_id" {
  description = "GCP Project ID"
  value       = var.project_id
}

output "region" {
  description = "GCP region"
  value       = var.region
}

# IAM Outputs
output "service_account_email" {
  description = "GitHub Actions service account email"
  value       = module.iam.service_account_email
}

output "github_secret_wif_provider" {
  description = "Value for GitHub secret WIF_PROVIDER (add to GitHub repository secrets)"
  value       = module.iam.github_secret_wif_provider
}

output "github_secret_wif_service_account" {
  description = "Value for GitHub secret WIF_SERVICE_ACCOUNT (add to GitHub repository secrets)"
  value       = module.iam.github_secret_wif_service_account
}

# Secrets Outputs
output "firebase_credentials_secret_id" {
  description = "Firebase credentials Secret Manager secret ID"
  value       = module.secrets.firebase_credentials_secret_id
}

output "session_secret_secret_id" {
  description = "Session secret Secret Manager secret ID"
  value       = module.secrets.session_secret_secret_id
}

# Cloud Run Outputs
output "service_url" {
  description = "Cloud Run service URL (DEV environment)"
  value       = module.cloud_run.service_url
}

output "service_name" {
  description = "Cloud Run service name"
  value       = module.cloud_run.service_name
}

# Firebase Outputs
output "firestore_database" {
  description = "Firestore database name"
  value       = module.firebase.database_name
}

output "firebase_console" {
  description = "Firebase Console URL"
  value       = module.firebase.console_link
}

# Summary Output
output "deployment_summary" {
  description = "Deployment summary"
  value = {
    environment     = "dev"
    service_url     = module.cloud_run.service_url
    project_id      = var.project_id
    region          = var.region
    service_account = module.iam.service_account_email
  }
}
