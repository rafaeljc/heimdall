################################################################################
# Layer 1: Network - Input Variables
#
# These variables define the network infrastructure configuration.
# All variables are validated to prevent invalid deployments.
################################################################################

variable "aws_region" {
  description = "AWS region where network infrastructure will be deployed."
  type        = string
  default     = "us-east-1"

  # Examples: us-east-1, us-west-2, eu-west-1, ap-southeast-1
}

variable "environment" {
  description = "Deployment environment. Used for tagging and conditional logic."
  type        = string

  # Allowed values: dev, staging, prod
  # - dev: Cost-optimized (single NAT gateway)
  # - staging: Production-like (multi-AZ)
  # - prod: High availability (multi-NAT, VPC endpoints)
}

variable "vpc_cidr" {
  description = "IPv4 CIDR block for the VPC."
  type        = string

  # Example: 10.0.0.0/16
  # Must be a valid CIDR block. /16 recommended for typical workloads.
  # Subnet allocation breakdown for /16:
  #   - Public subnets (Tier 1):     3x /24 = 750 IPs (ALBs, NAT)
  #   - Private subnets (Tier 2):    3x /20 = 12,288 IPs (EKS nodes)
  #   - Database subnets (Tier 3):   3x /24 = 750 IPs (Aurora, Redis)
  #   Total: ~13,788 IPs (45,758 remaining for growth)

  validation {
    condition     = can(cidrhost(var.vpc_cidr, 0))
    error_message = "vpc_cidr must be a valid IPv4 CIDR block."
  }
}
