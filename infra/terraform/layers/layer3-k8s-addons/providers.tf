################################################################################
# Layer 3: K8s Add-ons - Providers
################################################################################

terraform {
  required_version = ">= 1.3" # Required for lifecycle postconditions
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "~> 2.24"
    }
    helm = {
      source  = "hashicorp/helm"
      version = "~> 2.12"
    }
    http = {
      source  = "hashicorp/http"
      version = "~> 3.4"
    }
  }
}

provider "aws" {
  region = var.aws_region
  default_tags {
    tags = {
      Project     = "Heimdall"
      Environment = var.environment
      ManagedBy   = "Terraform"
      Layer       = "Layer3-K8s-Addons"
    }
  }
}

# Kubernetes Provider: Uses temporary credentials from EKS service account.
# Tokens are short-lived (15-minute lifespan) and regenerated on each Terraform run.
# This approach is secure for CI/CD environments and supports automatic credential rotation.
provider "kubernetes" {
  host                   = data.aws_eks_cluster.eks.endpoint
  cluster_ca_certificate = base64decode(data.aws_eks_cluster.eks.certificate_authority[0].data)
  token                  = data.aws_eks_cluster_auth.eks.token
}

# Helm Provider: Inherits Kubernetes provider configuration for chart deployments.
# Uses the same short-lived authentication tokens for atomic and reliable releases.
provider "helm" {
  kubernetes {
    host                   = data.aws_eks_cluster.eks.endpoint
    cluster_ca_certificate = base64decode(data.aws_eks_cluster.eks.certificate_authority[0].data)
    token                  = data.aws_eks_cluster_auth.eks.token
  }
}
