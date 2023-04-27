terraform {
  required_providers {
    ko = {
      source = "ko-build/ko"
    }
    google = {
      source = "hashicorp/google"
    }
  }
}

resource "ko_build" "image" {
  repo       = "gcr.io/${var.project_id}/${var.name}"
  importpath = "github.com/imjasonh/kontain.me/cmd/${var.name}"
  base_image = var.base_image
}

resource "google_cloud_run_service" "service" {
  name     = var.name
  location = var.location

  template {
    spec {
      service_account_name  = var.service_account_name
      container_concurrency = var.container_concurrency
      timeout_seconds       = var.timeout_seconds
      containers {
        image = ko_build.image.image_ref
        env {
          name  = "BUCKET"
          value = var.bucket
        }
        resources {
          limits = {
            cpu    = var.cpu
            memory = var.ram
          }
          requests = {
            cpu    = var.cpu
            memory = var.ram
          }
        }
      }
    }
  }

  depends_on = [
    google_project_service.run-api,
  ]
}

// Enable Cloud Run API.
resource "google_project_service" "run-api" {
  project = var.project_id
  service = "run.googleapis.com"
}

data "google_iam_policy" "noauth" {
  binding {
    role = "roles/run.invoker"
    members = [
      "allUsers",
    ]
  }
}

resource "google_cloud_run_service_iam_policy" "noauth" {
  location = google_cloud_run_service.service.location
  service  = google_cloud_run_service.service.name

  policy_data = data.google_iam_policy.noauth.policy_data
}

resource "google_cloud_run_domain_mapping" "mapping" {
  location = var.location
  name     = "${var.name}.${var.domain}"

  metadata {
    namespace = var.project_id
  }

  spec {
    route_name = google_cloud_run_service.service.name
  }
}

resource "google_dns_record_set" "dns-record" {
  name = "${var.name}.${var.domain}."
  type = google_cloud_run_domain_mapping.mapping.status[0].resource_records[0].type
  ttl  = 300

  managed_zone = var.dns_zone

  rrdatas = [google_cloud_run_domain_mapping.mapping.status[0].resource_records[0].rrdata]
}