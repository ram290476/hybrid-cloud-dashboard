# HashiCorp Terraform. required_version is >= 1.6.0. CI pins 1.9.8.
terraform {
  required_version = ">= 1.6.0"
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 5.45"
    }
  }
}

provider "google" {
  project = "hcd-local-placeholder"
  region  = "us-central1"
}

resource "google_container_cluster" "lab" {
  name                = "hcd-lab"
  location            = "us-central1"
  deletion_protection = false
  initial_node_count  = 1
}
