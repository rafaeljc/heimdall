################################################################################
# Aurora PostgreSQL Module - Outputs
################################################################################

output "cluster_identifier" {
  description = "The cluster identifier (name). Used for RDS-specific operations and resource references."
  value       = module.aurora.cluster_id
}

output "cluster_arn" {
  description = "The Amazon Resource Name (ARN) of the DB cluster for IAM policies and resource-based permissions."
  value       = module.aurora.cluster_arn
}

output "cluster_resource_id" {
  description = "The cluster resource ID, used for resource-based policies and cross-service operations."
  value       = module.aurora.cluster_resource_id
}

output "cluster_endpoint" {
  description = "Writer endpoint for the cluster. Use for application writes and DDL operations."
  value       = module.aurora.cluster_endpoint
}

output "cluster_reader_endpoint" {
  description = "A read-only endpoint for the cluster, automatically load-balanced across replicas. Use for analytical queries."
  value       = module.aurora.cluster_reader_endpoint
}

output "cluster_port" {
  description = "The database port (default: 5432). Use when establishing connections from applications."
  value       = module.aurora.cluster_port
}

output "cluster_master_user_secret_arn" {
  description = "The ARN of the AWS Secrets Manager secret containing the master credentials (username/password)."
  value       = module.aurora.cluster_master_user_secret[0].secret_arn
}

output "security_group_id" {
  description = "The ID of the security group attached to the Aurora cluster. Reference for ingress rules in dependent resources."
  value       = aws_security_group.aurora.id
}

output "database_name" {
  description = "The name of the default database created in the cluster."
  value       = module.aurora.cluster_database_name
}

output "master_username" {
  description = "The master username for the database."
  value       = module.aurora.cluster_master_username
}
