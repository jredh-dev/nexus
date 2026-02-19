# Terraform Backend Configuration
# Stores state in Google Cloud Storage for persistence and collaboration

terraform {
  backend "gcs" {
    bucket = "dea-noctua-terraform-state"
    prefix = "nexus/portal/dev"
  }
}

# Note: The GCS bucket must be created manually before running terraform init:
#
# gcloud storage buckets create gs://dea-noctua-terraform-state \
#   --project=dea-noctua \
#   --location=us-central1 \
#   --uniform-bucket-level-access \
#   --enable-versioning
#
# Then run: terraform init
