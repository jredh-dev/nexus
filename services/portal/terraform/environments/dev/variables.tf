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
  default     = "gcr.io/dea-noctua/nexus-portal-dev:latest"
}

variable "firebase_credentials" {
  description = "Firebase service account JSON credentials (as string)"
  type        = string
  sensitive   = true
}

variable "session_secret" {
  description = "Session encryption secret key (base64 encoded, 32+ bytes)"
  type        = string
  sensitive   = true
}

variable "jwt_signing_key" {
  description = "HMAC-SHA256 key shared across all nexus services for cross-service JWT auth (base64 encoded, 32+ bytes)"
  type        = string
  sensitive   = true
}
