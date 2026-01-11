#!/usr/bin/env bash
set -euo pipefail

# Forge development environment startup script
# Usage: ./scripts/dev.sh [--rebuild]

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "${SCRIPT_DIR}")"

# Configuration
CLUSTER_NAME="${CLUSTER_NAME:-forge-dev}"
K3D_REGISTRY_NAME="${K3D_REGISTRY_NAME:-forge-registry.localhost}"
K3D_REGISTRY_PORT="${K3D_REGISTRY_PORT:-5111}"
AGENT_NAMESPACE="${AGENT_NAMESPACE:-default}"

# Image configuration - for local dev, use the local registry
LOCAL_REGISTRY="localhost:${K3D_REGISTRY_PORT}"
K3D_REGISTRY="k3d-${K3D_REGISTRY_NAME}:${K3D_REGISTRY_PORT}"
AGENT_IMAGE_NAME="${AGENT_IMAGE_NAME:-forge-agent}"
AGENT_IMAGE_TAG="${AGENT_IMAGE_TAG:-latest}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
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

log_step() {
    echo -e "${BLUE}[STEP]${NC} $1"
}

# Parse arguments
REBUILD=false
while [[ $# -gt 0 ]]; do
    case $1 in
        --rebuild)
            REBUILD=true
            shift
            ;;
        --help|-h)
            echo "Usage: $0 [--rebuild]"
            echo ""
            echo "Options:"
            echo "  --rebuild    Rebuild the agent image before starting"
            echo "  --help       Show this help message"
            exit 0
            ;;
        *)
            log_error "Unknown option: $1"
            exit 1
            ;;
    esac
done

check_env_file() {
    if [[ ! -f "${PROJECT_ROOT}/.env" ]]; then
        log_warn "No .env file found. Creating from .env.example..."
        if [[ -f "${PROJECT_ROOT}/.env.example" ]]; then
            cp "${PROJECT_ROOT}/.env.example" "${PROJECT_ROOT}/.env"
            log_warn "Please edit .env and set your ANTHROPIC_API_KEY"
            log_error "ANTHROPIC_API_KEY is required for the agent to function"
            exit 1
        else
            log_error "No .env.example file found. Cannot create .env"
            exit 1
        fi
    fi

    # Source the .env file
    set -a
    source "${PROJECT_ROOT}/.env"
    set +a

    # Verify required vars
    if [[ -z "${ANTHROPIC_API_KEY:-}" ]]; then
        log_error "ANTHROPIC_API_KEY is not set in .env file"
        exit 1
    fi
}

ensure_cluster() {
    log_step "Ensuring k3d cluster is running..."

    if ! k3d cluster list 2>/dev/null | grep -q "${CLUSTER_NAME}"; then
        log_info "Creating k3d cluster..."
        "${SCRIPT_DIR}/k3d-setup.sh"
    else
        # Check if running
        if ! k3d cluster list | grep "${CLUSTER_NAME}" | grep -q "running"; then
            log_info "Starting existing cluster..."
            k3d cluster start "${CLUSTER_NAME}"
        else
            log_info "Cluster ${CLUSTER_NAME} is already running."
        fi

        # Make sure we're using the right context
        kubectl config use-context "k3d-${CLUSTER_NAME}" > /dev/null 2>&1
    fi
}

build_and_push_agent() {
    log_step "Building agent image..."

    cd "${PROJECT_ROOT}/agent/claudecode"

    # Build for the local registry
    docker build -t "${LOCAL_REGISTRY}/${AGENT_IMAGE_NAME}:${AGENT_IMAGE_TAG}" .

    log_step "Pushing agent image to local registry..."
    docker push "${LOCAL_REGISTRY}/${AGENT_IMAGE_NAME}:${AGENT_IMAGE_TAG}"

    log_info "Agent image pushed to ${LOCAL_REGISTRY}/${AGENT_IMAGE_NAME}:${AGENT_IMAGE_TAG}"

    cd "${PROJECT_ROOT}"
}

check_agent_image() {
    # Check if the image exists in the local registry
    log_step "Checking for agent image in local registry..."

    # Try to pull manifest to check if image exists
    if docker manifest inspect "${LOCAL_REGISTRY}/${AGENT_IMAGE_NAME}:${AGENT_IMAGE_TAG}" > /dev/null 2>&1; then
        log_info "Agent image found in local registry."
        return 0
    else
        log_warn "Agent image not found in local registry."
        return 1
    fi
}

create_agent_secret() {
    log_step "Creating/updating agent secrets in Kubernetes..."

    # Create a secret with the Anthropic API key that agents will use
    kubectl create secret generic agent-secrets \
        --namespace="${AGENT_NAMESPACE}" \
        --from-literal=ANTHROPIC_API_KEY="${ANTHROPIC_API_KEY}" \
        --dry-run=client -o yaml | kubectl apply -f -

    log_info "Agent secrets configured."
}

run_platform() {
    log_step "Starting platform server..."

    cd "${PROJECT_ROOT}/platform"

    # Set environment variables for local development
    export KUBE_CONFIG_PATH="${HOME}/.kube/config"
    export AGENT_NAMESPACE="${AGENT_NAMESPACE}"
    export CONTAINER_REGISTRY="${K3D_REGISTRY}"
    export CONTAINER_NAMESPACE=""
    export AGENT_IMAGE_NAME="${AGENT_IMAGE_NAME}"
    export AGENT_IMAGE_TAG="${AGENT_IMAGE_TAG}"
    export DEBUG=true

    log_info "Platform configuration:"
    log_info "  KUBE_CONFIG_PATH: ${KUBE_CONFIG_PATH}"
    log_info "  AGENT_NAMESPACE:  ${AGENT_NAMESPACE}"
    log_info "  AGENT_IMAGE:      ${K3D_REGISTRY}/${AGENT_IMAGE_NAME}:${AGENT_IMAGE_TAG}"
    log_info "  PORT:             ${PORT:-8080}"
    echo ""

    log_info "Starting Go platform server..."
    go run ./cmd/server
}

print_banner() {
    echo ""
    echo -e "${BLUE}╔═══════════════════════════════════════════════════════════╗${NC}"
    echo -e "${BLUE}║${NC}                 ${GREEN}Forge Development Server${NC}                  ${BLUE}║${NC}"
    echo -e "${BLUE}╚═══════════════════════════════════════════════════════════╝${NC}"
    echo ""
}

main() {
    print_banner

    # Check for .env and required variables
    check_env_file

    # Ensure k3d cluster is running
    ensure_cluster

    # Build and push agent image if --rebuild or if not present
    if [[ "${REBUILD}" == "true" ]]; then
        build_and_push_agent
    elif ! check_agent_image; then
        log_info "Building agent image since it's not in the registry..."
        build_and_push_agent
    fi

    # Create secrets for agents
    create_agent_secret

    # Run the platform
    run_platform
}

# Run main function
main "$@"
