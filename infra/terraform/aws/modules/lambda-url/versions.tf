# HashiCorp Terraform. required_version is >= 1.6.0. CI pins 1.9.8.
terraform {
  required_version = ">= 1.6.0"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.70"
    }
  }
}
