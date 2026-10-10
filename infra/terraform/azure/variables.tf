variable "subscription_id" {
  type    = string
  default = "00000000-0000-0000-0000-000000000000"
}

variable "location" {
  type    = string
  default = "eastus"
}

variable "blue_app_name" {
  type    = string
  default = "hcd-dotnet-api-blue"
}

variable "green_app_name" {
  type    = string
  default = "hcd-dotnet-api-green"
}

variable "storage_account_name" {
  type    = string
  default = "hcdstorage000"
}

variable "cosmos_account_name" {
  type    = string
  default = "hcd-cosmos-000"
}

variable "dotnet_image" {
  type    = string
  default = "ghcr.io/ram290476/hybrid-cloud-dashboard/dotnet-azure"
}

variable "dotnet_image_tag" {
  type    = string
  default = "dev"
}
