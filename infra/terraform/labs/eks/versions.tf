terraform {
  required_version = ">= 1.6.0"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.70"
    }
  }
}

provider "aws" {
  region                      = "us-west-2"
  access_key                  = "placeholder"
  secret_key                  = "placeholder"
  skip_credentials_validation = true
  skip_requesting_account_id  = true
  skip_metadata_api_check     = true
}

variable "role_arn" {
  type    = string
  default = "arn:aws:iam::000000000000:role/hcd-lab"
}

variable "subnet_ids" {
  type    = list(string)
  default = ["subnet-aaaaaaaa", "subnet-bbbbbbbb"]
}

resource "aws_eks_cluster" "lab" {
  name     = "hcd-lab"
  role_arn = var.role_arn
  version  = "1.31"

  vpc_config {
    subnet_ids = var.subnet_ids
  }
}
