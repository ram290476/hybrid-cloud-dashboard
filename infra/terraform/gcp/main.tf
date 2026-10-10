resource "google_cloud_run_v2_service" "go_api" {
  name     = "go-api"
  location = var.region
  ingress  = "INGRESS_TRAFFIC_ALL"

  template {
    scaling {
      min_instance_count = 0
      max_instance_count = 2
    }
    containers {
      image = var.go_image
      ports {
        container_port = 8080
      }
      env {
        name  = "APP_BACKEND"
        value = "gcp-go"
      }
      env {
        name  = "APP_LANG"
        value = "go"
      }
      env {
        name  = "APP_RUNTIME"
        value = "cloud-run"
      }
      env {
        name  = "PORT"
        value = "8080"
      }
      resources {
        limits = {
          cpu    = "1"
          memory = "512Mi"
        }
      }
    }
  }
}

resource "google_storage_bucket" "serving" {
  name                        = var.serving_bucket
  location                    = var.region
  uniform_bucket_level_access = true
  force_destroy               = true
}

resource "google_storage_bucket" "reference" {
  name                        = "${var.serving_bucket}-reference"
  location                    = var.region
  uniform_bucket_level_access = true
  force_destroy               = true
}

resource "google_firestore_database" "hcd" {
  project     = var.project_id
  name        = "hcd"
  location_id = var.region
  type        = "FIRESTORE_NATIVE"
}

resource "google_iam_workload_identity_pool" "github" {
  workload_identity_pool_id = "hcd-github"
  display_name              = "hcd-github"
}

resource "google_billing_budget" "hcd" {
  count           = var.billing_account == "" ? 0 : 1
  billing_account = var.billing_account
  display_name    = "hcd"

  amount {
    specified_amount {
      currency_code = "USD"
      units         = "50"
    }
  }
}
