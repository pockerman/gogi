# One VPC: the server in a public subnet, with an Elastic IP and no NAT gateway, and Postgres in two private subnets,
# as RDS asks for two availability zones
data "aws_availability_zones" "available" {
  state = "available"
}

locals {
  zones = slice(data.aws_availability_zones.available.names, 0, 2)
}

resource "aws_vpc" "gogi" {
  cidr_block           = "10.40.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = var.name }
}

resource "aws_internet_gateway" "gogi" {
  vpc_id = aws_vpc.gogi.id

  tags = { Name = var.name }
}

resource "aws_subnet" "public" {
  vpc_id            = aws_vpc.gogi.id
  cidr_block        = "10.40.0.0/24"
  availability_zone = local.zones[0]

  tags = { Name = "${var.name}-public" }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.gogi.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.gogi.id
  }

  tags = { Name = "${var.name}-public" }
}

resource "aws_route_table_association" "public" {
  subnet_id      = aws_subnet.public.id
  route_table_id = aws_route_table.public.id
}

resource "aws_subnet" "private" {
  count             = 2
  vpc_id            = aws_vpc.gogi.id
  cidr_block        = "10.40.${10 + count.index}.0/24"
  availability_zone = local.zones[count.index]

  tags = { Name = "${var.name}-private-${count.index}" }
}

resource "aws_security_group" "server" {
  name        = "${var.name}-server"
  description = "gogi's server: the gateway's gRPC over TLS, from the applications"
  vpc_id      = aws_vpc.gogi.id

  tags = { Name = "${var.name}-server" }
}

resource "aws_vpc_security_group_ingress_rule" "gateway" {
  for_each          = toset(var.client_cidrs)
  security_group_id = aws_security_group.server.id
  description       = "The gateway, from applications"
  ip_protocol       = "tcp"
  from_port         = 443
  to_port           = 443
  cidr_ipv4         = each.value
}

# images, the models' and providers' APIs, AWS' own APIs
resource "aws_vpc_security_group_egress_rule" "server" {
  security_group_id = aws_security_group.server.id
  description       = "Anywhere"
  ip_protocol       = "-1"
  cidr_ipv4         = "0.0.0.0/0"
}

resource "aws_security_group" "database" {
  name        = "${var.name}-database"
  description = "gogi's Postgres, from gogi's server only"
  vpc_id      = aws_vpc.gogi.id

  tags = { Name = "${var.name}-database" }
}

resource "aws_vpc_security_group_ingress_rule" "database" {
  security_group_id            = aws_security_group.database.id
  description                  = "Postgres, from the server"
  ip_protocol                  = "tcp"
  from_port                    = 5432
  to_port                      = 5432
  referenced_security_group_id = aws_security_group.server.id
}
