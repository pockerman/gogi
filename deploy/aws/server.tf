# The server runs ../compose.yml. Its data is on a disk of its own, so the server can be replaced, e.g. for new
# images, without losing it. The operator reaches it with SSM Session Manager: it has no SSH
data "aws_ssm_parameter" "al2023" {
  name = "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-x86_64"
}

resource "aws_eip" "server" {
  domain = "vpc"

  tags = { Name = var.name }
}

resource "aws_ebs_volume" "data" {
  availability_zone = aws_subnet.public.availability_zone
  size              = var.data_volume_gb
  type              = "gp3"
  encrypted         = true

  tags = {
    Name     = "${var.name}-data"
    Snapshot = "${var.name}-data"
  }
}

locals {
  compose_dir = "${path.module}/.."

  start_script = templatefile("${path.module}/start-gogi.sh", {
    region          = var.region
    name            = var.name
    images          = var.images
    registry        = split("/", var.images)[0]
    image_tag       = var.image_tag
    tls_name        = aws_eip.server.public_ip
    data_volume_id  = aws_ebs_volume.data.id
    database_secret = aws_secretsmanager_secret.database.name
    documents       = aws_s3_bucket.files["documents"].id
    prompts         = aws_s3_bucket.files["prompts"].id
    tls_secret      = aws_secretsmanager_secret.tls.name
    registry_secret = aws_secretsmanager_secret.registry.name
  })

  user_data = templatefile("${path.module}/cloud-init.yaml", {
    compose      = base64encode(file("${local.compose_dir}/compose.yml"))
    caddyfile    = base64encode(file("${local.compose_dir}/Caddyfile"))
    start_script = base64encode(local.start_script)
    # a new certificate replaces the server, which reads it when it starts
    certificate_serial = tls_locally_signed_cert.gateway.id
  })
}

resource "aws_instance" "server" {
  ami                    = data.aws_ssm_parameter.al2023.insecure_value
  instance_type          = var.instance_type
  subnet_id              = aws_subnet.public.id
  vpc_security_group_ids = [aws_security_group.server.id]
  iam_instance_profile   = aws_iam_instance_profile.server.name

  user_data_base64            = base64gzip(local.user_data)
  user_data_replace_on_change = true

  root_block_device {
    # the images: the Python worker's alone is 7 GB
    volume_size = 40
    volume_type = "gp3"
    encrypted   = true
  }

  metadata_options {
    http_tokens = "required"
    # the containers' own hop, so the services can use the server's role, e.g. for the credential store
    http_put_response_hop_limit = 2
  }

  tags = { Name = var.name }

  lifecycle {
    # a newer Amazon Linux is taken when the server is next replaced, not as soon as it's published
    ignore_changes = [ami]
  }
}

resource "aws_volume_attachment" "data" {
  device_name                    = "/dev/sdf"
  volume_id                      = aws_ebs_volume.data.id
  instance_id                    = aws_instance.server.id
  stop_instance_before_detaching = true
}

resource "aws_eip_association" "server" {
  allocation_id = aws_eip.server.id
  instance_id   = aws_instance.server.id
}

# daily snapshots of the data disk
resource "aws_dlm_lifecycle_policy" "data" {
  description        = "${var.name} data disk daily"
  execution_role_arn = aws_iam_role.snapshots.arn
  state              = "ENABLED"

  policy_details {
    resource_types = ["VOLUME"]
    target_tags    = { Snapshot = "${var.name}-data" }

    schedule {
      name = "daily"

      create_rule {
        interval      = 24
        interval_unit = "HOURS"
        times         = ["03:00"]
      }

      retain_rule {
        count = var.snapshot_days
      }

      copy_tags = true
    }
  }
}
