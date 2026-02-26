################################################################################
# ElastiCache Redis Module - Input Variables
################################################################################

variable "environment" {
  description = "The deployment environment (e.g., dev, staging, prod)."
  type        = string
}

variable "vpc_id" {
  description = "The ID of the VPC where the Redis cluster will be deployed."
  type        = string
}

variable "database_subnets" {
  description = "List of database subnet IDs to place the Redis nodes in (Tier 3 isolated subnets)."
  type        = list(string)
}

variable "allowed_cidr_blocks" {
  description = "List of CIDR blocks allowed to access Redis. Useful for fixed private subnets; prefer allowed_security_group_ids for dynamic workloads."
  type        = list(string)
  default     = []
}

variable "allowed_security_group_ids" {
  description = "List of Security Group IDs allowed to access Redis (e.g., EKS worker nodes SG). Preferred over CIDRs for dynamic/cross-VPC access."
  type        = list(string)
  default     = []
}

variable "node_type" {
  description = "The instance type to use for the Redis nodes."
  type        = string
  default     = "cache.t4g.small" # Minimum required size for Redis 7.1+
}

variable "num_cache_clusters" {
  description = "Number of cache nodes distributed across 3 availability zones. Default: 3 (1 primary + 2 read replicas)."
  type        = number
  default     = 3

  validation {
    condition     = var.num_cache_clusters >= 3
    error_message = "num_cache_clusters must be at least 3 for high availability and 3 AZ reliability (prod/staging)."
  }
}

variable "engine_version" {
  description = "The Redis engine version (e.g., 7.1, 7.2)."
  type        = string
  default     = "7.1"
}

variable "parameter_group_family" {
  description = "The ElastiCache parameter group family (e.g., redis7 for Redis 7.x)."
  type        = string
  default     = "redis7"
}
