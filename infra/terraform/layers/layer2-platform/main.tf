################################################################################
# Layer 2: Platform - Core Orchestration
#
# Wires together the Compute (EKS) and Data (Postgres, Redis) modules.
# Enforces Zero-Trust networking: databases only accept traffic from EKS nodes.
################################################################################

terraform {
  required_version = ">= 1.0"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0" # AWS Provider 5.x (current stable, blocks 6.0+)
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
      Project     = "Heimdall"        # Project identifier
      Environment = var.environment   # dev, staging, prod
      ManagedBy   = "Terraform"       # IaC tooling
      Layer       = "Layer2-Platform" # Infrastructure layer
    }
  }
}

# ------------------------------------------------------------------------------
# 1. Compute: Amazon EKS
# ------------------------------------------------------------------------------
module "eks" {
  source = "../../modules/compute/eks"

  environment = var.environment
  vpc_id      = data.aws_ssm_parameter.vpc_id.value

  cluster_endpoint_public_access_cidrs = var.cluster_endpoint_public_access_cidrs

  # SSM stores StringLists as comma-separated values. We split them back to lists.
  private_subnets = split(",", data.aws_ssm_parameter.private_subnets.value)
}

# ------------------------------------------------------------------------------
# 2. Relational Data: Aurora PostgreSQL
# ------------------------------------------------------------------------------
module "postgres" {
  source = "../../modules/data/postgres"

  environment          = var.environment
  vpc_id               = data.aws_ssm_parameter.vpc_id.value
  db_subnet_group_name = data.aws_ssm_parameter.database_subnet_group_name.value

  # ZERO TRUST: Only EKS Worker Nodes can access PostgreSQL
  allowed_security_group_ids = [module.eks.node_security_group_id]
}

# ------------------------------------------------------------------------------
# 3. In-Memory Cache: ElastiCache Redis
# ------------------------------------------------------------------------------
module "redis" {
  source = "../../modules/data/redis"

  environment      = var.environment
  vpc_id           = data.aws_ssm_parameter.vpc_id.value
  database_subnets = split(",", data.aws_ssm_parameter.database_subnets.value)

  # ZERO TRUST: Only EKS Worker Nodes can access Redis
  allowed_security_group_ids = [module.eks.node_security_group_id]
}
