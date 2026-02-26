################################################################################
# EKS Cluster Module - Input Variables
################################################################################

variable "environment" {
  description = "The deployment environment (e.g., dev, staging, prod). Controls sizing and access."
  type        = string
}

variable "vpc_id" {
  description = "The ID of the VPC where the EKS cluster will be deployed."
  type        = string
}

variable "private_subnets" {
  description = "**3 AZ Enforced**: List of private subnet IDs for EKS worker nodes. Minimum 3 subnets in different availability zones required for ALL environments (dev, staging, prod). Subnets from Layer 1 networking (Tier 1). Ensures multi-AZ redundancy and automatic pod distribution across zones."
  type        = list(string)

  validation {
    condition     = length(var.private_subnets) >= 3
    error_message = "At least 3 private subnets in different AZs required for multi-AZ redundancy (all environments). Subnets must be from Layer 1 networking Tier 1."
  }
}

variable "cluster_version" {
  description = "Kubernetes minor version to use for the EKS cluster (e.g., 1.29, 1.30). Must be 1.28+ for modern EKS features (Access Entries, Pod Identity Agent)."
  type        = string
  default     = "1.30"

  validation {
    condition     = tonumber(split(".", var.cluster_version)[1]) >= 28
    error_message = "Kubernetes version must be 1.28 or later for modern EKS features (Access Entries API, Pod Identity Agent)."
  }
}

variable "cluster_endpoint_public_access_cidrs" {
  description = "List of CIDR blocks allowed to access Kubernetes API (public endpoint). Default behavior: null = use smart environment defaults (dev/staging: allow all, prod: private-only unless explicit CIDRs). For production, restrict to corporate VPN/NAT IPs to limit API exposure."
  type        = list(string)
  default     = null
}

variable "node_instance_types" {
  description = "List of instance types for EKS worker nodes. Examples: c6a.large (AMD, general-purpose), t3.large (burstable), m6a.large (Intel, general). Default: c6a.large (AMD EPYC: 10% cheaper than Intel, same performance)."
  type        = list(string)
  default     = ["c6a.large"]
}

variable "node_min_size" {
  description = "**HA Enforced**: Minimum number of worker nodes. Minimum 3 required for multi-AZ redundancy (1 node per AZ). Applied to ALL environments (dev, staging, prod) to ensure HA always enabled."
  type        = number
  default     = 3

  validation {
    condition     = var.node_min_size >= 3
    error_message = "node_min_size must be at least 3 for HA (1 node per AZ for 3 AZ distribution)."
  }
}

variable "node_max_size" {
  description = "Maximum number of worker nodes. Used by Cluster Autoscaler or Karpenter to handle traffic spikes."
  type        = number
  default     = 5
}

variable "node_desired_size" {
  description = "Desired number of worker nodes on initial deployment. Default: 3 (1 per AZ for balanced distribution). Adjusted by cluster autoscaler at runtime based on pod demand."
  type        = number
  default     = 3
}
