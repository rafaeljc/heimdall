################################################################################
# Layer 1: Network - SSM Parameter Store Integration
#
# Publishes critical network infrastructure identifiers to AWS Systems Manager
# (SSM) Parameter Store. This creates a secure, loosely-coupled API contract
# for upstream layers (Layer 2+) to consume without requiring:
# - Direct access to Layer 1's Terraform state file
# - Knowledge of state backend configuration
# - Terraform installed or configured
#
# Benefits:
# - Security: Granular IAM permissions per parameter via SSM
# - Operability: Easy CLI/API access without state file management
# - Decoupling: Layers can be deployed independently
# - Discovery: Hierarchical naming enables batch retrieval
#
# Consumption Examples:
#
# 1. AWS CLI (single parameter):
#    aws ssm get-parameter --name /heimdall/prod/network/vpc_id \
#      --query 'Parameter.Value' --output text
#
# 2. AWS CLI (all parameters):
#    aws ssm get-parameters-by-path \
#      --path /heimdall/prod/network/ \
#      --recursive --query 'Parameters[].{Name:Name,Value:Value}'
#
# 3. Terraform (Layer 2):
#    data "aws_ssm_parameter" "vpc_id" {
#      name = "/heimdall/${var.environment}/network/vpc_id"
#    }
#    locals {
#      vpc_id = data.aws_ssm_parameter.vpc_id.value
#    }
#
# 4. Lambda/Application Code:
#    import boto3
#    ssm = boto3.client('ssm')
#    vpc_id = ssm.get_parameter(
#      Name='/heimdall/prod/network/vpc_id'
#    )['Parameter']['Value']
#
# Parameter Naming Convention:
#   /heimdall/{environment}/network/{resource_type}
#   - Project level: /heimdall/
#   - Environment: dev, staging, prod
#   - Component: network (Layer 1 identifier)
#   - Resource: vpc_id, private_subnets, etc.
#
# All parameters are tagged with Environment and Layer for:
# - Cost allocation by environment
# - Audit trail and compliance tracking
# - Lifecycle management and bulk operations
################################################################################

# VPC Identifier
# Required by all resources that need to reference the VPC
# Used for: Security groups, ENIs, VPC endpoints, VPC peering
resource "aws_ssm_parameter" "vpc_id" {
  name        = "/heimdall/${var.environment}/network/vpc_id"
  description = "VPC ID for ${var.environment} environment. Used for resource placement."
  type        = "String"
  value       = module.network.vpc_id
  tier        = "Standard"

  tags = {
    Environment = var.environment
    Layer       = "Layer1-Network"
    Resource    = "vpc_id"
  }
}

# Private Application Subnets (Tier 2)
# Comma-delimited list of subnet IDs for EKS worker nodes
# Each subnet in a different AZ for high availability
# Format: subnet-xxxxx,subnet-yyyyy,subnet-zzzzz
resource "aws_ssm_parameter" "private_subnets" {
  name        = "/heimdall/${var.environment}/network/private_subnets"
  description = "Private App Subnet IDs (Tier 2) for ${var.environment} environment. Comma-delimited."
  type        = "StringList"
  value       = join(",", module.network.private_subnets)
  tier        = "Standard"

  tags = {
    Environment = var.environment
    Layer       = "Layer1-Network"
    Resource    = "private_subnets"
    Tier        = "Application"
  }
}

# Public Subnets (Tier 1)
# Comma-delimited list of subnet IDs for load balancers
# Used for: ALBs, NAT gateways, internet-facing resources
# Each subnet in a different AZ for high availability
# Format: subnet-xxxxx,subnet-yyyyy,subnet-zzzzz
resource "aws_ssm_parameter" "public_subnets" {
  name        = "/heimdall/${var.environment}/network/public_subnets"
  description = "Public Subnet IDs (Tier 1) for ${var.environment} environment. Comma-delimited."
  type        = "StringList"
  value       = join(",", module.network.public_subnets)
  tier        = "Standard"

  tags = {
    Environment = var.environment
    Layer       = "Layer1-Network"
    Resource    = "public_subnets"
    Tier        = "DMZ"
  }
}

