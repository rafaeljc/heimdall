################################################################################
# Layer 2: Platform - Data Sources (Layer 1 API Contract)
#
# Retrieves network identifiers published by Layer 1 via SSM Parameter Store.
# This creates a secure, loosely-coupled dependency on Layer 1 outputs without
# requiring access to Layer 1's Terraform state file.
#
# Consumption Examples:
#
# 1. AWS CLI (single parameter):
#    aws ssm get-parameter --name /heimdall/prod/network/vpc_id \
#      --query 'Parameter.Value' --output text
#
# 2. AWS CLI (all parameters):
#    aws ssm get-parameters-by-path \
#      --path /heimdall/prod/network/ \
#      --recursive --query 'Parameters[].{Name:Name,Value:Value}'
#
# 3. Terraform (this layer):
#    data "aws_ssm_parameter" "vpc_id" {
#      name = "/heimdall/${var.environment}/network/vpc_id"
#    }
#
# Parameter Naming Convention (set by Layer 1):
#   /heimdall/{environment}/network/{resource_type}
#   - Project level: /heimdall/
#   - Environment: dev, staging, prod
#   - Component: network (Layer 1 identifier)
#   - Resource: vpc_id, private_subnets, database_subnets, etc.
#
# Validation: This layer validates that parameters contain expected values
# (proper format, sufficient count, valid CIDR ranges, etc.)
################################################################################

data "aws_ssm_parameter" "vpc_id" {
  name = "/heimdall/${var.environment}/network/vpc_id"

  lifecycle {
    postcondition {
      condition     = can(regex("^vpc-[a-z0-9]+$", self.value))
      error_message = "CRITICAL: Invalid VPC ID format retrieved from Layer 1."
    }
  }
}

data "aws_ssm_parameter" "private_subnets" {
  name = "/heimdall/${var.environment}/network/private_subnets"

  lifecycle {
    postcondition {
      # Validates that we have at least 2 subnets for EKS High Availability
      condition     = length(split(",", self.value)) >= 2
      error_message = "CRITICAL: Layer 1 did not provide at least 2 private subnets. EKS requires multi-AZ redundancy."
    }
    postcondition {
      # Validates the physical format of the subnet IDs
      condition     = alltrue([for s in split(",", self.value) : can(regex("^subnet-[a-z0-9]+$", s))])
      error_message = "CRITICAL: One or more private subnets retrieved from Layer 1 have an invalid format."
    }
  }
}

data "aws_ssm_parameter" "database_subnets" {
  name = "/heimdall/${var.environment}/network/database_subnets"

  lifecycle {
    postcondition {
      # Validates that we have at least 2 subnets for Aurora/Redis High Availability
      condition     = length(split(",", self.value)) >= 2
      error_message = "CRITICAL: Layer 1 did not provide at least 2 database subnets. RDS and ElastiCache require multi-AZ redundancy."
    }
    postcondition {
      # Validates the physical format of the subnet IDs
      condition     = alltrue([for s in split(",", self.value) : can(regex("^subnet-[a-z0-9]+$", s))])
      error_message = "CRITICAL: One or more database subnets retrieved from Layer 1 have an invalid format."
    }
  }
}

data "aws_ssm_parameter" "database_subnet_group_name" {
  name = "/heimdall/${var.environment}/network/database_subnet_group_name"

  lifecycle {
    postcondition {
      # Ensures the subnet group name is not an empty string
      condition     = length(self.value) > 0
      error_message = "CRITICAL: Database subnet group name from Layer 1 cannot be empty."
    }
  }
}
