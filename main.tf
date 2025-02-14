terraform {
  backend "gcs" {
    bucket = "kontaindotme-tfstate"
    prefix = "terraform/state"
  }

  required_providers {
    google = { source = "hashicorp/google" }
    ko     = { source = "ko-build/ko" }
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

variable "notification_channels" {
  type        = set(string)
  description = "The notification channels to send alerts to."
  default     = []
}

module "dashboard" {
  for_each = tomap(local.apps)
  source   = "git::https://github.com/chainguard-dev/terraform-infra-common//modules/dashboard/service?ref=main"

  project_id   = var.project_id
  service_name = each.key

  //alerts = lookup(each.value, "alert_id", "") == "" ? [] : [each.value.alert_id]

  notification_channels = var.notification_channels
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

  # Allow
  cors {
    origin          = ["*"]
    method          = ["GET", "HEAD"]
    response_header = ["*"]
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
  name     = "kontainme-zone"
  dns_name = "${var.domain}."

  depends_on = [google_project_service.dns-api]
}

// Enable Cloud DNS API.
resource "google_project_service" "dns-api" {
  project            = var.project_id
  service            = "dns.googleapis.com"
  disable_on_destroy = false
}

// Redirect kontain.me to github.com/imjasonh/kontain.me
resource "google_dns_record_set" "root-a-record" {
  name = "${var.domain}."
  type = "A"
  ttl  = 300

  managed_zone = google_dns_managed_zone.zone.name

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

  managed_zone = google_dns_managed_zone.zone.name

  rrdatas = [
    "2001:4860:4802:32::15",
    "2001:4860:4802:34::15",
    "2001:4860:4802:36::15",
    "2001:4860:4802:38::15",
  ]
}

// Enable Cloud Run API.
resource "google_project_service" "run-api" {
  project            = var.project_id
  service            = "run.googleapis.com"
  disable_on_destroy = false
}
