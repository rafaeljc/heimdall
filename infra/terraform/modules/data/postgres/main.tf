################################################################################
# Aurora PostgreSQL Module - Core Infrastructure
#
# Deploys a highly available Amazon Aurora PostgreSQL cluster across 3 AZs.
# 
# HA Configuration:
# - 3 instances: 1 primary + 2 read replicas distributed across 3 availability zones
# - Automatic failover: If primary fails, one read replica promotes in < 30 seconds
# - Zero-downtime patching: Read replicas handle traffic during maintenance
# - Database subnets (Tier 3): Isolated across AZs via Layer 1 networking
#
# Features:
# - TLS/SSL enforced for all connections (rds.force_ssl=1)
# - Storage encryption at-rest (AES-256)
# - Automated backups with retention policies (prod: 30d, staging: 7d, dev: 1d)
# - CloudWatch Logs exports for PostgreSQL audit trail
# - Secrets Manager integration for master password
# - IAM database authentication for temporary credentials
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
  name   = "heimdall-${var.environment}-aurora"
  engine = "aurora-postgresql"

  # Dynamic engine version from variable (defaults to 15.4 if not specified)
  engine_version = var.engine_version
  family         = "aurora-postgresql${substr(var.engine_version, 0, 2)}"

  # Environment-aware backup retention
  backup_retention_period = var.backup_retention_period != null ? var.backup_retention_period : (
    var.environment == "prod" ? 30 : (var.environment == "staging" ? 7 : 1)
  )
}

# ------------------------------------------------------------------------------
# Security Group for Aurora
# ------------------------------------------------------------------------------
resource "aws_security_group" "aurora" {
  name        = "${local.name}-sg"
  description = "Security group for Aurora PostgreSQL cluster in ${var.environment}"
  vpc_id      = var.vpc_id

  # Allow ingress from specified CIDRs (e.g., VPC internal traffic)
  dynamic "ingress" {
    for_each = length(var.allowed_cidr_blocks) > 0 ? [1] : []
    content {
      description = "PostgreSQL traffic from allowed CIDRs"
      from_port   = 5432
      to_port     = 5432
      protocol    = "tcp"
      cidr_blocks = var.allowed_cidr_blocks
    }
  }

  # Allow ingress from specific Security Groups (e.g., EKS Nodes SG)
  dynamic "ingress" {
    for_each = length(var.allowed_security_group_ids) > 0 ? [1] : []
    content {
      description     = "PostgreSQL traffic from allowed Security Groups"
      from_port       = 5432
      to_port         = 5432
      protocol        = "tcp"
      security_groups = var.allowed_security_group_ids
    }
  }

  # Explicit egress rules: restrict to necessary operations only
  # DNS resolution required for RDS endpoint discovery and AWS service communication
  egress {
    description = "DNS resolution (required for RDS endpoint management and backups)"
    from_port   = 53
    to_port     = 53
    protocol    = "udp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  # HTTPS for AWS service communication (snapshots, monitoring, encryption keys)
  egress {
    description = "HTTPS for AWS service communication (KMS, SNS, CloudWatch)"
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
# Aurora Cluster Parameter Group (Enforcing Security)
# 
# Configures cluster-level security parameters. Currently enforces TLS/SSL for all
# connections. Additional parameters can be added here for performance tuning
# (e.g., shared_buffers, max_connections) but should be tested thoroughly.
################################################################################
resource "aws_rds_cluster_parameter_group" "aurora_cluster" {
  name        = "${local.name}-cluster-pg"
  family      = local.family
  description = "Aurora Cluster Parameter Group enforcing SSL/TLS"

  parameter {
    name         = "rds.force_ssl"
    value        = "1"
    apply_method = "pending-reboot" # Requires cluster restart to take effect
  }

  tags = {
    Name        = "${local.name}-cluster-pg"
    Environment = var.environment
    Project     = "Heimdall"
  }
}

# ------------------------------------------------------------------------------
# Aurora Cluster via Official AWS Module
# ------------------------------------------------------------------------------
module "aurora" {
  source  = "terraform-aws-modules/rds-aurora/aws"
  version = "~> 9.0"

  name           = local.name
  engine         = local.engine
  engine_version = local.engine_version

  # Network Placement
  vpc_id               = var.vpc_id
  db_subnet_group_name = var.db_subnet_group_name

  # Apply our custom Security Group
  vpc_security_group_ids = [aws_security_group.aurora.id]
  create_security_group  = false

  # Instance Configuration
  instance_class = var.instance_class
  instances      = var.instances

  # Security & Compliance
  storage_encrypted               = true
  apply_immediately               = var.environment == "dev" ? true : false
  skip_final_snapshot             = var.environment == "prod" ? false : true
  deletion_protection             = var.enable_deletion_protection && var.environment == "prod" ? true : false
  enabled_cloudwatch_logs_exports = ["postgresql"] # PostgreSQL query logs for auditing/compliance

  # Backup Configuration (CRITICAL for disaster recovery and compliance)
  # Exports logs to /aws/rds/cluster/heimdall-{env}-aurora/postgresql for CloudWatch Logs Insights
  backup_retention_period      = local.backup_retention_period
  preferred_backup_window      = var.preferred_backup_window
  preferred_maintenance_window = var.preferred_maintenance_window

  # Parameter Groups
  create_db_cluster_parameter_group = false
  db_cluster_parameter_group_name   = aws_rds_cluster_parameter_group.aurora_cluster.id

  # Authentication
  iam_database_authentication_enabled = true
  master_username                     = "heimdalladmin"
  manage_master_user_password         = true # AWS Secrets Manager natively handles the password

  tags = {
    Environment     = var.environment
    Project         = "Heimdall"
    TerraformModule = "rds-aurora"
  }
}
