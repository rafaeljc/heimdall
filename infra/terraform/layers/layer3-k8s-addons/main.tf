################################################################################
# Layer 3: K8s Add-ons - Core Orchestration
#
# Deploys essential Kubernetes add-ons: Metrics Server (HPA support),
# AWS Load Balancer Controller (Ingress support via EKS Pod Identity),
# and ArgoCD (GitOps-driven deployments).
################################################################################

locals {
  cluster_name = data.aws_ssm_parameter.eks_cluster_name.value

  # Environment-aware configuration defaults
  helm_timeout = var.environment == "prod" ? 600 : 300 # 10 min for prod, 5 min for others

  argocd_config = {
    service_type = var.argocd_server_service_type != null ? var.argocd_server_service_type : (
      var.environment == "prod" ? "LoadBalancer" : "ClusterIP"
    )
    server_replicas = var.argocd_server_replicas != null ? var.argocd_server_replicas : (
      var.environment == "prod" ? 3 : (var.environment == "staging" ? 2 : 1)
    )
  }
}

# ------------------------------------------------------------------------------
# 1. Metrics Server (Prerequisite for HPA)
# ------------------------------------------------------------------------------
resource "helm_release" "metrics_server" {
  name       = "metrics-server"
  repository = "https://kubernetes-sigs.github.io/metrics-server/"
  chart      = "metrics-server"
  namespace  = "kube-system"
  version    = "3.12.0"
  timeout    = local.helm_timeout
  wait       = true
  atomic     = true # Rollback on failure

  lifecycle {
    postcondition {
      condition     = lower(self.status) == "deployed"
      error_message = "CRITICAL: Metrics Server failed to deploy. Check helm status metrics-server -n kube-system"
    }
  }
}

# ------------------------------------------------------------------------------
# 2. AWS Load Balancer Controller via EKS Pod Identity
# ------------------------------------------------------------------------------

# 2.1 Create IAM Role trusting pods.eks.amazonaws.com
resource "aws_iam_role" "aws_load_balancer_controller" {
  name = "heimdall-${var.environment}-albc-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = [
          "sts:AssumeRole",
          "sts:TagSession"
        ]
        Effect = "Allow"
        Principal = {
          Service = "pods.eks.amazonaws.com"
        }
      }
    ]
  })
}

# 2.2 Attach the downloaded official IAM Policy to the Role
resource "aws_iam_role_policy" "aws_load_balancer_controller" {
  name   = "AWSLoadBalancerControllerIAMPolicy"
  role   = aws_iam_role.aws_load_balancer_controller.id
  policy = data.http.aws_lbc_iam_policy.response_body
}

# 2.3 Bridge the IAM Role directly to the Kubernetes Service Account
resource "aws_eks_pod_identity_association" "aws_load_balancer_controller" {
  cluster_name    = local.cluster_name
  namespace       = "kube-system"
  service_account = "aws-load-balancer-controller"
  role_arn        = aws_iam_role.aws_load_balancer_controller.arn
}

# 2.4 Install the Controller via Helm
resource "helm_release" "aws_load_balancer_controller" {
  name       = "aws-load-balancer-controller"
  repository = "https://aws.github.io/eks-charts"
  chart      = "aws-load-balancer-controller"
  namespace  = "kube-system"
  version    = "1.7.1"
  timeout    = local.helm_timeout
  wait       = true
  atomic     = true # Rollback on failure

  set {
    name  = "clusterName"
    value = local.cluster_name
  }

  set {
    name  = "serviceAccount.create"
    value = "true"
  }

  set {
    name  = "serviceAccount.name"
    value = "aws-load-balancer-controller"
  }

  # Strict dependency: Must wait for the Pod Identity association to exist
  depends_on = [aws_eks_pod_identity_association.aws_load_balancer_controller]

  lifecycle {
    postcondition {
      condition     = lower(self.status) == "deployed"
      error_message = "CRITICAL: AWS Load Balancer Controller failed to deploy. Check helm status aws-load-balancer-controller -n kube-system"
    }
  }
}

# ------------------------------------------------------------------------------
# 3. ArgoCD (The GitOps Engine)
# ------------------------------------------------------------------------------
resource "kubernetes_namespace" "argocd" {
  metadata {
    name = "argocd"
  }
}

resource "helm_release" "argocd" {
  name       = "argocd"
  repository = "https://argoproj.github.io/argo-helm"
  chart      = "argo-cd"
  namespace  = kubernetes_namespace.argocd.metadata[0].name
  version    = "6.7.1"
  timeout    = local.helm_timeout
  wait       = true
  atomic     = true # Rollback on failure

  # Environment-aware server configuration
  set {
    name  = "server.service.type"
    value = local.argocd_config.service_type
  }

  set {
    name  = "server.replicas"
    value = local.argocd_config.server_replicas
  }

  # CRITICAL: ArgoCD Service creation triggers AWS Load Balancer Controller webhook validation.
  # This explicit dependency ensures ALBC webhook service is ready before ArgoCD attempts to
  # create its Service resources, preventing webhook timeout errors.
  depends_on = [helm_release.aws_load_balancer_controller]

  lifecycle {
    postcondition {
      condition     = lower(self.status) == "deployed"
      error_message = "CRITICAL: ArgoCD failed to deploy. Verify ALBC webhook ready: kubectl get svc -n kube-system aws-load-balancer-webhook-service"
    }
  }
}
