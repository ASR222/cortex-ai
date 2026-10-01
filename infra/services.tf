# The five Cloud Run services, their identities and who may call them.

resource "google_service_account" "runtime" {
  for_each     = toset(local.services)
  account_id   = "cortex-${each.key}"
  display_name = "CortexAI ${each.key}"
}

resource "google_cloud_run_v2_service" "svc" {
  for_each            = local.run
  name                = "cortex-${each.key}"
  location            = var.region
  ingress             = "INGRESS_TRAFFIC_ALL"
  deletion_protection = true

  template {
    service_account                  = google_service_account.runtime[each.key].email
    timeout                          = "300s"
    max_instance_request_concurrency = 80

    # Scale to zero when idle (free tier); cap instances as a cost guardrail.
    scaling {
      min_instance_count = each.value.min_instances
      max_instance_count = var.max_instances
    }

    containers {
      # Placeholder for brand-new services only. CI deploys real images with
      # `gcloud run services update --image`, and Terraform ignores the image
      # (see lifecycle below), so code deploys and infra changes don't fight.
      image = "us-docker.pkg.dev/cloudrun/container/hello"

      resources {
        limits = {
          cpu    = "1"
          memory = each.value.memory
        }
        cpu_idle          = true # CPU only while handling requests (request-based billing)
        startup_cpu_boost = true # shorter cold starts
      }

      dynamic "env" {
        for_each = each.value.env
        content {
          name  = env.key
          value = env.value
        }
      }

      dynamic "env" {
        for_each = local.service_secrets[each.key]
        content {
          name = local.secret_env[env.value]
          value_source {
            secret_key_ref {
              secret  = google_secret_manager_secret.app[env.value].secret_id
              version = "latest"
            }
          }
        }
      }
    }
  }

  lifecycle {
    ignore_changes = [
      client,
      client_version,
      template[0].containers[0].image,
    ]
  }

  depends_on = [
    google_project_service.apis,
    google_secret_manager_secret_iam_member.access,
  ]
}

# Internal services: only the specific callers in local.invokers.
resource "google_cloud_run_v2_service_iam_member" "invoker" {
  for_each = local.invokers
  name     = google_cloud_run_v2_service.svc[each.value.target].name
  location = var.region
  role     = "roles/run.invoker"
  member   = "serviceAccount:${google_service_account.runtime[each.value.caller].email}"
}

# The gateway is the only public service.
resource "google_cloud_run_v2_service_iam_member" "public_gateway" {
  name     = google_cloud_run_v2_service.svc["gateway"].name
  location = var.region
  role     = "roles/run.invoker"
  member   = "allUsers"
}
