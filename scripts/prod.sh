#!/usr/bin/env bash
set -euo pipefail

# Forge production environment startup script
# Usage: ./scripts/prod.sh [--skip-migrations] [--dry-run]

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "${SCRIPT_DIR}")"

# Configuration from environment (with defaults matching .env.example)
AGENT_NAMESPACE="${AGENT_NAMESPACE:-default}"
CONTAINER_REGISTRY="${CONTAINER_REGISTRY:-ghcr.io}"
CONTAINER_NAMESPACE="${CONTAINER_NAMESPACE:-notzree}"
AGENT_IMAGE_NAME="${AGENT_IMAGE_NAME:-forge-agent}"
AGENT_IMAGE_TAG="${AGENT_IMAGE_TAG:-latest}"
AGENT_IMAGE="${CONTAINER_REGISTRY}/${CONTAINER_NAMESPACE}/${AGENT_IMAGE_NAME}:${AGENT_IMAGE_TAG}"

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
SKIP_MIGRATIONS=false
DRY_RUN=false
while [[ $# -gt 0 ]]; do
    case $1 in
        --skip-migrations)
            SKIP_MIGRATIONS=true
            shift
            ;;
        --dry-run)
            DRY_RUN=true
            shift
            ;;
        --help|-h)
            echo "Usage: $0 [--skip-migrations] [--dry-run]"
            echo ""
            echo "Options:"
            echo "  --skip-migrations   Skip running database migrations"
            echo "  --dry-run           Print configuration without starting"
            echo "  --help              Show this help message"
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
        log_error "No .env file found. Copy .env.example to .env and configure it."
        exit 1
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

    if [[ -z "${DATABASE_URL_PROD:-}" ]]; then
        log_error "DATABASE_URL_PROD is not set in .env file"
        exit 1
    fi
}

check_kube_context() {
    log_step "Verifying Kubernetes context..."

    # If KUBE_CONTEXT is set, switch to it
    if [[ -n "${KUBE_CONTEXT:-}" ]]; then
        log_info "Switching to kube context: ${KUBE_CONTEXT}"
        kubectl config use-context "${KUBE_CONTEXT}" > /dev/null 2>&1 || {
            log_error "Failed to switch to context '${KUBE_CONTEXT}'. Check KUBE_CONTEXT value."
            exit 1
        }
    fi

    local CURRENT_CONTEXT
    CURRENT_CONTEXT=$(kubectl config current-context 2>/dev/null) || {
        log_error "Cannot determine current Kubernetes context. Is kubectl configured?"
        exit 1
    }

    echo ""
    log_warn "=== PRODUCTION DEPLOYMENT ==="
    log_warn "Kubernetes context: ${CURRENT_CONTEXT}"
    log_warn "Namespace:          ${AGENT_NAMESPACE}"
    log_warn "Agent image:        ${AGENT_IMAGE}"
    echo ""

    read -rp "Continue with this context? [y/N] " confirm
    if [[ "${confirm}" != "y" && "${confirm}" != "Y" ]]; then
        log_info "Aborted."
        exit 0
    fi

    # Verify connectivity
    kubectl cluster-info > /dev/null 2>&1 || {
        log_error "Cannot reach Kubernetes cluster. Check your kubeconfig."
        exit 1
    }
    log_info "Cluster is reachable."
}

check_agent_image() {
    log_step "Checking for agent image in registry..."

    if docker manifest inspect "${AGENT_IMAGE}" > /dev/null 2>&1; then
        log_info "Agent image found: ${AGENT_IMAGE}"
    else
        log_error "Agent image not found: ${AGENT_IMAGE}"
        log_error "Build and push the image first:"
        log_error "  just release-agent       # build + push with :latest"
        log_error "  just release-agent-sha   # build + push with git SHA tag"
        exit 1
    fi
}

