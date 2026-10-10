resource "azurerm_resource_group" "hcd" {
  name     = "hcd"
  location = var.location
}

resource "azurerm_service_plan" "hcd" {
  name                = "hcd-plan"
  resource_group_name = azurerm_resource_group.hcd.name
  location            = azurerm_resource_group.hcd.location
  os_type             = "Linux"
  sku_name            = "F1"
}

resource "azurerm_linux_web_app" "blue" {
  name                = var.blue_app_name
  resource_group_name = azurerm_resource_group.hcd.name
  location            = azurerm_resource_group.hcd.location
  service_plan_id     = azurerm_service_plan.hcd.id
  https_only          = true

  site_config {
    always_on = false
    application_stack {
      docker_image     = var.dotnet_image
      docker_image_tag = var.dotnet_image_tag
    }
    app_command_line = ""
  }

  app_settings = {
    APP_BACKEND   = "azure-dotnet"
    APP_LANG      = "dotnet"
    APP_RUNTIME   = "app-service"
    APP_COLOR     = "blue"
    WEBSITES_PORT = "8080"
  }
}

resource "azurerm_linux_web_app" "green" {
  name                = var.green_app_name
  resource_group_name = azurerm_resource_group.hcd.name
  location            = azurerm_resource_group.hcd.location
  service_plan_id     = azurerm_service_plan.hcd.id
  https_only          = true

  site_config {
    always_on = false
    application_stack {
      docker_image     = var.dotnet_image
      docker_image_tag = var.dotnet_image_tag
    }
  }

  app_settings = {
    APP_BACKEND   = "azure-dotnet"
    APP_LANG      = "dotnet"
    APP_RUNTIME   = "app-service"
    APP_COLOR     = "green"
    WEBSITES_PORT = "8080"
  }
}

resource "azurerm_storage_account" "hcd" {
  name                     = var.storage_account_name
  resource_group_name      = azurerm_resource_group.hcd.name
  location                 = azurerm_resource_group.hcd.location
  account_tier             = "Standard"
  account_replication_type = "LRS"
}

resource "azurerm_storage_container" "serving" {
  name                  = "hcd-serving"
  storage_account_name  = azurerm_storage_account.hcd.name
  container_access_type = "private"
}

resource "azurerm_cosmosdb_account" "hcd" {
  name                = var.cosmos_account_name
  location            = azurerm_resource_group.hcd.location
  resource_group_name = azurerm_resource_group.hcd.name
  offer_type          = "Standard"
  kind                = "GlobalDocumentDB"
  free_tier_enabled   = true

  consistency_policy {
    consistency_level = "Session"
  }

  geo_location {
    location          = azurerm_resource_group.hcd.location
    failover_priority = 0
  }
}

resource "azurerm_user_assigned_identity" "hcd" {
  name                = "hcd-app"
  resource_group_name = azurerm_resource_group.hcd.name
  location            = azurerm_resource_group.hcd.location
}
