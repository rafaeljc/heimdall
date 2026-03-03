################################################################################
# EKS Cluster Module - Core Infrastructure
#
# Deploys a production-ready Amazon EKS Cluster using modern AWS standards.
#
# High Availability Enforcement (3 AZ & 3 Nodes):
# - Requires minimum 3 private subnets across different AZs (all environments)
# - Requires minimum 3 worker nodes (1 per AZ default)
# - Automatic pod distribution and zone-aware failover
#
# Architecture & Security Features:
# - KMS Envelope Encryption: Kubernetes Secrets (etcd) encrypted at-rest via AWS KMS
# - Access Management: Modern EKS Access Entry API (native AWS RBAC, no aws-auth ConfigMap)
# - Pod Identity: eks-pod-identity-agent addon (modern alternative to IRSA)
# - Compute: Managed Node Groups deployed exclusively in private subnets (Tier 1)
# - SSM Access: Worker nodes accessible via AWS Systems Manager (no SSH keys, no bastions)
# - Smart API Security: Production defaults to private-only endpoint (VPN access required)
################################################################################

terraform {
  required_version = ">= 1.0"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0"
    }
  }
}

locals {
  cluster_name = "heimdall-${var.environment}-eks"
}

# ------------------------------------------------------------------------------
# KMS Key for Kubernetes Secrets Encryption (SOC2/Compliance Requirement)
# ------------------------------------------------------------------------------
resource "aws_kms_key" "eks_secrets" {
  description             = "EKS Secret Encryption Key for ${local.cluster_name}"
  enable_key_rotation     = true
  deletion_window_in_days = 7

  tags = {
    Environment = var.environment
    Project     = "Heimdall"
  }
}

resource "aws_kms_alias" "eks_secrets" {
  name          = "alias/${local.cluster_name}-secrets"
  target_key_id = aws_kms_key.eks_secrets.key_id
}

# ------------------------------------------------------------------------------
# Amazon EKS Cluster via Official AWS Module
# ------------------------------------------------------------------------------
module "eks" {
  source  = "terraform-aws-modules/eks/aws"
  version = "~> 20.0" # Locking to v20+ to ensure modern defaults (Access Entries)

  cluster_name    = local.cluster_name
  cluster_version = var.cluster_version

  # Network Configuration
  vpc_id     = var.vpc_id
  subnet_ids = var.private_subnets

  # API Endpoint Access
  cluster_endpoint_private_access = true
  # Turn off public access entirely in prod if no specific VPN CIDRs are provided
  cluster_endpoint_public_access = var.environment == "prod" && var.cluster_endpoint_public_access_cidrs == null ? false : true
  # AWS requires at least one CIDR if public access is true. If false, it ignores this list
  cluster_endpoint_public_access_cidrs = var.cluster_endpoint_public_access_cidrs != null ? var.cluster_endpoint_public_access_cidrs : ["0.0.0.0/0"]

  # Encryption Configuration
  create_kms_key = false
  cluster_encryption_config = {
    provider_key_arn = aws_kms_key.eks_secrets.arn
    resources        = ["secrets"]
  }

  # Authentication: EKS Access Entries (AWS Native RBAC)
  enable_cluster_creator_admin_permissions = true
  authentication_mode                      = "API"

  # Core EKS Add-ons (Including the new Pod Identity Agent)
  cluster_addons = {
    coredns = {
      most_recent = true
    }
    kube-proxy = {
      most_recent = true
    }
    vpc-cni = {
      most_recent = true
    }
    eks-pod-identity-agent = {
      most_recent = true
    }
  }

  # ------------------------------------------------------------------------------
  # Managed Node Groups (Worker Nodes)
  # ------------------------------------------------------------------------------
  eks_managed_node_group_defaults = {
    ami_type       = "AL2_x86_64" # Amazon Linux 2 (x86 architecture for AMD/Intel)
    instance_types = var.node_instance_types

    # Enable secure remote access to nodes via AWS SSM (No bastion hosts needed)
    iam_role_additional_policies = {
      AmazonSSMManagedInstanceCore = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
    }
  }

  eks_managed_node_groups = {
    # The primary compute pool for microservices
    app_pool = {
      min_size     = var.node_min_size
      max_size     = var.node_max_size
      desired_size = var.node_desired_size

      # Ensures instances are spread across the provided Multi-AZ private subnets
      subnet_ids = var.private_subnets

      labels = {
        pool = "application"
      }

      tags = {
        Environment = var.environment
        Project     = "Heimdall"
        NodePool    = "app_pool"
      }
    }
  }

  tags = {
    Environment     = var.environment
    Project         = "Heimdall"
    TerraformModule = "eks"
  }
}
