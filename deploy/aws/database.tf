# Postgres for gogi's services, and the ingestion Lambda. It takes only TLS connections, as RDS' Postgres 16 does by
# default
resource "random_password" "database" {
  length  = 32
  special = false
}

resource "aws_db_subnet_group" "gogi" {
  name       = var.name
  subnet_ids = aws_subnet.private[*].id
}

resource "aws_db_instance" "gogi" {
  identifier     = var.name
  engine         = "postgres"
  engine_version = "16"
  instance_class = var.db_instance_class

  allocated_storage     = var.db_allocated_storage_gb
  max_allocated_storage = var.db_allocated_storage_gb * 10
  storage_type          = "gp3"
  storage_encrypted     = true

  db_name  = "gogi"
  username = "gogi"
  password = random_password.database.result

  db_subnet_group_name   = aws_db_subnet_group.gogi.name
  vpc_security_group_ids = [aws_security_group.database.id]
  publicly_accessible    = false

  backup_retention_period    = var.db_backup_days
  auto_minor_version_upgrade = true
  deletion_protection        = var.deletion_protection
  skip_final_snapshot        = false
  final_snapshot_identifier  = "${var.name}-final"
}
