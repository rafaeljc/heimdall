################################################################################
# Networking Module - Core Infrastructure
#
# Implements a highly available, 3-tier VPC architecture (Public, Private App, 
# and Private Data) using the official AWS VPC module.
#
# Features:
# - Automated IPAM (IP Address Management) using cidrsubnet
# - Strict data layer isolation (No outbound internet access for databases)
# - VPC Flow Logs enabled for SOC2/PCI-DSS compliance auditing
# - Pre-configured tags for AWS Load Balancer Controller (EKS integration)
################################################################################

terraform {
  required_version = ">= 1.0"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0"
    }
  }
}

locals {
  name = "heimdall-${var.environment}"

  # ----------------------------------------------------------------------------
  # Automated Subnet Calculation (IPAM)
  # Assuming base CIDR is 10.0.0.0/16:
  # ----------------------------------------------------------------------------

  # Tier 1: Public Subnets (/24) -> ~250 IPs per AZ (For ALBs, NAT Gateways)
  # Example: 10.0.0.0/24, 10.0.1.0/24, 10.0.2.0/24
  public_subnets = [for k, v in var.azs : cidrsubnet(var.vpc_cidr, 8, k)]

  # Tier 2: Private App Subnets (/20) -> ~4,096 IPs per AZ (For EKS Worker Nodes)
  # Example: 10.0.16.0/20, 10.0.32.0/20, 10.0.48.0/20
  private_subnets = [for k, v in var.azs : cidrsubnet(var.vpc_cidr, 4, k + 1)]

  # Tier 3: Private Data Subnets (/24) -> ~250 IPs per AZ (For RDS, ElastiCache)
  # Example: 10.0.100.0/24, 10.0.101.0/24, 10.0.102.0/24
  database_subnets = [for k, v in var.azs : cidrsubnet(var.vpc_cidr, 8, k + 100)]
}

module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "~> 5.5.0" # Pinned for stability

  name = local.name
  cidr = var.vpc_cidr
  azs  = var.azs

  public_subnets   = local.public_subnets
  private_subnets  = local.private_subnets
  database_subnets = local.database_subnets

  # ----------------------------------------------------------------------------
  # Data Layer Isolation
  # ----------------------------------------------------------------------------
  create_database_subnet_group           = true
  create_database_subnet_route_table     = true
  create_database_internet_gateway_route = false
  create_database_nat_gateway_route      = false

  # ----------------------------------------------------------------------------
  # NAT Gateway Configuration (High Availability)
  # ----------------------------------------------------------------------------
  enable_nat_gateway     = true
  single_nat_gateway     = var.environment == "prod" ? false : true
  one_nat_gateway_per_az = var.environment == "prod" ? true : false

  # ----------------------------------------------------------------------------
  # DNS Configuration (Required for EKS and RDS)
  # ----------------------------------------------------------------------------
  enable_dns_hostnames = true
  enable_dns_support   = true

  # ----------------------------------------------------------------------------
  # Security & Compliance
  # ----------------------------------------------------------------------------
  # Forcefully deny all traffic on the default VPC security group
  manage_default_security_group  = true
  default_security_group_ingress = []
  default_security_group_egress  = []

  # Enable VPC Flow Logs to CloudWatch (SOC2/Compliance requirement)
  enable_flow_log                      = true
  create_flow_log_cloudwatch_log_group = true
  create_flow_log_cloudwatch_iam_role  = true
  flow_log_max_aggregation_interval    = 60

  # ----------------------------------------------------------------------------
  # Kubernetes Integration Tags
  # Required by the AWS Load Balancer Controller to discover subnets
  # ----------------------------------------------------------------------------
  public_subnet_tags = {
    "kubernetes.io/role/elb" = 1
  }

  private_subnet_tags = {
    "kubernetes.io/role/internal-elb" = 1
  }

  tags = {
    TerraformModule = "networking"
    Environment     = var.environment
    Project         = "Heimdall"
  }
}

# ============================================================================
# VPC Gateway Endpoints - Cost Optimization
# ============================================================================
# Gateway endpoints reduce NAT Gateway data transfer costs by ~50% for S3 and
# DynamoDB access while improving security by keeping traffic within AWS.

data "aws_region" "current" {}

resource "aws_vpc_endpoint" "s3" {
  vpc_id            = module.vpc.vpc_id
  service_name      = "com.amazonaws.${data.aws_region.current.name}.s3"
  vpc_endpoint_type = "Gateway"
  route_table_ids   = concat(module.vpc.private_route_table_ids, module.vpc.database_route_table_ids)

  tags = {
    Name        = "${local.name}-s3-endpoint"
    Environment = var.environment
    Project     = "Heimdall"
  }
}

resource "aws_vpc_endpoint" "dynamodb" {
  vpc_id            = module.vpc.vpc_id
  service_name      = "com.amazonaws.${data.aws_region.current.name}.dynamodb"
  vpc_endpoint_type = "Gateway"
  route_table_ids   = concat(module.vpc.private_route_table_ids, module.vpc.database_route_table_ids)

  tags = {
    Name        = "${local.name}-dynamodb-endpoint"
    Environment = var.environment
    Project     = "Heimdall"
  }
}
