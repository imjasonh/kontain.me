locals {
  apps = {
    "apko" : {
      cpu                   = 1
      ram                   = "512Mi"
      container_concurrency = 1
      timeout_seconds       = 900 # 15m
      base_image            = "cgr.dev/chainguard/static:latest-glibc"
    }
    "flatten" : {
      cpu                   = 1
      ram                   = "1Gi"
      container_concurrency = 80
      timeout_seconds       = 120 # 2m
      base_image            = "cgr.dev/chainguard/static:latest-glibc"
    }
    "ko" : {
      cpu                   = 2
      ram                   = "4Gi"
      container_concurrency = 1
      timeout_seconds       = 900 # 15m
      base_image            = "cgr.dev/chainguard/go:latest-dev"
    }
    "mirror" : {
      cpu                   = 2
      ram                   = "4Gi"
      container_concurrency = 1
      timeout_seconds       = 900 # 15m
      base_image            = "cgr.dev/chainguard/static:latest-glibc"
    }
    "random" : {
      cpu                   = 1
      ram                   = "256Mi"
      container_concurrency = 1000
      timeout_seconds       = 60 # 1m
      base_image            = "cgr.dev/chainguard/static:latest-glibc"
    }
    wait : {
      cpu                   = 1
      ram                   = "1Gi"
      container_concurrency = 80
      timeout_seconds       = 60 # 1m
      base_image            = "cgr.dev/chainguard/static:latest-glibc"
    }
  }
}

module "app" {
  for_each   = local.apps
  depends_on = [google_project_service.run-api]
  source     = "./module"
  name       = each.key

  project_id           = var.project_id
  location             = var.location
  domain               = var.domain
  dns_zone             = local.dns_zone
  bucket               = google_storage_bucket.bucket.name
  service_account_name = google_service_account.service_account.email

  cpu                   = each.value.cpu
  ram                   = each.value.ram
  container_concurrency = each.value.container_concurrency
  timeout_seconds       = each.value.timeout_seconds
  base_image            = each.value.base_image
}

output "cloudrun_url" { value = { for k, _ in local.apps : k => module.app[k].cloudrun_url } }
output "vanity_url" { value = { for k, _ in local.apps : k => module.app[k].vanity_url } }
