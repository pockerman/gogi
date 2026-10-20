# Document ingestion. The documents service queues each ingestion; the worker reads the document from S3, parses it,
# has the model service on the server embed its chunks and stores them in Chroma on the server, and reports on the
# job in Postgres.
#
# Until ingestion_image_tag is set, the worker runs on the server, polling a queue table in Postgres. Once the
# Lambda's image is in the ECR repository below (push-ingestion-image.sh), setting it to the image's tag queues
# ingestions in SQS instead, and a Lambda in the database's subnets ingests them: the server only answers it.

locals {
  ingestion_function = var.ingestion_image_tag != ""
  # the Lambda's time limit, and how long SQS hides a message it's handed: six times as long, as AWS advises
  ingestion_timeout_seconds = 900
}

# where the Lambda's image is; Lambda runs images from ECR only
resource "aws_ecr_repository" "ingestion" {
  name                 = "${var.name}-ingestion"
  image_tag_mutability = "MUTABLE"
  force_delete         = true

  image_scanning_configuration {
    scan_on_push = true
  }
}

resource "aws_ecr_lifecycle_policy" "ingestion" {
  repository = aws_ecr_repository.ingestion.name
  policy = jsonencode({
    rules = [{
      rulePriority = 1
      description  = "Keep the last 5 images"
      selection    = { tagStatus = "any", countType = "imageCountMoreThan", countNumber = 5 }
      action       = { type = "expire" }
    }]
  })
}

# the private subnets reach S3 through a gateway endpoint, which costs nothing: they have no NAT gateway
resource "aws_route_table" "private" {
  vpc_id = aws_vpc.gogi.id

  tags = { Name = "${var.name}-private" }
}

resource "aws_route_table_association" "private" {
  count          = 2
  subnet_id      = aws_subnet.private[count.index].id
  route_table_id = aws_route_table.private.id
}

resource "aws_vpc_endpoint" "s3" {
  vpc_id            = aws_vpc.gogi.id
  service_name      = "com.amazonaws.${var.region}.s3"
  vpc_endpoint_type = "Gateway"
  route_table_ids   = [aws_route_table.private.id]

  tags = { Name = "${var.name}-s3" }
}

resource "aws_sqs_queue" "ingestion_dead_letters" {
  count                     = local.ingestion_function ? 1 : 0
  name                      = "${var.name}-ingestion-dead-letters"
  message_retention_seconds = 1209600
}

resource "aws_sqs_queue" "ingestion" {
  count                      = local.ingestion_function ? 1 : 0
  name                       = "${var.name}-ingestion"
  visibility_timeout_seconds = 6 * local.ingestion_timeout_seconds
  message_retention_seconds  = 345600
  sqs_managed_sse_enabled    = true

  # a message the Lambda fails on three times goes to the dead letters; the Lambda has failed its job by then
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.ingestion_dead_letters[0].arn
    maxReceiveCount     = var.ingestion_max_attempts
  })
}

resource "aws_security_group" "ingestion" {
  count       = local.ingestion_function ? 1 : 0
  name        = "${var.name}-ingestion"
  description = "The ingestion Lambda: it calls Postgres, Chroma and the model service, and S3"
  vpc_id      = aws_vpc.gogi.id

  tags = { Name = "${var.name}-ingestion" }
}

# the subnets have no route out but the S3 endpoint's
resource "aws_vpc_security_group_egress_rule" "ingestion" {
  count             = local.ingestion_function ? 1 : 0
  security_group_id = aws_security_group.ingestion[0].id
  description       = "The VPC and S3"
  ip_protocol       = "-1"
  cidr_ipv4         = "0.0.0.0/0"
}

resource "aws_vpc_security_group_ingress_rule" "database_from_ingestion" {
  count                        = local.ingestion_function ? 1 : 0
  security_group_id            = aws_security_group.database.id
  description                  = "Postgres, from the ingestion Lambda"
  ip_protocol                  = "tcp"
  from_port                    = 5432
  to_port                      = 5432
  referenced_security_group_id = aws_security_group.ingestion[0].id
}

