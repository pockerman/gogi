# What the server reads when it starts. They're made from the state, so they're deleted at once when destroyed
resource "aws_secretsmanager_secret" "database" {
  name                    = "${var.name}/deployment/database"
  description             = "gogi's Postgres: host, port, database, username and password"
  recovery_window_in_days = 0
}

resource "aws_secretsmanager_secret_version" "database" {
  secret_id = aws_secretsmanager_secret.database.id
  secret_string = jsonencode({
    host     = aws_db_instance.gogi.address
    port     = aws_db_instance.gogi.port
    dbname   = aws_db_instance.gogi.db_name
    username = aws_db_instance.gogi.username
    password = random_password.database.result
  })
}

resource "aws_secretsmanager_secret" "tls" {
  name                    = "${var.name}/deployment/tls"
  description             = "The gateway's certificate and key"
  recovery_window_in_days = 0
}

resource "aws_secretsmanager_secret_version" "tls" {
  secret_id = aws_secretsmanager_secret.tls.id
  secret_string = jsonencode({
    certificate = "${tls_locally_signed_cert.gateway.cert_pem}${tls_self_signed_cert.ca.cert_pem}"
    key         = tls_private_key.gateway.private_key_pem
  })
}

# Where the images are private, the operator puts a token that can read them here, so it's never in the state:
#   aws secretsmanager put-secret-value --secret-id gogi/deployment/registry \
#     --secret-string '{"username":"<GitHub user>","token":"<token with read:packages>"}'
# Without a value, the server pulls the images without signing in.
resource "aws_secretsmanager_secret" "registry" {
  name                    = "${var.name}/deployment/registry"
  description             = "A token for the registry of gogi's images, if they're private"
  recovery_window_in_days = 0
}
