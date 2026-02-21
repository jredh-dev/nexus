# Development Environment Variables

variable "project_id" {
  description = "GCP Project ID"
  type        = string
  default     = "dea-noctua"
}

variable "region" {
  description = "GCP region"
  type        = string
  default     = "us-central1"
}

variable "cloud_run_image" {
  description = "Cloud Run container image URL"
  type        = string
  default     = "gcr.io/dea-noctua/nexus-web-dev:latest"
}

variable "service_account_email" {
  description = "Service account email for Cloud Run (shared with portal)"
  type        = string
}

variable "portal_url" {
  description = "Portal backend URL for SSR proxy"
  type        = string
  default     = "https://nexus-portal-dev-2tvic4xjjq-uc.a.run.app"
}
