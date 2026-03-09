#!/usr/bin/env bash
# import.sh — Import manually created GCP resources into Terraform state.
#
# Run this ONCE from the environments/dev/ directory after `terraform init`.
# Requires Application Default Credentials with owner/editor access to dea-noctua.
#
# Usage:
#   cd services/deadman/terraform/environments/dev
#   terraform init
#   bash import.sh
#
# Safe to re-run: terraform import is idempotent (it errors on duplicates but
# won't corrupt state).

set -euo pipefail

PROJECT="dea-noctua"
REGION="us-west1"
INSTANCE="nexus-dev-west1"
SA="github-actions-ci@dea-noctua.iam.gserviceaccount.com"

echo "==> Importing Cloud SQL instance"
terraform import \
  module.cloud_sql.google_sql_database_instance.instance \
  "projects/${PROJECT}/instances/${INSTANCE}"

echo "==> Importing Cloud SQL database"
terraform import \
  module.cloud_sql.google_sql_database.database \
  "${PROJECT}/${INSTANCE}/deadman"

echo "==> Importing Cloud SQL user"
terraform import \
  module.cloud_sql.google_sql_user.user \
  "${INSTANCE}/deadman"

echo "==> Importing cloudsql.client IAM binding"
terraform import \
  module.cloud_sql.google_project_iam_member.cloudsql_client \
  "${PROJECT} roles/cloudsql.client serviceAccount:${SA}"

echo "==> Importing GCP Secret: deadman-db-password-dev"
terraform import \
  module.secrets.google_secret_manager_secret.deadman_db_password \
  "projects/${PROJECT}/secrets/deadman-db-password-dev"

echo "==> Importing GCP Secret version: deadman-db-password-dev/1"
terraform import \
  module.secrets.google_secret_manager_secret_version.deadman_db_password \
  "projects/${PROJECT}/secrets/deadman-db-password-dev/versions/1"

echo "==> Importing Cloud Run domain mapping: deadman.jredh.com"
terraform import \
  module.cloud_run.google_cloud_run_domain_mapping.mapping[0] \
  "locations/${REGION}/namespaces/${PROJECT}/domainmappings/deadman.jredh.com"

echo "==> Importing Cloud DNS CNAME: deadman.jredh.com"
terraform import \
  module.dns.google_dns_record_set.cname \
  "projects/${PROJECT}/managedZones/jredh-com/rrsets/deadman.jredh.com./CNAME"

echo ""
echo "All imports complete. Run 'terraform plan' to verify no unexpected diffs."