create_agent_secret() {
    log_step "Creating/updating agent secrets in Kubernetes..."

    local SECRET_ARGS=(
        --namespace="${AGENT_NAMESPACE}"
        --from-literal=ANTHROPIC_API_KEY="${ANTHROPIC_API_KEY}"
    )

    if [[ -n "${OPENCODE_API_KEY:-}" ]]; then
        SECRET_ARGS+=(--from-literal=OPENCODE_API_KEY="${OPENCODE_API_KEY}")
        log_info "Including OPENCODE_API_KEY in agent secrets."
    fi

    kubectl create secret generic agent-secrets \
        "${SECRET_ARGS[@]}" \
        --dry-run=client -o yaml | kubectl apply -f -

    log_info "Agent secrets configured."
}

run_migrations() {
    if [[ "${SKIP_MIGRATIONS}" == "true" ]]; then
        log_info "Skipping migrations (--skip-migrations)."
        return
    fi

    log_step "Running database migrations against production..."

    local MIGRATIONS_DIR="${PROJECT_ROOT}/platform/internal/sqlc/migrations"
    goose -dir "${MIGRATIONS_DIR}" postgres "${DATABASE_URL_PROD}" up

    log_info "Migrations complete."
}

run_platform() {
    log_step "Starting platform server (production)..."

    cd "${PROJECT_ROOT}/platform"

    export DATABASE_URL="${DATABASE_URL_PROD}"
    export DEBUG=false
    export AGENT_NAMESPACE="${AGENT_NAMESPACE}"
    export CONTAINER_REGISTRY="${CONTAINER_REGISTRY}"
    export CONTAINER_NAMESPACE="${CONTAINER_NAMESPACE}"
    export AGENT_IMAGE_NAME="${AGENT_IMAGE_NAME}"
    export AGENT_IMAGE_TAG="${AGENT_IMAGE_TAG}"

    # Use kubeconfig if set, otherwise rely on in-cluster config
    if [[ -n "${KUBE_CONFIG_PATH:-}" ]]; then
        export KUBE_CONFIG_PATH="${KUBE_CONFIG_PATH}"
        log_info "Using kubeconfig: ${KUBE_CONFIG_PATH}"
    else
        log_info "No KUBE_CONFIG_PATH set; using in-cluster config."
    fi

    # In production, NODE_HOST should be empty (use pod IPs directly)
    export NODE_HOST=""

    log_info "Platform configuration:"
    log_info "  DATABASE_URL:       (set from DATABASE_URL_PROD)"
    log_info "  AGENT_NAMESPACE:    ${AGENT_NAMESPACE}"
    log_info "  AGENT_IMAGE:        ${AGENT_IMAGE}"
    log_info "  KUBE_CONFIG_PATH:   ${KUBE_CONFIG_PATH:-<in-cluster>}"
    log_info "  NODE_HOST:          <empty, using pod IPs>"
    log_info "  DEBUG:              false"
    log_info "  PORT:               ${PORT:-8080}"
    echo ""

    log_info "Starting Go platform server..."
    go run ./cmd/server
}

print_banner() {
    echo ""
    echo -e "${RED}╔═══════════════════════════════════════════════════════════╗${NC}"
    echo -e "${RED}║${NC}                 ${YELLOW}Forge Production Server${NC}                   ${RED}║${NC}"
    echo -e "${RED}╚═══════════════════════════════════════════════════════════╝${NC}"
    echo ""
}

print_dry_run() {
    echo ""
    log_info "=== DRY RUN — No changes will be made ==="
    echo ""
    log_info "Kubernetes context:  $(kubectl config current-context 2>/dev/null || echo '<unknown>')"
    log_info "Namespace:           ${AGENT_NAMESPACE}"
    log_info "Agent image:         ${AGENT_IMAGE}"
    log_info "Database:            DATABASE_URL_PROD is set"
    log_info "KUBE_CONFIG_PATH:    ${KUBE_CONFIG_PATH:-<in-cluster>}"
    log_info "NODE_HOST:           <empty, using pod IPs>"
    log_info "Skip migrations:     ${SKIP_MIGRATIONS}"
    echo ""
}

main() {
    print_banner

    check_env_file

    if [[ "${DRY_RUN}" == "true" ]]; then
        print_dry_run
        exit 0
    fi

    check_kube_context
    check_agent_image
    create_agent_secret
    run_migrations
    run_platform
}

main "$@"
