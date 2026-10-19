# gogi has no domain, so no public certificate authority will certify its gateway. The deployment is its own
# authority instead: applications are given its certificate (the ca_certificate output) to check the gateway against
resource "tls_private_key" "ca" {
  algorithm   = "ECDSA"
  ecdsa_curve = "P256"
}

resource "tls_self_signed_cert" "ca" {
  private_key_pem       = tls_private_key.ca.private_key_pem
  is_ca_certificate     = true
  validity_period_hours = 10 * 365 * 24

  subject {
    common_name  = "${var.name} certificate authority"
    organization = var.name
  }

  allowed_uses = ["cert_signing", "crl_signing"]
}

resource "tls_private_key" "gateway" {
  algorithm   = "ECDSA"
  ecdsa_curve = "P256"
}

resource "tls_cert_request" "gateway" {
  private_key_pem = tls_private_key.gateway.private_key_pem
  ip_addresses    = [azurerm_public_ip.server.ip_address]
  dns_names       = var.domain == "" ? [] : [var.domain]

  subject {
    common_name  = var.domain == "" ? azurerm_public_ip.server.ip_address : var.domain
    organization = var.name
  }
}

resource "tls_locally_signed_cert" "gateway" {
  cert_request_pem      = tls_cert_request.gateway.cert_request_pem
  ca_private_key_pem    = tls_private_key.ca.private_key_pem
  ca_cert_pem           = tls_self_signed_cert.ca.cert_pem
  validity_period_hours = var.certificate_validity_days * 24
  early_renewal_hours   = 30 * 24

  allowed_uses = ["digital_signature", "key_encipherment", "server_auth"]
}
