# Document ingestion. The documents service queues each ingestion; the worker reads the document from Blob Storage,
# parses it, has the model service on the server embed its chunks and stores them in Chroma on the server, and
# reports on the job in Postgres.
#
# Until ingestion_function is true, the worker runs on the server, polling a queue table in Postgres. With it, the
# documents service queues ingestions in a Storage queue instead, and a Function in the network (Flex Consumption,
# so it costs nothing while idle) ingests them: the server only answers it. Its code is published with
# workers/azure_function/build.sh and func azure functionapp publish.

locals {
  ingestion_queue = "ingestion"
}

resource "azurerm_storage_queue" "ingestion" {
  count              = var.ingestion_function ? 1 : 0
  name               = local.ingestion_queue
  storage_account_id = azurerm_storage_account.files.id
}

# the Function's code, which Flex Consumption deploys from a container
resource "azurerm_storage_container" "ingestion_deployments" {
  count                 = var.ingestion_function ? 1 : 0
  name                  = "ingestion-deployments"
  storage_account_id    = azurerm_storage_account.files.id
  container_access_type = "private"
}

# the Function runs in a subnet of its own, from which the server's Chroma and model service, and Postgres, are
# reached; Storage through its service endpoint
resource "azurerm_subnet" "ingestion" {
  count                = var.ingestion_function ? 1 : 0
  name                 = "ingestion"
  resource_group_name  = azurerm_resource_group.gogi.name
  virtual_network_name = azurerm_virtual_network.gogi.name
  address_prefixes     = ["10.42.2.0/24"]
  service_endpoints    = ["Microsoft.Storage"]

  delegation {
    name = "functions"
    service_delegation {
      name    = "Microsoft.App/environments"
      actions = ["Microsoft.Network/virtualNetworks/subnets/join/action"]
    }
  }
}

# Chroma and the model service are published on the server's private address for the Function alone
resource "azurerm_network_security_rule" "server_from_ingestion" {
  count                       = var.ingestion_function ? 1 : 0
  name                        = "ingestion"
  priority                    = 110
  direction                   = "Inbound"
  access                      = "Allow"
  protocol                    = "Tcp"
  source_port_range           = "*"
  destination_port_ranges     = ["8000", "50057"]
  source_address_prefixes     = azurerm_subnet.ingestion[0].address_prefixes
  destination_address_prefix  = local.server_private_ip
  resource_group_name         = azurerm_resource_group.gogi.name
  network_security_group_name = azurerm_network_security_group.server.name
}

resource "azurerm_user_assigned_identity" "ingestion" {
  count               = var.ingestion_function ? 1 : 0
  name                = "${var.name}-ingestion"
  location            = azurerm_resource_group.gogi.location
  resource_group_name = azurerm_resource_group.gogi.name
}

# the Function's own state and code, the documents' uploads to read, and the queue's messages; the storage account
# takes no shared keys
resource "azurerm_role_assignment" "ingestion_storage" {
  for_each             = var.ingestion_function ? toset(["Storage Blob Data Owner", "Storage Queue Data Contributor"]) : toset([])
  scope                = azurerm_storage_account.files.id
  role_definition_name = each.value
  principal_id         = azurerm_user_assigned_identity.ingestion[0].principal_id
}

# the documents service queues ingestions
resource "azurerm_role_assignment" "server_ingestion_queue" {
  count                = var.ingestion_function ? 1 : 0
  scope                = azurerm_storage_queue.ingestion[0].resource_manager_id
  role_definition_name = "Storage Queue Data Message Sender"
  principal_id         = azurerm_user_assigned_identity.server.principal_id
}

resource "azurerm_service_plan" "ingestion" {
  count               = var.ingestion_function ? 1 : 0
  name                = "${var.name}-ingestion"
  location            = azurerm_resource_group.gogi.location
  resource_group_name = azurerm_resource_group.gogi.name
  os_type             = "Linux"
  sku_name            = "FC1"
}

resource "azurerm_function_app_flex_consumption" "ingestion" {
  count               = var.ingestion_function ? 1 : 0
  name                = "${var.name}-ingestion-${random_string.suffix.result}"
  location            = azurerm_resource_group.gogi.location
  resource_group_name = azurerm_resource_group.gogi.name
  service_plan_id     = azurerm_service_plan.ingestion[0].id

  runtime_name    = "python"
  runtime_version = "3.12"

  storage_container_type            = "blobContainer"
  storage_container_endpoint        = "${azurerm_storage_account.files.primary_blob_endpoint}${azurerm_storage_container.ingestion_deployments[0].name}"
  storage_authentication_type       = "UserAssignedIdentity"
  storage_user_assigned_identity_id = azurerm_user_assigned_identity.ingestion[0].id

  # the server embeds the chunks: a few ingestions at a time keep it answering applications
  maximum_instance_count = var.ingestion_concurrency
  instance_memory_in_mb  = 2048

  virtual_network_subnet_id = azurerm_subnet.ingestion[0].id

  identity {
    type         = "UserAssigned"
    identity_ids = [azurerm_user_assigned_identity.ingestion[0].id]
  }

  site_config {}

  app_settings = {
    # the host's own state, and the queue that triggers the Function, as its identity
    AzureWebJobsStorage__accountName      = azurerm_storage_account.files.name
    AzureWebJobsStorage__credential       = "managedidentity"
    AzureWebJobsStorage__clientId         = azurerm_user_assigned_identity.ingestion[0].client_id
    GOGI_INGESTION_QUEUE_NAME             = local.ingestion_queue
    GOGI_INGESTION_QUEUE__queueServiceUri = azurerm_storage_account.files.primary_queue_endpoint
    GOGI_INGESTION_QUEUE__credential      = "managedidentity"
    GOGI_INGESTION_QUEUE__clientId        = azurerm_user_assigned_identity.ingestion[0].client_id
    # host.json's maxDequeueCount is no lower
    GOGI_INGESTION_MAX_ATTEMPTS = "3"

    GOGI_OBJECT_STORE         = "azure"
    AZURE_STORAGE_ACCOUNT_URL = azurerm_storage_account.files.primary_blob_endpoint
    AZURE_CLIENT_ID           = azurerm_user_assigned_identity.ingestion[0].client_id
    VECTOR_STORAGE_TYPE       = "chromadb"
    VECTOR_STORAGE_HOST       = local.server_private_ip
    VECTOR_STORAGE_PORT       = "8000"
    GOGI_LLM_SERVICE_ADDR     = "${local.server_private_ip}:50057"
    DB_HOST                   = azurerm_postgresql_flexible_server.gogi.fqdn
    DB_PORT                   = "5432"
    DB_NAME                   = azurerm_postgresql_flexible_server_database.gogi.name
    DB_USER                   = azurerm_postgresql_flexible_server.gogi.administrator_login
    DB_PASSWORD               = random_password.database.result
  }

  depends_on = [azurerm_role_assignment.ingestion_storage]
}
