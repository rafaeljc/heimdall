#!/usr/bin/env bash

################################################################################
# Heimdall GitOps Bootstrap
#
# Purpose:
#   Deploys the ArgoCD root application and creates Heimdall namespaces.
#   ArgoCD itself is installed by Layer 3 (Terraform).
#
# Prerequisites:
#   - Kubernetes cluster with ArgoCD installed (Layer 3)
#   - kubectl configured with cluster access
#
# Usage:
#   task infra:gitops:bootstrap
#
################################################################################

# SRE Best Practices: Strict Bash Strict Mode
# -e: Exit immediately if a command exits with a non-zero status.
# -u: Treat unset variables as an error.
# -o pipefail: Return value of a pipeline is the status of the last command to exit with a non-zero status.
set -euo pipefail

# Constants
readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly ROOT_APP_FILE="${SCRIPT_DIR}/root-app.yaml"
readonly ARGOCD_NAMESPACE="argocd"
readonly REQUIRED_NAMESPACES=("heimdall-prod" "heimdall-staging" "heimdall-dev")

# Colors for structured logging
readonly GREEN='\033[0;32m'
readonly BLUE='\033[0;34m'
readonly YELLOW='\033[0;33m'
readonly RED='\033[0;31m'
readonly NC='\033[0m' # No Color

# --- Helper Functions ---

log_info() {
  echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
  echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warn() {
  echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
  echo -e "${RED}[ERROR]${NC} $1" >&2
}

# --- Core Logic ---

check_prerequisites() {
  log_info "Verifying prerequisites..."

  # 1. Check if kubectl is installed
  if ! command -v kubectl >/dev/null 2>&1; then
    log_error "kubectl is not installed or not in PATH."
    exit 1
  fi

  # 2. Check if the Root App file exists
  if [[ ! -f "${ROOT_APP_FILE}" ]]; then
    log_error "Root application manifest not found at: ${ROOT_APP_FILE}"
    exit 1
  fi

  # 3. Check cluster connectivity
  if ! kubectl cluster-info >/dev/null 2>&1; then
    log_error "Cannot connect to the Kubernetes cluster. Check your KUBECONFIG."
    exit 1
  fi

  log_success "Prerequisites verified."
}

wait_for_argocd_crds() {
  log_info "Waiting for ArgoCD Custom Resource Definitions (CRDs) to initialize..."
  
  # Wait up to 2 minutes for the CRD to be established
  # This prevents race conditions right after Terraform finishes applying Layer 3
  if ! kubectl wait --for=condition=established --timeout=120s crd/applications.argoproj.io >/dev/null 2>&1; then
    log_error "ArgoCD Application CRD is not available. Ensure ArgoCD is fully installed in the cluster."
    exit 1
  fi
  
  log_success "ArgoCD CRDs are ready."
}

setup_namespaces() {
  log_info "Provisioning core namespaces..."
  
  # Although ArgoCD's CreateNamespace=true handles this, pre-creating them allows us 
  # to attach specific organizational labels before ArgoCD takes over.
  for ns in "${REQUIRED_NAMESPACES[@]}"; do
    if kubectl get namespace "${ns}" >/dev/null 2>&1; then
      log_warn "Namespace '${ns}' already exists. Applying labels..."
    else
      kubectl create namespace "${ns}" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
    fi
    
    # Standardize labels for observability and cost-allocation
    kubectl label namespace "${ns}" app.kubernetes.io/part-of=heimdall --overwrite >/dev/null
  done
  
  log_success "Core namespaces provisioned."
}

deploy_root_app() {
  log_info "Igniting GitOps Bootstrap (Deploying Root Application)..."
  
  kubectl apply -f "${ROOT_APP_FILE}"
  
  log_success "Root application deployed successfully."
}

main() {
  log_info "Starting Heimdall GitOps Bootstrap Phase..."
  
  check_prerequisites
  wait_for_argocd_crds
  setup_namespaces
  deploy_root_app
  
  log_info "================================================================="
  log_success "GitOps Bootstrap Complete!"
  log_info "ArgoCD is now monitoring the repository and will sync workloads."
  log_info "Run 'kubectl get applications -n argocd' to watch the progress."
  log_info "================================================================="
}

# Execute main function with all script arguments
main "$@"
