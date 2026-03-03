################################################################################
# Layer 1: Network - Outputs
#
# Exports critical network infrastructure identifiers for consumption by Layer 2
# and external systems. These values are also published to SSM Parameter Store.
################################################################################

# VPC Identifier
output "vpc_id" {
  description = "VPC ID. Required for all resource placement in the VPC."
  value       = module.network.vpc_id
}

# Private Application Subnets (Tier 2)
output "private_subnets" {
  description = "Private App subnet IDs (Tier 2). Used for EKS worker nodes and application resources."
  value       = module.network.private_subnets
}

# Public Subnets (Tier 1)
output "public_subnets" {
  description = "Public subnet IDs (Tier 1). Used for load balancers and internet-facing resources."
  value       = module.network.public_subnets
}

# Database Subnets (Tier 3)
output "database_subnets" {
  description = "Database subnet IDs (Tier 3, isolated). Used for Aurora, ElastiCache, and other data services."
  value       = module.network.database_subnets
}

# Database Subnet Group Name
output "database_subnet_group_name" {
  description = "RDS database subnet group name. Required for Aurora cluster provisioning."
  value       = module.network.database_subnet_group_name
}

# Default Security Group (Hardened)
output "default_security_group_id" {
  description = "Hardened default security group ID (all ingress/egress denied). Use for reference-only."
  value       = module.network.default_security_group_id
}

# S3 Gateway VPC Endpoint
output "s3_endpoint_id" {
  description = "S3 Gateway VPC endpoint ID. Reduces data transfer costs by ~50%."
  value       = module.network.s3_endpoint_id
}

# DynamoDB Gateway VPC Endpoint
output "dynamodb_endpoint_id" {
  description = "DynamoDB Gateway VPC endpoint ID. Enables state locking and cost optimization."
  value       = module.network.dynamodb_endpoint_id
}

# SSM Parameter Outputs (for reference)
output "ssm_parameters" {
  description = "Map of published SSM Parameter Store paths for Layer 2 consumption"
  value = {
    vpc_id                     = aws_ssm_parameter.vpc_id.name
    private_subnets            = aws_ssm_parameter.private_subnets.name
    public_subnets             = aws_ssm_parameter.public_subnets.name
    database_subnets           = aws_ssm_parameter.database_subnets.name
    database_subnet_group_name = aws_ssm_parameter.database_subnet_group_name.name
    default_security_group_id  = aws_ssm_parameter.default_security_group_id.name
    s3_endpoint_id             = aws_ssm_parameter.s3_endpoint_id.name
    dynamodb_endpoint_id       = aws_ssm_parameter.dynamodb_endpoint_id.name
  }
}
