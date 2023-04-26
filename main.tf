provider "google" {
  project = var.project_id
}

variable "project_id" {
  type        = string
  description = "The project ID to deploy to."
}

variable "location" {
  type        = string
  description = "The location of the Cloud Run service."
  default     = "us-east4"
}

variable "domain" {
  type        = string
  description = "The domain to map the Cloud Run service to."
}

resource "google_storage_bucket" "bucket" {
  name     = "${var.project_id}-kontainme"
  location = "US"

  uniform_bucket_level_access = true

  # Delete objects after 1 day.
  lifecycle_rule {
    condition {
      age = 1
    }
    action {
      type = "Delete"
    }
  }
}

# Make the bucket publicly readable.
resource "google_storage_bucket_iam_member" "public-read" {
  bucket = google_storage_bucket.bucket.name
  role   = "roles/storage.objectViewer"
  member = "allUsers"
}

resource "google_service_account" "service_account" {
  account_id = "kontaindotme"
}

resource "google_storage_bucket_iam_member" "bucket-member" {
  bucket = google_storage_bucket.bucket.name
  role   = "roles/storage.admin"
  member = "serviceAccount:${google_service_account.service_account.email}"
}

resource "google_dns_managed_zone" "zone" {
  count = var.dns_zone != "" ? 0 : 1 // If var.dns_zone is unset, create and manage this zone.

  name     = "kontainme-zone"
  dns_name = "${var.domain}."

  depends_on = [
    google_project_service.dns-api,
  ]
}

// Enable Cloud DNS API.
resource "google_project_service" "dns-api" {
  project = var.project_id
  service = "dns.googleapis.com"
}

variable "dns_zone" {
  type        = string
  description = "If set, use this pre-existing DNS zone"
  default     = ""
}

locals {
  // If var.dns_zone is set, use it. Otherwise, use the managed zone.
  dns_zone = var.dns_zone != "" ? var.dns_zone : google_dns_managed_zone.zone[0].name

  apps = {
    "apko" : {
      cpu                   = 1
      ram                   = "512Mi"
      container_concurrency = 1
      timeout_seconds       = 900 # 15m
    },
    "buildpack" : {
      cpu                   = 2
      ram                   = "4Gi"
      container_concurrency = 1
      timeout_seconds       = 900 # 15m
    },
    "flatten" : {
      cpu                   = 1
      ram                   = "1Gi"
      container_concurrency = 80
      timeout_seconds       = 120 # 2m
    },
    "kaniko" : {
      cpu                   = 2
      ram                   = "4Gi"
      container_concurrency = 1
      timeout_seconds       = 900 # 15m
    },
    "ko" : {
      cpu                   = 2
      ram                   = "4Gi"
      container_concurrency = 1
      timeout_seconds       = 900 # 15m
    },
    "mirror" : {
      cpu                   = 2
      ram                   = "4Gi"
      container_concurrency = 1
      timeout_seconds       = 900 # 15m
    },
    "random" : {
      cpu                   = 1
      ram                   = "256Mi"
      container_concurrency = 1000
      timeout_seconds       = 60 # 1m
    },
    "wait" : {
      cpu                   = 1
      ram                   = "1Gi"
      container_concurrency = 80
      timeout_seconds       = 60 # 1m
    },
  }
}

module "app" {
  for_each = local.apps
  source   = "./module"

  project_id           = var.project_id
  location             = var.location
  domain               = var.domain
  dns_zone             = local.dns_zone
  bucket               = google_storage_bucket.bucket.name
  service_account_name = google_service_account.service_account.email

  name                  = each.key
  cpu                   = each.value.cpu
  ram                   = each.value.ram
  container_concurrency = each.value.container_concurrency
  timeout_seconds       = each.value.timeout_seconds
}

output "cloudrun_url" {
  value = {
    for k, v in local.apps : k => module.app[k].cloudrun_url
  }
}

output "vanity_url" {
  value = {
    for k, v in local.apps : k => module.app[k].vanity_url
  }
}
