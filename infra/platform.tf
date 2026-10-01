# APIs, container registry, file storage and secrets.

resource "google_project_service" "apis" {
  for_each = toset(local.apis)
  service  = each.key
  # Never switch an API off just because it left this config: other things
  # in the project may depend on it.
  disable_on_destroy = false
}

resource "google_artifact_registry_repository" "cortex" {
  location      = var.region
  repository_id = "cortex"
  format        = "DOCKER"
  description   = "CortexAI container images"

  # Keeps storage near the free 0.5 GB: the 3 newest images per service stay,
  # anything else older than a day is deleted.
  cleanup_policy_dry_run = false
  cleanup_policies {
    id     = "keep-latest-3"
    action = "KEEP"
    most_recent_versions {
      keep_count = 3
    }
  }
  cleanup_policies {
    id     = "delete-older"
    action = "DELETE"
    condition {
      older_than = "86400s"
    }
  }

  depends_on = [google_project_service.apis]
}

resource "google_storage_bucket" "files" {
  name                        = var.gcs_bucket
  location                    = upper(var.region)
  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"

  # User files live here; Terraform must never delete or replace the bucket.
  lifecycle {
    prevent_destroy = true
  }
}

# Only the secret *containers* are managed here. Values are uploaded by
# deploy/gcp/bootstrap.sh so they never appear in Terraform state or code.
resource "google_secret_manager_secret" "app" {
  for_each  = var.secrets
  secret_id = each.key

  replication {
    auto {}
  }

  lifecycle {
    prevent_destroy = true
  }

  depends_on = [google_project_service.apis]
}

# Secrets always exist before Terraform runs (bootstrap.sh creates them while
# uploading their values), so they are imported in every project, new or old.
import {
  for_each = var.secrets
  to       = google_secret_manager_secret.app[each.key]
  id       = "projects/${var.project_id}/secrets/${each.key}"
}

resource "google_secret_manager_secret_iam_member" "access" {
  for_each  = local.secret_access
  secret_id = google_secret_manager_secret.app[each.value.secret].id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.runtime[each.value.service].email}"
}

resource "google_storage_bucket_iam_member" "agent_files" {
  bucket = google_storage_bucket.files.name
  role   = "roles/storage.objectAdmin"
  member = "serviceAccount:${google_service_account.runtime["agent"].email}"
}
