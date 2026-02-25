################################################################################
# Layer 1: Network - Outputs
#
# These outputs form the API contract between Layer 1 and Layer 2.
# Layer 2 consumes these via terraform_remote_state data source.
#
# Output format: layer1_network_<resource_type>_<descriptor>
#
# Example Layer 2 consumption:
#   data "terraform_remote_state" "layer1" {
#     backend = "s3"
#     config = {
#       bucket = "terraform-state"
#       key    = "layer1-network/terraform.tfstate"
#       region = "us-east-1"
#     }
#   }
#   
#   resource "aws_eks_cluster" "main" {
#     vpc_config {
#       subnet_ids = data.terraform_remote_state.layer1.outputs.private_subnets
#     }
#   }
################################################################################

output "vpc_id" {
  description = "The ID of the VPC. Used by Layer 2 for resource placement."
  value       = module.network.vpc_id
}

output "private_subnets" {
  description = "List of Private App subnet IDs (Tier 2). Used for EKS Worker Nodes."
  value       = module.network.private_subnets
}

output "public_subnets" {
  description = "List of Public subnet IDs (Tier 1). Used for Application Load Balancers."
  value       = module.network.public_subnets
}

output "database_subnets" {
  description = "List of Database subnet IDs (Tier 3). Used for Aurora and ElastiCache. Isolated from internet."
  value       = module.network.database_subnets
}

output "database_subnet_group_name" {
  description = "Name of the RDS database subnet group. Required for Aurora cluster provisioning."
  value       = module.network.database_subnet_group_name
}

output "default_security_group_id" {
  description = "ID of the default security group (hardened with all ingress/egress denied). Reference for Layer 2."
  value       = module.network.default_security_group_id
}

output "s3_endpoint_id" {
  description = "ID of the S3 Gateway VPC endpoint for cost-optimized S3 access."
  value       = module.network.s3_endpoint_id
}

output "dynamodb_endpoint_id" {
  description = "ID of the DynamoDB Gateway VPC endpoint for state locking and cost optimization."
  value       = module.network.dynamodb_endpoint_id
}
