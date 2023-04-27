terraform {
  backend "gcs" {
    bucket  = "kontaindotme-tfstate"
    prefix  = "terraform/state"
  }
}

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

// Redirect kontain.me to github.com/imjasonh/kontain.me
resource "google_dns_record_set" "root-a-record" {
  name = "${var.domain}."
  type = "A"
  ttl  = 300

  managed_zone = local.dns_zone

  rrdatas = [
    "216.239.32.21",
    "216.239.34.21",
    "216.239.36.21",
    "216.239.38.21",
  ]
}

// Redirect kontain.me to github.com/imjasonh/kontain.me
resource "google_dns_record_set" "root-aaaa-record" {
  name = "${var.domain}."
  type = "AAAA"
  ttl  = 300

  managed_zone = local.dns_zone

  rrdatas = [
    "2001:4860:4802:32::15",
    "2001:4860:4802:34::15",
    "2001:4860:4802:36::15",
    "2001:4860:4802:38::15",
  ]
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
      base_image           = "cgr.dev/chainguard/static:latest-glibc"
    },
    "buildpack" : {
      cpu                   = 2
      ram                   = "4Gi"
      container_concurrency = 1
      timeout_seconds       = 900 # 15m
      base_image            = "gcr.io/buildpacks/builder"
    },
    "flatten" : {
      cpu                   = 1
      ram                   = "1Gi"
      container_concurrency = 80
      timeout_seconds       = 120 # 2m
      base_image           = "cgr.dev/chainguard/static:latest-glibc"
    },
    "kaniko" : {
      cpu                   = 2
      ram                   = "4Gi"
      container_concurrency = 1
      timeout_seconds       = 900 # 15m
      base_image            = "gcr.io/kaniko-project/executor:debug"
    },
    "ko" : {
      cpu                   = 2
      ram                   = "4Gi"
      container_concurrency = 1
      timeout_seconds       = 900 # 15m
      base_image            = "golang"
      # After https://github.com/chainguard-images/images/pull/511
      #base_image            = "cgr.dev/chainguard/go:latest-dev"
    },
    "mirror" : {
      cpu                   = 2
      ram                   = "4Gi"
      container_concurrency = 1
      timeout_seconds       = 900 # 15m
      base_image           = "cgr.dev/chainguard/static:latest-glibc"
    },
    "random" : {
      cpu                   = 1
      ram                   = "256Mi"
      container_concurrency = 1000
      timeout_seconds       = 60 # 1m
      base_image           = "cgr.dev/chainguard/static:latest-glibc"
    },
    "wait" : {
      cpu                   = 1
      ram                   = "1Gi"
      container_concurrency = 80
      timeout_seconds       = 60 # 1m
      base_image           = "cgr.dev/chainguard/static:latest-glibc"
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
  base_image            = each.value.base_image
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
