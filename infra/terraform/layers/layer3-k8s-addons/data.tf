################################################################################
# Layer 3: K8s Add-ons - Data Sources
#
# Fetches cluster credentials dynamically and downloads required IAM policies.
################################################################################

# 1. Read Cluster Name from Layer 2
data "aws_ssm_parameter" "eks_cluster_name" {
  name = "/heimdall/${var.environment}/platform/eks_cluster_name"

  lifecycle {
    postcondition {
      condition     = can(regex("^heimdall-.*-eks$", self.value))
      error_message = "CRITICAL: Invalid EKS cluster name format from Layer 2. Expected format: heimdall-{env}-eks"
    }
  }
}

# 2. Fetch Live Cluster Configuration
data "aws_eks_cluster" "eks" {
  name = data.aws_ssm_parameter.eks_cluster_name.value
}

# 3. Generate Short-Lived Auth Token (15 min lifespan)
data "aws_eks_cluster_auth" "eks" {
  name = data.aws_ssm_parameter.eks_cluster_name.value
}

# 4. Fetch the official AWS Load Balancer Controller IAM Policy
data "http" "aws_lbc_iam_policy" {
  url = "https://raw.githubusercontent.com/kubernetes-sigs/aws-load-balancer-controller/v2.7.1/docs/install/iam_policy.json"

  # SECURITY: Ensure the downloaded payload is a valid JSON before proceeding
  lifecycle {
    postcondition {
      condition     = can(jsondecode(self.response_body))
      error_message = "CRITICAL: Failed to fetch a valid JSON IAM policy for the AWS Load Balancer Controller."
    }
  }
}