# Database Subnets (Tier 3)
# Comma-delimited list of subnet IDs for RDS and ElastiCache
# CRITICAL: These subnets are isolated from internet (no NAT routes)
# Used for: Aurora database instances, ElastiCache clusters, secrets storage
# Each subnet in a different AZ for high availability
# Format: subnet-xxxxx,subnet-yyyyy,subnet-zzzzz
resource "aws_ssm_parameter" "database_subnets" {
  name        = "/heimdall/${var.environment}/network/database_subnets"
  description = "Database Subnet IDs (Tier 3, isolated) for ${var.environment} environment. Comma-delimited."
  type        = "StringList"
  value       = join(",", module.network.database_subnets)
  tier        = "Standard"

  tags = {
    Environment = var.environment
    Layer       = "Layer1-Network"
    Resource    = "database_subnets"
    Tier        = "Data"
    Isolation   = "true"
  }
}

# Database Subnet Group Name
# Required for RDS cluster creation
# Used by: Aurora, RDS instances (Multi-AZ deployments)
# Name format: heimdall-{environment}
resource "aws_ssm_parameter" "database_subnet_group_name" {
  name        = "/heimdall/${var.environment}/network/database_subnet_group_name"
  description = "RDS Database Subnet Group name for ${var.environment} environment."
  type        = "String"
  value       = module.network.database_subnet_group_name
  tier        = "Standard"

  tags = {
    Environment = var.environment
    Layer       = "Layer1-Network"
    Resource    = "database_subnet_group_name"
    Service     = "RDS"
  }
}

# Default Security Group (Hardened)
# SECURITY: Explicitly denies all ingress and egress traffic
# Use this as reference only; never attach to resources
# Creates zero-trust foundation; explicit rules required for traffic
resource "aws_ssm_parameter" "default_security_group_id" {
  name        = "/heimdall/${var.environment}/network/default_security_group_id"
  description = "Hardened Default Security Group ID for ${var.environment} environment."
  type        = "String"
  value       = module.network.default_security_group_id
  tier        = "Standard"

  tags = {
    Environment = var.environment
    Layer       = "Layer1-Network"
    Resource    = "default_security_group_id"
    Security    = "hardened"
  }
}

# S3 Gateway VPC Endpoint
# Enables private access to S3 without NAT gateway
# Reduces data transfer costs by ~50% for S3 traffic
# Used by: Terraform state access, application data access, logs
resource "aws_ssm_parameter" "s3_endpoint_id" {
  name        = "/heimdall/${var.environment}/network/s3_endpoint_id"
  description = "S3 Gateway VPC Endpoint ID for ${var.environment} environment."
  type        = "String"
  value       = module.network.s3_endpoint_id
  tier        = "Standard"

  tags = {
    Environment      = var.environment
    Layer            = "Layer1-Network"
    Resource         = "s3_endpoint_id"
    Service          = "S3"
    CostOptimization = "true"
  }
}

# DynamoDB Gateway VPC Endpoint
# Enables private access to DynamoDB without NAT gateway
# Reduces data transfer costs by ~50% for DynamoDB traffic
# Used by: Terraform state locking, application data access
resource "aws_ssm_parameter" "dynamodb_endpoint_id" {
  name        = "/heimdall/${var.environment}/network/dynamodb_endpoint_id"
  description = "DynamoDB Gateway VPC Endpoint ID for ${var.environment} environment."
  type        = "String"
  value       = module.network.dynamodb_endpoint_id
  tier        = "Standard"

  tags = {
    Environment      = var.environment
    Layer            = "Layer1-Network"
    Resource         = "dynamodb_endpoint_id"
    Service          = "DynamoDB"
    CostOptimization = "true"
  }
}
