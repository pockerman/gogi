variable "subscription_id" {
  description = "The Azure subscription gogi runs in"
  type        = string
}

variable "location" {
  description = "The Azure region gogi runs in"
  type        = string
  default     = "uksouth"
}

variable "name" {
  description = "The deployment's name: it names its resource group and prefixes its resources"
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

variable "vm_size" {
  description = "The server: its Python workers each hold an embedding model in memory"
  type        = string
  default     = "Standard_B4ms"
}

variable "data_disk_gb" {
  description = "The size of the disk for Chroma's and the embedding models' data"
  type        = number
  default     = 32
}

variable "file_versions_days" {
  description = "How many days deleted files are kept in Blob Storage"
  type        = number
  default     = 30
}

variable "backup_days" {
  description = "How many days of the data disk's daily backups, and of Postgres' backups, are kept"
  type        = number
  default     = 7
}

variable "db_sku" {
  description = "The Postgres server gogi's services share"
  type        = string
  default     = "B_Standard_B2s"
}

variable "ingestion_function" {
  description = "Whether documents are ingested by a Function (publish its code with workers/azure_function/build.sh) rather than the worker on the server; changing it replaces the server, keeping its data"
  type        = bool
  default     = false
}

variable "ingestion_concurrency" {
  description = "How many ingestions the Function runs at once: the server embeds their chunks"
  type        = number
  default     = 2
}

variable "db_storage_mb" {
  description = "Postgres' storage"
  type        = number
  default     = 32768
}

variable "certificate_validity_days" {
  description = "How long the gateway's certificate is valid; terraform apply renews it in its last 30 days"
  type        = number
  default     = 825
}

variable "deletion_protection" {
  description = "Whether Postgres is locked against being deleted, e.g. by terraform destroy"
  type        = bool
  default     = true
}
