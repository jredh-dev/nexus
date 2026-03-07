# Terraform Backend Configuration
# Stores state in GCS alongside other nexus services.

terraform {
  backend "gcs" {
    bucket = "dea-noctua-terraform-state"
    prefix = "nexus/deadman/dev"
  }
}

# Note: The GCS bucket must already exist (shared with portal).
# Run: terraform init
