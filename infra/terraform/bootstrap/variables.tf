################################################################################
# Bootstrap Layer - Input Variables
################################################################################

variable "aws_region" {
  description = "AWS region where bootstrap resources will be created"
  type        = string
  default     = "us-east-1"
}

variable "bucket_name_suffix" {
  description = <<-EOT
    Unique suffix for S3 bucket name to ensure global uniqueness.
    The final bucket name will be: heimdall-tfstate-production-{suffix}
    
    Examples: 
    - "abc" (your initials)
    - "prod-001" (account identifier)
    - "company-dev" (organization prefix)
    
    Requirements:
    - Only lowercase letters, numbers, and hyphens allowed
    - Cannot be empty
    - Should reflect your organization/account to prevent conflicts
  EOT
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9-]+$", var.bucket_name_suffix)) && length(var.bucket_name_suffix) > 0
    error_message = "Suffix must contain only lowercase letters, numbers, and hyphens, and must not be empty."
  }
}
