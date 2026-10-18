# Where gogi keeps files: documents' uploads and prompts' texts, each in a container of one storage account. The
# account takes no shared keys: the server signs in as its managed identity. Deleted files are kept for a while, to
# recover from mistakes
resource "azurerm_storage_account" "files" {
  # a storage account's name is in its address, which every Azure customer shares: letters and digits only
  name                     = "${replace(var.name, "-", "")}${random_string.suffix.result}"
  location                 = azurerm_resource_group.gogi.location
  resource_group_name      = azurerm_resource_group.gogi.name
  account_tier             = "Standard"
  account_replication_type = "LRS"
  account_kind             = "StorageV2"

  min_tls_version                 = "TLS1_2"
  https_traffic_only_enabled      = true
  allow_nested_items_to_be_public = false
  shared_access_key_enabled       = false
  default_to_oauth_authentication = true

  blob_properties {
    delete_retention_policy {
      days = var.file_versions_days
    }
    container_delete_retention_policy {
      days = var.file_versions_days
    }
  }
}

resource "azurerm_storage_container" "files" {
  for_each              = toset(["documents", "prompts"])
  name                  = each.value
  storage_account_id    = azurerm_storage_account.files.id
  container_access_type = "private"
}

resource "azurerm_role_assignment" "server_files" {
  scope                = azurerm_storage_account.files.id
  role_definition_name = "Storage Blob Data Contributor"
  principal_id         = azurerm_user_assigned_identity.server.principal_id
}
