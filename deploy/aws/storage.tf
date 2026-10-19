# Where gogi keeps files: documents' uploads and prompts' texts. S3's names are shared by everyone, so they end in
# a suffix of the deployment's. Deleted and replaced files are kept for a while, to recover from mistakes
resource "random_string" "buckets" {
  length  = 8
  upper   = false
  special = false
}

locals {
  buckets = {
    documents = "${var.name}-documents-${random_string.buckets.result}"
    prompts   = "${var.name}-prompts-${random_string.buckets.result}"
  }
}

resource "aws_s3_bucket" "files" {
  for_each = local.buckets
  bucket   = each.value
}

resource "aws_s3_bucket_public_access_block" "files" {
  for_each                = aws_s3_bucket.files
  bucket                  = each.value.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_ownership_controls" "files" {
  for_each = aws_s3_bucket.files
  bucket   = each.value.id

  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "files" {
  for_each = aws_s3_bucket.files
  bucket   = each.value.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_versioning" "files" {
  for_each = aws_s3_bucket.files
  bucket   = each.value.id

  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "files" {
  for_each = aws_s3_bucket.files
  bucket   = each.value.id

  rule {
    id     = "earlier-versions"
    status = "Enabled"
    filter {}

    noncurrent_version_expiration {
      noncurrent_days = var.file_versions_days
    }
    # a deleted file's marker, once its versions are gone
    expiration {
      expired_object_delete_marker = true
    }
    abort_incomplete_multipart_upload {
      days_after_initiation = 7
    }
  }

  depends_on = [aws_s3_bucket_versioning.files]
}
