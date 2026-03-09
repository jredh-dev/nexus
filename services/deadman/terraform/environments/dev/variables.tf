# Development Environment Variables

variable "project_id" {
  description = "GCP Project ID"
  type        = string
  default     = "dea-noctua"
}

variable "region" {
  description = "GCP region for all resources (Cloud Run, Cloud SQL, etc.)"
  type        = string
  default     = "us-west1"
}

variable "cloud_run_image" {
  description = "Cloud Run container image URL"
  type        = string
  default     = "gcr.io/dea-noctua/nexus-deadman-dev:latest"
}

variable "custom_domain" {
  description = "Custom domain for the Cloud Run service"
  type        = string
  default     = "deadman.jredh.com"
}

variable "dns_zone_name" {
  description = "Cloud DNS managed zone name (resource name, not DNS name)"
  type        = string
  default     = "jredh-com"
}

variable "deadman_public_url" {
  description = "Public URL of the deadman service (injected as DEADMAN_PUBLIC_URL)"
  type        = string
  default     = "https://deadman.jredh.com"
}

variable "twilio_account_sid" {
  description = "Twilio Account SID"
  type        = string
  sensitive   = true
}

variable "twilio_auth_token" {
  description = "Twilio Auth Token"
  type        = string
  sensitive   = true
}

variable "twilio_from" {
  description = "Twilio phone number in E.164 format"
  type        = string
  default     = "+15706006135"
}

variable "deadman_phone" {
  description = "Owner phone number in E.164 format"
  type        = string
  default     = "+6016914667"
}

variable "deadman_db_password" {
  description = "PostgreSQL password for the deadman Cloud SQL user"
  type        = string
  sensitive   = true
}
