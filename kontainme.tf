////// Providers

terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "3.48.0"
    }
  }
}

provider "google" {
  project = var.project
}

////// Variables

variable "image-prefix" {
  type    = string
  default = "gcr.io/kontaindotme/github.com/imjasonh/kontain.me/cmd/"
}

variable "project" {
  type    = string
  default = "kontaindotme"
}

variable "region" {
  type    = string
  default = "us-central1"
}

variable "bucket" {
  type    = string
  default = "kontaindotme"
}

variable "services" {
  type = map(object({
    memory      = string
    cpu         = string
    concurrency = string
    timeout     = string
  }))
  description = "services to deploy"

  default = {
    "api" = {
      memory      = "2Gi"
      cpu         = "1"
      concurrency = "1"
      timeout     = "300" # 5m
    }
    "buildpack" = {
      memory      = "4Gi"
      cpu         = "2"
      concurrency = "1"
      timeout     = "900" # 15m
    }
    "flatten" = {
      memory      = "1Gi"
      cpu         = "1"
      concurrency = "80"
      timeout     = "60" # 1m
    }
    "ko" = {
      memory      = "4Gi"
      cpu         = "2"
      concurrency = "1"
      timeout     = "900" # 15m
    }
    "mirror" = {
      memory      = "1Gi"
      cpu         = "1"
      concurrency = "80"
      timeout     = "60" # 1m
    }
    "random" = {
      memory      = "1Gi"
      cpu         = "1"
      concurrency = "80"
      timeout     = "60" # 1m
    }
    "viz" = {
      memory      = "1Gi"
      cpu         = "1"
      concurrency = "80"
      timeout     = "60" # 1m
    }
    "flatten" = {
      memory      = "1Gi"
      cpu         = "1"
      concurrency = "80"
      timeout     = "60" # 1m
    }
  }
}

////// Cloud Storage

resource "google_storage_bucket" "bucket" {
  name          = var.bucket
  location      = "US"

  uniform_bucket_level_access = false

  // Delete objects older than 1 day.
  lifecycle_rule {
    condition {
      age = 1
    }
    action {
      type = "Delete"
    }
  }
}

// TODO: Configure public access + compute SA writer

////// Cloud Run

// Enable Cloud Run API.
resource "google_project_service" "run" {
  service = "run.googleapis.com"
}

// Deploy image to each region.
resource "google_cloud_run_service" "services" {
  for_each = var.services
  name     = each.key
  location = var.region

  template {
    spec {
      timeout_seconds = each.value.timeout
      containers {
        image = "${var.image-prefix}${each.key}"
        env {
          name  = "BUCKET"
          value = var.bucket
        }
        resources {
          limits = {
            memory = each.value.memory
            cpu = each.value.cpu
          }
        }
      }
    }
  }

  traffic {
    percent         = 100
    latest_revision = true
  }

  depends_on = [google_project_service.run]
}

// Make each service invokable by all users.
resource "google_cloud_run_service_iam_member" "allUsers" {
  for_each = var.services

  service  = google_cloud_run_service.services[each.key].name
  location = var.region
  role     = "roles/run.invoker"
  member   = "allUsers"

  depends_on = [google_cloud_run_service.services]
}

output "services" {
  value = {
    for svc in google_cloud_run_service.services :
    svc.name => svc.status[0].url
  }
}
