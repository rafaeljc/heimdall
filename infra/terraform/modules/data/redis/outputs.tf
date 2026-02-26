################################################################################
# ElastiCache Redis Module - Outputs
#
# Outputs provide operational information for consuming modules and applications:
# - Endpoint addresses for read/write connections
# - Authentication secret references for retrieving AUTH tokens
# - Security group ID for ingress rule dependencies
#
# Usage:
# - In Terraform: Reference via module.redis.output_name in other modules
# - In applications: Use endpoints with AUTH token from Secrets Manager
# - In EKS: Inject as environment variables or mounted secrets
################################################################################

output "replication_group_id" {
  description = "The ID of the replication group (cluster identifier). Use for AWS CLI operations, monitoring, and resource identification across layers."
  value       = aws_elasticache_replication_group.redis.id
}

output "primary_endpoint_address" {
  description = "The DNS address of the primary node endpoint for write operations. Format: {cluster-id}.xxxxx.ng.0001.{region}.cache.amazonaws.com"
  value       = aws_elasticache_replication_group.redis.primary_endpoint_address
}

output "primary_endpoint_port" {
  description = "The port number of the primary endpoint (default: 6379). Required for connection strings in applications."
  value       = aws_elasticache_replication_group.redis.port
}

output "reader_endpoint_address" {
  description = "The DNS address of the reader endpoint for read-only operations, load-balanced across replicas. Use for analytics, monitoring, non-critical reads."
  value       = aws_elasticache_replication_group.redis.reader_endpoint_address
}

output "configuration_endpoint_address" {
  description = "The configuration endpoint for cluster mode enabled replication groups. Null if cluster mode is disabled. Used by cluster-aware clients (redis-py cluster mode)."
  value       = aws_elasticache_replication_group.redis.configuration_endpoint_address
}

output "port" {
  description = "The port number (default: 6379). ElastiCache Redis always uses 6379; this output provides it for clarity in connection string construction."
  value       = aws_elasticache_replication_group.redis.port
}

output "auth_token_secret_arn" {
  description = "The ARN of the AWS Secrets Manager secret containing the Redis AUTH token. Use with AWS SDK for programmatic secret retrieval and rotation."
  value       = aws_secretsmanager_secret.redis_auth.arn
}

output "auth_token_secret_name" {
  description = "The name of the AWS Secrets Manager secret ({environment}/redis/auth-token). Use with AWS CLI: aws secretsmanager get-secret-value --secret-id <name>"
  value       = aws_secretsmanager_secret.redis_auth.name
}

output "security_group_id" {
  description = "The ID of the Redis security group. Reference this in dependent resources (e.g., EKS node security groups) to create ingress rules for Redis access."
  value       = aws_security_group.redis.id
}

output "cluster_enabled" {
  description = "Boolean indicating if cluster mode is enabled. False by default (replication mode). Use to determine client-side connection patterns (cluster-aware vs. single-endpoint)."
  value       = aws_elasticache_replication_group.redis.cluster_enabled
}

output "replication_group_arn" {
  description = "The ARN of the Redis replication group."
  value       = aws_elasticache_replication_group.redis.arn
}

output "cluster_address" {
  description = "The primary endpoint address of the Redis cluster."
  value       = aws_elasticache_replication_group.redis.primary_endpoint_address
}

output "subnet_group_name" {
  description = "The name of the subnet group used by Redis."
  value       = aws_elasticache_subnet_group.redis.name
}
