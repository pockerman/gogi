# The server runs ../compose.yml. Its data is on a disk of its own, so the server can be replaced, e.g. for new
# images, without losing it. Nothing reaches it but the gateway's port: the operator runs commands on it with
# az vm run-command
resource "azurerm_managed_disk" "data" {
  name                 = "${var.name}-data"
  location             = azurerm_resource_group.gogi.location
  resource_group_name  = azurerm_resource_group.gogi.name
  storage_account_type = "StandardSSD_LRS"
  create_option        = "Empty"
  disk_size_gb         = var.data_disk_gb
}

# Azure asks for a key for SSH, though the server takes no SSH; nobody is given it
resource "tls_private_key" "unused_ssh" {
  algorithm = "ED25519"
}

locals {
  compose_dir = "${path.module}/.."

  start_script = templatefile("${path.module}/start-gogi.sh", {
    name          = var.name
    images        = var.images
    registry      = split("/", var.images)[0]
    image_tag     = var.image_tag
    tls_name      = azurerm_public_ip.server.ip_address
    vault_url     = azurerm_key_vault.gogi.vault_uri
    client_id     = azurerm_user_assigned_identity.server.client_id
    data_disk_lun = 0
  })

  custom_data = templatefile("${path.module}/cloud-init.yaml", {
    compose      = base64encode(file("${local.compose_dir}/compose.yml"))
    caddyfile    = base64encode(file("${local.compose_dir}/Caddyfile"))
    start_script = base64encode(local.start_script)
    # a new certificate replaces the server, which reads it when it starts
    certificate_serial = tls_locally_signed_cert.gateway.id
  })
}

resource "azurerm_linux_virtual_machine" "server" {
  name                  = var.name
  location              = azurerm_resource_group.gogi.location
  resource_group_name   = azurerm_resource_group.gogi.name
  size                  = var.vm_size
  network_interface_ids = [azurerm_network_interface.server.id]

  admin_username                  = "gogi"
  disable_password_authentication = true
  admin_ssh_key {
    username   = "gogi"
    public_key = tls_private_key.unused_ssh.public_key_openssh
  }

  identity {
    type         = "UserAssigned"
    identity_ids = [azurerm_user_assigned_identity.server.id]
  }

  # changing it replaces the server
  custom_data = base64gzip(local.custom_data)

  os_disk {
    # the images: the Python worker's alone is 7 GB
    disk_size_gb         = 64
    caching              = "ReadWrite"
    storage_account_type = "StandardSSD_LRS"
  }

  source_image_reference {
    publisher = "Canonical"
    offer     = "ubuntu-24_04-lts"
    sku       = "server"
    version   = "latest"
  }

  # its secrets are in the vault before it starts
  depends_on = [
    azurerm_key_vault_secret.database,
    azurerm_key_vault_secret.minio,
    azurerm_key_vault_secret.tls,
  ]
}

resource "azurerm_virtual_machine_data_disk_attachment" "data" {
  managed_disk_id    = azurerm_managed_disk.data.id
  virtual_machine_id = azurerm_linux_virtual_machine.server.id
  lun                = 0
  caching            = "ReadWrite"
}

# Postgres is kept from being deleted, e.g. by terraform destroy, until the lock is taken off
resource "azurerm_management_lock" "database" {
  count      = var.deletion_protection ? 1 : 0
  name       = "${var.name}-database"
  scope      = azurerm_postgresql_flexible_server.gogi.id
  lock_level = "CanNotDelete"
  notes      = "gogi's database: set deletion_protection = false to delete it"
}
