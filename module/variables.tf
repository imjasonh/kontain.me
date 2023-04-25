variable "project_id" {
  type        = string
  description = "The project ID to deploy to."
}

variable "name" {
  type        = string
  description = "The name of the Cloud Run service."
}

variable "domain" {
  type        = string
  description = "The domain to map the Cloud Run service to."
}

variable "dns_zone" {
  type        = string
  description = "The DNS zone to map the Cloud Run service to."
}

variable "location" {
  type        = string
  description = "The location of the Cloud Run service."
}

variable "service_account_name" {
  type        = string
  description = "The service account to run the Cloud Run service as."
}

variable "bucket" {
  type        = string
  description = "The GCS bucket to write to."
}

variable "cpu" {
  type        = string
  description = "The CPU to request for the Cloud Run service."
  default     = "1"
}

variable "ram" {
  type        = string
  description = "The RAM to request for the Cloud Run service."
  default     = "2Gi"
}

variable "container_concurrency" {
  type        = number
  description = "The container concurrency to request for the Cloud Run service."
  default     = 1000
}

variable "timeout_seconds" {
  type        = number
  description = "The timeout to request for the Cloud Run service."
  default     = 60 # 1m
}
