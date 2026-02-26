################################################################################
# Layer 2: Platform - Outputs
#
# Exports critical infrastructure identifiers for consumption by Layer 3
# and external systems. These values are also published to SSM Parameter Store.
################################################################################

# EKS Cluster Outputs
output "eks_cluster_name" {
  description = "Name of the EKS cluster"
  value       = module.eks.cluster_name
}

output "eks_cluster_endpoint" {
  description = "Endpoint for your EKS Kubernetes API"
  value       = module.eks.cluster_endpoint
}

output "eks_cluster_arn" {
  description = "ARN of the EKS cluster"
  value       = module.eks.cluster_arn
}

output "eks_cluster_version" {
  description = "The Kubernetes server version for the cluster"
  value       = module.eks.cluster_version
}

output "eks_node_security_group_id" {
  description = "Security group ID attached to the EKS worker nodes"
  value       = module.eks.node_security_group_id
}

output "eks_node_iam_role_arn" {
  description = "IAM role ARN for EKS worker nodes"
  value       = module.eks.node_iam_role_arn
}

output "eks_node_iam_role_name" {
  description = "IAM role name for EKS worker nodes"
  value       = module.eks.node_iam_role_name
}

# PostgreSQL Outputs
output "postgres_cluster_endpoint" {
  description = "Aurora PostgreSQL cluster endpoint (primary)"
  value       = module.postgres.cluster_endpoint
}

output "postgres_cluster_reader_endpoint" {
  description = "Aurora PostgreSQL cluster reader endpoint (read replicas)"
  value       = module.postgres.cluster_reader_endpoint
}

output "postgres_cluster_arn" {
  description = "Amazon Resource Name (ARN) of the DB cluster"
  value       = module.postgres.cluster_arn
}

output "postgres_cluster_port" {
  description = "The database port"
  value       = module.postgres.cluster_port
}

output "postgres_database_name" {
  description = "The database name"
  value       = module.postgres.database_name
}

output "postgres_master_username" {
  description = "The master username for the database"
  value       = module.postgres.master_username
  sensitive   = true
}

output "postgres_cluster_master_user_secret_arn" {
  description = "AWS Secrets Manager ARN for the master user credentials"
  value       = module.postgres.cluster_master_user_secret_arn
  sensitive   = true
}

output "postgres_security_group_id" {
  description = "Security group ID of the PostgreSQL cluster"
  value       = module.postgres.security_group_id
}

# Redis/ElastiCache Outputs
output "redis_primary_endpoint_address" {
  description = "ElastiCache Redis primary endpoint address"
  value       = module.redis.primary_endpoint_address
}

output "redis_primary_endpoint_port" {
  description = "ElastiCache Redis primary endpoint port"
  value       = module.redis.primary_endpoint_port
}

output "redis_replication_group_id" {
  description = "The ID of the replication group"
  value       = module.redis.replication_group_id
}

output "redis_replication_group_arn" {
  description = "ARN of the replication group"
  value       = module.redis.replication_group_arn
}

output "redis_cluster_address" {
  description = "Address of the cluster configuration endpoint (if enabled)"
  value       = module.redis.cluster_address
}

output "redis_auth_token_secret_arn" {
  description = "AWS Secrets Manager ARN for the Redis AUTH token"
  value       = module.redis.auth_token_secret_arn
  sensitive   = true
}

output "redis_security_group_id" {
  description = "Security group ID of the Redis cluster"
  value       = module.redis.security_group_id
}

output "redis_subnet_group_name" {
  description = "Name of the subnet group used by the replication group"
  value       = module.redis.subnet_group_name
}

# SSM Parameter Outputs (for reference)
output "ssm_parameters" {
  description = "Map of published SSM Parameter Store paths for Layer 3 consumption"
  value = {
    eks_cluster_name      = aws_ssm_parameter.eks_cluster_name.name
    eks_cluster_endpoint  = aws_ssm_parameter.eks_cluster_endpoint.name
    eks_node_iam_role_arn = aws_ssm_parameter.eks_node_iam_role_arn.name
    postgres_endpoint     = aws_ssm_parameter.postgres_endpoint.name
    postgres_secret_arn   = aws_ssm_parameter.postgres_secret_arn.name
    redis_endpoint        = aws_ssm_parameter.redis_endpoint.name
    redis_secret_arn      = aws_ssm_parameter.redis_secret_arn.name
  }
}
