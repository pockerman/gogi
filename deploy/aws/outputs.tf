output "gateway_address" {
  description = "Where applications call gogi, e.g. their GOGI_GATEWAY_URL"
  value       = "${aws_eip.server.public_ip}:443"
}

output "ca_certificate" {
  description = "The certificate applications check the gateway against, e.g. their GOGI_GATEWAY_CA_FILE"
  value       = tls_self_signed_cert.ca.cert_pem
}

output "server_public_ip" {
  description = "The server's address, to let it call an application's own services"
  value       = aws_eip.server.public_ip
}

output "instance_id" {
  description = "The server, to reach with: aws ssm start-session --target <instance_id>"
  value       = aws_instance.server.id
}

output "credential_prefix" {
  description = "Where applications' provider keys are kept in Secrets Manager"
  value       = "${var.name}/credentials/"
}

output "ingestion_repository" {
  description = "Where push-ingestion-image.sh pushes the ingestion Lambda's image"
  value       = aws_ecr_repository.ingestion.repository_url
}

output "ingestion" {
  description = "Where documents are ingested: the Lambda and its queues, or the server"
  value = local.ingestion_function ? {
    lambda            = aws_lambda_function.ingestion[0].function_name
    queue             = aws_sqs_queue.ingestion[0].url
    dead_letter_queue = aws_sqs_queue.ingestion_dead_letters[0].url
  } : { worker = "on the server, polling Postgres" }
}

output "registry_secret" {
  description = "Where to put a token for the images, if they're private"
  value       = aws_secretsmanager_secret.registry.name
}
