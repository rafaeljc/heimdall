################################################################################
# Networking Module - Outputs
#
# These outputs expose infrastructure details for consumption by Layer 2.
# All outputs are required by downstream layers (EKS, RDS, etc.).
################################################################################

output "vpc_id" {
  description = "VPC ID. Required for all resource placement in the VPC."
  value       = module.vpc.vpc_id
}

output "private_subnets" {
  description = "Private App subnet IDs (Tier 2, /20 size). Used for EKS worker nodes and EC2 instances."
  value       = module.vpc.private_subnets
}

output "public_subnets" {
  description = "Public subnet IDs (Tier 1, /24 size). Used for ALBs, NAT gateways, and internet-facing resources."
  value       = module.vpc.public_subnets
}

output "database_subnets" {
  description = "Database subnet IDs (Tier 3, /24 size). Used for Aurora, ElastiCache. Isolated from internet."
  value       = module.vpc.database_subnets
}

output "database_subnet_group_name" {
  description = "RDS database subnet group name. Required for Aurora cluster provisioning."
  value       = module.vpc.database_subnet_group_name
}

output "default_security_group_id" {
  description = "Hardened default security group ID (all ingress/egress denied). Use for reference-only; create custom SGs instead."
  value       = module.vpc.default_security_group_id
}

output "s3_endpoint_id" {
  description = "S3 Gateway VPC endpoint ID. Reduces NAT gateway data transfer costs by ~50%."
  value       = aws_vpc_endpoint.s3.id
}

output "dynamodb_endpoint_id" {
  description = "DynamoDB Gateway VPC endpoint ID. Enables state locking and reduces data transfer costs."
  value       = aws_vpc_endpoint.dynamodb.id
}
