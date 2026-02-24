################################################################################
# Bootstrap Layer - Output Values
#
# These outputs expose critical information about the bootstrap infrastructure
# needed by downstream Terraform configurations and team members.
################################################################################

output "state_bucket_name" {
  description = "Name of the S3 bucket storing Terraform state"
  value       = aws_s3_bucket.terraform_state.id
}

output "state_bucket_arn" {
  description = "ARN of the S3 bucket storing Terraform state"
  value       = aws_s3_bucket.terraform_state.arn
}

output "lock_table_name" {
  description = "Name of the DynamoDB table for Terraform state locking"
  value       = aws_dynamodb_table.terraform_locks.name
}

output "lock_table_arn" {
  description = "ARN of the DynamoDB table for Terraform state locking"
  value       = aws_dynamodb_table.terraform_locks.arn
}
