################################################################################
# Heimdall Infrastructure - Layer 0: Bootstrap / Foundation
# 
# This module creates the core AWS infrastructure needed to manage all other
# Terraform configurations. It is a ONE-TIME manual deployment that establishes:
#
# 1. Terraform State Management (S3 bucket with versioning & encryption)
# 2. State Locking Mechanism (DynamoDB table to prevent concurrent modifications)
#
# IMPORTANT: This layer must be deployed manually BEFORE deploying any other
# infrastructure layers. After initial deployment, this can be left as-is.
#
# Reference: https://developer.hashicorp.com/terraform/language/backends/s3
################################################################################

terraform {
  required_version = ">= 1.0"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }

  # Backend is configured dynamically during bootstrap migration.
  # See setup.sh for the migration process.
  # Initially, Terraform uses local state in .terraform/terraform.tfstate
}

provider "aws" {
  region = var.aws_region

  default_tags {
    tags = {
      Project     = "Heimdall"
      Environment = "Management"
      ManagedBy   = "Terraform"
      Layer       = "Bootstrap"
    }
  }
}

# =============================================================================
# S3 BUCKET FOR TERRAFORM STATE
# 
# Stores all Terraform state files in a centralized, versioned, and encrypted
# location. State files contain sensitive information and infrastructure details.
#
# Security features:
# - Versioning enabled: Allows recovery from accidental state corruption
# - Encryption: AES256 SSE protects state files at rest
# - Public access blocked: Prevents unauthorized access
# - Lifecycle protection: Prevents accidental deletion
# =============================================================================
resource "aws_s3_bucket" "terraform_state" {
  # checkov:skip=CKV_AWS_144: "Cross-region replication is overkill for portfolio state"
  # checkov:skip=CKV_AWS_145: "Default AES256 encryption is acceptable for this scope"
  # checkov:skip=CKV2_AWS_62: "Event notifications are not required for state files"
  # checkov:skip=CKV2_AWS_61: "Lifecycle configuration is not needed for state files"

  bucket = "heimdall-tfstate-production-${var.bucket_name_suffix}"

  # Prevents accidental deletion of the state bucket
  lifecycle {
    prevent_destroy = true
  }
}

# Versioning configuration
# Maintains a complete history of state file changes for recovery and auditing
resource "aws_s3_bucket_versioning" "terraform_state" {
  bucket = aws_s3_bucket.terraform_state.id

  versioning_configuration {
    status = "Enabled"
  }
}

# Server-side encryption configuration
# Encrypts state files at rest using AWS-managed AES256 encryption
# This ensures sensitive data (passwords, keys, IDs) are protected
resource "aws_s3_bucket_server_side_encryption_configuration" "terraform_state" {
  bucket = aws_s3_bucket.terraform_state.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

# Public access block configuration
# Ensures the bucket is never publicly accessible, even through misconfigured
# policies or ACLs. This is critical for protecting sensitive state files.
resource "aws_s3_bucket_public_access_block" "terraform_state" {
  bucket                  = aws_s3_bucket.terraform_state.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# S3 bucket logging
# Enables access logging to track all S3 operations for audit and compliance
resource "aws_s3_bucket_logging" "terraform_state" {
  bucket = aws_s3_bucket.terraform_state.id

  target_bucket = aws_s3_bucket.terraform_state.id
  target_prefix = "logs/"
}

# S3 lifecycle policy
# Expires old versions after 90 days to manage costs while maintaining auditability
resource "aws_s3_bucket_lifecycle_configuration" "terraform_state" {
  bucket = aws_s3_bucket.terraform_state.id

  rule {
    id     = "expire-old-versions"
    status = "Enabled"

    filter {}

    noncurrent_version_expiration {
      noncurrent_days = 90
    }
  }
}

# S3 bucket policy to prevent deletion via CLI and automation
# Denies all DeleteBucket and DeleteObject operations from any principal, blocking:
# - CLI commands (aws s3 rm, aws s3api delete-object, etc.)
# - Terraform destroy operations
# - Lambda, CI/CD, or any automated processes
#
# To delete the bucket, an admin must:
# 1. Manually navigate to the S3 bucket policy in AWS console
# 2. Explicitly remove or modify this policy
# 3. Attempt deletion through the console
#
# Note: Restrict IAM permissions to prevent unauthorized policy modification.
# This policy protects against accidental/automated deletion, not determined actors
# with full IAM permissions.
resource "aws_s3_bucket_policy" "terraform_state" {
  bucket = aws_s3_bucket.terraform_state.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid       = "DenyDeleteActions"
        Effect    = "Deny"
        Principal = "*"
        Action = [
          "s3:DeleteBucket",
          "s3:DeleteObject",
          "s3:DeleteObjectVersion"
        ]
        Resource = [
          aws_s3_bucket.terraform_state.arn,
          "${aws_s3_bucket.terraform_state.arn}/*"
        ]
      }
    ]
  })
}

# =============================================================================
# DYNAMODB TABLE FOR STATE LOCKING
#
# Prevents concurrent Terraform operations on shared state. When one team
# member runs `terraform apply`, others cannot simultaneously modify state,
# which prevents data loss and infrastructure inconsistencies.
#
# How it works:
# - Each Terraform operation acquires a lock before modifying state
# - The lock is held for the duration of the operation
# - Other operations must wait until the lock is released
# - If a lock is held indefinitely, it indicates a failed operation
#
# Reference: https://developer.hashicorp.com/terraform/language/state/locking
# =============================================================================
resource "aws_dynamodb_table" "terraform_locks" {
  # checkov:skip=CKV_AWS_28: "DynamoDB PITR is unnecessary for a portfolio state lock table"
  # checkov:skip=CKV_AWS_119: "Default AWS managed KMS encryption is sufficient for state locks"
  # checkov:skip=CKV2_AWS_16: "Auto-scaling is unnecessary for a single-developer lock table"

  name                        = "heimdall-tflock-production-${var.bucket_name_suffix}"
  billing_mode                = "PAY_PER_REQUEST"
  hash_key                    = "LockID"
  deletion_protection_enabled = true

  attribute {
    name = "LockID"
    type = "S"
  }

  # Explicitly enable encryption (enabled by default, but declared for audit/compliance)
  server_side_encryption {
    enabled = true
  }

  # Prevent accidental deletion
  lifecycle {
    prevent_destroy = true
  }
}
