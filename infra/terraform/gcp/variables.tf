variable "project_id" {
  type    = string
  default = "hcd-local-placeholder"
}

variable "region" {
  type    = string
  default = "us-central1"
}

variable "billing_account" {
  type        = string
  default     = ""
  description = "Leave empty to skip the budget resource. Never commit a real billing account id."
}

variable "go_image" {
  type    = string
  default = "us-central1-docker.pkg.dev/hcd-local-placeholder/hcd/go-gcp:dev"
}

variable "serving_bucket" {
  type    = string
  default = "hcd-serving-placeholder"
}
