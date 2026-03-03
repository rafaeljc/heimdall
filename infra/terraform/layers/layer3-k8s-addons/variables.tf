################################################################################
# Layer 3: K8s Add-ons - Input Variables
#
# These variables control K8s add-on deployment, service exposure, and
# environment-specific configurations for Helm charts and Kubernetes resources.
################################################################################

variable "aws_region" {
  description = "AWS region where the EKS cluster is deployed."
  type        = string
  default     = "us-east-1"
}

variable "environment" {
  description = "Deployment environment. Controls add-on configurations and service exposure."
  type        = string

  # Allowed values: dev, staging, prod
  # - dev: Minimal replicas, ClusterIP services, reduced resource requests
  # - staging: Production-like configuration for testing
  # - prod: HA replicas, LoadBalancer services, optimized resource allocation
}

variable "argocd_server_service_type" {
  description = "ArgoCD server service exposure type. Determines how ArgoCD UI is accessed."
  type        = string
  default     = null # Uses environment-aware default if not specified

  # Examples:
  # - ClusterIP: Internal access only (default for dev/staging)
  # - LoadBalancer: External access via AWS NLB (default for prod)
  # - NodePort: Internal node access (not recommended)
}

variable "argocd_server_replicas" {
  description = "Number of ArgoCD server replicas for HA (prod only)."
  type        = number
  default     = null # Uses environment-aware default if not specified

  # Recommended values:
  # - dev: 1 (cost-optimized)
  # - staging: 2 (HA testing)
  # - prod: 3 (high availability)
}

variable "vpc_cidr" {
  description = "CIDR block for the VPC. Used for network segmentation and security group rules."
  type        = string
  default     = "10.0.0.0/16"
}
