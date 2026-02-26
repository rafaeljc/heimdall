################################################################################
# EKS Cluster Module - Outputs
#
# Exposes critical cluster identifiers, endpoints, and security group IDs required by
# downstream infrastructure for database access, workload IAM, and cluster integration.
#
# Key Outputs:
# - cluster_name, cluster_endpoint: Connection details for kubectl and CI/CD
# - node_security_group_id: Reference in RDS/Redis for pod access
# - node_iam_role_arn: Attach policies for workload access to AWS services
# - node_group_name: For cluster autoscaler / Karpenter configuration
################################################################################

output "cluster_name" {
  description = "The name of the EKS cluster."
  value       = module.eks.cluster_name
}

output "cluster_endpoint" {
  description = "Endpoint for your Kubernetes API server. Used by kubectl and CI/CD pipelines."
  value       = module.eks.cluster_endpoint
}

output "cluster_certificate_authority_data" {
  description = "Base64 encoded certificate data required to communicate securely with the cluster."
  value       = module.eks.cluster_certificate_authority_data
}

output "cluster_security_group_id" {
  description = "Security group ID attached to the EKS cluster control plane."
  value       = module.eks.cluster_security_group_id
}

output "node_security_group_id" {
  description = "Security group ID attached to the EKS worker nodes. Use this in RDS/Redis to allow database access from pods."
  value       = module.eks.node_security_group_id
}

output "oidc_provider_arn" {
  description = "The ARN of the OIDC Provider. Required for older IRSA setups (kept for backward compatibility with older Helm charts)."
  value       = module.eks.oidc_provider_arn
}

output "node_iam_role_arn" {
  description = "ARN of the EKS worker node IAM role. Use for attaching policies to enable workload access to RDS, S3, Redis, and other AWS services."
  value       = module.eks.eks_managed_node_groups["app_pool"].iam_role_arn
}

output "node_group_name" {
  description = "Name of the primary managed node group (app_pool). Use for cluster autoscaler and Karpenter configuration."
  value       = module.eks.eks_managed_node_groups["app_pool"].node_group_id
}

output "cluster_arn" {
  description = "The Amazon Resource Name (ARN) of the EKS cluster."
  value       = module.eks.cluster_arn
}

output "cluster_version" {
  description = "The Kubernetes version of the EKS cluster."
  value       = module.eks.cluster_version
}

output "node_iam_role_name" {
  description = "Name of the EKS worker node IAM role."
  value       = module.eks.eks_managed_node_groups["app_pool"].iam_role_name
}
