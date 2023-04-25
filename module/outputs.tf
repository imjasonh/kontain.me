output "cloudrun_url" {
  value = google_cloud_run_service.service.status[0].url
}
output "vanity_url" {
  value = google_cloud_run_domain_mapping.mapping.name
}
