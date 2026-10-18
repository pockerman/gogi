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
  description = "The server: its Python workers each hold an embedding model in memory"
  type        = string
  default     = "t3.xlarge"
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
  description = "The Postgres server, which gogi's services and Temporal share"
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

variable "certificate_validity_days" {
  description = "How long the gateway's certificate is valid; terraform apply renews it in its last 30 days"
  type        = number
  default     = 825
}
