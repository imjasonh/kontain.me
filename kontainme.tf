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

variable "project" {
  type    = string
  default = "kontaindotme"
}

variable "bucket" {
  type    = string
  default = "kontaindotme"
}

variable "region" {
  type    = string
  default = "us-central1"
}

variable "images" {
  type        = map(string)
  description = "images to deploy"
}

variable "services" {
  type = map(object({
    memory      = string
    cpu         = string
    concurrency = string
    timeout     = string
  }))
  description = "services to deploy"
}

////// Cloud Storage

resource "google_storage_bucket" "bucket" {
  name     = var.bucket
  location = "US"

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

////// App Engine + Cloud Tasks

// Enable Cloud Tasks API.
resource "google_project_service" "cloudtasks" {
  service = "cloudtasks.googleapis.com"
}

resource "google_app_engine_application" "app" {
  project     = var.project
  location_id = "us-central" // TODO: gross, GAE locations != GCP regions :'(
}

resource "google_cloud_tasks_queue" "wait-queue" {
  name = "wait-queue"
  location = var.region

  depends_on = [google_project_service.cloudtasks, google_app_engine_application.app]
}

////// Cloud Run

// Enable Cloud Run API.
resource "google_project_service" "run" {
  service = "run.googleapis.com"
}

// TODO: don't need this; can't delete it?
resource "google_project_service" "datastore" {
  service = "datastore.googleapis.com"
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
        image = var.images[each.key]
        env {
          name  = "BUCKET"
          value = var.bucket
        }
        resources {
          limits = {
            memory = each.value.memory
            cpu    = each.value.cpu
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

// TODO: Ensure domain mappings.

output "services" {
  value = {
    for svc in google_cloud_run_service.services :
    svc.name => svc.status[0].url
  }
}
