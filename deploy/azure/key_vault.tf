# One Key Vault: the deployment's secrets, which the server reads when it starts, and applications' provider keys,
# which gogi keeps under its prefix (gogi/credentials/, as gogi-2fcredentials-2f... in Key Vault's names). The server
# signs in to it as its own managed identity
data "azurerm_client_config" "current" {}

resource "azurerm_user_assigned_identity" "server" {
  name                = "${var.name}-server"
  location            = azurerm_resource_group.gogi.location
  resource_group_name = azurerm_resource_group.gogi.name
}

resource "azurerm_key_vault" "gogi" {
  # a vault's name is in its address, which every Azure customer shares
  name                       = "${var.name}-${random_string.suffix.result}"
  location                   = azurerm_resource_group.gogi.location
  resource_group_name        = azurerm_resource_group.gogi.name
  tenant_id                  = data.azurerm_client_config.current.tenant_id
  sku_name                   = "standard"
  soft_delete_retention_days = 7
  purge_protection_enabled   = false

  # the operator, who runs Terraform and puts the registry's token
  access_policy {
    tenant_id          = data.azurerm_client_config.current.tenant_id
    object_id          = data.azurerm_client_config.current.object_id
    secret_permissions = ["Get", "List", "Set", "Delete", "Purge", "Recover"]
  }

  # the server: it reads the deployment's secrets, and keeps applications' provider keys
  access_policy {
    tenant_id          = data.azurerm_client_config.current.tenant_id
    object_id          = azurerm_user_assigned_identity.server.principal_id
    secret_permissions = ["Get", "Set"]
  }
}

resource "random_password" "minio" {
  length  = 32
  special = false
}

resource "azurerm_key_vault_secret" "database" {
  name         = "deployment-database"
  key_vault_id = azurerm_key_vault.gogi.id
  content_type = "application/json"
  value = jsonencode({
    host     = azurerm_postgresql_flexible_server.gogi.fqdn
    port     = 5432
    dbname   = azurerm_postgresql_flexible_server_database.gogi.name
    username = azurerm_postgresql_flexible_server.gogi.administrator_login
    password = random_password.database.result
  })
}

resource "azurerm_key_vault_secret" "minio" {
  name         = "deployment-minio"
  key_vault_id = azurerm_key_vault.gogi.id
  value        = random_password.minio.result
}

resource "azurerm_key_vault_secret" "tls" {
  name         = "deployment-tls"
  key_vault_id = azurerm_key_vault.gogi.id
  content_type = "application/json"
  value = jsonencode({
    certificate = "${tls_locally_signed_cert.gateway.cert_pem}${tls_self_signed_cert.ca.cert_pem}"
    key         = tls_private_key.gateway.private_key_pem
  })
}

# Where the images are private, the operator puts a token that can read them, so it's never in the state:
#   az keyvault secret set --vault-name <key_vault_name> --name deployment-registry \
#     --value '{"username":"<GitHub user>","token":"<token with read:packages>"}'
# Without it, the server pulls the images without signing in.
