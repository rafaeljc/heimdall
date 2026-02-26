################################################################################
# Layer 2: Platform - Contract Publishing
#
# Publishes EKS, Postgres, and Redis connection details to Parameter Store
# for downstream consumption by Application Deployments (Layer 3).
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
#    aws ssm get-parameter --name /heimdall/prod/platform/eks_cluster_name \
#      --query 'Parameter.Value' --output text
#
# 2. AWS CLI (all parameters):
#    aws ssm get-parameters-by-path \
#      --path /heimdall/prod/platform/ \
#      --recursive --query 'Parameters[].{Name:Name,Value:Value}'
#
# 3. Terraform (Layer 3):
#    data "aws_ssm_parameter" "eks_cluster_name" {
#      name = "/heimdall/${var.environment}/platform/eks_cluster_name"
#    }
#
# Parameter Naming Convention:
#   /heimdall/{environment}/platform/{resource_type}
#   /heimdall/{environment}/data/{resource_type}
#   - Project level: /heimdall/
#   - Environment: dev, staging, prod
#   - Component: platform (EKS) or data (Postgres, Redis)
#   - Resource: eks_cluster_name, postgres_endpoint, redis_endpoint, etc.
#
# All parameters are tagged with Environment and Component for:
# - Cost allocation by environment and service
# - Audit trail and compliance tracking
# - Lifecycle management and bulk operations
################################################################################

# EKS Cluster Name
# Required by: Kubernetes clients, deployment pipelines, monitoring
# Used for: kubectl context configuration, AWS API calls, resource tagging
resource "aws_ssm_parameter" "eks_cluster_name" {
  name        = "/heimdall/${var.environment}/platform/eks_cluster_name"
  description = "EKS Cluster Name for ${var.environment} environment."
  type        = "String"
  value       = module.eks.cluster_name
  tier        = "Standard"

  tags = {
    Environment = var.environment
    Layer       = "Layer2-Platform"
    Component   = "eks"
    Resource    = "cluster_name"
  }
}

# EKS Cluster Endpoint
# Required by: Kubernetes API clients, OIDC configuration, cluster access
# Used for: kubectl configuration, pod credential webhooks, direct API calls
resource "aws_ssm_parameter" "eks_cluster_endpoint" {
  name        = "/heimdall/${var.environment}/platform/eks_cluster_endpoint"
  description = "EKS Cluster API Endpoint for ${var.environment} environment."
  type        = "String"
  value       = module.eks.cluster_endpoint
  tier        = "Standard"

  tags = {
    Environment = var.environment
    Layer       = "Layer2-Platform"
    Component   = "eks"
    Resource    = "cluster_endpoint"
  }
}

# EKS Node IAM Role ARN
# Required by: IAM role mapping, IRSA (IAM Roles for Service Accounts)
# Used for: Pod identity configuration, cross-account access, permission delegation
resource "aws_ssm_parameter" "eks_node_iam_role_arn" {
  name        = "/heimdall/${var.environment}/platform/eks_node_iam_role_arn"
  description = "IAM Role ARN for EKS Worker Nodes in ${var.environment} environment."
  type        = "String"
  value       = module.eks.node_iam_role_arn
  tier        = "Standard"

  tags = {
    Environment = var.environment
    Layer       = "Layer2-Platform"
    Component   = "eks"
    Resource    = "node_iam_role_arn"
  }
}

# PostgreSQL Cluster Endpoint
# Required by: Application database connections, migration tools
# Used for: Connection strings, RDS proxy configuration, read/write operations
resource "aws_ssm_parameter" "postgres_endpoint" {
  name        = "/heimdall/${var.environment}/data/postgres_endpoint"
  description = "Aurora PostgreSQL Primary Endpoint for ${var.environment} environment."
  type        = "String"
  value       = module.postgres.cluster_endpoint
  tier        = "Standard"

  tags = {
    Environment = var.environment
    Layer       = "Layer2-Platform"
    Component   = "postgres"
    Resource    = "cluster_endpoint"
    Service     = "Aurora"
  }
}

# PostgreSQL Credentials (Secrets Manager ARN)
# SECURITY: Secrets Manager ARN, not credentials themselves
# Required by: Applications needing database credentials
# Used for: Secret retrieval, credential rotation, audit logging
resource "aws_ssm_parameter" "postgres_secret_arn" {
  name        = "/heimdall/${var.environment}/data/postgres_secret_arn"
  description = "Secrets Manager ARN for PostgreSQL credentials in ${var.environment} environment."
  type        = "String"
  value       = module.postgres.cluster_master_user_secret_arn
  tier        = "Standard"

  tags = {
    Environment = var.environment
    Layer       = "Layer2-Platform"
    Component   = "postgres"
    Resource    = "secret_arn"
    Service     = "Aurora"
    Security    = "sensitive"
  }
}

# Redis/ElastiCache Endpoint
# Required by: Cache clients, connection poolers
# Used for: Redis connection strings, cache operations, cluster discovery
resource "aws_ssm_parameter" "redis_endpoint" {
  name        = "/heimdall/${var.environment}/data/redis_endpoint"
  description = "ElastiCache Redis Primary Endpoint for ${var.environment} environment."
  type        = "String"
  value       = module.redis.primary_endpoint_address
  tier        = "Standard"

  tags = {
    Environment = var.environment
    Layer       = "Layer2-Platform"
    Component   = "redis"
    Resource    = "primary_endpoint"
    Service     = "ElastiCache"
  }
}

# Redis AUTH Token (Secrets Manager ARN)
# SECURITY: Secrets Manager ARN, not token itself
# Required by: Cache clients requiring authentication
# Used for: Redis AUTH token retrieval, credential rotation
resource "aws_ssm_parameter" "redis_secret_arn" {
  name        = "/heimdall/${var.environment}/data/redis_secret_arn"
  description = "Secrets Manager ARN for Redis AUTH token in ${var.environment} environment."
  type        = "String"
  value       = module.redis.auth_token_secret_arn
  tier        = "Standard"

  tags = {
    Environment = var.environment
    Layer       = "Layer2-Platform"
    Component   = "redis"
    Resource    = "secret_arn"
    Service     = "ElastiCache"
    Security    = "sensitive"
  }
}
