terraform {
  required_version = ">= 1.6"

  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "~> 4.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
    tls = {
      source  = "hashicorp/tls"
      version = "~> 4.0"
    }
  }

  # The state holds the database's password and the certificate authority's key: keep it somewhere private and
  # encrypted, e.g. an Azure Storage container of its own, by copying backend.tf.example to backend.tf
}

provider "azurerm" {
  subscription_id = var.subscription_id

  features {
    key_vault {
      # the vault and its secrets are deleted with the deployment, not kept for recovery
      purge_soft_delete_on_destroy    = true
      recover_soft_deleted_key_vaults = false
    }
  }
}
