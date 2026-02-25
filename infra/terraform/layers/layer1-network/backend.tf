################################################################################
# Layer 1: Network - Terraform Backend Configuration
#
# This file configures remote state management for Layer 1 infrastructure.
# State files contain sensitive information and must be encrypted and locked.
#
# Backend Configuration:
# - Storage: S3 bucket with versioning and encryption
# - Locking: DynamoDB table to prevent concurrent modifications
# - Access: Restricted via IAM policies and bucket policies
#
# Initialization:
#   terraform init -backend-config=../../bootstrap/backend.hcl
#
# The bootstrap/backend.hcl must include:
# - bucket: S3 bucket name for state storage
# - region: AWS region for S3 and DynamoDB
# - dynamodb_table: DynamoDB table name for state locking
# - encrypt: true (ensures AES-256 encryption at rest)
#
# State file path: s3://<bucket>/layer1-network/terraform.tfstate
# Lock table: <dynamodb_table> with key: layer1-network/terraform.tfstate
################################################################################

terraform {
  backend "s3" {
    key = "layer1-network/terraform.tfstate"
  }
}
