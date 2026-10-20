resource "azurerm_resource_group" "gogi" {
  name     = var.name
  location = var.location
}

# One virtual network: the server with a public IP address, and Postgres in a subnet of its own, reachable only from
# inside the network
locals {
  server_private_ip = cidrhost(azurerm_subnet.server.address_prefixes[0], 10)
}

resource "azurerm_virtual_network" "gogi" {
  name                = var.name
  location            = azurerm_resource_group.gogi.location
  resource_group_name = azurerm_resource_group.gogi.name
  address_space       = ["10.42.0.0/16"]
}

resource "azurerm_subnet" "server" {
  name                 = "server"
  resource_group_name  = azurerm_resource_group.gogi.name
  virtual_network_name = azurerm_virtual_network.gogi.name
  address_prefixes     = ["10.42.0.0/24"]
}

resource "azurerm_subnet" "database" {
  name                 = "database"
  resource_group_name  = azurerm_resource_group.gogi.name
  virtual_network_name = azurerm_virtual_network.gogi.name
  address_prefixes     = ["10.42.1.0/24"]

  delegation {
    name = "postgres"
    service_delegation {
      name    = "Microsoft.DBforPostgreSQL/flexibleServers"
      actions = ["Microsoft.Network/virtualNetworks/subnets/join/action"]
    }
  }
}

resource "azurerm_public_ip" "server" {
  name                = var.name
  location            = azurerm_resource_group.gogi.location
  resource_group_name = azurerm_resource_group.gogi.name
  allocation_method   = "Static"
  sku                 = "Standard"
}

resource "azurerm_network_security_group" "server" {
  name                = "${var.name}-server"
  location            = azurerm_resource_group.gogi.location
  resource_group_name = azurerm_resource_group.gogi.name

  # the gateway, from applications; nothing else comes in, SSH neither
  security_rule {
    name                       = "gateway"
    priority                   = 100
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "443"
    source_address_prefixes    = var.client_cidrs
    destination_address_prefix = "*"
  }
}

resource "azurerm_network_interface" "server" {
  name                = "${var.name}-server"
  location            = azurerm_resource_group.gogi.location
  resource_group_name = azurerm_resource_group.gogi.name

  ip_configuration {
    name      = "server"
    subnet_id = azurerm_subnet.server.id
    # fixed, so the ingestion Function's settings don't change when the server is replaced
    private_ip_address_allocation = "Static"
    private_ip_address            = local.server_private_ip
    public_ip_address_id          = azurerm_public_ip.server.id
  }
}

resource "azurerm_network_interface_security_group_association" "server" {
  network_interface_id      = azurerm_network_interface.server.id
  network_security_group_id = azurerm_network_security_group.server.id
}
