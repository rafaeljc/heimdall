################################################################################
# Layer 1: Network Infrastructure
#
# This layer deploys the foundational VPC architecture required for all
# upstream components (EKS, Aurora, Redis, etc.).
#
# Architecture:
#   - 3-tier VPC (Public, Private App, Private Data)
#   - Multi-AZ (3 Availability Zones for HA)
#   - Automated subnet allocation (no manual CIDR calculation)
#   - NAT Gateway HA (conditional: 1 for non-prod, 3 for prod)
#   - VPC Flow Logs (compliance auditing)
#   - Gateway VPC Endpoints (cost optimization)
#
# Prerequisites:
#   - Layer 0 (Bootstrap) deployed
#   - AWS credentials configured
#   - Terraform backend initialized
#
# Expected Deployment Time: 3-5 minutes
# Cost Impact: ~$115/month (3 NAT + Flow Logs + endpoints)
################################################################################

terraform {
  required_version = ">= 1.0"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0" # AWS Provider 5.x (current stable)
    }
  }
}

provider "aws" {
  region = var.aws_region

  # Default tags applied to ALL resources created in this layer
  # Ensures consistent tagging across infrastructure for:
  # - Cost allocation (by Project, Environment)
  # - Access control (by Environment)
  # - Lifecycle management (by Layer)
  default_tags {
    tags = {
      Project     = "Heimdall"       # Project identifier
      Environment = var.environment  # dev, staging, prod
      ManagedBy   = "Terraform"      # IaC tooling
      Layer       = "Layer1-Network" # Infrastructure layer
    }
  }
}

# Query available AZs in the selected region
# This ensures subnets are created in AZs that actually exist in the region
# (regions have different AZ names, e.g., us-east-1a vs eu-west-1a)
#
# Data source filters for available AZs only:
# - Excludes AZs in maintenance or unavailable state
# - Dynamically adapts to regional capacity
data "aws_availability_zones" "available" {
  state = "available"
}

# Instantiate the networking module
# This module encapsulates all VPC configuration logic and outputs the
# infrastructure needed by Layer 2 (Platform).
#
# Module inputs:
#   - environment: Used for conditional HA logic (prod gets multi-NAT)
#   - vpc_cidr: Base CIDR block; subnet allocation happens internally
#   - azs: List of 3 AZs; slice() ensures exactly 3 even if more available
#
# Module outputs consumed by Layer 2:
#   - vpc_id: VPC identifier
#   - public_subnets: For ALBs and internet-facing resources
#   - private_subnets: For EKS worker nodes
#   - database_subnets: For Aurora, ElastiCache (isolated tier)
#   - flow_logs_cloudwatch_log_group_name: For compliance monitoring
#   - s3_endpoint_id, dynamodb_endpoint_id: For cost tracking
#
# Why 3 AZs exactly?
#   - Minimum for true HA (1 AZ failure still leaves 2 operational)
#   - Standard AWS best practice
#   - Balances cost vs. availability
#   - Most regions have >= 3 AZs
module "network" {
  source = "../../modules/networking"

  environment = var.environment
  vpc_cidr    = var.vpc_cidr

  # Use slice() to guarantee exactly 3 AZs, even if the region has more
  # This ensures consistent deployment across regions with varying AZ counts
  # Examples:
  #   - us-east-1: 6 AZs → slice(0,3) → [us-east-1a, 1b, 1c]
  #   - eu-west-1: 3 AZs → slice(0,3) → [eu-west-1a, 1b, 1c]
  azs = slice(data.aws_availability_zones.available.names, 0, 3)
}
