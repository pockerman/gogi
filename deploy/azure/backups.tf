# Daily backups of the data disk, by Azure Backup, which snapshots it into the resource group
resource "azurerm_data_protection_backup_vault" "gogi" {
  name                = var.name
  location            = azurerm_resource_group.gogi.location
  resource_group_name = azurerm_resource_group.gogi.name
  datastore_type      = "OperationalStore"
  redundancy          = "LocallyRedundant"

  identity {
    type = "SystemAssigned"
  }
}

# what Azure Backup needs to back the disk up
resource "azurerm_role_assignment" "backup_reads_disk" {
  scope                = azurerm_managed_disk.data.id
  role_definition_name = "Disk Backup Reader"
  principal_id         = azurerm_data_protection_backup_vault.gogi.identity[0].principal_id
}

resource "azurerm_role_assignment" "backup_snapshots" {
  scope                = azurerm_resource_group.gogi.id
  role_definition_name = "Disk Snapshot Contributor"
  principal_id         = azurerm_data_protection_backup_vault.gogi.identity[0].principal_id
}

resource "azurerm_data_protection_backup_policy_disk" "daily" {
  name     = "daily"
  vault_id = azurerm_data_protection_backup_vault.gogi.id

  backup_repeating_time_intervals = ["R/2026-01-01T03:00:00+00:00/P1D"]
  default_retention_duration      = "P${var.backup_days}D"
}

resource "azurerm_data_protection_backup_instance_disk" "data" {
  name                         = "${var.name}-data"
  location                     = azurerm_resource_group.gogi.location
  vault_id                     = azurerm_data_protection_backup_vault.gogi.id
  disk_id                      = azurerm_managed_disk.data.id
  snapshot_resource_group_name = azurerm_resource_group.gogi.name
  backup_policy_id             = azurerm_data_protection_backup_policy_disk.daily.id

  depends_on = [
    azurerm_role_assignment.backup_reads_disk,
    azurerm_role_assignment.backup_snapshots,
  ]
}
