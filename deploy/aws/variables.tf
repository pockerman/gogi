variable "region" {
  description = "The AWS region gogi runs in"
  type        = string
  default     = "eu-west-2"
}

variable "name" {
  description = "The deployment's name: it prefixes its resources and secrets"
  type        = string
  default     = "gogi"
}

variable "client_cidrs" {
  description = "Where applications call gogi's gateway from, e.g. [\"203.0.113.7/32\"] for an application's server; [\"0.0.0.0/0\"] lets any address try, with a key"
  type        = list(string)
}

variable "domain" {
  description = "A domain name for the gateway, once there is one, e.g. gogi.example.org; the certificate is for the server's public IP address either way"
  type        = string
  default     = ""
}

variable "images" {
  description = "Where gogi's images are published"
  type        = string
  default     = "ghcr.io/pockerman"
}

variable "image_tag" {
  description = "The images to run, e.g. main or sha-<commit>; changing it replaces the server, keeping its data"
  type        = string
  default     = "main"
}

variable "instance_type" {
  description = "The server: gogi needs about 2.5 GB, its two small embedding models half a GB of it"
  type        = string
  default     = "t3.medium"
}

variable "data_volume_gb" {
  description = "The size of the disk for Chroma's and the embedding models' data"
  type        = number
  default     = 30
}

variable "snapshot_days" {
  description = "How many daily snapshots of the data disk are kept"
  type        = number
  default     = 7
}

variable "file_versions_days" {
  description = "How many days the files' deleted and replaced versions are kept in S3"
  type        = number
  default     = 30
}

variable "db_instance_class" {
  description = "The Postgres server gogi's services share"
  type        = string
  default     = "db.t4g.small"
}

variable "db_allocated_storage_gb" {
  description = "Postgres' storage; it grows on its own up to ten times this"
  type        = number
  default     = 20
}

variable "db_backup_days" {
  description = "How many days of Postgres' backups are kept"
  type        = number
  default     = 7
}

variable "deletion_protection" {
  description = "Whether Postgres is protected from being deleted, e.g. by terraform destroy"
  type        = bool
  default     = true
}

variable "ingestion_image_tag" {
  description = "The tag of the ingestion Lambda's image in the ingestion ECR repository (push-ingestion-image.sh); empty for the worker on the server, polling Postgres. Changing it replaces the server, keeping its data"
  type        = string
  default     = ""
}

variable "ingestion_memory_mb" {
  description = "The ingestion Lambda's memory; it reads, parses and chunks a whole document"
  type        = number
  default     = 1024
}

variable "ingestion_concurrency" {
  description = "How many ingestions the Lambda runs at once (2 at least): the server embeds their chunks"
  type        = number
  default     = 2
}

variable "ingestion_max_attempts" {
  description = "How many times an ingestion is tried before its job fails"
  type        = number
  default     = 3
}

variable "certificate_validity_days" {
  description = "How long the gateway's certificate is valid; terraform apply renews it in its last 30 days"
  type        = number
  default     = 825
}
