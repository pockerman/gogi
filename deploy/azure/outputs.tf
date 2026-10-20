output "gateway_address" {
  description = "Where applications call gogi, e.g. their GOGI_GATEWAY_URL"
  value       = "${azurerm_public_ip.server.ip_address}:443"
}

output "ca_certificate" {
  description = "The certificate applications check the gateway against, e.g. their GOGI_GATEWAY_CA_FILE"
  value       = tls_self_signed_cert.ca.cert_pem
}

output "server_public_ip" {
  description = "The server's address, to let it call an application's own services"
  value       = azurerm_public_ip.server.ip_address
}

output "resource_group" {
  description = "The deployment's resource group, e.g. for: az vm run-command invoke -g <resource_group> -n <vm_name>"
  value       = azurerm_resource_group.gogi.name
}

output "vm_name" {
  description = "The server"
  value       = azurerm_linux_virtual_machine.server.name
}

output "key_vault_name" {
  description = "The vault of the deployment's secrets and applications' provider keys"
  value       = azurerm_key_vault.gogi.name
}

output "ingestion" {
  description = "Where documents are ingested: the Function app to publish the ingestion code to, or the server"
  value = var.ingestion_function ? {
    function_app = azurerm_function_app_flex_consumption.ingestion[0].name
    queue        = "${azurerm_storage_account.files.primary_queue_endpoint}${local.ingestion_queue}"
  } : { worker = "on the server, polling Postgres" }
}
