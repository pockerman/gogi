# Azure Database for PostgreSQL, for gogi's services, and for Temporal, which makes its own two databases on it. It's
# in the network only, and takes only TLS connections, as Azure's does by default
resource "random_password" "database" {
  length  = 32
  special = false
}

resource "azurerm_private_dns_zone" "database" {
  name                = "${var.name}.private.postgres.database.azure.com"
  resource_group_name = azurerm_resource_group.gogi.name
}

resource "azurerm_private_dns_zone_virtual_network_link" "database" {
  name                  = "${var.name}-database"
  resource_group_name   = azurerm_resource_group.gogi.name
  private_dns_zone_name = azurerm_private_dns_zone.database.name
  virtual_network_id    = azurerm_virtual_network.gogi.id
}

resource "random_string" "suffix" {
  length  = 6
  upper   = false
  special = false
}

resource "azurerm_postgresql_flexible_server" "gogi" {
  # the server's name is in its address, which every Azure customer shares
  name                = "${var.name}-${random_string.suffix.result}"
  location            = azurerm_resource_group.gogi.location
  resource_group_name = azurerm_resource_group.gogi.name
  version             = "16"
  sku_name            = var.db_sku
  storage_mb          = var.db_storage_mb

  delegated_subnet_id           = azurerm_subnet.database.id
  private_dns_zone_id           = azurerm_private_dns_zone.database.id
  public_network_access_enabled = false

  administrator_login    = "gogi"
  administrator_password = random_password.database.result

  backup_retention_days = var.backup_days

  depends_on = [azurerm_private_dns_zone_virtual_network_link.database]

  lifecycle {
    # Azure picks the zone; it isn't moved afterwards
    ignore_changes = [zone]
  }
}

resource "azurerm_postgresql_flexible_server_database" "gogi" {
  name      = "gogi"
  server_id = azurerm_postgresql_flexible_server.gogi.id
  charset   = "UTF8"
  collation = "en_US.utf8"
}
