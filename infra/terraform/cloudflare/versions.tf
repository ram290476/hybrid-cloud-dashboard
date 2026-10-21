# HashiCorp Terraform. required_version is >= 1.6.0. CI pins 1.9.8.
terraform {
  required_version = ">= 1.6.0"
  required_providers {
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 4.45"
    }
  }
}

# Present so the gated cloudflare_record can validate. With
# enable_cloudflare_dns left false, apply creates no Cloudflare objects.
# The token stays a placeholder. Do not apply this root.
provider "cloudflare" {
  api_token = var.api_token
}
