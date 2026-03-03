################################################################################
# Aurora PostgreSQL Module - Input Variables
################################################################################

variable "environment" {
  description = "The deployment environment (e.g., dev, staging, prod)."
  type        = string
}

variable "vpc_id" {
  description = "The ID of the VPC where the cluster will be deployed."
  type        = string
}

variable "db_subnet_group_name" {
  description = "The name of the DB subnet group (from Layer 1) to place the instances in."
  type        = string
}

variable "allowed_cidr_blocks" {
  description = "List of CIDR blocks allowed to access the database (e.g., VPC private subnets)."
  type        = list(string)
  default     = []
}

variable "allowed_security_group_ids" {
  description = "List of Security Group IDs allowed to access the database (e.g., EKS worker nodes SG)."
  type        = list(string)
  default     = []
}

variable "instance_class" {
  description = "The instance type to use for the database nodes."
  type        = string
  default     = "db.t4g.medium" # ARM-based (Graviton2) for better price/performance
}

variable "instances" {
  description = "Map of cluster instances for multi-AZ reliability. Default: 3 instances (1 primary + 2 read replicas across 3 AZs)."
  type        = map(any)
  default = {
    1 = {} # Primary instance (AZ-1)
    2 = {} # Read Replica 1 (AZ-2)
    3 = {} # Read Replica 2 (AZ-3) - for 3 AZ reliability
  }
}

variable "engine_version" {
  description = "PostgreSQL engine version (e.g., 15.15, 16.11). Check AWS RDS documentation for available versions."
  type        = string
  default     = "15.15"
}

variable "backup_retention_period" {
  description = "Number of days to retain backups (prod: 30, staging: 7, dev: 1). Required for disaster recovery."
  type        = number
  default     = null # Will be set based on environment
}

variable "preferred_backup_window" {
  description = "UTC time window for daily backups (e.g., '03:00-04:00'). Avoid peak application usage."
  type        = string
  default     = "03:00-04:00"
}

variable "preferred_maintenance_window" {
  description = "UTC time window for maintenance (e.g., 'mon:04:00-mon:05:00'). Ensure minimal impact to users."
  type        = string
  default     = "mon:04:00-mon:05:00"
}

variable "enable_deletion_protection" {
  description = "Protect cluster from accidental deletion (strongly recommended for prod)."
  type        = bool
  default     = true
}
