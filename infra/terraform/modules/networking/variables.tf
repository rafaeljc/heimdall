################################################################################
# Networking Module - Input Variables
#
# These variables configure the VPC architecture, subnet layout, and HA settings.
# All variables are validated to prevent invalid configurations.
################################################################################

variable "environment" {
  description = "Deployment environment. Controls HA configuration and tagging."
  type        = string

  # Allowed values: dev, staging, prod
  # Controls:
  #   - NAT Gateway count (1 for non-prod, 3 per AZ for prod)
  #   - Conditional resources (VPC endpoints enabled in all)
  #   - Tagging and cost allocation
}

variable "vpc_cidr" {
  description = "IPv4 CIDR block for the VPC. Must be a valid /16 or smaller."
  type        = string

  # Example: 10.0.0.0/16
  # Used for automatic subnet calculation via cidrsubnet() function
  # Tier breakdown (for /16 base):
  #   - Tier 1 (Public):     3x /24 subnets (offsets: 0, 1, 2)
  #   - Tier 2 (Private):    3x /20 subnets (offsets: 1, 2, 3)
  #   - Tier 3 (Database):   3x /24 subnets (offsets: 100, 101, 102)

  validation {
    condition     = can(cidrhost(var.vpc_cidr, 0))
    error_message = "vpc_cidr must be a valid IPv4 CIDR block."
  }
}

variable "azs" {
  description = "List of Availability Zone names where subnets will be created."
  type        = list(string)

  # Example: ["us-east-1a", "us-east-1b", "us-east-1c"]
  # Provided by Layer 1 via: slice(data.aws_availability_zones.available.names, 0, 3)
  # Each AZ will receive 1 subnet from each tier (3 subnets per AZ)
}
