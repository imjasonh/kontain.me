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

resource "ko_build" "image" {
  for_each = local.apps

  repo       = "gcr.io/${var.project_id}/${each.key}"
  importpath = "github.com/imjasonh/kontain.me/cmd/${each.key}"
  base_image = each.value.base_image
}

resource "google_cloud_run_service" "service" {
  for_each = local.apps
  name     = each.key
  location = var.location

  template {
    spec {
      service_account_name  = google_service_account.service_account.email
      container_concurrency = each.value.container_concurrency
      timeout_seconds       = each.value.timeout_seconds
      containers {
        image = ko_build.image[each.key].image_ref
        env {
          name  = "BUCKET"
          value = google_storage_bucket.bucket.name
        }
        resources {
          limits = {
            cpu    = each.value.cpu
            memory = each.value.ram
          }
          requests = {
            cpu    = each.value.cpu
            memory = each.value.ram
          }
        }
      }
    }
  }
}

data "google_iam_policy" "noauth" {
  binding {
    role    = "roles/run.invoker"
    members = ["allUsers"]
  }
}

resource "google_cloud_run_service_iam_policy" "noauth" {
  for_each = local.apps
  location = var.location
  service  = google_cloud_run_service.service[each.key].name

  policy_data = data.google_iam_policy.noauth.policy_data
}

resource "google_cloud_run_domain_mapping" "mapping" {
  for_each = local.apps
  location = var.location
  name     = "${each.key}.${var.domain}"

  metadata {
    namespace = var.project_id
  }

  spec {
    route_name = google_cloud_run_service.service[each.key].name
  }
}

resource "google_dns_record_set" "dns-record" {
  for_each = local.apps

  name = "${each.key}.${var.domain}."
  type = google_cloud_run_domain_mapping.mapping[each.key].status[0].resource_records[0].type
  ttl  = 300

  managed_zone = google_dns_managed_zone.zone.name

  rrdatas = [google_cloud_run_domain_mapping.mapping[each.key].status[0].resource_records[0].rrdata]
}

resource "null_resource" "test" {
  for_each   = local.apps
  depends_on = [google_dns_record_set.dns-record]
  provisioner "local-exec" { command = file("${path.module}/cmd/${each.key}/test.sh") }
}

output "cloudrun_url" { value = { for k, _ in local.apps : k => google_cloud_run_service.service[k].status[0].url } }
output "vanity_url" { value = { for k, _ in local.apps : k => google_cloud_run_domain_mapping.mapping[k].name } }
