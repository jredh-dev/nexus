# Cloud Run Service Configuration

terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 5.0"
    }
  }
}

variable "project_id" {
  description = "GCP Project ID"
  type        = string
}

variable "region" {
  description = "GCP region for Cloud Run service"
  type        = string
  default     = "us-central1"
}

variable "service_name" {
  description = "Cloud Run service name"
  type        = string
}

variable "image" {
  description = "Container image URL (e.g., gcr.io/project/image:tag)"
  type        = string
}

variable "service_account_email" {
  description = "Service account email for the Cloud Run service"
  type        = string
}

variable "environment_variables" {
  description = "Environment variables for the service"
  type        = map(string)
  default     = {}
}

variable "secrets" {
  description = "Map of secret names to Secret Manager secret references (name:version)"
  type        = map(string)
  default     = {}
}

variable "memory" {
  description = "Memory limit for the service"
  type        = string
  default     = "512Mi"
}

variable "cpu" {
  description = "CPU limit for the service"
  type        = string
  default     = "1"
}

variable "min_instances" {
  description = "Minimum number of instances"
  type        = number
  default     = 0
}

variable "max_instances" {
  description = "Maximum number of instances"
  type        = number
  default     = 10
}

variable "allow_unauthenticated" {
  description = "Allow unauthenticated access"
  type        = bool
  default     = true
}

variable "labels" {
  description = "Labels to apply to the service"
  type        = map(string)
  default     = {}
}

# Cloud Run Service
resource "google_cloud_run_service" "service" {
  project  = var.project_id
  name     = var.service_name
  location = var.region

  template {
    spec {
      service_account_name = var.service_account_email

      containers {
        image = var.image

        ports {
          container_port = 8080
        }

        resources {
          limits = {
            memory = var.memory
            cpu    = var.cpu
          }
        }

        # Environment variables
        dynamic "env" {
          for_each = var.environment_variables
          content {
            name  = env.key
            value = env.value
          }
        }

        # Secrets as environment variables
        dynamic "env" {
          for_each = var.secrets
          content {
            name = env.key
            value_from {
              secret_key_ref {
                name = split(":", env.value)[0]
                key  = split(":", env.value)[1]
              }
            }
          }
        }
      }

      # Auto-scaling
      container_concurrency = 80
    }

    metadata {
      annotations = {
        "autoscaling.knative.dev/minScale" = tostring(var.min_instances)
        "autoscaling.knative.dev/maxScale" = tostring(var.max_instances)
        "run.googleapis.com/client-name"   = "terraform"
      }

      labels = merge(
        var.labels,
        {
          "managed-by" = "terraform"
        }
      )
    }
  }

  traffic {
    percent         = 100
    latest_revision = true
  }

  # Prevent Terraform from reverting manual deployments from CI/CD
  lifecycle {
    ignore_changes = [
      template[0].spec[0].containers[0].image,
      template[0].metadata[0].annotations["run.googleapis.com/client-version"],
      template[0].metadata[0].annotations["client.knative.dev/user-image"],
    ]
  }
}

# Allow public access (if enabled)
resource "google_cloud_run_service_iam_member" "public_access" {
  count = var.allow_unauthenticated ? 1 : 0

  project  = var.project_id
  location = var.region
  service  = google_cloud_run_service.service.name
  role     = "roles/run.invoker"
  member   = "allUsers"
}

# Outputs
output "service_url" {
  description = "Cloud Run service URL"
  value       = google_cloud_run_service.service.status[0].url
}

output "service_name" {
  description = "Cloud Run service name"
  value       = google_cloud_run_service.service.name
}

output "service_id" {
  description = "Cloud Run service resource ID"
  value       = google_cloud_run_service.service.id
}
