# Adopts resources that already exist (created earlier by the old setup.sh and
# `gcloud run deploy`) into Terraform state instead of recreating them.
#
# Terraform reads each one, records it in state, and from then on manages it.
# Once `terraform apply` has succeeded these blocks are no-ops. For a brand-new
# project, delete this file: Terraform will create everything instead.
#
# IAM *member* bindings are not listed: creating a binding that already exists
# is a harmless no-op, so Terraform simply takes them over.

import {
  for_each = toset(local.apis)
  to       = google_project_service.apis[each.key]
  id       = "${var.project_id}/${each.key}"
}

import {
  to = google_artifact_registry_repository.cortex
  id = "projects/${var.project_id}/locations/${var.region}/repositories/cortex"
}

import {
  to = google_storage_bucket.files
  id = var.gcs_bucket
}

import {
  for_each = toset(local.services)
  to       = google_service_account.runtime[each.key]
  id       = "projects/${var.project_id}/serviceAccounts/cortex-${each.key}@${var.project_id}.iam.gserviceaccount.com"
}

import {
  to = google_service_account.deployer
  id = "projects/${var.project_id}/serviceAccounts/cortex-deployer@${var.project_id}.iam.gserviceaccount.com"
}

import {
  to = google_iam_workload_identity_pool.github
  id = "projects/${var.project_id}/locations/global/workloadIdentityPools/github"
}

import {
  to = google_iam_workload_identity_pool_provider.github
  id = "projects/${var.project_id}/locations/global/workloadIdentityPools/github/providers/github-oidc"
}

import {
  for_each = local.run
  to       = google_cloud_run_v2_service.svc[each.key]
  id       = "projects/${var.project_id}/locations/${var.region}/services/cortex-${each.key}"
}
