################################################################################
# Layer 2: Platform - Input Variables
#
# These variables define the platform infrastructure configuration.
# All variables have sensible defaults or validation rules to prevent misuse.
################################################################################

variable "aws_region" {
  description = "AWS region where platform infrastructure will be deployed."
  type        = string
  default     = "us-east-1"

  # Examples: us-east-1, us-west-2, eu-west-1, ap-southeast-1
}

variable "environment" {
  description = "Deployment environment. Used for tagging, conditional logic, and resource naming."
  type        = string

  # Allowed values: dev, staging, prod
  # - dev: Cost-optimized (single AZ, burstable instances)
  # - staging: Production-like (multi-AZ, medium instances)
  # - prod: High availability (multi-AZ, memory-optimized instances)
}

variable "vpc_cidr" {
  description = "CIDR block for the VPC. Used for network segmentation and security group rules."
  type        = string
  default     = "10.0.0.0/16"
}

variable "cluster_endpoint_public_access_cidrs" {
  description = "List of CIDR blocks allowed to access the EKS cluster endpoint."
  type        = list(string)
  default     = ["0.0.0.0/0"]
}
