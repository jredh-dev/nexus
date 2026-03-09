# Cloud Run Service Configuration for deadman switch
# Mirrors the portal cloud-run module pattern with additions:
#   - google-beta provider for domain mapping
#   - Cloud SQL instance annotation (Unix socket via proxy)
#   - Optional custom domain mapping
#   - CLOUD_SQL_CONNECTION_NAME + DEADMAN_PUBLIC_URL env vars

terraform {
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

variable "project_id" {
  description = "GCP Project ID"
  type        = string
}

variable "region" {
  description = "GCP region for Cloud Run service"
  type        = string
  default     = "us-west1"
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
  description = "Environment variables for the service (do NOT include PORT — Cloud Run reserves it)"
  type        = map(string)
  default     = {}
}

variable "secrets" {
  description = "Map of env var names to Secret Manager secret references (secret_id:version)"
  type        = map(string)
  default     = {}
}

variable "memory" {
  description = "Memory limit for the service"
  type        = string
  default     = "256Mi"
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
  default     = 5
}

variable "allow_unauthenticated" {
  description = "Allow unauthenticated access (required for Twilio webhook)"
  type        = bool
  default     = true
}

variable "labels" {
  description = "Labels to apply to the service"
  type        = map(string)
  default     = {}
}

variable "cloud_sql_connection_name" {
  description = "Cloud SQL connection name (project:region:instance). Empty string disables."
  type        = string
  default     = ""
}

variable "custom_domain" {
  description = "Custom domain to map to the service (e.g. deadman.jredh.com). Empty string skips."
  type        = string
  default     = ""
}

# -----------------------------------------------------------------------
# Cloud Run Service
# -----------------------------------------------------------------------
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
          # Cloud Run injects PORT=8080 at runtime; deadman reads it via os.Getenv("PORT").
          container_port = 8080
        }

        resources {
          limits = {
            memory = var.memory
            cpu    = var.cpu
          }
        }

        # Plain environment variables.
        # IMPORTANT: do NOT pass PORT here — Cloud Run reserves it and will reject the deploy.
        dynamic "env" {
          for_each = var.environment_variables
          content {
            name  = env.key
            value = env.value
          }
        }

        # Secrets injected as environment variables from Secret Manager.
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

      container_concurrency = 80
    }

    metadata {
      annotations = merge(
        {
          "autoscaling.knative.dev/minScale" = tostring(var.min_instances)
          "autoscaling.knative.dev/maxScale" = tostring(var.max_instances)
          "run.googleapis.com/client-name"   = "terraform"
        },
        # Attach Cloud SQL instance so the proxy socket is available in /cloudsql/<connection>.
        var.cloud_sql_connection_name != "" ? {
          "run.googleapis.com/cloudsql-instances" = var.cloud_sql_connection_name
        } : {}
      )

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

  # Ignore image changes — CI/CD handles deployments after Terraform bootstraps.
  lifecycle {
    ignore_changes = [
      template[0].spec[0].containers[0].image,
      template[0].metadata[0].annotations["run.googleapis.com/client-version"],
      template[0].metadata[0].annotations["client.knative.dev/user-image"],
    ]
  }
}

# Allow unauthenticated access so Twilio can POST to /sms
resource "google_cloud_run_service_iam_member" "public_access" {
  count = var.allow_unauthenticated ? 1 : 0

  project  = var.project_id
  location = var.region
  service  = google_cloud_run_service.service.name
  role     = "roles/run.invoker"
  member   = "allUsers"
}

# -----------------------------------------------------------------------
# Domain Mapping (google-beta — GA resource not yet stable)
# Only created when custom_domain is non-empty.
# -----------------------------------------------------------------------
resource "google_cloud_run_domain_mapping" "mapping" {
  count    = var.custom_domain != "" ? 1 : 0
  provider = google-beta

  project  = var.project_id
  location = var.region
  name     = var.custom_domain

  metadata {
    namespace = var.project_id
  }

  spec {
    route_name = google_cloud_run_service.service.name
  }

  # Domain mapping status changes are driven externally (DNS propagation).
  # certificate_mode defaults to AUTOMATIC but is not returned by the API after creation,
  # causing Terraform to want to replace the resource. Ignore it.
  lifecycle {
    ignore_changes = [
      metadata[0].annotations,
      spec[0].certificate_mode,
    ]
  }
}

# -----------------------------------------------------------------------
# Outputs
# -----------------------------------------------------------------------
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
