# Development Environment Outputs

output "project_id" {
  description = "GCP Project ID"
  value       = var.project_id
}

output "region" {
  description = "GCP region"
  value       = var.region
}

output "service_url" {
  description = "Cloud Run service URL (DEV environment)"
  value       = module.cloud_run.service_url
}

output "service_name" {
  description = "Cloud Run service name"
  value       = module.cloud_run.service_name
}

output "deployment_summary" {
  description = "Deployment summary"
  value = {
    environment = "dev"
    service_url = module.cloud_run.service_url
    project_id  = var.project_id
    region      = var.region
    portal_url  = var.portal_url
  }
}
