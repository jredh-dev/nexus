# DNS Module for deadman switch
# Creates a CNAME record pointing deadman.jredh.com → ghs.googlehosted.com
# to back the Cloud Run domain mapping.
#
# Prereq: Cloud DNS zone jredh-com must already exist in project dea-noctua.

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

variable "dns_zone_name" {
  description = "Cloud DNS managed zone name (not the DNS name — the zone resource name)"
  type        = string
  default     = "jredh-com"
}

variable "custom_domain" {
  description = "Custom domain for the service (must end without trailing dot)"
  type        = string
  default     = "deadman.jredh.com"
}

variable "cname_target" {
  description = "CNAME target (Cloud Run domain mapping endpoint)"
  type        = string
  default     = "ghs.googlehosted.com."
}

# -----------------------------------------------------------------------
# CNAME record: deadman.jredh.com → ghs.googlehosted.com
# The trailing dot on dns_name is required by Cloud DNS.
# -----------------------------------------------------------------------
resource "google_dns_record_set" "cname" {
  project      = var.project_id
  managed_zone = var.dns_zone_name
  name         = "${var.custom_domain}."
  type         = "CNAME"
  ttl          = 300

  rrdatas = [var.cname_target]
}

# -----------------------------------------------------------------------
# Outputs
# -----------------------------------------------------------------------
output "dns_name" {
  description = "The DNS record name created"
  value       = google_dns_record_set.cname.name
}
