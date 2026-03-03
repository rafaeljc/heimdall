################################################################################
# Layer 3: K8s Add-ons - Outputs
################################################################################

output "argocd_namespace" {
  description = "The namespace where ArgoCD is installed."
  value       = kubernetes_namespace.argocd.metadata[0].name
}

output "aws_load_balancer_controller_role_arn" {
  description = "The IAM Role ARN used by the AWS Load Balancer Controller via EKS Pod Identity."
  value       = aws_iam_role.aws_load_balancer_controller.arn
}
