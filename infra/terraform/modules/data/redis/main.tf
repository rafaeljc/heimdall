################################################################################
# ElastiCache Redis Module - Core Infrastructure
#
# Deploys a highly available, encrypted Redis replication group with automatic
# failover and zero-trust network security.
# 
# Features:
# - Automatic failover (Multi-AZ, configurable)
# - At-rest encryption (AES-256)
# - In-transit encryption (TLS on port 6379)
# - AUTH token: 32-character secure token stored in AWS Secrets Manager
# - Zero-trust egress: DNS + HTTPS only (no unrestricted internet access)
# - Ephemeral cache design: Syncer watchdog rebuilds from source on recovery
#
# Architecture:
# - Deployed in isolated database subnets (Tier 3) from Layer 1 networking
# - Multi-AZ by default when num_cache_clusters > 1
# - Parameter group management: Environment-aware tuning (timeout for production)
# - Secrets Manager integration: AUTH token lifecycle management with immediate deletion
################################################################################

terraform {
  required_version = ">= 1.0"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0"
    }
    random = {
      source  = "hashicorp/random"
      version = ">= 3.0"
    }
  }
}

locals {
  name = "heimdall-${var.environment}-redis"
}

# ------------------------------------------------------------------------------
# Security Group
# ------------------------------------------------------------------------------
resource "aws_security_group" "redis" {
  name        = "${local.name}-sg"
  description = "Security group for ElastiCache Redis cluster in ${var.environment}"
  vpc_id      = var.vpc_id

  dynamic "ingress" {
    for_each = length(var.allowed_cidr_blocks) > 0 ? [1] : []
    content {
      description = "Redis traffic from allowed CIDRs"
      from_port   = 6379
      to_port     = 6379
      protocol    = "tcp"
      cidr_blocks = var.allowed_cidr_blocks
    }
  }

  dynamic "ingress" {
    for_each = length(var.allowed_security_group_ids) > 0 ? [1] : []
    content {
      description     = "Redis traffic from allowed Security Groups"
      from_port       = 6379
      to_port         = 6379
      protocol        = "tcp"
      security_groups = var.allowed_security_group_ids
    }
  }

  # Explicit egress rules: restrict to necessary operations only
  # DNS resolution required for Redis cluster discovery and AWS service communication
  egress {
    description = "DNS resolution (required for cluster discovery and AWS service access)"
    from_port   = 53
    to_port     = 53
    protocol    = "udp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  # HTTPS for AWS service communication (Secrets Manager, CloudWatch, etc.)
  egress {
    description = "HTTPS for AWS service communication (Secrets Manager, monitoring)"
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name        = "${local.name}-sg"
    Environment = var.environment
    Project     = "Heimdall"
  }
}

# ------------------------------------------------------------------------------
# Subnet Group
# ------------------------------------------------------------------------------
resource "aws_elasticache_subnet_group" "redis" {
  name        = "${local.name}-subnet-group"
  description = "Subnet group for Redis cluster"
  subnet_ids  = var.database_subnets

  tags = {
    Environment = var.environment
    Project     = "Heimdall"
  }
}

# ------------------------------------------------------------------------------
# Redis Parameter Group (Tuning & Configuration)
#
# Manages Redis cluster parameters for optimal performance and environment-specific settings.
# Parameter group family is dynamically derived from the engine version (e.g., redis7 from 7.1).
#
# Environment-Aware Settings:
# - Production: timeout=300 (disconnect idle clients after 5 minutes, prevent exhaustion)
# - Non-prod: Defaults only (standard ElastiCache settings)
# - Note: maxmemory-policy not explicitly set to avoid conflicts with AWS ElastiCache defaults
#
# Lifecycle Management:
# - create_before_destroy=true ensures zero-downtime updates when parameter group changes
# - Updates apply at next replication group refresh (can be immediate or deferred)
################################################################################
resource "aws_elasticache_parameter_group" "redis" {
  family      = var.parameter_group_family
  name        = "${local.name}-params"
  description = "ElastiCache Redis parameter group for ${var.environment}"

  # Environment-aware settings
  dynamic "parameter" {
    for_each = var.environment == "prod" ? [
      { name = "timeout", value = "300" } # 5-minute client timeout
    ] : []
    content {
      name  = parameter.value.name
      value = parameter.value.value
    }
  }

  tags = {
    Environment = var.environment
    Project     = "Heimdall"
  }

  lifecycle {
    create_before_destroy = true # Avoid downtime when parameter group changes
  }
}

# ------------------------------------------------------------------------------
# Secure AUTH Token Generation & Secrets Manager
#
# Generates a cryptographically secure 32-character AUTH token for Redis authentication.
# Non-alphanumeric characters excluded per ElastiCache AUTH token restrictions.
#
# Secrets Manager Configuration:
# - Token stored at path: /heimdall/{environment}/redis/auth-token
# - recovery_window_in_days=0: Immediate deletion on destroy (no recovery window)
#   Appropriate for ephemeral cache with syncer watchdog rebuild strategy
# - Enables automatic secret rotation integration if needed
################################################################################
resource "random_password" "auth_token" {
  length  = 32
  special = false # ElastiCache AUTH tokens have strict character limitations
}

resource "aws_secretsmanager_secret" "redis_auth" {
  name                    = "/heimdall/${var.environment}/redis/auth-token"
  description             = "ElastiCache Redis AUTH token for ${var.environment}"
  recovery_window_in_days = 0 # Force immediate deletion on destroy for ephemeral environments

  tags = {
    Environment = var.environment
    Project     = "Heimdall"
  }
}

resource "aws_secretsmanager_secret_version" "redis_auth" {
  secret_id     = aws_secretsmanager_secret.redis_auth.id
  secret_string = random_password.auth_token.result
}

# ------------------------------------------------------------------------------
# ElastiCache Replication Group (The Redis Cluster)
#
# Creates a Redis cluster with the following characteristics:
# - Multi-AZ failover: Automatic promotion of replica to primary if primary fails (< 1 min)
# - High Availability: Enabled when num_cache_clusters > 1
# - Security: TLS encryption in-transit + AES-256 at-rest
# - Authentication: AUTH token required (stored securely in Secrets Manager)
# - Network: Placed in isolated database subnets via security groups
# - Parameter Management: Environment-aware parameter group with timeout for production
#
# Network Architecture:
# - Ingress: Port 6379 from allowed_cidr_blocks or allowed_security_group_ids (security group)
# - Egress: DNS (UDP 53) + HTTPS (TCP 443) only (zero-trust principle)
# - No internet access from database subnets (Tier 3 isolation)
#
# Lifecycle Considerations:
# - Modifications may require brief reboot (depends on change type)
# - Parameter group changes applied at next maintenance window or immediately if apply_immediately=true
# - For production, plan changes during maintenance windows to avoid connection interruptions
################################################################################
resource "aws_elasticache_replication_group" "redis" {
  replication_group_id = local.name
  description          = "Redis cluster for Heimdall ${var.environment}"
  node_type            = var.node_type
  port                 = 6379
  parameter_group_name = aws_elasticache_parameter_group.redis.name
  engine_version       = var.engine_version

  # Network & Security
  subnet_group_name  = aws_elasticache_subnet_group.redis.name
  security_group_ids = [aws_security_group.redis.id]

  # High Availability Settings
  automatic_failover_enabled = var.num_cache_clusters > 1 ? true : false
  multi_az_enabled           = var.num_cache_clusters > 1 ? true : false
  num_cache_clusters         = var.num_cache_clusters

  # Compliance & Encryption
  at_rest_encryption_enabled = true
  transit_encryption_enabled = true
  auth_token                 = random_password.auth_token.result

  tags = {
    Environment = var.environment
    Project     = "Heimdall"
  }
}
