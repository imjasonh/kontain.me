terraform {
  required_version = ">= 1.7.0"

  # Same bucket as the deploy stack, different prefix.
  backend "gcs" {
    bucket = "kontaindotme-tfstate"
    prefix = "bootstrap"
  }

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = ">= 6.0.0, < 8.0.0"
    }
  }
}

provider "google" {
  project = local.project_id
}
