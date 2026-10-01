output "site_url" {
  description = "The public site (gateway)."
  value       = google_cloud_run_v2_service.svc["gateway"].uri
}

output "workload_identity_provider" {
  description = "Used by GitHub Actions (google-github-actions/auth)."
  value       = google_iam_workload_identity_pool_provider.github.name
}

output "deployer_service_account" {
  value = google_service_account.deployer.email
}
