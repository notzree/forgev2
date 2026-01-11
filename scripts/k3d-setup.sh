#!/usr/bin/env bash
set -euo pipefail

# k3d cluster setup script for Forge local development
# This script creates a k3d cluster with the necessary configuration

CLUSTER_NAME="${CLUSTER_NAME:-forge-dev}"
K3D_REGISTRY_NAME="${K3D_REGISTRY_NAME:-forge-registry.localhost}"
K3D_REGISTRY_PORT="${K3D_REGISTRY_PORT:-5111}"
# NodePort range for local development access (limited range to avoid k3d issues)
NODEPORT_RANGE_START="${NODEPORT_RANGE_START:-30000}"
NODEPORT_RANGE_END="${NODEPORT_RANGE_END:-30100}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

check_dependencies() {
    log_info "Checking dependencies..."

    if ! command -v k3d &> /dev/null; then
        log_error "k3d is not installed. Install it with: brew install k3d"
        exit 1
    fi

    if ! command -v docker &> /dev/null; then
        log_error "docker is not installed. Please install Docker Desktop."
        exit 1
    fi

    if ! docker info &> /dev/null; then
        log_error "Docker daemon is not running. Please start Docker."
        exit 1
    fi

    if ! command -v kubectl &> /dev/null; then
        log_warn "kubectl is not installed. Install it with: brew install kubectl"
        log_warn "You won't be able to interact with the cluster without kubectl."
    fi

    log_info "All dependencies satisfied."
}

create_registry() {
    # Check if registry already exists
    if k3d registry list | grep -q "${K3D_REGISTRY_NAME}"; then
        log_info "Registry ${K3D_REGISTRY_NAME} already exists."
        return 0
    fi

    log_info "Creating local registry ${K3D_REGISTRY_NAME}:${K3D_REGISTRY_PORT}..."
    k3d registry create "${K3D_REGISTRY_NAME}" --port "${K3D_REGISTRY_PORT}"
}

create_cluster() {
    # Check if cluster already exists
    if k3d cluster list | grep -q "${CLUSTER_NAME}"; then
        log_info "Cluster ${CLUSTER_NAME} already exists."

        # Ensure it's running
        if ! k3d cluster list | grep "${CLUSTER_NAME}" | grep -q "running"; then
            log_info "Starting cluster ${CLUSTER_NAME}..."
            k3d cluster start "${CLUSTER_NAME}"
        fi
        return 0
    fi

    log_info "Creating k3d cluster: ${CLUSTER_NAME}..."
    log_info "Exposing NodePort range ${NODEPORT_RANGE_START}-${NODEPORT_RANGE_END} for local development..."

    k3d cluster create "${CLUSTER_NAME}" \
        --registry-use "k3d-${K3D_REGISTRY_NAME}:${K3D_REGISTRY_PORT}" \
        --api-port 6550 \
        --servers 1 \
        --agents 1 \
        --port "${NODEPORT_RANGE_START}-${NODEPORT_RANGE_END}:${NODEPORT_RANGE_START}-${NODEPORT_RANGE_END}@server:0" \
        --k3s-arg "--service-node-port-range=${NODEPORT_RANGE_START}-${NODEPORT_RANGE_END}@server:0" \
        --wait

    log_info "Cluster ${CLUSTER_NAME} created successfully."
}

setup_kubeconfig() {
    log_info "Setting up kubeconfig..."

    # k3d automatically merges the kubeconfig, but we'll verify
    if kubectl config get-contexts | grep -q "k3d-${CLUSTER_NAME}"; then
        kubectl config use-context "k3d-${CLUSTER_NAME}"
        log_info "Switched to context: k3d-${CLUSTER_NAME}"
    else
        log_error "Failed to find cluster context. Try running: k3d kubeconfig merge ${CLUSTER_NAME}"
        exit 1
    fi
}

verify_cluster() {
    log_info "Verifying cluster is ready..."

    # Wait for nodes to be ready
    kubectl wait --for=condition=Ready nodes --all --timeout=60s

    # Check core system pods
    kubectl get pods -n kube-system

    log_info "Cluster is ready!"
}

print_summary() {
    echo ""
    echo "=========================================="
    echo "  Forge Dev Cluster Ready"
    echo "=========================================="
    echo ""
    echo "Cluster:      ${CLUSTER_NAME}"
    echo "Context:      k3d-${CLUSTER_NAME}"
    echo "Registry:     localhost:${K3D_REGISTRY_PORT}"
    echo "NodePorts:    ${NODEPORT_RANGE_START}-${NODEPORT_RANGE_END} (exposed to localhost)"
    echo ""
    echo "To push images to the local registry:"
    echo "  docker tag <image> localhost:${K3D_REGISTRY_PORT}/<image>"
    echo "  docker push localhost:${K3D_REGISTRY_PORT}/<image>"
    echo ""
    echo "To use in pods, reference images as:"
    echo "  k3d-${K3D_REGISTRY_NAME}:${K3D_REGISTRY_PORT}/<image>"
    echo ""
    echo "NodePort services are accessible at localhost:<nodeport>"
    echo ""
}

main() {
    log_info "Setting up k3d development cluster..."

    check_dependencies
    create_registry
    create_cluster
    setup_kubeconfig
    verify_cluster
    print_summary
}

# Run main function
main "$@"
