# HashiCorp Terraform. required_version is >= 1.6.0. CI pins 1.9.8.
terraform {
  required_version = ">= 1.6.0"
  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "~> 3.117"
    }
  }
}

provider "azurerm" {
  features {}
  subscription_id            = "00000000-0000-0000-0000-000000000000"
  skip_provider_registration = true
}

variable "location" {
  type    = string
  default = "eastus"
}

resource "azurerm_resource_group" "lab" {
  name     = "hcd-lab"
  location = var.location
}

resource "azurerm_kubernetes_cluster" "lab" {
  name                = "hcd-lab"
  location            = azurerm_resource_group.lab.location
  resource_group_name = azurerm_resource_group.lab.name
  dns_prefix          = "hcdlab"

  default_node_pool {
    name       = "default"
    node_count = 1
    vm_size    = "Standard_B2s"
  }

  identity {
    type = "SystemAssigned"
  }
}