# Chroma and the model service are published on the server's private address for the Lambda alone
resource "aws_vpc_security_group_ingress_rule" "server_from_ingestion" {
  for_each                     = local.ingestion_function ? { chroma = 8000, llms = 50057 } : {}
  security_group_id            = aws_security_group.server.id
  description                  = "The ${each.key} port, from the ingestion Lambda"
  ip_protocol                  = "tcp"
  from_port                    = each.value
  to_port                      = each.value
  referenced_security_group_id = aws_security_group.ingestion[0].id
}

data "aws_iam_policy_document" "lambda_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "ingestion" {
  count              = local.ingestion_function ? 1 : 0
  name               = "${var.name}-ingestion"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
}

# its network interfaces in the VPC, and its logs
resource "aws_iam_role_policy_attachment" "ingestion_vpc" {
  count      = local.ingestion_function ? 1 : 0
  role       = aws_iam_role.ingestion[0].name
  policy_arn = "arn:${data.aws_partition.current.partition}:iam::aws:policy/service-role/AWSLambdaVPCAccessExecutionRole"
}

data "aws_iam_policy_document" "ingestion" {
  count = local.ingestion_function ? 1 : 0

  statement {
    sid       = "Queue"
    actions   = ["sqs:ReceiveMessage", "sqs:DeleteMessage", "sqs:GetQueueAttributes", "sqs:ChangeMessageVisibility"]
    resources = [aws_sqs_queue.ingestion[0].arn]
  }

  # the documents' uploads, to read
  statement {
    sid       = "Documents"
    actions   = ["s3:GetObject"]
    resources = ["${aws_s3_bucket.files["documents"].arn}/*"]
  }
}

resource "aws_iam_role_policy" "ingestion" {
  count  = local.ingestion_function ? 1 : 0
  name   = "${var.name}-ingestion"
  role   = aws_iam_role.ingestion[0].id
  policy = data.aws_iam_policy_document.ingestion[0].json
}

resource "aws_cloudwatch_log_group" "ingestion" {
  count             = local.ingestion_function ? 1 : 0
  name              = "/aws/lambda/${var.name}-ingestion"
  retention_in_days = 30
}

resource "aws_lambda_function" "ingestion" {
  count         = local.ingestion_function ? 1 : 0
  function_name = "${var.name}-ingestion"
  role          = aws_iam_role.ingestion[0].arn
  package_type  = "Image"
  image_uri     = "${aws_ecr_repository.ingestion.repository_url}:${var.ingestion_image_tag}"
  architectures = ["x86_64"]
  memory_size   = var.ingestion_memory_mb
  timeout       = local.ingestion_timeout_seconds

  vpc_config {
    subnet_ids         = aws_subnet.private[*].id
    security_group_ids = [aws_security_group.ingestion[0].id]
  }

  environment {
    variables = {
      GOGI_INGESTION_MAX_ATTEMPTS = tostring(var.ingestion_max_attempts)
      GOGI_OBJECT_STORE           = "s3"
      GOGI_S3_ENDPOINT            = "s3.${var.region}.amazonaws.com"
      GOGI_S3_REGION              = var.region
      VECTOR_STORAGE_TYPE         = "chromadb"
      VECTOR_STORAGE_HOST         = local.server_private_ip
      VECTOR_STORAGE_PORT         = "8000"
      GOGI_LLM_SERVICE_ADDR       = "${local.server_private_ip}:50057"
      DB_HOST                     = aws_db_instance.gogi.address
      DB_PORT                     = tostring(aws_db_instance.gogi.port)
      DB_NAME                     = aws_db_instance.gogi.db_name
      DB_USER                     = aws_db_instance.gogi.username
      # encrypted at rest by Lambda; the subnets reach no Secrets Manager to read it from
      DB_PASSWORD = random_password.database.result
    }
  }

  depends_on = [aws_cloudwatch_log_group.ingestion, aws_iam_role_policy_attachment.ingestion_vpc]
}

resource "aws_lambda_event_source_mapping" "ingestion" {
  count            = local.ingestion_function ? 1 : 0
  event_source_arn = aws_sqs_queue.ingestion[0].arn
  function_name    = aws_lambda_function.ingestion[0].arn
  batch_size       = 1
  # only the messages that failed are delivered again
  function_response_types = ["ReportBatchItemFailures"]

  # the server embeds the chunks: a few ingestions at a time keep it answering applications
  scaling_config {
    maximum_concurrency = var.ingestion_concurrency
  }
}
