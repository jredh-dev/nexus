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
  default     = "gcr.io/dea-noctua/nexus-deadman-dev:latest"
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
  description = "PostgreSQL password for the deadman database"
  type        = string
  sensitive   = true
}
